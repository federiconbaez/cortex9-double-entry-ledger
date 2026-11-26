package app

import (
	"bytes"
	"testing"
	"time"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/domain"
	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports"
)

// FuzzDecodeEvent: DecodeEvent never panics on arbitrary bytes, and every
// record it accepts is a fixed point of the codec: re-encoding the decoded
// event and decoding it again yields byte-identical JSON. A record can only
// enter the domain through its constructors, so an accepted record is also
// a valid domain event.
func FuzzDecodeEvent(f *testing.F) {
	types := []string{domain.TypeAccountOpened, domain.TypeEntryPosted, "Unknown"}
	f.Add(uint8(0), []byte(`{"id":"cash","name":"Caja","type":"asset","currency":"ARS","policy":"no-overdraft","opened_at":"2026-01-01T00:00:00Z"}`))
	f.Add(uint8(0), []byte(`{"id":"l","name":"L","type":"liability","currency":"USD","policy":"overdraft-limit","limit":500,"opened_at":"2026-01-01T00:00:00.123456789-03:00"}`))
	f.Add(uint8(1), []byte(`{"id":"e1","posted_at":"2026-01-01T00:00:00Z","memo":"m","lines":[{"account":"a","side":"debit","minor":5,"currency":"ARS"},{"account":"b","side":"credit","minor":5,"currency":"ARS"}]}`))
	f.Add(uint8(1), []byte(`{"id":"r1","posted_at":"2026-01-02T00:00:00Z","reversal_of":"e1","lines":[{"account":"b","side":"debit","minor":5,"currency":"ARS"},{"account":"a","side":"credit","minor":5,"currency":"ARS"}]}`))
	f.Add(uint8(1), []byte(`{"lines":[{"minor":9223372036854775807}]}`))
	f.Add(uint8(2), []byte(`{}`))
	f.Fuzz(func(t *testing.T, sel uint8, data []byte) {
		r := ports.Record{Stream: "ledger-f", Version: 1, Type: types[int(sel)%len(types)], Data: data}
		ev, err := DecodeEvent(r)
		if err != nil {
			return
		}
		at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		first, err := EncodeEvent(ev, at)
		if err != nil {
			t.Fatalf("accepted event does not re-encode: %v", err)
		}
		if first.Type != r.Type {
			t.Fatalf("type changed: %q -> %q", r.Type, first.Type)
		}
		ev2, err := DecodeEvent(first)
		if err != nil {
			t.Fatalf("re-encoded record rejected: %v\n%s", err, first.Data)
		}
		second, err := EncodeEvent(ev2, at)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first.Data, second.Data) {
			t.Fatalf("codec is not a fixed point:\n%s\n%s", first.Data, second.Data)
		}
	})
}
