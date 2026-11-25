// Package app holds the application services. They orchestrate only:
// load the aggregate, call one domain method, commit through the Unit of
// Work. Business rules live in the domain.
package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/domain"
	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports"
)

// StreamName maps a ledger id to its event stream.
func StreamName(ledgerID string) string { return "ledger-" + ledgerID }

// LedgerRepository is the Repository pattern over the event store:
// aggregates are loaded by replaying their stream.
type LedgerRepository struct{ store ports.EventStore }

// NewLedgerRepository returns a repository over store.
func NewLedgerRepository(store ports.EventStore) *LedgerRepository {
	return &LedgerRepository{store: store}
}

// Load replays the ledger's stream. A missing stream is an empty ledger.
func (r *LedgerRepository) Load(ctx context.Context, ledgerID string) (*domain.Ledger, error) {
	if err := domain.ValidateID(ledgerID); err != nil {
		return nil, err
	}
	recs, err := r.store.Load(ctx, StreamName(ledgerID))
	if err != nil {
		return nil, err
	}
	evs := make([]domain.Event, 0, len(recs))
	for _, rec := range recs {
		ev, err := DecodeEvent(rec)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", domain.ErrCorruptHistory, err) //nolint:errorlint // sentinel kind + detail
		}
		evs = append(evs, ev)
	}
	return domain.Replay(evs)
}

// Projector consumes committed records (the CQRS read side).
type Projector interface {
	Apply(rec ports.Record) error
}

// ErrProjectionStale means events were committed but the read model failed
// to apply them; the write succeeded and the projection must be rebuilt.
var ErrProjectionStale = errors.New("app: committed, but read model is stale")

// ErrUnitOfWork reports misuse of a UnitOfWork.
var ErrUnitOfWork = errors.New("app: unit of work misuse")

// UnitOfWork tracks one aggregate, commits its pending events with
// optimistic concurrency and then feeds them to the projector.
// The consistency boundary is one ledger (ADR 0001): there is no
// multi-stream commit.
type UnitOfWork struct {
	store     ports.EventStore
	clock     ports.Clock
	projector Projector
	stream    string
	ledger    *domain.Ledger
	done      bool
}

// NewUnitOfWork returns an empty unit of work.
func NewUnitOfWork(store ports.EventStore, clock ports.Clock, projector Projector) *UnitOfWork {
	return &UnitOfWork{store: store, clock: clock, projector: projector}
}

// Register tracks the aggregate to commit.
func (u *UnitOfWork) Register(ledgerID string, l *domain.Ledger) error {
	if u.ledger != nil || u.done {
		return fmt.Errorf("%w: one aggregate per unit of work", ErrUnitOfWork)
	}
	u.stream, u.ledger = StreamName(ledgerID), l
	return nil
}

// Commit appends pending events at the aggregate's expected version.
func (u *UnitOfWork) Commit(ctx context.Context) error {
	if u.done || u.ledger == nil {
		return fmt.Errorf("%w: nothing registered or already committed", ErrUnitOfWork)
	}
	u.done = true
	pending := u.ledger.Pending()
	if len(pending) == 0 {
		return nil
	}
	now := u.clock.Now()
	recs := make([]ports.Record, 0, len(pending))
	for _, ev := range pending {
		rec, err := EncodeEvent(ev, now)
		if err != nil {
			return err
		}
		recs = append(recs, rec)
	}
	expected := u.ledger.Version()
	if _, err := u.store.Append(ctx, u.stream, expected, recs); err != nil {
		return err
	}
	u.ledger.MarkCommitted()
	if u.projector == nil {
		return nil
	}
	for i := range recs {
		recs[i].Stream, recs[i].Version = u.stream, expected+uint64(i)+1 //nolint:gosec // i >= 0
	}
	err := applyAll(u.projector, recs)
	if errors.Is(err, ErrProjectionGap) {
		// Another writer (process or service instance without this
		// projector) appended in between: catch up from the store.
		// Apply is idempotent, so re-reading the whole stream is safe.
		var all []ports.Record
		if all, err = u.store.Load(ctx, u.stream); err == nil {
			err = applyAll(u.projector, all)
		}
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProjectionStale, err) //nolint:errorlint // sentinel + detail
	}
	return nil
}

func applyAll(p Projector, recs []ports.Record) error {
	for _, r := range recs {
		if err := p.Apply(r); err != nil {
			return err
		}
	}
	return nil
}
