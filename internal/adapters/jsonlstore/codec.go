package jsonlstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"time"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports"
)

// ErrCorrupt reports a complete line that cannot be trusted. It is never
// auto-repaired: committed history is not rewritten.
var ErrCorrupt = errors.New("jsonlstore: corrupt log")

// CorruptError locates the bad line (1-based) and its byte offset.
type CorruptError struct {
	Line   int
	Offset int
	Reason string
}

func (e *CorruptError) Error() string {
	return fmt.Sprintf("jsonlstore: corrupt log at line %d (offset %d): %s", e.Line, e.Offset, e.Reason)
}

// Is makes errors.Is(err, ErrCorrupt) true.
func (e *CorruptError) Is(target error) bool { return target == ErrCorrupt } //nolint:errorlint // sentinel identity

type wireRec struct {
	Stream  string    `json:"stream"`
	Version uint64    `json:"version"`
	Type    string    `json:"type"`
	At      time.Time `json:"at"`
	Data    []byte    `json:"data"`
}

type wireLine struct {
	CRC uint32          `json:"crc"`
	Rec json.RawMessage `json:"rec"`
}

// encodeLine renders one record as `{"crc":N,"rec":{...}}\n`.
func encodeLine(r ports.Record) ([]byte, error) {
	rec, err := json.Marshal(wireRec{Stream: r.Stream, Version: r.Version, Type: r.Type, At: r.RecordedAt.UTC(), Data: r.Data})
	if err != nil {
		return nil, err
	}
	return encodeEnvelope(rec)
}

func encodeEnvelope(rec []byte) ([]byte, error) {
	line, err := json.Marshal(wireLine{CRC: crc32.ChecksumIEEE(rec), Rec: rec})
	if err != nil {
		return nil, err
	}
	return append(line, '\n'), nil
}

func strictUnmarshal(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if d.More() {
		return errors.New("trailing data")
	}
	return nil
}

// decodeLine parses one line (without its '\n').
func decodeLine(b []byte) (ports.Record, error) {
	var wl wireLine
	if err := strictUnmarshal(b, &wl); err != nil {
		return ports.Record{}, fmt.Errorf("envelope: %w", err)
	}
	if len(wl.Rec) == 0 {
		return ports.Record{}, errors.New("missing rec")
	}
	if got := crc32.ChecksumIEEE(wl.Rec); got != wl.CRC {
		return ports.Record{}, fmt.Errorf("crc mismatch: stored %d, computed %d", wl.CRC, got)
	}
	var wr wireRec
	if err := strictUnmarshal(wl.Rec, &wr); err != nil {
		return ports.Record{}, fmt.Errorf("rec: %w", err)
	}
	if wr.Stream == "" || wr.Type == "" || wr.Version == 0 {
		return ports.Record{}, errors.New("rec: stream, type and version are required")
	}
	return ports.Record{Stream: wr.Stream, Version: wr.Version, Type: wr.Type, Data: wr.Data, RecordedAt: wr.At}, nil
}

// Scan decodes a whole log image. It returns the records, the length of
// the valid prefix (always ending in '\n'), and an error for a corrupt
// complete line. Bytes after the last '\n' are a torn tail: they are not
// an error and are excluded from valid (the caller truncates them).
func Scan(data []byte) (recs []ports.Record, valid int, err error) {
	versions := map[string]uint64{}
	off, lineNo := 0, 0
	for off < len(data) {
		nl := bytes.IndexByte(data[off:], '\n')
		if nl < 0 {
			break
		}
		lineNo++
		r, derr := decodeLine(data[off : off+nl])
		if derr != nil {
			return nil, off, &CorruptError{Line: lineNo, Offset: off, Reason: derr.Error()}
		}
		if want := versions[r.Stream] + 1; r.Version != want {
			return nil, off, &CorruptError{Line: lineNo, Offset: off,
				Reason: fmt.Sprintf("stream %q version %d, want %d", r.Stream, r.Version, want)}
		}
		versions[r.Stream] = r.Version
		recs = append(recs, r)
		off += nl + 1
	}
	return recs, off, nil
}
