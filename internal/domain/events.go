package domain

import "time"

// Event is a fact recorded in a ledger stream.
type Event interface {
	EventType() string
	isEvent()
}

// Event type names (stable: they are persisted).
const (
	TypeAccountOpened = "AccountOpened"
	TypeEntryPosted   = "EntryPosted"
)

// AccountOpened records the creation of an account.
type AccountOpened struct {
	Account  Account
	OpenedAt time.Time
}

// EventType implements Event.
func (AccountOpened) EventType() string { return TypeAccountOpened }
func (AccountOpened) isEvent()          {}

// EntryPosted records a balanced journal entry (possibly a reversal).
type EntryPosted struct {
	Entry JournalEntry
}

// EventType implements Event.
func (EntryPosted) EventType() string { return TypeEntryPosted }
func (EntryPosted) isEvent()          {}
