// Package eventstoretest is the contract test suite every ports.EventStore
// adapter must pass. Adapters call Run from their own _test.go files.
package eventstoretest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports"
)

// Factory returns a fresh, empty store. reopen, if non-nil, closes and
// reopens the same underlying storage (for durable adapters).
type Factory func(t *testing.T) (store ports.EventStore, reopen func() ports.EventStore)

var at = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

func recs(types ...string) []ports.Record {
	out := make([]ports.Record, len(types))
	for i, ty := range types {
		out[i] = ports.Record{Type: ty, Data: []byte(fmt.Sprintf(`{"n":%d}`, i)), RecordedAt: at}
	}
	return out
}

// Run executes the contract.
func Run(t *testing.T, newStore Factory) {
	ctx := context.Background()

	t.Run("empty stream loads nothing", func(t *testing.T) {
		s, _ := newStore(t)
		got, err := s.Load(ctx, "s")
		if err != nil || len(got) != 0 {
			t.Fatalf("%v %v", got, err)
		}
		all, err := s.ReadAll(ctx)
		if err != nil || len(all) != 0 {
			t.Fatalf("%v %v", all, err)
		}
	})

	t.Run("append assigns consecutive versions", func(t *testing.T) {
		s, _ := newStore(t)
		v, err := s.Append(ctx, "s", 0, recs("A", "B"))
		if err != nil || v != 2 {
			t.Fatalf("v=%d err=%v", v, err)
		}
		v, err = s.Append(ctx, "s", 2, recs("C"))
		if err != nil || v != 3 {
			t.Fatalf("v=%d err=%v", v, err)
		}
		got, _ := s.Load(ctx, "s")
		for i, r := range got {
			if r.Version != uint64(i+1) || r.Stream != "s" { //nolint:gosec // small test index
				t.Fatalf("record %d: %+v", i, r)
			}
		}
		if got[2].Type != "C" || string(got[0].Data) != `{"n":0}` || !got[0].RecordedAt.Equal(at) {
			t.Fatalf("content: %+v", got)
		}
	})

	t.Run("wrong expected version conflicts and writes nothing", func(t *testing.T) {
		s, _ := newStore(t)
		if _, err := s.Append(ctx, "s", 0, recs("A")); err != nil {
			t.Fatal(err)
		}
		for _, exp := range []uint64{0, 2, 99} {
			_, err := s.Append(ctx, "s", exp, recs("X"))
			if !errors.Is(err, ports.ErrVersionConflict) {
				t.Fatalf("expected %d: err=%v", exp, err)
			}
			var ce *ports.ConflictError
			if !errors.As(err, &ce) || ce.Actual != 1 || ce.Expected != exp || ce.Error() == "" {
				t.Fatalf("conflict detail: %v", err)
			}
		}
		got, _ := s.Load(ctx, "s")
		if len(got) != 1 {
			t.Fatalf("conflicting append wrote: %d", len(got))
		}
	})

	t.Run("invalid appends are rejected", func(t *testing.T) {
		s, _ := newStore(t)
		if _, err := s.Append(ctx, "", 0, recs("A")); !errors.Is(err, ports.ErrInvalidAppend) {
			t.Fatalf("empty stream: %v", err)
		}
		if _, err := s.Append(ctx, "s", 0, nil); !errors.Is(err, ports.ErrInvalidAppend) {
			t.Fatalf("no records: %v", err)
		}
	})

	t.Run("streams are independent and ReadAll keeps global order", func(t *testing.T) {
		s, _ := newStore(t)
		_, _ = s.Append(ctx, "a", 0, recs("A1"))
		_, _ = s.Append(ctx, "b", 0, recs("B1", "B2"))
		_, _ = s.Append(ctx, "a", 1, recs("A2"))
		all, err := s.ReadAll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var order []string
		for _, r := range all {
			order = append(order, r.Type)
		}
		if fmt.Sprint(order) != "[A1 B1 B2 A2]" {
			t.Fatalf("order %v", order)
		}
	})

	t.Run("returned records are copies", func(t *testing.T) {
		s, _ := newStore(t)
		in := recs("A")
		_, _ = s.Append(ctx, "s", 0, in)
		in[0].Data[0] = 'X'
		got, _ := s.Load(ctx, "s")
		got[0].Data[0] = 'Y'
		again, _ := s.Load(ctx, "s")
		if string(again[0].Data) != `{"n":0}` {
			t.Fatalf("aliasing: %q", again[0].Data)
		}
	})

	t.Run("cancelled context is honoured", func(t *testing.T) {
		s, _ := newStore(t)
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := s.Append(cctx, "s", 0, recs("A")); !errors.Is(err, context.Canceled) {
			t.Fatalf("append: %v", err)
		}
		if _, err := s.Load(cctx, "s"); !errors.Is(err, context.Canceled) {
			t.Fatalf("load: %v", err)
		}
		if _, err := s.ReadAll(cctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("readall: %v", err)
		}
	})

	t.Run("concurrent appends with same expected version: exactly one wins", func(t *testing.T) {
		s, _ := newStore(t)
		const n = 16
		var wg sync.WaitGroup
		var mu sync.Mutex
		wins, conflicts := 0, 0
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := s.Append(ctx, "s", 0, recs("A"))
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					wins++
				case errors.Is(err, ports.ErrVersionConflict):
					conflicts++
				default:
					t.Errorf("unexpected: %v", err)
				}
			}()
		}
		wg.Wait()
		if wins != 1 || conflicts != n-1 {
			t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
		}
	})

	t.Run("data survives reopen", func(t *testing.T) {
		s, reopen := newStore(t)
		if reopen == nil {
			t.Skip("adapter is not durable")
		}
		_, _ = s.Append(ctx, "a", 0, recs("A1", "A2"))
		_, _ = s.Append(ctx, "b", 0, recs("B1"))
		s2 := reopen()
		got, err := s2.Load(ctx, "a")
		if err != nil || len(got) != 2 || got[1].Type != "A2" || !got[1].RecordedAt.Equal(at) {
			t.Fatalf("%+v %v", got, err)
		}
		if v, err := s2.Append(ctx, "a", 2, recs("A3")); err != nil || v != 3 {
			t.Fatalf("append after reopen: %d %v", v, err)
		}
		if _, err := s2.Append(ctx, "b", 0, recs("X")); !errors.Is(err, ports.ErrVersionConflict) {
			t.Fatalf("versions not restored: %v", err)
		}
	})
}
