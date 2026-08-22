package ai

import (
	"os"
	"testing"
	"time"
)

// TestMain compresses retry backoff so exhaustion tests stay fast.
func TestMain(m *testing.M) {
	proposalBackoffBase = time.Millisecond
	os.Exit(m.Run())
}
