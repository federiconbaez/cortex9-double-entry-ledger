package domain

// Specification is the classic Specification pattern: a composable,
// side-effect-free business rule.
type Specification[T any] interface {
	IsSatisfiedBy(candidate T) bool
}

// SpecFunc adapts a function to Specification.
type SpecFunc[T any] func(T) bool

// IsSatisfiedBy calls f.
func (f SpecFunc[T]) IsSatisfiedBy(c T) bool { return f(c) }

// And is satisfied when every spec is.
func And[T any](specs ...Specification[T]) Specification[T] {
	return SpecFunc[T](func(c T) bool {
		for _, s := range specs {
			if !s.IsSatisfiedBy(c) {
				return false
			}
		}
		return true
	})
}

// Or is satisfied when any spec is.
func Or[T any](specs ...Specification[T]) Specification[T] {
	return SpecFunc[T](func(c T) bool {
		for _, s := range specs {
			if s.IsSatisfiedBy(c) {
				return true
			}
		}
		return false
	})
}

// Not negates a spec.
func Not[T any](s Specification[T]) Specification[T] {
	return SpecFunc[T](func(c T) bool { return !s.IsSatisfiedBy(c) })
}

// BalanceProposal is the candidate evaluated before a posting: the
// account and its normal balance before and after the movement.
type BalanceProposal struct {
	Account  Account
	Current  int64
	Proposed int64
}

// NonWorsening is satisfied when the movement does not lower the normal balance.
func NonWorsening() Specification[BalanceProposal] {
	return SpecFunc[BalanceProposal](func(p BalanceProposal) bool { return p.Proposed >= p.Current })
}

// WithinOverdraftPolicy is satisfied when the proposed balance respects the account policy.
func WithinOverdraftPolicy() Specification[BalanceProposal] {
	return SpecFunc[BalanceProposal](func(p BalanceProposal) bool { return p.Account.policy.Allows(p.Proposed) })
}

// SufficientBalance is the posting guard (invariant I9): a movement is
// accepted if it respects the overdraft policy, or if it does not make
// the balance worse (so an already-negative account can be repaid).
func SufficientBalance() Specification[BalanceProposal] {
	return Or(WithinOverdraftPolicy(), NonWorsening())
}
