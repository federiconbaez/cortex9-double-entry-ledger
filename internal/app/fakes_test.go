package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/adapters/memstore"
	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/domain"
	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports"
)

// Hand-written fakes (no mock libraries).

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(time.Second)
	return c.t
}

type seqIDs struct{ n int }

func (s *seqIDs) NewID() string { s.n++; return fmt.Sprintf("gen-%d", s.n) }

// racingStore simulates a concurrent writer: before the first `races`
// appends it sneaks in a foreign event, so the caller's expected version
// is stale and the store returns a conflict.
type racingStore struct {
	*memstore.Store
	races    int
	sneak    func(ctx context.Context) error
	attempts int
}

func (r *racingStore) Append(ctx context.Context, stream string, expected uint64, recs []ports.Record) (uint64, error) {
	r.attempts++
	if r.races > 0 {
		r.races--
		if err := r.sneak(ctx); err != nil {
			return 0, err
		}
	}
	return r.Store.Append(ctx, stream, expected, recs)
}

var errBoom = errors.New("boom")

type failingStore struct {
	ports.EventStore
	failLoad, failAppend, failReadAll bool
}

func (f failingStore) Load(ctx context.Context, s string) ([]ports.Record, error) {
	if f.failLoad {
		return nil, errBoom
	}
	return f.EventStore.Load(ctx, s)
}

func (f failingStore) Append(ctx context.Context, s string, e uint64, r []ports.Record) (uint64, error) {
	if f.failAppend {
		return 0, errBoom
	}
	return f.EventStore.Append(ctx, s, e, r)
}

func (f failingStore) ReadAll(ctx context.Context) ([]ports.Record, error) {
	if f.failReadAll {
		return nil, errBoom
	}
	return f.EventStore.ReadAll(ctx)
}

type failingProjector struct{}

func (failingProjector) Apply(ports.Record) error { return errBoom }

func domainID(s string) domain.AccountID { return domain.AccountID(s) }
