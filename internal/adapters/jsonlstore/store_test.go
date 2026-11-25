package jsonlstore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports"
	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports/eventstoretest"
)

var (
	ctx = context.Background()
	at  = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
)

func open(t *testing.T, path string) (*Store, RecoveryReport) {
	t.Helper()
	s, rep, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, rep
}

func TestContract(t *testing.T) {
	eventstoretest.Run(t, func(t *testing.T) (ports.EventStore, func() ports.EventStore) {
		path := filepath.Join(t.TempDir(), "events.jsonl")
		s, _ := open(t, path)
		return s, func() ports.EventStore {
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s2, _ := open(t, path)
			return s2
		}
	})
}

func rec(ty string) []ports.Record {
	return []ports.Record{{Type: ty, Data: []byte(`{"k":"` + ty + `"}`), RecordedAt: at}}
}

func seed(t *testing.T, path string, n int) {
	t.Helper()
	s, _ := open(t, path)
	for i := 0; i < n; i++ {
		if _, err := s.Append(ctx, "ledger-1", uint64(i), rec("E")); err != nil { //nolint:gosec // small test index
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func appendRaw(t *testing.T, path string, b []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // test temp file
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTornTailIsTruncatedAndReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l.jsonl")
	seed(t, path, 3)
	clean, _ := os.ReadFile(path) //nolint:gosec // test temp file
	// A crash in the middle of write(2): a partial line without '\n'.
	full, err := encodeLine(ports.Record{Stream: "ledger-1", Version: 4, Type: "E", RecordedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	torn := full[:len(full)/2]
	appendRaw(t, path, torn)

	s, rep := open(t, path)
	if rep.Records != 3 || rep.TruncatedBytes != int64(len(torn)) || rep.ValidBytes != int64(len(clean)) {
		t.Fatalf("report %+v", rep)
	}
	after, _ := os.ReadFile(path) //nolint:gosec // test temp file
	if !bytes.Equal(after, clean) {
		t.Fatal("file not truncated back to last complete line")
	}
	if v, err := s.Append(ctx, "ledger-1", 3, rec("E")); err != nil || v != 4 {
		t.Fatalf("append after recovery: %d %v", v, err)
	}
}

func TestCompleteLineWithoutNewlineIsDiscarded(t *testing.T) {
	// Regression guard for the documented rule: even a record that parses
	// is discarded if its '\n' is missing, because Append never acked it.
	path := filepath.Join(t.TempDir(), "l.jsonl")
	seed(t, path, 1)
	full, _ := encodeLine(ports.Record{Stream: "ledger-1", Version: 2, Type: "E", RecordedAt: at})
	appendRaw(t, path, full[:len(full)-1])
	s, rep := open(t, path)
	got, _ := s.Load(ctx, "ledger-1")
	if len(got) != 1 || rep.TruncatedBytes != int64(len(full)-1) {
		t.Fatalf("got %d records, report %+v", len(got), rep)
	}
}

func TestCorruptCompleteLinesRefuseToOpen(t *testing.T) {
	good, _ := encodeLine(ports.Record{Stream: "s", Version: 1, Type: "E", RecordedAt: at})
	v3, _ := encodeLine(ports.Record{Stream: "s", Version: 3, Type: "E", RecordedAt: at})
	flipped := bytes.Replace(good, []byte(`"type":"E"`), []byte(`"type":"F"`), 1)
	cases := map[string]struct {
		content []byte
		line    int
	}{
		"garbage line":      {append(append([]byte{}, good...), "not json\n"...), 2},
		"empty line":        {append(append([]byte{}, good...), '\n'), 2},
		"crc mismatch":      {flipped, 1},
		"version gap":       {append(append([]byte{}, good...), v3...), 2},
		"unknown field":     {[]byte(`{"crc":0,"rec":{},"x":1}` + "\n"), 1},
		"missing rec":       {[]byte(`{"crc":0}` + "\n"), 1},
		"trailing data":     {[]byte(`{"crc":0,"rec":{}} {}` + "\n"), 1},
		"missing fields":    {lineWith(t, `{"stream":"","version":1,"type":"E","at":"2026-11-25T12:00:00Z","data":null}`), 1},
		"bad rec structure": {lineWith(t, `{"stream":"s","version":"one"}`), 1},
		"corrupt then torn": {append(append([]byte("x\n"), good...), good[:5]...), 1},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "l.jsonl")
			if err := os.WriteFile(path, c.content, 0o600); err != nil {
				t.Fatal(err)
			}
			_, _, err := Open(path)
			var ce *CorruptError
			if !errors.Is(err, ErrCorrupt) || !errors.As(err, &ce) || ce.Line != c.line || ce.Error() == "" {
				t.Fatalf("err = %v, want corrupt at line %d", err, c.line)
			}
			after, _ := os.ReadFile(path) //nolint:gosec // test temp file
			if !bytes.Equal(after, c.content) {
				t.Fatal("a corrupt log must not be modified")
			}
		})
	}
}

// lineWith wraps a raw rec with a correct CRC.
func lineWith(t *testing.T, rec string) []byte {
	t.Helper()
	b, err := encodeEnvelope([]byte(rec))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type faultyFile struct {
	file
	failWrite, failSync, failTruncate bool
	partial                           int
}

func (f *faultyFile) Write(b []byte) (int, error) {
	if f.failWrite {
		n, _ := f.file.Write(b[:f.partial])
		return n, errors.New("injected: disk full")
	}
	return f.file.Write(b)
}

func (f *faultyFile) Sync() error {
	if f.failSync {
		return errors.New("injected: EIO")
	}
	return f.file.Sync()
}

func (f *faultyFile) Truncate(n int64) error {
	if f.failTruncate {
		return errors.New("injected: truncate")
	}
	return f.file.Truncate(n)
}

func TestFailedWriteIsRolledBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l.jsonl")
	s, _ := open(t, path)
	if _, err := s.Append(ctx, "s", 0, rec("A")); err != nil {
		t.Fatal(err)
	}
	ff := &faultyFile{file: s.f, failWrite: true, partial: 7}
	s.f = ff
	if _, err := s.Append(ctx, "s", 1, rec("B")); err == nil {
		t.Fatal("want write error")
	}
	ff.failWrite = false
	if v, err := s.Append(ctx, "s", 1, rec("B")); err != nil || v != 2 {
		t.Fatalf("store must stay usable after rolled-back write: %d %v", v, err)
	}
	_ = s.Close()
	s2, rep := open(t, path)
	got, _ := s2.Load(ctx, "s")
	if len(got) != 2 || rep.TruncatedBytes != 0 {
		t.Fatalf("records %d report %+v", len(got), rep)
	}
}

func TestFailedRollbackBreaksStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l.jsonl")
	s, _ := open(t, path)
	s.f = &faultyFile{file: s.f, failWrite: true, failTruncate: true, partial: 3}
	if _, err := s.Append(ctx, "s", 0, rec("A")); err == nil {
		t.Fatal("want error")
	}
	if _, err := s.Append(ctx, "s", 0, rec("A")); !errors.Is(err, ErrStoreFailed) {
		t.Fatalf("got %v", err)
	}
	_ = s.Close()
	// Reopening recovers: the 3 stray bytes are a torn tail.
	_, rep := open(t, path)
	if rep.TruncatedBytes != 3 || rep.Records != 0 {
		t.Fatalf("report %+v", rep)
	}
}

func TestFailedFsyncBreaksStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l.jsonl")
	s, _ := open(t, path)
	s.f = &faultyFile{file: s.f, failSync: true}
	_, err := s.Append(ctx, "s", 0, rec("A"))
	if !errors.Is(err, ErrStoreFailed) {
		t.Fatalf("got %v", err)
	}
	// Never retry a failed fsync: every later append is refused.
	if _, err := s.Append(ctx, "s", 0, rec("A")); !errors.Is(err, ErrStoreFailed) {
		t.Fatalf("got %v", err)
	}
	if got, _ := s.Load(ctx, "s"); len(got) != 0 {
		t.Fatal("unacknowledged record visible in memory")
	}
}

func TestCloseAndOpenErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l.jsonl")
	s, _ := open(t, path)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal("double close must be a no-op")
	}
	if _, err := s.Append(ctx, "s", 0, rec("A")); !errors.Is(err, ErrClosed) {
		t.Fatalf("append after close: %v", err)
	}
	if _, _, err := Open(t.TempDir()); err == nil {
		t.Fatal("opening a directory must fail")
	}
	if _, _, err := Open(filepath.Join(t.TempDir(), "missing", "x.jsonl")); err == nil {
		t.Fatal("missing parent dir must fail")
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	in := ports.Record{Stream: "ledger-1", Version: 7, Type: "EntryPosted", Data: []byte{0, 1, 2, 255}, RecordedAt: at.Add(123 * time.Nanosecond)}
	b, err := encodeLine(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := decodeLine(b[:len(b)-1])
	if err != nil {
		t.Fatal(err)
	}
	if out.Stream != in.Stream || out.Version != in.Version || out.Type != in.Type || !bytes.Equal(out.Data, in.Data) || !out.RecordedAt.Equal(in.RecordedAt) {
		t.Fatalf("%+v != %+v", out, in)
	}
}
