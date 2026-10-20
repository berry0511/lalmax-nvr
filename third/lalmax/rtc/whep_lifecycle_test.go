package rtc

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/q191201771/lal/pkg/base"
)

func TestWHEPQueueBoundedAndSlowViewerClosed(t *testing.T) {
	if boundedWHEPQueueSize(0) <= 0 || boundedWHEPQueueSize(100000) > 2048 {
		t.Fatal("WHEP queue size is not bounded")
	}
	session := &whepSession{
		streamID: "a", subscriberId: "viewer",
		msgChan: make(chan base.RtmpMsg, 1),
		closeChan: make(chan bool, 2),
	}
	session.enqueue(base.RtmpMsg{})
	if len(session.msgChan) != 1 { t.Fatal("first media message missing") }
	session.enqueue(base.RtmpMsg{})
	if !session.closing.Load() { t.Fatal("slow viewer was not disconnected") }
	select {
	case <-session.closeChan:
	default:
		t.Fatal("no slow-viewer close signal")
	}
	session.OnStop()
	session.Close() // idempotent, must never block after the first close
}

func TestWHEPDeleteTerminatesOnlyAuthorizedSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := &RtcServer{}
	srv.subscriberAuthorizer = func(_, _, token string) error {
		if token != "allowed" { return fmt.Errorf("denied") }
		return nil
	}
	session := &whepSession{
		streamID: "camera-a", subscriberId: "viewer-1",
		msgChan: make(chan base.RtmpMsg, 1),
		closeChan: make(chan bool, 2),
	}
	srv.whepSessions.Store(session.subscriberId, session)
	r := gin.New()
	r.DELETE("/webrtc/whep/:sessionid", srv.HandleWHEPDelete)
	request := func(path string) int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, path, nil))
		return w.Code
	}
	if got := request("/webrtc/whep/viewer-1"); got != http.StatusUnauthorized { t.Fatalf("unauthorized DELETE status %d", got) }
	if _, ok := srv.whepSessions.Load("viewer-1"); !ok { t.Fatal("unauthorized request removed session") }
	if got := request("/webrtc/whep/viewer-1?token=allowed"); got != http.StatusNoContent { t.Fatalf("authorized DELETE status %d", got) }
	if _, ok := srv.whepSessions.Load("viewer-1"); ok { t.Fatal("DELETE kept the session registered") }
	if !session.closing.Load() { t.Fatal("session did not receive termination") }
	if got := request("/webrtc/whep/viewer-1?token=allowed"); got != http.StatusNotFound { t.Fatalf("repeat DELETE status %d", got) }
}

func TestWHEPCachedReplayLargerThanQueueDoesNotDisconnect(t *testing.T) {
	const replayFrames = 1101
	session := &whepSession{
		streamID: "cached", subscriberId: "cached-viewer",
		msgChan: make(chan base.RtmpMsg, 32),
		closeChan: make(chan bool, 2),
		done: make(chan struct{}),
	}
	session.OnReplayStart()
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		for i := 0; i < replayFrames; i++ {
			session.enqueue(base.RtmpMsg{Header: base.RtmpHeader{MsgTypeId: base.RtmpTypeIdVideo}})
		}
		session.OnReplayStop()
	}()
	// Deliberately let the producer fill its bounded queue before we drain it.
	// The old fail-fast enqueue closed the session during this window.
	time.Sleep(20 * time.Millisecond)
	for i := 0; i < replayFrames+2; i++ {
		select {
		case msg := <-session.msgChan:
			session.consumeReplayControl(msg)
		case <-time.After(3 * time.Second):
			t.Fatalf("cached replay stalled at message %d", i)
		}
	}
	select {
	case <-producerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("replay producer did not terminate")
	}
	if session.closing.Load() { t.Fatal("cached GOP was misclassified as slow live viewer") }
	if session.replayEnqueuing.Load() { t.Fatal("consumer did not finish draining the cached replay") }
	if cap(session.msgChan) != 32 { t.Fatal("replay removed bounded queue protection") }
	// After replay completion, a real-time overflow must still close.
	for i := 0; i < 33; i++ { session.enqueue(base.RtmpMsg{}) }
	if !session.closing.Load() { t.Fatal("live queue overflow must still disconnect slow viewers") }
}
