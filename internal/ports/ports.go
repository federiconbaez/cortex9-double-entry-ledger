// Package ports declares the driven ports of the hexagon. Adapters
// implement them; the application layer depends only on these interfaces.
package ports

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Record is a persisted event envelope. Data is opaque to the store.
type Record struct {
	Stream     string
	Version    uint64 // 1-based position inside Stream
	Type       string
	Data       []byte
	RecordedAt time.Time
}

// EventStore is an append-only, per-stream versioned log.
//
// Append writes recs atomically at versions expected+1..expected+n only if
// the stream's current version equals expected; otherwise it returns a
// *ConflictError (errors.Is(err, ErrVersionConflict)) and writes nothing.
// The store overwrites Stream and Version of the given records.
type EventStore interface {
	Append(ctx context.Context, stream string, expected uint64, recs []Record) (uint64, error)
	Load(ctx context.Context, stream string) ([]Record, error)
	ReadAll(ctx context.Context) ([]Record, error)
}

// Clock is the only source of time for the application.
type Clock interface{ Now() time.Time }

// IDGenerator produces unique identifiers.
type IDGenerator interface{ NewID() string }

// ErrVersionConflict is matched by every *ConflictError.
var ErrVersionConflict = errors.New("eventstore: version conflict")

// ErrInvalidAppend is returned for an empty stream name or no records.
var ErrInvalidAppend = errors.New("eventstore: invalid append")

// ConflictError carries the expected and actual versions.
type ConflictError struct {
	Stream           string
	Expected, Actual uint64
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("eventstore: version conflict on %q: expected %d, actual %d", e.Stream, e.Expected, e.Actual)
}

// Is makes errors.Is(err, ErrVersionConflict) true.
func (e *ConflictError) Is(target error) bool { return target == ErrVersionConflict } //nolint:errorlint // sentinel identity

// CloneRecords deep-copies records so callers and stores never share buffers.
func CloneRecords(in []Record) []Record {
	out := make([]Record, len(in))
	for i, r := range in {
		r.Data = append([]byte(nil), r.Data...)
		out[i] = r
	}
	return out
}
