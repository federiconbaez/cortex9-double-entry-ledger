package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/domain"
	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports"
)

// MaxAttempts bounds optimistic-concurrency retries per command.
const MaxAttempts = 3

// OpenAccountCmd opens an account in a ledger.
type OpenAccountCmd struct {
	Ledger, Account, Name, Type, Currency string
	Policy                                string // PolicyUnrestricted | PolicyNoOverdraft | PolicyLimit
	Limit                                 int64
}

// LineCmd is one line of PostEntryCmd.
type LineCmd struct {
	Account, Side string
	Minor         int64
	Currency      string
}

// PostEntryCmd posts a journal entry. EntryID doubles as an idempotency
// key: a retried command with the same id is rejected as duplicate
// instead of being applied twice. Empty means "generate one".
type PostEntryCmd struct {
	Ledger, EntryID, Memo string
	Lines                 []LineCmd
}

// ReverseCmd reverses a posted entry.
type ReverseCmd struct {
	Ledger, EntryID, ReversalID, Memo string
}

// Service is the command side (CQRS write model).
type Service struct {
	repo      *LedgerRepository
	store     ports.EventStore
	clock     ports.Clock
	ids       ports.IDGenerator
	projector Projector
}

// NewService wires the command side. projector may be nil.
func NewService(store ports.EventStore, clock ports.Clock, ids ports.IDGenerator, projector Projector) *Service {
	return &Service{repo: NewLedgerRepository(store), store: store, clock: clock, ids: ids, projector: projector}
}

// execute loads, runs one domain operation, commits, and retries on a
// version conflict by reloading (the command is re-validated against
// fresh state, never blindly re-appended).
func (s *Service) execute(ctx context.Context, ledgerID string, op func(*domain.Ledger) error) error {
	var err error
	for attempt := 0; attempt < MaxAttempts; attempt++ {
		var l *domain.Ledger
		if l, err = s.repo.Load(ctx, ledgerID); err != nil {
			return err
		}
		if err = op(l); err != nil {
			return err
		}
		uow := NewUnitOfWork(s.store, s.clock, s.projector)
		if err = uow.Register(ledgerID, l); err != nil {
			return err
		}
		if err = uow.Commit(ctx); !errors.Is(err, ports.ErrVersionConflict) {
			return err
		}
	}
	return fmt.Errorf("app: gave up after %d attempts: %w", MaxAttempts, err)
}

// OpenAccount handles OpenAccountCmd.
func (s *Service) OpenAccount(ctx context.Context, c OpenAccountCmd) error {
	typ, err := domain.ParseAccountType(c.Type)
	if err != nil {
		return err
	}
	cur, err := domain.ParseCurrency(c.Currency)
	if err != nil {
		return err
	}
	pol, err := ParsePolicy(c.Policy, c.Limit)
	if err != nil {
		return err
	}
	a, err := domain.NewAccount(domain.AccountID(c.Account), c.Name, typ, cur, pol)
	if err != nil {
		return err
	}
	at := s.clock.Now()
	return s.execute(ctx, c.Ledger, func(l *domain.Ledger) error { return l.OpenAccount(a, at) })
}

// PostEntry handles PostEntryCmd and returns the entry id.
func (s *Service) PostEntry(ctx context.Context, c PostEntryCmd) (string, error) {
	id := c.EntryID
	if id == "" {
		id = s.ids.NewID()
	}
	dtos := make([]lineDTO, len(c.Lines))
	for i, l := range c.Lines {
		dtos[i] = lineDTO(l)
	}
	lines, err := buildLines(dtos)
	if err != nil {
		return "", err
	}
	e, err := domain.NewJournalEntry(domain.EntryID(id), s.clock.Now(), c.Memo, lines...)
	if err != nil {
		return "", err
	}
	if err := s.execute(ctx, c.Ledger, func(l *domain.Ledger) error { return l.Post(e) }); err != nil {
		return "", err
	}
	return id, nil
}

// Reverse handles ReverseCmd and returns the reversal entry id.
func (s *Service) Reverse(ctx context.Context, c ReverseCmd) (string, error) {
	id := c.ReversalID
	if id == "" {
		id = s.ids.NewID()
	}
	at := s.clock.Now()
	err := s.execute(ctx, c.Ledger, func(l *domain.Ledger) error {
		return l.Reverse(domain.EntryID(c.EntryID), domain.EntryID(id), at, c.Memo)
	})
	if err != nil {
		return "", err
	}
	return id, nil
}
