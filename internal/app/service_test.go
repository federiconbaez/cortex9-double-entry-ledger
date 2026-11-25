package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/adapters/jsonlstore"
	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/adapters/memstore"
	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/domain"
	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports"
)

var (
	ctx = context.Background()
	t0  = time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
)

type harness struct {
	store ports.EventStore
	proj  *BalanceProjection
	svc   *Service
}

func newHarness(t *testing.T, store ports.EventStore) *harness {
	t.Helper()
	p := NewBalanceProjection()
	h := &harness{store: store, proj: p, svc: NewService(store, &fakeClock{t: t0}, &seqIDs{}, p)}
	for _, c := range []OpenAccountCmd{
		{Ledger: "bank", Account: "cash", Name: "Caja", Type: "asset", Currency: "ARS"},
		{Ledger: "bank", Account: "wallet", Name: "Billetera cliente", Type: "liability", Currency: "ARS", Policy: PolicyNoOverdraft},
		{Ledger: "bank", Account: "credit", Name: "Linea de credito", Type: "liability", Currency: "ARS", Policy: PolicyLimit, Limit: 500},
		{Ledger: "bank", Account: "fees", Name: "Comisiones", Type: "revenue", Currency: "ARS"},
	} {
		if err := h.svc.OpenAccount(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

func deposit(id string, n int64) PostEntryCmd {
	return PostEntryCmd{Ledger: "bank", EntryID: id, Memo: "deposito", Lines: []LineCmd{
		{Account: "cash", Side: "debit", Minor: n, Currency: "ARS"},
		{Account: "wallet", Side: "credit", Minor: n, Currency: "ARS"},
	}}
}

func balance(t *testing.T, p *BalanceProjection, acct string) int64 {
	t.Helper()
	v, ok := p.Balance("bank", acct)
	if !ok {
		t.Fatalf("no row for %s", acct)
	}
	return v.Balance
}

func TestServicePostsAndProjects(t *testing.T) {
	h := newHarness(t, memstore.New())
	if _, err := h.svc.PostEntry(ctx, deposit("d1", 1000)); err != nil {
		t.Fatal(err)
	}
	fee := PostEntryCmd{Ledger: "bank", Lines: []LineCmd{
		{Account: "wallet", Side: "debit", Minor: 30, Currency: "ARS"},
		{Account: "fees", Side: "credit", Minor: 30, Currency: "ARS"},
	}}
	id, err := h.svc.PostEntry(ctx, fee)
	if err != nil || id != "gen-1" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if balance(t, h.proj, "cash") != 1000 || balance(t, h.proj, "wallet") != 970 || balance(t, h.proj, "fees") != 30 {
		t.Fatalf("balances %+v", h.proj.Balances("bank"))
	}
	if tb := h.proj.TrialBalance("bank"); tb["ARS"] != 0 {
		t.Fatalf("trial balance %v", tb)
	}
	if h.proj.Version("bank") != 6 || h.proj.Version("nope") != 0 || len(h.proj.Ledgers()) != 1 {
		t.Fatal("versions")
	}
	rid, err := h.svc.Reverse(ctx, ReverseCmd{Ledger: "bank", EntryID: "d1", ReversalID: "d1-rev"})
	if !errors.Is(err, domain.ErrInsufficientFunds) || rid != "" {
		t.Fatalf("reversing a spent deposit must fail: %v", err)
	}
	rid, err = h.svc.Reverse(ctx, ReverseCmd{Ledger: "bank", EntryID: id, Memo: "devolucion"})
	if err != nil || rid != "gen-2" || balance(t, h.proj, "wallet") != 1000 {
		t.Fatalf("reverse: %q %v", rid, err)
	}
}

func TestServiceRejectionsWriteNothing(t *testing.T) {
	h := newHarness(t, memstore.New())
	before, _ := h.store.ReadAll(ctx)
	bad := []error{}
	_, err := h.svc.PostEntry(ctx, deposit("d1", 0))
	bad = append(bad, err)
	_, err = h.svc.PostEntry(ctx, PostEntryCmd{Ledger: "bank", EntryID: "x", Lines: []LineCmd{{Account: "cash", Side: "debit", Minor: 1, Currency: "ARS"}}})
	bad = append(bad, err)
	_, err = h.svc.PostEntry(ctx, PostEntryCmd{Ledger: "bank", EntryID: "x", Lines: []LineCmd{
		{Account: "wallet", Side: "debit", Minor: 1, Currency: "ARS"}, {Account: "cash", Side: "credit", Minor: 1, Currency: "ARS"}}})
	bad = append(bad, err)
	_, err = h.svc.PostEntry(ctx, PostEntryCmd{Ledger: "bank", EntryID: "x", Lines: []LineCmd{
		{Account: "cash", Side: "sideways", Minor: 1, Currency: "ARS"}, {Account: "cash", Side: "credit", Minor: 1, Currency: "ARS"}}})
	bad = append(bad, err)
	_, err = h.svc.PostEntry(ctx, PostEntryCmd{Ledger: "bank", EntryID: "x", Lines: []LineCmd{
		{Account: "cash", Side: "debit", Minor: 1, Currency: "ars"}, {Account: "cash", Side: "credit", Minor: 1, Currency: "ARS"}}})
	bad = append(bad, err)
	_, err = h.svc.PostEntry(ctx, deposit("bad id!", 1))
	bad = append(bad, err)
	_, err = h.svc.PostEntry(ctx, PostEntryCmd{Ledger: "bad ledger!", EntryID: "x", Lines: deposit("x", 1).Lines})
	bad = append(bad, err)
	_, err = h.svc.Reverse(ctx, ReverseCmd{Ledger: "bank", EntryID: "ghost"})
	bad = append(bad, err)
	for _, c := range []OpenAccountCmd{
		{Ledger: "bank", Account: "cash", Name: "dup", Type: "asset", Currency: "ARS"},
		{Ledger: "bank", Account: "a", Name: "n", Type: "magic", Currency: "ARS"},
		{Ledger: "bank", Account: "a", Name: "n", Type: "asset", Currency: "?"},
		{Ledger: "bank", Account: "a", Name: "n", Type: "asset", Currency: "ARS", Policy: "yolo"},
		{Ledger: "bank", Account: "a", Name: "n", Type: "asset", Currency: "ARS", Policy: PolicyLimit, Limit: -1},
		{Ledger: "bank", Account: "a", Name: "", Type: "asset", Currency: "ARS"},
	} {
		bad = append(bad, h.svc.OpenAccount(ctx, c))
	}
	for i, err := range bad {
		var de *domain.Error
		if !errors.As(err, &de) {
			t.Errorf("case %d: want a typed domain error, got %v", i, err)
		}
	}
	after, _ := h.store.ReadAll(ctx)
	if len(after) != len(before) {
		t.Fatalf("rejected commands wrote %d records", len(after)-len(before))
	}
}

func TestEntryIDIsIdempotencyKey(t *testing.T) {
	h := newHarness(t, memstore.New())
	if _, err := h.svc.PostEntry(ctx, deposit("d1", 100)); err != nil {
		t.Fatal(err)
	}
	_, err := h.svc.PostEntry(ctx, deposit("d1", 100))
	if !errors.Is(err, domain.ErrDuplicateEntry) || balance(t, h.proj, "cash") != 100 {
		t.Fatalf("retry applied twice: %v", err)
	}
}

// Also a regression test: the first version fed only this command's
// records to the in-process projection, so events written by another
// service instance produced ErrProjectionGap. The UoW now catches up
// from the store.
func TestOptimisticConcurrencyRetry(t *testing.T) {
	inner := memstore.New()
	rs := &racingStore{Store: inner}
	h := newHarness(t, rs)
	other := NewService(inner, &fakeClock{t: t0}, &seqIDs{}, nil)
	n := 0
	rs.sneak = func(ctx context.Context) error {
		n++
		_, err := other.PostEntry(ctx, deposit(fmt.Sprintf("foreign-%d", n), 10))
		return err
	}
	rs.races, rs.attempts = 2, 0
	if _, err := h.svc.PostEntry(ctx, deposit("mine", 5)); err != nil {
		t.Fatal(err)
	}
	if rs.attempts != 3 {
		t.Fatalf("attempts = %d, want 3 (2 conflicts + success)", rs.attempts)
	}
	l, _ := NewLedgerRepository(inner).Load(ctx, "bank")
	b, _ := l.Balance("cash")
	if b.Minor() != 25 {
		t.Fatalf("cash = %d, want 25 (no lost update)", b.Minor())
	}
	if got := balance(t, h.proj, "cash"); got != 25 {
		t.Fatalf("read model cash = %d, want 25 (caught up foreign events)", got)
	}
	// Exhausting retries surfaces the conflict.
	rs.races, rs.attempts = MaxAttempts, 0
	_, err := h.svc.PostEntry(ctx, deposit("mine-2", 5))
	if !errors.Is(err, ports.ErrVersionConflict) || rs.attempts != MaxAttempts {
		t.Fatalf("err=%v attempts=%d", err, rs.attempts)
	}
}

func TestInfrastructureFailuresPropagate(t *testing.T) {
	base := memstore.New()
	h := newHarness(t, base)
	for name, fs := range map[string]failingStore{
		"load":   {EventStore: base, failLoad: true},
		"append": {EventStore: base, failAppend: true},
	} {
		t.Run(name, func(t *testing.T) {
			svc := NewService(fs, &fakeClock{t: t0}, &seqIDs{}, nil)
			if _, err := svc.PostEntry(ctx, deposit("z", 1)); !errors.Is(err, errBoom) {
				t.Fatalf("got %v", err)
			}
		})
	}
	if _, err := Rebuild(ctx, failingStore{EventStore: base, failReadAll: true}); !errors.Is(err, errBoom) {
		t.Fatalf("rebuild: %v", err)
	}
	svc := NewService(base, &fakeClock{t: t0}, &seqIDs{}, failingProjector{})
	_, err := svc.PostEntry(ctx, deposit("p1", 1))
	if !errors.Is(err, ErrProjectionStale) {
		t.Fatalf("projection failure: %v", err)
	}
	// The write itself is durable; rebuilding the read model catches up.
	p, err := Rebuild(ctx, base)
	if err != nil || balance(t, p, "cash") != 1 {
		t.Fatalf("rebuild: %v", err)
	}
	_ = h
}

func TestUnitOfWorkMisuse(t *testing.T) {
	u := NewUnitOfWork(memstore.New(), &fakeClock{t: t0}, nil)
	if err := u.Commit(ctx); !errors.Is(err, ErrUnitOfWork) {
		t.Fatal(err)
	}
	l := domain.NewLedger()
	if err := u.Register("a", l); err != nil {
		t.Fatal(err)
	}
	if err := u.Register("b", domain.NewLedger()); !errors.Is(err, ErrUnitOfWork) {
		t.Fatal("second aggregate must be refused")
	}
	if err := u.Commit(ctx); err != nil {
		t.Fatal("empty commit is a no-op")
	}
	if err := u.Commit(ctx); !errors.Is(err, ErrUnitOfWork) {
		t.Fatal("double commit")
	}
}

func TestProjectionIsIdempotentAndDetectsGaps(t *testing.T) {
	h := newHarness(t, memstore.New())
	_, _ = h.svc.PostEntry(ctx, deposit("d1", 7))
	recs, _ := h.store.ReadAll(ctx)
	p := NewBalanceProjection()
	for _, r := range append(recs, recs...) { // duplicate delivery
		if err := p.Apply(r); err != nil {
			t.Fatal(err)
		}
	}
	if balance(t, p, "cash") != 7 {
		t.Fatal("duplicate delivery double-counted")
	}
	gap := recs[len(recs)-1]
	gap.Version += 5
	if err := p.Apply(gap); !errors.Is(err, ErrProjectionGap) {
		t.Fatalf("gap: %v", err)
	}
	if _, ok := p.Balance("bank", "ghost"); ok || p.Balances("ghost") != nil {
		t.Fatal("unknown lookups")
	}
	// Records that contradict the read model are refused.
	q := NewBalanceProjection()
	if err := q.Apply(ports.Record{Stream: "ledger-x", Version: 1, Type: "Nope"}); err == nil {
		t.Fatal("unknown type")
	}
	post := recs[len(recs)-1]
	post.Stream, post.Version = "ledger-y", 1
	if err := q.Apply(post); err == nil {
		t.Fatal("posting to unknown account")
	}
	open := recs[0]
	open.Stream = "ledger-z"
	_ = q.Apply(open)
	open.Version = 2
	if err := q.Apply(open); err == nil {
		t.Fatal("duplicate account")
	}
}

func TestEndToEndOnJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	s, _, err := jsonlstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, s)
	_, _ = h.svc.PostEntry(ctx, deposit("d1", 1234))
	live := h.proj.Balances("bank")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, rep, err := jsonlstore.Open(path)
	if err != nil || rep.Records != 5 {
		t.Fatalf("%v %+v", err, rep)
	}
	defer func() { _ = s2.Close() }()
	p, err := Rebuild(ctx, s2)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Balances("bank"); len(got) != len(live) || got[0] != live[0] || got[1] != live[1] {
		t.Fatalf("rebuilt %+v != live %+v", got, live)
	}
}
