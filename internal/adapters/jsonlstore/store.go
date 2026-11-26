// Package jsonlstore is a durable, append-only ports.EventStore backed by
// a single JSON-lines file. See docs/adr/0002 for the recovery rules.
package jsonlstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports"
)

// ErrStoreFailed means a previous write or fsync failed in a way that
// leaves the on-disk state uncertain; the store refuses further appends
// and must be reopened (which re-runs recovery).
var ErrStoreFailed = errors.New("jsonlstore: store failed, reopen required")

// ErrLocked means another open Store (in this or another process) holds
// the exclusive lock on the file. The log has a single writer by design.
var ErrLocked = errors.New("jsonlstore: file is locked by another store")

// ErrClosed is returned after Close.
var ErrClosed = errors.New("jsonlstore: closed")

// file is the subset of *os.File used after recovery; tests inject faults.
type file interface {
	io.Writer
	Sync() error
	Truncate(size int64) error
	Close() error
}

// RecoveryReport describes what Open found.
type RecoveryReport struct {
	Records        int   // committed records loaded
	ValidBytes     int64 // size of the file after recovery
	TruncatedBytes int64 // torn tail removed (0 when the file was clean)
}

// Store is safe for concurrent use within one process. Open takes an
// exclusive, non-blocking lock on the file (flock on Unix-like systems), so
// a second Store on the same path, in any process, fails with ErrLocked
// instead of silently interleaving writes with a stale in-memory index.
type Store struct {
	mu      sync.Mutex
	f       file
	size    int64
	streams map[string][]ports.Record
	all     []ports.Record
	broken  error
}

var _ ports.EventStore = (*Store)(nil)

// Open opens or creates the log at path, recovering a torn tail.
func Open(path string) (*Store, RecoveryReport, error) {
	_, statErr := os.Stat(path) //nolint:gosec // path is chosen by the operator
	created := errors.Is(statErr, os.ErrNotExist)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600) //nolint:gosec // path is chosen by the operator
	if err != nil {
		return nil, RecoveryReport{}, err
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return nil, RecoveryReport{}, err
	}
	s, rep, err := recoverFrom(f)
	if err != nil {
		_ = f.Close()
		return nil, rep, err
	}
	if created {
		if err := syncDir(filepath.Dir(path)); err != nil {
			_ = f.Close()
			return nil, rep, err
		}
	}
	return s, rep, nil
}

func recoverFrom(f *os.File) (*Store, RecoveryReport, error) {
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, RecoveryReport{}, err
	}
	recs, valid, err := Scan(data)
	rep := RecoveryReport{Records: len(recs), ValidBytes: int64(valid)}
	if err != nil {
		return nil, rep, err
	}
	if valid < len(data) {
		if err := f.Truncate(int64(valid)); err != nil {
			return nil, rep, fmt.Errorf("jsonlstore: truncating torn tail: %w", err)
		}
		if err := f.Sync(); err != nil {
			return nil, rep, fmt.Errorf("jsonlstore: fsync after truncate: %w", err)
		}
		rep.TruncatedBytes = int64(len(data) - valid)
	}
	s := &Store{f: f, size: int64(valid), streams: map[string][]ports.Record{}}
	for _, r := range recs {
		s.streams[r.Stream] = append(s.streams[r.Stream], r)
	}
	s.all = recs
	return s, rep, nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir) //nolint:gosec // directory of the operator-chosen path
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}

// Close releases the file and its lock. Further calls return ErrClosed.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if errors.Is(s.broken, ErrClosed) {
		return nil
	}
	s.broken = ErrClosed
	return s.f.Close()
}

// Append implements ports.EventStore: one write, then fsync, then ack.
func (s *Store) Append(ctx context.Context, stream string, expected uint64, recs []ports.Record) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if stream == "" || len(recs) == 0 {
		return 0, ports.ErrInvalidAppend
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken != nil {
		return 0, s.broken
	}
	cur := uint64(len(s.streams[stream]))
	if cur != expected {
		return 0, &ports.ConflictError{Stream: stream, Expected: expected, Actual: cur}
	}
	in := ports.CloneRecords(recs)
	var buf []byte
	for i := range in {
		in[i].Stream = stream
		in[i].Version = expected + uint64(i) + 1 //nolint:gosec // i >= 0
		in[i].RecordedAt = in[i].RecordedAt.UTC()
		line, err := encodeLine(in[i])
		if err != nil {
			return 0, err
		}
		buf = append(buf, line...)
	}
	n, err := s.f.Write(buf)
	if err != nil {
		if terr := s.f.Truncate(s.size); terr != nil {
			s.broken = fmt.Errorf("%w: write: %v; rollback truncate: %v", ErrStoreFailed, err, terr) //nolint:errorlint // only the sentinel is wrapped
		}
		return 0, fmt.Errorf("jsonlstore: write: %w", err)
	}
	if err := s.f.Sync(); err != nil {
		s.broken = fmt.Errorf("%w: fsync: %v", ErrStoreFailed, err) //nolint:errorlint // only the sentinel is wrapped
		return 0, s.broken
	}
	s.size += int64(n)
	s.streams[stream] = append(s.streams[stream], in...)
	s.all = append(s.all, in...)
	return expected + uint64(len(in)), nil
}

// Load implements ports.EventStore.
func (s *Store) Load(ctx context.Context, stream string) ([]ports.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return ports.CloneRecords(s.streams[stream]), nil
}

// ReadAll implements ports.EventStore.
func (s *Store) ReadAll(ctx context.Context) ([]ports.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return ports.CloneRecords(s.all), nil
}
