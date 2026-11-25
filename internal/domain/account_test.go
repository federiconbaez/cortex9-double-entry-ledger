package domain

import (
	"strings"
	"testing"
)

func TestIDs(t *testing.T) {
	for _, ok := range []string{"a", "acc-1", "x_y.z:9", strings.Repeat("a", 64)} {
		if err := AccountID(ok).Validate(); err != nil {
			t.Fatalf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "a b", "ñ", strings.Repeat("a", 65), "a/b"} {
		wantErr(t, EntryID(bad).Validate(), ErrInvalidID)
		wantErr(t, ValidateID(bad), ErrInvalidID)
	}
}

func TestSideAndTypeParsing(t *testing.T) {
	for _, s := range []Side{Debit, Credit} {
		got, err := ParseSide(s.String())
		if err != nil || got != s {
			t.Fatalf("%v: %v", s, err)
		}
	}
	if _, err := ParseSide("up"); err == nil {
		t.Fatal("want error")
	}
	if Side(9).String() != "side(9)" || AccountType(9).String() != "type(9)" {
		t.Fatal("unknown String forms")
	}
	for _, typ := range []AccountType{Asset, Liability, Equity, Revenue, Expense} {
		got, err := ParseAccountType(typ.String())
		if err != nil || got != typ {
			t.Fatalf("%v: %v", typ, err)
		}
	}
	_, err := ParseAccountType("magic")
	wantErr(t, err, ErrInvalidAccount)
	normals := map[AccountType]Side{Asset: Debit, Expense: Debit, Liability: Credit, Equity: Credit, Revenue: Credit}
	for typ, side := range normals {
		if typ.NormalSide() != side {
			t.Fatalf("%v normal side", typ)
		}
	}
}

func TestOverdraftPolicies(t *testing.T) {
	lim, err := OverdraftLimit(100)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		p       OverdraftPolicy
		bal     int64
		allowed bool
	}{
		{Unrestricted(), -1 << 62, true},
		{NoOverdraft(), 0, true},
		{NoOverdraft(), -1, false},
		{lim, -100, true},
		{lim, -101, false},
	}
	for _, c := range cases {
		if c.p.Allows(c.bal) != c.allowed {
			t.Fatalf("%v allows(%d) != %v", c.p, c.bal, c.allowed)
		}
	}
	if n, b := lim.Limit(); n != 100 || !b || lim.String() != "overdraft-limit(100)" {
		t.Fatal("limit accessors")
	}
	if _, b := Unrestricted().Limit(); b || Unrestricted().String() != "unrestricted" || NoOverdraft().String() != "no-overdraft" {
		t.Fatal("policy accessors")
	}
	_, err = OverdraftLimit(-1)
	wantErr(t, err, ErrInvalidAmount)
}

func TestNewAccountValidation(t *testing.T) {
	a := account(t, "cash", Asset, ars, NoOverdraft())
	if a.ID() != "cash" || a.Name() != "acct cash" || a.Type() != Asset || a.Currency() != ars || a.Policy() != NoOverdraft() {
		t.Fatal("accessors")
	}
	_, err := NewAccount("", "n", Asset, ars, Unrestricted())
	wantErr(t, err, ErrInvalidID)
	_, err = NewAccount("a", "", Asset, ars, Unrestricted())
	wantErr(t, err, ErrInvalidAccount)
	_, err = NewAccount("a", "n", AccountType(0), ars, Unrestricted())
	wantErr(t, err, ErrInvalidAccount)
	_, err = NewAccount("a", "n", Asset, Currency{}, Unrestricted())
	wantErr(t, err, ErrInvalidCurrency)
}
