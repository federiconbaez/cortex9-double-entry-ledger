// Package domain contains the pure double-entry ledger model: money,
// accounts, journal entries, the Ledger aggregate, its events and the
// specifications that guard postings. It performs no I/O and imports
// only a small whitelist of the standard library (enforced by tests).
package domain
