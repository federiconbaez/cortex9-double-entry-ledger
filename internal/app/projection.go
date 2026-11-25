package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/domain"
	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports"
)

// ErrProjectionGap is returned when a record skips a version.
var ErrProjectionGap = errors.New("app: projection received out-of-order record")

// BalanceView is the read model row for one account.
type BalanceView struct {
	Account  string
	Type     string
	Currency string
	Debits   int64 // total debited
	Credits  int64 // total credited
	Balance  int64 // in the account's natural sign
	normal   domain.Side
}

type ledgerView struct {
	rows  map[string]*BalanceView
	order []string
	last  uint64
}

// BalanceProjection is the CQRS read model: balances per account,
// computed independently from the aggregate by folding records.
// Apply is idempotent per (stream, version).
type BalanceProjection struct {
	mu      sync.RWMutex
	ledgers map[string]*ledgerView
}

// NewBalanceProjection returns an empty read model.
func NewBalanceProjection() *BalanceProjection {
	return &BalanceProjection{ledgers: map[string]*ledgerView{}}
}

var _ Projector = (*BalanceProjection)(nil)

// Apply folds one committed record into the read model.
func (p *BalanceProjection) Apply(rec ports.Record) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	lv := p.ledgers[rec.Stream]
	if lv == nil {
		lv = &ledgerView{rows: map[string]*BalanceView{}}
		p.ledgers[rec.Stream] = lv
	}
	if rec.Version <= lv.last {
		return nil // duplicate delivery
	}
	if rec.Version != lv.last+1 {
		return fmt.Errorf("%w: %s got v%d after v%d", ErrProjectionGap, rec.Stream, rec.Version, lv.last)
	}
	ev, err := DecodeEvent(rec)
	if err != nil {
		return err
	}
	switch e := ev.(type) {
	case domain.AccountOpened:
		a := e.Account
		id := string(a.ID())
		if _, dup := lv.rows[id]; dup {
			return fmt.Errorf("app: projection: duplicate account %s", id)
		}
		lv.rows[id] = &BalanceView{Account: id, Type: a.Type().String(), Currency: a.Currency().String(), normal: a.Type().NormalSide()}
		lv.order = append(lv.order, id)
	case domain.EntryPosted:
		next := map[string]BalanceView{}
		for _, l := range e.Entry.Lines() {
			id := string(l.Account())
			row, ok := next[id]
			if !ok {
				cur := lv.rows[id]
				if cur == nil {
					return fmt.Errorf("app: projection: unknown account %s", id)
				}
				row = *cur
			}
			amt := l.Amount().Minor()
			var ok1, ok2 bool
			if l.Side() == domain.Debit {
				row.Debits, ok1 = add(row.Debits, amt)
			} else {
				row.Credits, ok1 = add(row.Credits, amt)
			}
			if l.Side() == row.normal {
				row.Balance, ok2 = add(row.Balance, amt)
			} else {
				row.Balance, ok2 = add(row.Balance, -amt)
			}
			if !ok1 || !ok2 {
				return fmt.Errorf("app: projection: overflow on %s", id)
			}
			next[id] = row
		}
		for id, row := range next {
			*lv.rows[id] = row
		}
	}
	lv.last = rec.Version
	return nil
}

func add(a, b int64) (int64, bool) {
	s := a + b
	if (b > 0 && s < a) || (b < 0 && s > a) {
		return 0, false
	}
	return s, true
}

// Balance returns one account's row.
func (p *BalanceProjection) Balance(ledgerID, account string) (BalanceView, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	lv := p.ledgers[StreamName(ledgerID)]
	if lv == nil || lv.rows[account] == nil {
		return BalanceView{}, false
	}
	return *lv.rows[account], true
}

// Balances returns all rows of a ledger in opening order.
func (p *BalanceProjection) Balances(ledgerID string) []BalanceView {
	p.mu.RLock()
	defer p.mu.RUnlock()
	lv := p.ledgers[StreamName(ledgerID)]
	if lv == nil {
		return nil
	}
	out := make([]BalanceView, 0, len(lv.order))
	for _, id := range lv.order {
		out = append(out, *lv.rows[id])
	}
	return out
}

// TrialBalance returns Σdebits−Σcredits per currency from the read model.
func (p *BalanceProjection) TrialBalance(ledgerID string) map[string]int64 {
	out := map[string]int64{}
	for _, r := range p.Balances(ledgerID) {
		out[r.Currency] += r.Debits - r.Credits
	}
	return out
}

// Version returns the last applied version of a ledger.
func (p *BalanceProjection) Version(ledgerID string) uint64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if lv := p.ledgers[StreamName(ledgerID)]; lv != nil {
		return lv.last
	}
	return 0
}

// Ledgers returns the ledger ids known to the read model, sorted.
func (p *BalanceProjection) Ledgers() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]string, 0, len(p.ledgers))
	for s := range p.ledgers {
		out = append(out, s[len("ledger-"):])
	}
	slices.Sort(out)
	return out
}

// Rebuild discards the read model and refolds the whole store.
func Rebuild(ctx context.Context, store ports.EventStore) (*BalanceProjection, error) {
	recs, err := store.ReadAll(ctx)
	if err != nil {
		return nil, err
	}
	p := NewBalanceProjection()
	for _, r := range recs {
		if err := p.Apply(r); err != nil {
			return nil, err
		}
	}
	return p, nil
}
