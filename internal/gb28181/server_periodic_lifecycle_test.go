package gb28181

import (
	"context"
	"testing"
	"time"
)

// Periodic loops must terminate when the Server is stopped, even if their
// 60-second tick has not fired. Repeated SIP restart must not leak scanners.
func TestGB28181PeriodicScansExitOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{ctx: ctx, cancel: cancel}
	s.backgroundWG.Add(2)
	go func() { defer s.backgroundWG.Done(); s.startTickerCheck() }()
	go func() { defer s.backgroundWG.Done(); s.startChannelMissingScan() }()
	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2*time.Second):
		t.Fatal("periodic scanner still running after Stop")
	}
	s.Stop() // shutdown must be idempotent
}
