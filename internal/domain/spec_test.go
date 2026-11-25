package domain

import "testing"

func TestSpecificationCombinators(t *testing.T) {
	even := SpecFunc[int](func(n int) bool { return n%2 == 0 })
	pos := SpecFunc[int](func(n int) bool { return n > 0 })
	cases := []struct {
		s    Specification[int]
		n    int
		want bool
	}{
		{And[int](even, pos), 2, true},
		{And[int](even, pos), -2, false},
		{Or[int](even, pos), -2, true},
		{Or[int](even, pos), -3, false},
		{Not[int](even), 3, true},
		{And[int](), 1, true},
		{Or[int](), 1, false},
	}
	for i, c := range cases {
		if got := c.s.IsSatisfiedBy(c.n); got != c.want {
			t.Fatalf("case %d: got %v", i, got)
		}
	}
}

func TestSufficientBalance(t *testing.T) {
	a := account(t, "w", Liability, ars, NoOverdraft())
	s := SufficientBalance()
	for _, c := range []struct {
		cur, next int64
		want      bool
	}{
		{100, 0, true},
		{100, -1, false},
		{-50, -20, true},  // repaying is non-worsening
		{-50, -60, false}, // worsening an existing overdraft
	} {
		if got := s.IsSatisfiedBy(BalanceProposal{Account: a, Current: c.cur, Proposed: c.next}); got != c.want {
			t.Fatalf("%d->%d got %v", c.cur, c.next, got)
		}
	}
}

func TestErrorFormatting(t *testing.T) {
	if ErrUnbalanced.Error() != "ledger: unbalanced" {
		t.Fatal(ErrUnbalanced.Error())
	}
	e := errf(KindUnbalanced, "x")
	if e.Error() != "ledger: unbalanced: x" {
		t.Fatal(e.Error())
	}
}
