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
