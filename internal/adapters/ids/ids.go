// Package ids provides ports.IDGenerator adapters.
package ids

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync/atomic"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports"
)

// Random returns 128-bit random hex ids from crypto/rand.
type Random struct{}

// NewID implements ports.IDGenerator.
func (Random) NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never returns an error since Go 1.24
	return hex.EncodeToString(b[:])
}

// Sequence returns prefix-1, prefix-2, ... (deterministic, for demos/tests).
type Sequence struct {
	Prefix string
	n      atomic.Uint64
}

// NewID implements ports.IDGenerator.
func (s *Sequence) NewID() string { return fmt.Sprintf("%s-%d", s.Prefix, s.n.Add(1)) }

var (
	_ ports.IDGenerator = Random{}
	_ ports.IDGenerator = (*Sequence)(nil)
)
