package jsonlstore

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const helperEnv = "JSONLSTORE_LOCK_HELPER_PATH"

// TestLockHelperProcess is not a real test: the cross-process test re-runs
// the test binary with helperEnv set, and this function then tries to open
// the store from that second process and reports what happened.
func TestLockHelperProcess(t *testing.T) {
	path := os.Getenv(helperEnv)
	if path == "" {
		t.Skip("helper process only")
	}
	s, _, err := Open(path)
	switch {
	case errors.Is(err, ErrLocked):
		_, _ = os.Stdout.WriteString("RESULT=locked\n")
	case err != nil:
		_, _ = os.Stdout.WriteString("RESULT=error " + err.Error() + "\n")
	default:
		_ = s.Close()
		_, _ = os.Stdout.WriteString("RESULT=opened\n")
	}
}

func openFromAnotherProcess(t *testing.T, path string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockHelperProcess$", "-test.count=1") //nolint:gosec // re-exec of the test binary itself
	cmd.Env = append(os.Environ(), helperEnv+"="+path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "RESULT=") {
			return strings.TrimPrefix(line, "RESULT=")
		}
	}
	t.Fatalf("helper printed no result:\n%s", out)
	return ""
}

func TestSecondStoreInSameProcessIsLocked(t *testing.T) {
	if !lockSupported {
		t.Skip("no file lock on this platform")
	}
	path := filepath.Join(t.TempDir(), "events.jsonl")
	seed(t, path, 2)
	s, _ := open(t, path)
	if _, _, err := Open(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Open: got %v, want ErrLocked", err)
	}
	// The failed Open must not have disturbed the holder.
	if _, err := s.Append(ctx, "ledger-1", 2, rec("E")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, rep := open(t, path)
	if rep.Records != 3 {
		t.Fatalf("after reopen: %d records, want 3", rep.Records)
	}
	_ = s2
}

func TestStoreIsLockedAcrossProcesses(t *testing.T) {
	if !lockSupported {
		t.Skip("no file lock on this platform")
	}
	path := filepath.Join(t.TempDir(), "events.jsonl")
	seed(t, path, 1)
	s, _ := open(t, path)
	if got := openFromAnotherProcess(t, path); got != "locked" {
		t.Fatalf("other process while held: %q, want locked", got)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got := openFromAnotherProcess(t, path); got != "opened" {
		t.Fatalf("other process after Close: %q, want opened", got)
	}
}

func TestLockedOpenDoesNotTruncateATornTail(t *testing.T) {
	if !lockSupported {
		t.Skip("no file lock on this platform")
	}
	path := filepath.Join(t.TempDir(), "events.jsonl")
	seed(t, path, 1)
	s, _ := open(t, path)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // test path
	if err != nil {
		t.Fatal(err)
	}
	// Simulates bytes the holder is in the middle of writing.
	if _, err := f.WriteString(`{"crc":1,"rec":`); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	before, _ := os.Stat(path)
	if _, _, err := Open(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("got %v, want ErrLocked", err)
	}
	after, _ := os.Stat(path)
	if before.Size() != after.Size() {
		t.Fatalf("a locked Open truncated the file: %d -> %d", before.Size(), after.Size())
	}
	_ = s
}
