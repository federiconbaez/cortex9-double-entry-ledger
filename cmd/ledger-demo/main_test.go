package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestDemoIsDeterministicAndReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.jsonl")
	var first, second bytes.Buffer
	if err := run(context.Background(), path, &first); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), path, &second); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"insufficient funds: true", "trial balance: map[ARS:0]", "wallet   liability  ARS"} {
		if !strings.Contains(first.String(), want) {
			t.Fatalf("missing %q in:\n%s", want, first.String())
		}
	}
	tail := func(s string) string { return s[strings.Index(s, "\naccount"):] }
	if tail(first.String()) != tail(second.String()) {
		t.Fatalf("rebuilt state differs:\n%s\n---\n%s", first.String(), second.String())
	}
	if !strings.HasPrefix(second.String(), "log "+path+": 6 records") {
		t.Fatal(second.String())
	}
	if err := run(context.Background(), t.TempDir(), &first); err == nil {
		t.Fatal("directory as log must fail")
	}
}
