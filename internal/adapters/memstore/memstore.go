// Package memstore is an in-memory ports.EventStore, safe for concurrent use.
package memstore

import (
	"context"
	"sync"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports"
)

// Store keeps every record in memory.
type Store struct {
	mu      sync.RWMutex
	streams map[string][]ports.Record
	all     []ports.Record
}

// New returns an empty store.
func New() *Store { return &Store{streams: map[string][]ports.Record{}} }

var _ ports.EventStore = (*Store)(nil)

// Append implements ports.EventStore.
func (s *Store) Append(ctx context.Context, stream string, expected uint64, recs []ports.Record) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if stream == "" || len(recs) == 0 {
		return 0, ports.ErrInvalidAppend
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := uint64(len(s.streams[stream]))
	if cur != expected {
		return 0, &ports.ConflictError{Stream: stream, Expected: expected, Actual: cur}
	}
	in := ports.CloneRecords(recs)
	for i := range in {
		in[i].Stream = stream
		in[i].Version = expected + uint64(i) + 1 //nolint:gosec // i >= 0
	}
	s.streams[stream] = append(s.streams[stream], in...)
	s.all = append(s.all, in...)
	return expected + uint64(len(in)), nil
}

// Load implements ports.EventStore.
func (s *Store) Load(ctx context.Context, stream string) ([]ports.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return ports.CloneRecords(s.streams[stream]), nil
}

// ReadAll implements ports.EventStore.
func (s *Store) ReadAll(ctx context.Context) ([]ports.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return ports.CloneRecords(s.all), nil
}
