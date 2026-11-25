package domain

import (
	"fmt"
	"math/rand"
	"testing"
	"testing/quick"
	"time"
)

// scenario drives a ledger with a seeded random mix of valid and invalid
// commands. It is a hand-written generator: no mock or property library.
type scenario struct {
	r       *rand.Rand
	l       *Ledger
	history []Event
	byCur   map[Currency][]AccountID
	posted  []EntryID
	n       int
}

func newScenario(t testing.TB, seed int64) *scenario {
	t.Helper()
	s := &scenario{r: rand.New(rand.NewSource(seed)), l: NewLedger(), byCur: map[Currency][]AccountID{}} //nolint:gosec // deterministic test RNG
	types := []AccountType{Asset, Liability, Equity, Revenue, Expense}
	n := 3 + s.r.Intn(6)
	for i := 0; i < n; i++ {
		cur := ars
		if i%3 == 2 {
			cur = usd
		}
		var p OverdraftPolicy
		switch s.r.Intn(3) {
		case 0:
			p = Unrestricted()
		case 1:
			p = NoOverdraft()
		default:
			p, _ = OverdraftLimit(int64(s.r.Intn(1000)))
		}
		a := account(t, fmt.Sprintf("acc-%d", i), types[s.r.Intn(len(types))], cur, p)
		if err := s.l.OpenAccount(a, t0); err != nil {
			t.Fatal(err)
		}
		s.byCur[cur] = append(s.byCur[cur], a.ID())
	}
	s.commit()
	return s
}

func (s *scenario) commit() {
	s.history = append(s.history, s.l.Pending()...)
	s.l.MarkCommitted()
}

// randomEntry builds a balanced entry in one currency (or, rarely, a
// deliberately invalid one referencing an unknown account).
func (s *scenario) randomEntry(t testing.TB) JournalEntry {
	t.Helper()
	cur := ars
	if s.r.Intn(3) == 0 && len(s.byCur[usd]) > 0 {
		cur = usd
	}
	accts := s.byCur[cur]
	k := 2 + s.r.Intn(3)
	lines := make([]Line, 0, k+1)
	var total int64
	for i := 0; i < k-1; i++ {
		amt := int64(1 + s.r.Intn(2000))
		total += amt
		lines = append(lines, line(t, string(accts[s.r.Intn(len(accts))]), Debit, amt, cur))
	}
	credit := string(accts[s.r.Intn(len(accts))])
	if s.r.Intn(20) == 0 {
		credit = "ghost"
	}
	lines = append(lines, line(t, credit, Credit, total, cur))
	s.r.Shuffle(len(lines), func(i, j int) { lines[i], lines[j] = lines[j], lines[i] })
	s.n++
	return entry(t, fmt.Sprintf("e-%d", s.n), lines...)
}

func (s *scenario) step(t testing.TB) error {
	t.Helper()
	if len(s.posted) > 0 && s.r.Intn(6) == 0 {
		s.n++
		return s.l.Reverse(s.posted[s.r.Intn(len(s.posted))], EntryID(fmt.Sprintf("r-%d", s.n)), t0.Add(time.Duration(s.n)*time.Second), "")
	}
	e := s.randomEntry(t)
	err := s.l.Post(e)
	if err == nil {
		s.posted = append(s.posted, e.ID())
	}
	return err
}

func checkTrialBalanceZero(t testing.TB, l *Ledger) bool {
	t.Helper()
	tb, err := l.TrialBalance()
	if err != nil {
		t.Log(err)
		return false
	}
	for c, v := range tb {
		if v != 0 {
			t.Logf("trial balance %s = %d", c, v)
			return false
		}
	}
	return true
}

var quickCfg = &quick.Config{MaxCount: 300}

// I6 + I10: after any sequence of accepted/rejected commands the trial
// balance is zero, and a rejected command changes nothing.
func TestPropertyTrialBalanceAlwaysZero(t *testing.T) {
	var accepted, rejected int
	prop := func(seed int64) bool {
		s := newScenario(t, seed)
		for i := 0; i < 60; i++ {
			before := snapshot(t, s.l)
			err := s.step(t)
			if err != nil {
				rejected++
				if snapshot(t, s.l) != before || len(s.l.Pending()) != 0 {
					t.Logf("seed %d: rejected command mutated state: %v", seed, err)
					return false
				}
				continue
			}
			accepted++
			s.commit()
			if !checkTrialBalanceZero(t, s.l) {
				return false
			}
		}
		return true
	}
	if err := quick.Check(prop, quickCfg); err != nil {
		t.Fatal(err)
	}
	// Guard against a vacuous property: both paths must be exercised.
	if accepted < 1000 || rejected < 1000 {
		t.Fatalf("generator too narrow: accepted=%d rejected=%d", accepted, rejected)
	}
	t.Logf("accepted=%d rejected=%d", accepted, rejected)
}

// I7: replaying the committed history (twice) yields the live state.
func TestPropertyReplayDeterminism(t *testing.T) {
	prop := func(seed int64) bool {
		s := newScenario(t, seed)
		for i := 0; i < 60; i++ {
			if s.step(t) == nil {
				s.commit()
			}
		}
		a, err1 := Replay(s.history)
		b, err2 := Replay(s.history)
		if err1 != nil || err2 != nil {
			t.Log(err1, err2)
			return false
		}
		live := snapshot(t, s.l)
		return snapshot(t, a) == live && snapshot(t, b) == live && a.Version() == uint64(len(s.history))
	}
	if err := quick.Check(prop, quickCfg); err != nil {
		t.Fatal(err)
	}
}

// I6: every balance equals an independent naive fold of the line deltas.
func TestPropertyBalancesAreFoldOfEvents(t *testing.T) {
	prop := func(seed int64) bool {
		s := newScenario(t, seed)
		for i := 0; i < 60; i++ {
			if s.step(t) == nil {
				s.commit()
			}
		}
		fold := map[AccountID]int64{}
		for _, ev := range s.history {
			if p, ok := ev.(EntryPosted); ok {
				for _, ln := range p.Entry.Lines() {
					if ln.Side() == Debit {
						fold[ln.Account()] += ln.Amount().Minor()
					} else {
						fold[ln.Account()] -= ln.Amount().Minor()
					}
				}
			}
		}
		for _, a := range s.l.Accounts() {
			got, _ := s.l.SignedBalance(a.ID())
			if got.Minor() != fold[a.ID()] {
				t.Logf("seed %d: %s = %d, fold = %d", seed, a.ID(), got.Minor(), fold[a.ID()])
				return false
			}
		}
		return true
	}
	if err := quick.Check(prop, quickCfg); err != nil {
		t.Fatal(err)
	}
}
