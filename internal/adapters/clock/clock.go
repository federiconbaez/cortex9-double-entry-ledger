// Package clock provides ports.Clock adapters.
package clock

import (
	"sync"
	"time"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports"
)

// System is the wall clock in UTC.
type System struct{}

// Now implements ports.Clock.
func (System) Now() time.Time { return time.Now().UTC() }

// Stepping is a deterministic clock: each call returns start + n*step.
// It makes demos and replays reproducible byte for byte.
type Stepping struct {
	mu   sync.Mutex
	next time.Time
	step time.Duration
}

// NewStepping returns a Stepping clock.
func NewStepping(start time.Time, step time.Duration) *Stepping {
	return &Stepping{next: start.UTC(), step: step}
}

// Now implements ports.Clock.
func (s *Stepping) Now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.next
	s.next = s.next.Add(s.step)
	return t
}

var (
	_ ports.Clock = System{}
	_ ports.Clock = (*Stepping)(nil)
)
