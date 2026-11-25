package domain

import "fmt"

// Kind classifies a domain error. Kinds are stable and safe to match on.
type Kind string

// Error kinds.
const (
	KindInvalidCurrency   Kind = "invalid_currency"
	KindInvalidID         Kind = "invalid_id"
	KindInvalidAmount     Kind = "invalid_amount"
	KindCurrencyMismatch  Kind = "currency_mismatch"
	KindOverflow          Kind = "overflow"
	KindTooFewLines       Kind = "too_few_lines"
	KindUnbalanced        Kind = "unbalanced"
	KindInvalidAccount    Kind = "invalid_account"
	KindDuplicateAccount  Kind = "duplicate_account"
	KindUnknownAccount    Kind = "unknown_account"
	KindDuplicateEntry    Kind = "duplicate_entry"
	KindUnknownEntry      Kind = "unknown_entry"
	KindAlreadyReversed   Kind = "already_reversed"
	KindInsufficientFunds Kind = "insufficient_funds"
	KindCorruptHistory    Kind = "corrupt_history"
)

// Error is the single typed error of the domain. errors.Is matches on Kind,
// so callers compare against the Err* sentinels regardless of Detail.
type Error struct {
	Kind   Kind
	Detail string
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return "ledger: " + string(e.Kind)
	}
	return "ledger: " + string(e.Kind) + ": " + e.Detail
}

// Is reports whether target is a domain *Error with the same Kind.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error) //nolint:errorlint // Is must inspect target itself
	return ok && t.Kind == e.Kind
}

// Sentinels for errors.Is.
var (
	ErrInvalidCurrency   = &Error{Kind: KindInvalidCurrency}
	ErrInvalidID         = &Error{Kind: KindInvalidID}
	ErrInvalidAmount     = &Error{Kind: KindInvalidAmount}
	ErrCurrencyMismatch  = &Error{Kind: KindCurrencyMismatch}
	ErrOverflow          = &Error{Kind: KindOverflow}
	ErrTooFewLines       = &Error{Kind: KindTooFewLines}
	ErrUnbalanced        = &Error{Kind: KindUnbalanced}
	ErrInvalidAccount    = &Error{Kind: KindInvalidAccount}
	ErrDuplicateAccount  = &Error{Kind: KindDuplicateAccount}
	ErrUnknownAccount    = &Error{Kind: KindUnknownAccount}
	ErrDuplicateEntry    = &Error{Kind: KindDuplicateEntry}
	ErrUnknownEntry      = &Error{Kind: KindUnknownEntry}
	ErrAlreadyReversed   = &Error{Kind: KindAlreadyReversed}
	ErrInsufficientFunds = &Error{Kind: KindInsufficientFunds}
	ErrCorruptHistory    = &Error{Kind: KindCorruptHistory}
)

func errf(k Kind, format string, args ...any) error {
	return &Error{Kind: k, Detail: fmt.Sprintf(format, args...)}
}
