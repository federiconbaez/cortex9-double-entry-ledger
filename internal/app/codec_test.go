package app

import (
	"errors"
	"testing"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/adapters/memstore"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/domain"
	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports"
)

type alienEvent struct{ domain.Event }

func (alienEvent) EventType() string { return "Alien" }

func TestCodecRejectsInvalidRecords(t *testing.T) {
	bad := []ports.Record{
		{Type: domain.TypeAccountOpened, Data: []byte(`{`)},
		{Type: domain.TypeAccountOpened, Data: []byte(`{"id":"a","name":"n","type":"magic","currency":"ARS"}`)},
		{Type: domain.TypeAccountOpened, Data: []byte(`{"id":"a","name":"n","type":"asset","currency":"ars"}`)},
		{Type: domain.TypeAccountOpened, Data: []byte(`{"id":"a","name":"n","type":"asset","currency":"ARS","policy":"x"}`)},
		{Type: domain.TypeEntryPosted, Data: []byte(`[]`)},
		{Type: domain.TypeEntryPosted, Data: []byte(`{"id":"e","posted_at":"2026-01-01T00:00:00Z","lines":[{"account":"a","side":"debit","minor":5,"currency":"ARS"},{"account":"b","side":"credit","minor":4,"currency":"ARS"}]}`)},
		{Type: domain.TypeEntryPosted, Data: []byte(`{"id":"e","posted_at":"2026-01-01T00:00:00Z","lines":[{"account":"a","side":"up","minor":5,"currency":"ARS"}]}`)},
		{Type: domain.TypeEntryPosted, Data: []byte(`{"id":"e","posted_at":"2026-01-01T00:00:00Z","lines":[{"account":"a","side":"debit","minor":5,"currency":"A"}]}`)},
		{Type: domain.TypeEntryPosted, Data: []byte(`{"id":"e","posted_at":"2026-01-01T00:00:00Z","lines":[{"account":"a","side":"debit","minor":-5,"currency":"ARS"}]}`)},
		{Type: "Unknown"},
	}
	for i, r := range bad {
		if _, err := DecodeEvent(r); err == nil {
			t.Errorf("record %d decoded", i)
		}
	}
	if _, err := EncodeEvent(alienEvent{}, t0); err == nil {
		t.Fatal("alien event encoded")
	}
}

func TestRepositoryReportsCorruptHistory(t *testing.T) {
	store := memstore.New()
	if _, err := store.Append(ctx, StreamName("bank"), 0, []ports.Record{{Type: domain.TypeEntryPosted, Data: []byte(`{`)}}); err != nil {
		t.Fatal(err)
	}
	_, err := NewLedgerRepository(store).Load(ctx, "bank")
	if !errors.Is(err, domain.ErrCorruptHistory) {
		t.Fatalf("got %v", err)
	}
}

func TestPolicyRoundTrip(t *testing.T) {
	lim, _ := domain.OverdraftLimit(42)
	for _, p := range []domain.OverdraftPolicy{domain.Unrestricted(), domain.NoOverdraft(), lim} {
		n, l := policyName(p)
		back, err := ParsePolicy(n, l)
		if err != nil || back != p {
			t.Fatalf("%v -> %s/%d -> %v %v", p, n, l, back, err)
		}
	}
}
