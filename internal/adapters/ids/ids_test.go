package ids

import "testing"

func TestIDs(t *testing.T) {
	a, b := Random{}.NewID(), Random{}.NewID()
	if len(a) != 32 || a == b {
		t.Fatalf("%q %q", a, b)
	}
	s := &Sequence{Prefix: "e"}
	if s.NewID() != "e-1" || s.NewID() != "e-2" {
		t.Fatal("sequence")
	}
}
