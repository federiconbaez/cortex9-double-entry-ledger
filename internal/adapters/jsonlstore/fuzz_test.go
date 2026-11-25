package jsonlstore

import (
	"bytes"
	"testing"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports"
)

// FuzzScan: the decoder never panics, the valid prefix is a prefix ending
// in '\n', a clean scan is idempotent on its prefix, and re-encoding the
// decoded records scans back to the same records.
func FuzzScan(f *testing.F) {
	var log []byte
	for i := uint64(1); i <= 3; i++ {
		l, _ := encodeLine(ports.Record{Stream: "s", Version: i, Type: "E", Data: []byte("x"), RecordedAt: at})
		log = append(log, l...)
	}
	f.Add(log)
	f.Add(log[:len(log)-4])
	f.Add([]byte("\n"))
	f.Add([]byte(`{"crc":0,"rec":{}}` + "\n"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		recs, valid, err := Scan(data)
		if valid < 0 || valid > len(data) {
			t.Fatalf("valid %d out of range", valid)
		}
		if valid > 0 && data[valid-1] != '\n' {
			t.Fatal("valid prefix must end in newline")
		}
		if err != nil {
			return
		}
		again, v2, err := Scan(data[:valid])
		if err != nil || v2 != valid || len(again) != len(recs) {
			t.Fatalf("prefix rescan: %v %d %d", err, v2, len(again))
		}
		var re []byte
		for _, r := range recs {
			l, err := encodeLine(r)
			if err != nil {
				t.Fatal(err)
			}
			re = append(re, l...)
		}
		back, vb, err := Scan(re)
		if err != nil || vb != len(re) || len(back) != len(recs) {
			t.Fatalf("re-encode: %v", err)
		}
		for i := range back {
			if back[i].Stream != recs[i].Stream || back[i].Version != recs[i].Version ||
				back[i].Type != recs[i].Type || !bytes.Equal(back[i].Data, recs[i].Data) ||
				!back[i].RecordedAt.Equal(recs[i].RecordedAt) {
				t.Fatalf("record %d changed", i)
			}
		}
	})
}
