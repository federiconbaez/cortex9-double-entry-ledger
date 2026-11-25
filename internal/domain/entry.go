package domain

import (
	"slices"
	"time"
)

// Line is one debit or credit of a journal entry. Amount is always > 0;
// the direction is carried by Side.
type Line struct {
	account AccountID
	side    Side
	amount  Money
}

// NewLine validates and builds a Line.
func NewLine(account AccountID, side Side, amount Money) (Line, error) {
	if err := account.Validate(); err != nil {
		return Line{}, err
	}
	if !side.valid() {
		return Line{}, errf(KindInvalidAmount, "invalid side %d", uint8(side))
	}
	if amount.Currency().IsZero() {
		return Line{}, errf(KindInvalidCurrency, "line on %s has zero currency", account)
	}
	if !amount.IsPositive() {
		return Line{}, errf(KindInvalidAmount, "line on %s: amount %s must be > 0", account, amount)
	}
	return Line{account: account, side: side, amount: amount}, nil
}

// Account returns the account the line moves.
func (l Line) Account() AccountID { return l.account }

// Side returns debit or credit.
func (l Line) Side() Side { return l.side }

// Amount returns the positive amount.
func (l Line) Amount() Money { return l.amount }

// signed returns the debit-positive delta of the line.
func (l Line) signed() int64 {
	if l.side == Debit {
		return l.amount.minor
	}
	return -l.amount.minor // amount > 0, so no overflow
}

const maxMemoLen = 500

// JournalEntry is an immutable, balanced set of lines. Fields are
// unexported and accessors return copies, so an entry cannot change
// after construction (invariant I5).
type JournalEntry struct {
	id         EntryID
	postedAt   time.Time
	memo       string
	reversalOf EntryID
	lines      []Line
}

// NewJournalEntry validates invariant I3 and builds an entry.
func NewJournalEntry(id EntryID, postedAt time.Time, memo string, lines ...Line) (JournalEntry, error) {
	return newEntry(id, postedAt, memo, "", lines)
}

// NewReversalEntry builds an entry that reverses `of`. It is used by the
// Ledger and by codecs rehydrating history.
func NewReversalEntry(id EntryID, postedAt time.Time, memo string, of EntryID, lines ...Line) (JournalEntry, error) {
	if err := of.Validate(); err != nil {
		return JournalEntry{}, err
	}
	return newEntry(id, postedAt, memo, of, lines)
}

func newEntry(id EntryID, postedAt time.Time, memo string, of EntryID, lines []Line) (JournalEntry, error) {
	if err := id.Validate(); err != nil {
		return JournalEntry{}, err
	}
	if of == id {
		return JournalEntry{}, errf(KindInvalidID, "entry %s cannot reverse itself", id)
	}
	if postedAt.IsZero() {
		return JournalEntry{}, errf(KindInvalidAmount, "entry %s has zero time", id)
	}
	if len(memo) > maxMemoLen {
		return JournalEntry{}, errf(KindInvalidAmount, "memo longer than %d bytes", maxMemoLen)
	}
	if len(lines) < 2 {
		return JournalEntry{}, errf(KindTooFewLines, "entry %s has %d lines", id, len(lines))
	}
	type sums struct{ debit, credit int64 }
	per := map[Currency]*sums{}
	order := []Currency{}
	for i, l := range lines {
		if !l.side.valid() || !l.amount.IsPositive() || l.amount.cur.IsZero() || l.account.Validate() != nil {
			return JournalEntry{}, errf(KindInvalidAmount, "entry %s line %d is not a valid Line", id, i)
		}
		s, ok := per[l.amount.cur]
		if !ok {
			s = &sums{}
			per[l.amount.cur] = s
			order = append(order, l.amount.cur)
		}
		var sumOK bool
		if l.side == Debit {
			s.debit, sumOK = addInt64(s.debit, l.amount.minor)
		} else {
			s.credit, sumOK = addInt64(s.credit, l.amount.minor)
		}
		if !sumOK {
			return JournalEntry{}, errf(KindOverflow, "entry %s %s total", id, l.amount.cur)
		}
	}
	for _, c := range order {
		if s := per[c]; s.debit != s.credit {
			return JournalEntry{}, errf(KindUnbalanced, "entry %s %s: debits %d != credits %d", id, c, s.debit, s.credit)
		}
	}
	return JournalEntry{id: id, postedAt: postedAt.UTC(), memo: memo, reversalOf: of, lines: slices.Clone(lines)}, nil
}

// ID returns the entry id.
func (e JournalEntry) ID() EntryID { return e.id }

// PostedAt returns the posting time (UTC).
func (e JournalEntry) PostedAt() time.Time { return e.postedAt }

// Memo returns the free-text memo.
func (e JournalEntry) Memo() string { return e.memo }

// ReversalOf returns the reversed entry id, or "" for an ordinary entry.
func (e JournalEntry) ReversalOf() EntryID { return e.reversalOf }

// Lines returns a copy of the lines.
func (e JournalEntry) Lines() []Line { return slices.Clone(e.lines) }

// reversed returns the mirror entry (every side flipped).
func (e JournalEntry) reversed(id EntryID, at time.Time, memo string) (JournalEntry, error) {
	lines := make([]Line, len(e.lines))
	for i, l := range e.lines {
		l.side = Credit + Debit - l.side
		lines[i] = l
	}
	return NewReversalEntry(id, at, memo, e.id, lines...)
}
