package recorder

import (
	"testing"

	"github.com/lalmax-pro/lalmax-nvr/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/q191201771/lal/pkg/base"
)

func TestRecorderGroupQueueOverflowObservable(t *testing.T) {
	m := metrics.NewMetrics()
	session := &recSession{
		cameraID: "overflow-camera",
		queue: make(chan base.RtmpMsg, 1),
		stop: make(chan struct{}),
		metrics: m,
	}
	subscriber := &recSubscriber{s: session}
	subscriber.OnMsg(base.RtmpMsg{})
	subscriber.OnMsg(base.RtmpMsg{})
	if drops := session.queueDrops.Load(); drops != 1 {
		t.Fatalf("queue overflow was not counted: %d", drops)
	}
	if got := testutil.ToFloat64(m.RecorderGroupQueueDropsTotal.WithLabelValues("overflow-camera")); got != 1 {
		t.Fatalf("prometheus drop counter: got %g want 1", got)
	}
	close(session.stop)
	subscriber.OnMsg(base.RtmpMsg{})
	if got := session.queueDrops.Load(); got != 1 {
		t.Fatalf("stopped recorder must not count a new dropped frame: %d", got)
	}
}

func TestRecorderQueueLossWaitsForVideoKeyframe(t *testing.T) {
	session := &recSession{cameraID: "camera", gop: []base.RtmpMsg{{}}}
	session.mediaGap.Store(true)
	// Non-video, non-keyframe packets after a loss must not be appended
	// to a newly opened segment or the cached GOP.
	session.handle(base.RtmpMsg{})
	if !session.awaitKeyframe { t.Fatal("lost keyframe resync state") }
	if len(session.gop) != 0 { t.Fatal("stale GOP retained after overflow") }
	if session.mediaGap.Load() { t.Fatal("overflow flag not consumed") }
}
