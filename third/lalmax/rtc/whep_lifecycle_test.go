package rtc

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/q191201771/lal/pkg/base"
	maxlogic "github.com/q191201771/lalmax/logic"
	"github.com/pion/webrtc/v4"
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

// WHEP snapshot replay is independent of both the live bounded queue and
// Group's per-subscriber write lock. This reproduces the 200-frame / 32-queue
// publisher stall reported by upstream.
func TestWHEPCachedReplayDoesNotBlockPublisher(t *testing.T) {
	mgr := maxlogic.NewComplexGroupManager()
	group, created := mgr.GetOrCreateGroup(maxlogic.NewStreamKey("live", "whep-snapshot-test"), "test", nil, 1, 1024)
	if !created || group == nil { t.Fatal("failed to create group") }
	video := func(key bool) base.RtmpMsg {
		frame := byte(base.RtmpAvcInterFrame)
		if key { frame = base.RtmpAvcKeyFrame }
		return base.RtmpMsg{Header: base.RtmpHeader{MsgTypeId: base.RtmpTypeIdVideo},
			Payload: []byte{frame, base.RtmpAvcPacketTypeNalu, 0, 0, 0, 7}}
	}
	group.OnMsg(video(true))
	for i := 0; i < 200; i++ { group.OnMsg(video(false)) }
	session := &whepSession{
		streamID: "whep-snapshot-test", subscriberId: "viewer",
		msgChan: make(chan base.RtmpMsg, 32),
		closeChan: make(chan bool, 2), done: make(chan struct{}),
	}
	// This dummy track lets the test exercise the real H.264 resume gate
	// without creating an ICE connection or packetizing the cached media.
	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "test")
	if err != nil { t.Fatal(err) }
	session.videoTrack = track
	session.replayPending.Store(true)
	snapshot, ok := group.AddSubscriberWithSnapshot(maxlogic.SubscriberInfo{
		SubscriberID: session.subscriberId, Protocol: maxlogic.SubscriberProtocolWHEP,
	}, session)
	if !ok || len(snapshot) < 200 { t.Fatalf("GOP snapshot too small: ok=%v count=%d", ok, len(snapshot)) }
	published := make(chan struct{})
	go func() {
		group.OnMsg(video(true))
		for i := 0; i < 200; i++ { group.OnMsg(video(false)) }
		close(published)
	}()
	select {
	case <-published:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Group.OnMsg blocked on WHEP replay, affecting live publisher")
	}
	if session.closing.Load() { t.Fatal("cached GOP falsely disconnected viewer") }
	if len(session.msgChan) != 0 || cap(session.msgChan) != 32 {
		t.Fatal("cached GOP filled or unbounded the live queue")
	}
	// Finishing the cached snapshot must resync to a fresh keyframe; stale
	// delta frames and audio must not join an already-paced cached GOP.
	session.awaitKeyframe.Store(true)
	session.replayPending.Store(false)
	session.OnMsg(video(false))
	if len(session.msgChan) != 0 { t.Fatal("stale delta enqueued before keyframe") }
	session.OnMsg(video(true))
	if len(session.msgChan) != 1 { t.Fatal("fresh keyframe did not resume live WHEP") }
	session.OnStop()
	group.RemoveSubscriber(session.subscriberId)
}

func TestWHEPQueueRemainsBoundedAfterReplay(t *testing.T) {
	session := &whepSession{
		subscriberId: "slow-live", streamID: "live",
		msgChan: make(chan base.RtmpMsg, 32),
		closeChan: make(chan bool, 2),
		done: make(chan struct{}),
	}
	for i := 0; i < 33; i++ { session.enqueue(base.RtmpMsg{}) }
	if !session.closing.Load() { t.Fatal("slow viewer overflow did not close") }
	if len(session.msgChan) > 32 { t.Fatal("queue exceeded configured bound") }
}
