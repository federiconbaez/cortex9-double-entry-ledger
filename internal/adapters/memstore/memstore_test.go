package memstore

import (
	"testing"

	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports"
	"github.com/federiconbaez/cortex9-double-entry-ledger/internal/ports/eventstoretest"
)

func TestContract(t *testing.T) {
	eventstoretest.Run(t, func(*testing.T) (ports.EventStore, func() ports.EventStore) { return New(), nil })
}
