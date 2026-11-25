package domain

import "fmt"

// AccountID identifies an account inside a ledger.
type AccountID string

// EntryID identifies a journal entry inside a ledger.
type EntryID string

const maxIDLen = 64

func validID(s string) error {
	if s == "" || len(s) > maxIDLen {
		return errf(KindInvalidID, "%q must be 1..%d chars", s, maxIDLen)
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '-' || c == '_' || c == '.' || c == ':'
		if !ok {
			return errf(KindInvalidID, "%q has invalid char %q", s, c)
		}
	}
	return nil
}

// ValidateID checks the syntax shared by every identifier of the model
// (ledger, account and entry ids): 1..64 chars of [A-Za-z0-9._:-].
func ValidateID(s string) error { return validID(s) }

// Validate checks the identifier syntax.
func (id AccountID) Validate() error { return validID(string(id)) }

// Validate checks the identifier syntax.
func (id EntryID) Validate() error { return validID(string(id)) }

// Side is the side of a journal line.
type Side uint8

// Sides.
const (
	Debit Side = iota + 1
	Credit
)

func (s Side) String() string {
	switch s {
	case Debit:
		return "debit"
	case Credit:
		return "credit"
	}
	return fmt.Sprintf("side(%d)", uint8(s))
}

// ParseSide parses "debit" or "credit".
func ParseSide(s string) (Side, error) {
	switch s {
	case "debit":
		return Debit, nil
	case "credit":
		return Credit, nil
	}
	return 0, errf(KindInvalidAmount, "unknown side %q", s)
}

func (s Side) valid() bool { return s == Debit || s == Credit }

// AccountType is the accounting class of an account.
type AccountType uint8

// Account types.
const (
	Asset AccountType = iota + 1
	Liability
	Equity
	Revenue
	Expense
)

var accountTypeNames = map[AccountType]string{
	Asset: "asset", Liability: "liability", Equity: "equity", Revenue: "revenue", Expense: "expense",
}

func (t AccountType) String() string {
	if n, ok := accountTypeNames[t]; ok {
		return n
	}
	return fmt.Sprintf("type(%d)", uint8(t))
}

// ParseAccountType parses the String form of an AccountType.
func ParseAccountType(s string) (AccountType, error) {
	for t, n := range accountTypeNames {
		if n == s {
			return t, nil
		}
	}
	return 0, errf(KindInvalidAccount, "unknown account type %q", s)
}

// NormalSide is the side that increases the account's natural balance.
func (t AccountType) NormalSide() Side {
	if t == Asset || t == Expense {
		return Debit
	}
	return Credit
}

type policyKind uint8

const (
	policyUnrestricted policyKind = iota
	policyNoOverdraft
	policyLimit
)

// OverdraftPolicy bounds how negative an account's normal balance may go.
type OverdraftPolicy struct {
	kind  policyKind
	limit int64
}

// Unrestricted allows any balance (typical for equity or clearing accounts).
func Unrestricted() OverdraftPolicy { return OverdraftPolicy{kind: policyUnrestricted} }

// NoOverdraft forbids a negative normal balance.
func NoOverdraft() OverdraftPolicy { return OverdraftPolicy{kind: policyNoOverdraft} }

// OverdraftLimit allows the normal balance down to -limit minor units.
func OverdraftLimit(limit int64) (OverdraftPolicy, error) {
	if limit < 0 {
		return OverdraftPolicy{}, errf(KindInvalidAmount, "overdraft limit %d < 0", limit)
	}
	return OverdraftPolicy{kind: policyLimit, limit: limit}, nil
}

// Allows reports whether a normal balance is acceptable under the policy.
func (p OverdraftPolicy) Allows(normalBalance int64) bool {
	switch p.kind {
	case policyNoOverdraft:
		return normalBalance >= 0
	case policyLimit:
		return normalBalance >= -p.limit
	}
	return true
}

// Limit returns the overdraft limit and whether the policy is bounded.
// NoOverdraft reports (0, true); Unrestricted reports (0, false).
func (p OverdraftPolicy) Limit() (int64, bool) {
	return p.limit, p.kind != policyUnrestricted
}

func (p OverdraftPolicy) String() string {
	switch p.kind {
	case policyNoOverdraft:
		return "no-overdraft"
	case policyLimit:
		return fmt.Sprintf("overdraft-limit(%d)", p.limit)
	}
	return "unrestricted"
}

// Account is an immutable value describing an account. Its balance is
// never stored here: it is derived by the Ledger from events.
type Account struct {
	id     AccountID
	name   string
	typ    AccountType
	cur    Currency
	policy OverdraftPolicy
}

// NewAccount validates and builds an Account.
func NewAccount(id AccountID, name string, typ AccountType, cur Currency, policy OverdraftPolicy) (Account, error) {
	if err := id.Validate(); err != nil {
		return Account{}, err
	}
	if name == "" || len(name) > 200 {
		return Account{}, errf(KindInvalidAccount, "name must be 1..200 bytes")
	}
	if _, ok := accountTypeNames[typ]; !ok {
		return Account{}, errf(KindInvalidAccount, "invalid type %d", uint8(typ))
	}
	if cur.IsZero() {
		return Account{}, errf(KindInvalidCurrency, "account %s has zero currency", id)
	}
	return Account{id: id, name: name, typ: typ, cur: cur, policy: policy}, nil
}

// ID returns the account id.
func (a Account) ID() AccountID { return a.id }

// Name returns the display name.
func (a Account) Name() string { return a.name }

// Type returns the accounting class.
func (a Account) Type() AccountType { return a.typ }

// Currency returns the only currency the account accepts.
func (a Account) Currency() Currency { return a.cur }

// Policy returns the overdraft policy.
func (a Account) Policy() OverdraftPolicy { return a.policy }

// normal converts a debit-positive signed balance to the account's natural sign.
func (a Account) normal(signed int64) (int64, bool) {
	if a.typ.NormalSide() == Debit {
		return signed, true
	}
	if signed == -signed && signed != 0 { // math.MinInt64
		return 0, false
	}
	return -signed, true
}
