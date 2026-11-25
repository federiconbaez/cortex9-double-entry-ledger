package domain

import (
	"testing"
	"time"
)

func timeZone(_ *testing.T) *time.Location { return time.FixedZone("ART", -3*3600) }
