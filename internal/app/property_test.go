package app

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"testing"
	"testing/quick"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/adapters/jsonlstore"
	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/adapters/memstore"
	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports"
)

// runRandom drives the Service with seeded random commands.
func runRandom(t *testing.T, h *harness, seed int64, steps int) {
	t.Helper()
	r := rand.New(rand.NewSource(seed)) //nolint:gosec // deterministic test RNG
	accts := []string{"cash", "wallet", "credit", "fees"}
	var posted []string
	for i := 0; i < steps; i++ {
		if len(posted) > 0 && r.Intn(5) == 0 {
			_, _ = h.svc.Reverse(ctx, ReverseCmd{Ledger: "bank", EntryID: posted[r.Intn(len(posted))]})
			continue
		}
		n := int64(1 + r.Intn(800))
		d, c := accts[r.Intn(4)], accts[r.Intn(4)]
		id := fmt.Sprintf("p-%d", i)
		if _, err := h.svc.PostEntry(ctx, PostEntryCmd{Ledger: "bank", EntryID: id, Lines: []LineCmd{
			{Account: d, Side: "debit", Minor: n, Currency: "ARS"},
			{Account: c, Side: "credit", Minor: n, Currency: "ARS"},
		}}); err == nil {
			posted = append(posted, id)
		}
	}
}

// I8: live read model == Rebuild(store) == aggregate replayed from the
// store, and the read-model trial balance is zero.
func checkProjectionIsFold(t *testing.T, h *harness) bool {
	t.Helper()
	rebuilt, err := Rebuild(ctx, h.store)
	if err != nil {
		t.Log(err)
		return false
	}
	l, err := NewLedgerRepository(h.store).Load(ctx, "bank")
	if err != nil {
		t.Log(err)
		return false
	}
	live := h.proj.Balances("bank")
	if fmt.Sprint(live) != fmt.Sprint(rebuilt.Balances("bank")) {
		t.Logf("live %v != rebuilt %v", live, rebuilt.Balances("bank"))
		return false
	}
	for _, row := range live {
		b, err := l.Balance(domainID(row.Account))
		if err != nil || b.Minor() != row.Balance {
			t.Logf("%s: aggregate %v, read model %d (%v)", row.Account, b, row.Balance, err)
			return false
		}
	}
	return h.proj.TrialBalance("bank")["ARS"] == 0 && h.proj.Version("bank") == l.Version()
}

func TestPropertyProjectionEqualsFoldMem(t *testing.T) {
	prop := func(seed int64) bool {
		h := newHarness(t, memstore.New())
		runRandom(t, h, seed, 40)
		return checkProjectionIsFold(t, h)
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatal(err)
	}
}

func TestPropertyProjectionEqualsFoldJSONL(t *testing.T) {
	prop := func(seed int64) bool {
		path := filepath.Join(t.TempDir(), "p.jsonl")
		s, _, err := jsonlstore.Open(path)
		if err != nil {
			t.Log(err)
			return false
		}
		h := newHarness(t, s)
		runRandom(t, h, seed, 25)
		ok := checkProjectionIsFold(t, h)
		// Durability: a reopened store yields the same fold.
		_ = s.Close()
		s2, _, err := jsonlstore.Open(path)
		if err != nil {
			t.Log(err)
			return false
		}
		defer func() { _ = s2.Close() }()
		rebuilt, err := Rebuild(ctx, ports.EventStore(s2))
		return ok && err == nil && fmt.Sprint(rebuilt.Balances("bank")) == fmt.Sprint(h.proj.Balances("bank"))
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 20}); err != nil {
		t.Fatal(err)
	}
}
