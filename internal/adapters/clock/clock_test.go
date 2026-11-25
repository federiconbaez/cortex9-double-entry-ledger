package clock

import (
	"testing"
	"time"
)

func TestClocks(t *testing.T) {
	if (System{}).Now().Location() != time.UTC {
		t.Fatal("system clock must be UTC")
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := NewStepping(start, time.Second)
	if !s.Now().Equal(start) || !s.Now().Equal(start.Add(time.Second)) {
		t.Fatal("stepping")
	}
}
