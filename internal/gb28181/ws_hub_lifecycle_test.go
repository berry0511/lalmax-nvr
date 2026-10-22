package gb28181

import (
	"testing"
	"time"
)

func TestGB28181WebSocketHubStopsWithoutLeakingRunLoop(t *testing.T) {
	h := NewWSHub()
	done := make(chan struct{})
	go func() { h.Run(); close(done) }()
	h.Broadcast(Event{Type: EventDeviceOnline})
	h.Stop()
	select {
	case <-done:
	case <-time.After(2*time.Second):
		t.Fatal("WebSocket Hub.Run continued after Stop")
	}
	h.Stop() // idempotent
	// A broadcast arriving after Stop must never block a signaling goroutine.
	sent := make(chan struct{})
	go func(){ h.Broadcast(Event{Type: EventDeviceOffline}); close(sent) }()
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("Broadcast blocked after the hub stopped")
	}
}
