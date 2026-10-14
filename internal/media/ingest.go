package media

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lalmax-pro/lalmax-nvr/internal/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
)

var ingestLogger = slog.Default().With("component", "media-ingest")

// ingestSubscriber subscribes to protocol-specific push stream events.
type ingestSubscriber interface {
	subscribe(ctx context.Context) (<-chan ingestEvent, error)
	protocol() string
}

type rtmpEventSubscriber interface {
	SubscribeRTMPEvents(ctx context.Context) (<-chan RTMPEvent, error)
}

type srtEventSubscriber interface {
	SubscribeSRTEvents(ctx context.Context) (<-chan SRTEvent, error)
}

type whipEventSubscriber interface {
	SubscribeWHIPEvents(ctx context.Context) (<-chan WHIPEvent, error)
}

type rtmpSubscriber struct {
	upstream rtmpEventSubscriber
}

func (s rtmpSubscriber) protocol() string { return "rtmp" }

func (s rtmpSubscriber) subscribe(ctx context.Context) (<-chan ingestEvent, error) {
	events, err := s.upstream.SubscribeRTMPEvents(ctx)
	if err != nil {
		return nil, err
	}

	out := make(chan ingestEvent, 64)
	go func() {
		defer close(out)
		for ev := range events {
			out <- ingestEvent{
				Protocol:    ev.Protocol,
				StreamID:    ev.StreamID,
				Type:        ev.Type,
				SessionID:   ev.SessionID,
				Traceparent: ev.Traceparent,
				Tracestate:  ev.Tracestate,
			}
		}
	}()
	return out, nil
}

type srtSubscriber struct {
	upstream srtEventSubscriber
}

func (s srtSubscriber) protocol() string { return "srt" }

func (s srtSubscriber) subscribe(ctx context.Context) (<-chan ingestEvent, error) {
	events, err := s.upstream.SubscribeSRTEvents(ctx)
	if err != nil {
		return nil, err
	}

	out := make(chan ingestEvent, 64)
	go func() {
		defer close(out)
		for ev := range events {
			out <- ingestEvent{
				Protocol:    ev.Protocol,
				StreamID:    ev.StreamID,
				Type:        ev.Type,
				SessionID:   ev.SessionID,
				Traceparent: ev.Traceparent,
				Tracestate:  ev.Tracestate,
			}
		}
	}()
	return out, nil
}

type whipSubscriber struct {
	upstream whipEventSubscriber
}

func (s whipSubscriber) protocol() string { return "whip" }

func (s whipSubscriber) subscribe(ctx context.Context) (<-chan ingestEvent, error) {
	events, err := s.upstream.SubscribeWHIPEvents(ctx)
	if err != nil {
		return nil, err
	}

	out := make(chan ingestEvent, 64)
	go func() {
		defer close(out)
		for ev := range events {
			out <- ingestEvent{
				Protocol:    ev.Protocol,
				StreamID:    ev.StreamID,
				Type:        ev.Type,
				SessionID:   ev.SessionID,
				Traceparent: ev.Traceparent,
				Tracestate:  ev.Tracestate,
			}
		}
	}()
	return out, nil
}

type ingestEvent struct {
	Protocol    string
	StreamID    string
	SessionID   string
	Traceparent string
	Tracestate  string
	Type        string
}

// OnIngestStart is called when a push stream is detected and mapped to a camera.
type OnIngestStart func(cameraID string, streamName string)

// OnIngestStop is called when a push stream disappears.
type OnIngestStop func(cameraID string, streamName string)

// IngestHandler watches lalmax push-stream events and maps stream names to cameras.
type IngestHandler struct {
	subscriber ingestSubscriber
	resolv     func(streamName string) (cameraID string, ok bool)
	onStart    OnIngestStart
	onStop     OnIngestStop

	mu     sync.Mutex
	active map[string]string // streamName -> cameraID
	cancel context.CancelFunc
	done   chan struct{}
}

// NewRTMPIngestHandler creates a handler for RTMP push events emitted by lalmax.
func NewRTMPIngestHandler(subscriber rtmpEventSubscriber, resolv func(string) (string, bool), onStart OnIngestStart, onStop OnIngestStop) *IngestHandler {
	return newIngestHandler(rtmpSubscriber{upstream: subscriber}, resolv, onStart, onStop)
}

// NewSRTIngestHandler creates a handler for SRT push events emitted by lalmax.
func NewSRTIngestHandler(subscriber srtEventSubscriber, resolv func(string) (string, bool), onStart OnIngestStart, onStop OnIngestStop) *IngestHandler {
	return newIngestHandler(srtSubscriber{upstream: subscriber}, resolv, onStart, onStop)
}

// NewWHIPIngestHandler creates a handler for WHIP / customize publish events.
func NewWHIPIngestHandler(subscriber whipEventSubscriber, resolv func(string) (string, bool), onStart OnIngestStart, onStop OnIngestStop) *IngestHandler {
	return newIngestHandler(whipSubscriber{upstream: subscriber}, resolv, onStart, onStop)
}

// newIngestHandler creates a generic protocol ingest handler.
func newIngestHandler(subscriber ingestSubscriber, resolv func(string) (string, bool), onStart OnIngestStart, onStop OnIngestStop) *IngestHandler {
	return &IngestHandler{
		subscriber: subscriber,
		resolv:     resolv,
		onStart:    onStart,
		onStop:     onStop,
		active:     make(map[string]string),
	}
}

// Start begins watching for push streams via lalmax events.
func (h *IngestHandler) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	h.cancel = cancel
	h.done = make(chan struct{})

	go h.subscribeAndRun(ctx)
	ingestLogger.Info("ingest handler started", "protocol", h.subscriber.protocol())
	return nil
}

// Stop stops the ingest handler.
func (h *IngestHandler) Stop() {
	if h.cancel != nil {
		h.cancel()
	}
	if h.done != nil {
		<-h.done
	}
}

// ActiveIngests returns the number of active ingest streams.
func (h *IngestHandler) ActiveIngests() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.active)
}

func (h *IngestHandler) subscribeAndRun(ctx context.Context) {
	defer close(h.done)

	for {
		events, err := h.subscriber.subscribe(ctx)
		if err == nil {
			h.run(ctx, events)
		} else if ctx.Err() == nil {
			ingestLogger.Warn("failed to subscribe to ingest events, retrying", "protocol", h.subscriber.protocol(), "error", fmt.Errorf("subscribe lalmax events: %w", err))
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (h *IngestHandler) run(ctx context.Context, events <-chan ingestEvent) {
	for ev := range events {
		h.handleEvent(ctx, ev)
	}
}

func (h *IngestHandler) handleEvent(ctx context.Context, ev ingestEvent) {
	if ev.Traceparent != "" {
		carrier := propagation.MapCarrier{"traceparent": ev.Traceparent}
		if ev.Tracestate != "" {
			carrier["tracestate"] = ev.Tracestate
		}
		ctx = propagation.TraceContext{}.Extract(ctx, carrier)
	}
	protocol := h.subscriber.protocol()
	if !ingestProtocolMatch(protocol, ev.Protocol) {
		return
	}

	streamName := ev.StreamID
	if streamName == "" {
		return
	}
	ctx, span := observability.StartBusinessSpan(ctx, "media.ingest.event",
		attribute.String("nvr.stream.id", streamName),
		attribute.String("nvr.media.protocol", protocol),
		attribute.String("nvr.media.event.type", ev.Type),
	)
	if ev.SessionID != "" {
		span.SetAttributes(attribute.String("nvr.media.session.id", ev.SessionID))
	}
	defer span.End()

	switch ev.Type {
	case "pub_start", "stream_active":
		h.handleStreamStart(ctx, streamName)
	case "pub_stop", "stream_stopped":
		h.handleStreamStop(ctx, streamName)
	}
}

func (h *IngestHandler) handleStreamStart(ctx context.Context, streamName string) {
	h.mu.Lock()
	if _, exists := h.active[streamName]; exists {
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()

	cameraID, ok := h.resolv(streamName)
	if !ok {
		ingestLogger.DebugContext(ctx, "push stream not mapped to camera", "protocol", h.subscriber.protocol(), "stream", streamName)
		return
	}

	h.mu.Lock()
	h.active[streamName] = cameraID
	h.mu.Unlock()

	ingestLogger.InfoContext(ctx, "ingest detected", "protocol", h.subscriber.protocol(), "camera_id", cameraID, "stream", streamName)

	if h.onStart != nil {
		h.onStart(cameraID, streamName)
	}
}

func (h *IngestHandler) handleStreamStop(ctx context.Context, streamName string) {
	h.mu.Lock()
	cameraID, exists := h.active[streamName]
	if !exists {
		h.mu.Unlock()
		return
	}
	delete(h.active, streamName)
	h.mu.Unlock()

	ingestLogger.InfoContext(ctx, "ingest stopped", "protocol", h.subscriber.protocol(), "camera_id", cameraID, "stream", streamName)

	if h.onStop != nil {
		h.onStop(cameraID, streamName)
	}
}

// BuildReverseMap builds a reverse lookup map from stream_key -> camera_id
// from a config that maps camera_id -> stream_key.
func BuildReverseMap(streamKeys map[string]string) map[string]string {
	reverse := make(map[string]string, len(streamKeys))
	for cameraID, streamKey := range streamKeys {
		if streamKey != "" {
			reverse[streamKey] = cameraID
		}
	}
	return reverse
}

// SRTStreamName extracts the published media stream ID from an SRT stream ID.
// Plain stream IDs remain unchanged for clients that do not use the SRT format.
func SRTStreamName(streamID string) string {
	if !strings.HasPrefix(streamID, "#!::") {
		return streamID
	}
	for _, field := range strings.Split(strings.TrimPrefix(streamID, "#!::"), ",") {
		key, value, ok := strings.Cut(field, "=")
		if ok && (key == "h" || key == "r") && value != "" {
			return value
		}
	}
	return ""
}

func ingestProtocolMatch(want, got string) bool {
	got = strings.ToLower(strings.TrimSpace(got))
	if got == "" {
		return true
	}
	switch want {
	case "whip":
		switch got {
		case "whip", "webrtc", "whip_push", "customize":
			return true
		default:
			return false
		}
	default:
		return got == want
	}
}
