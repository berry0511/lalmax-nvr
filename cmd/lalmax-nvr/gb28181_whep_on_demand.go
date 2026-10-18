package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	lalmaxserver "github.com/q191201771/lalmax/server"
)

const (
	gbWHEPOnDemandCooldown = 15 * time.Second
	gbWHEPOnDemandMaxConcurrent = 4
)

// gbWHEPOnDemandHook bridges LALMax's missing-WHEP-stream hook to NVR's
// GB28181 SIP INVITE. Only known, online GB28181 channels may be started.
// This hook never creates an external RTMP publisher and never sends BYE.
type gbWHEPOnDemandHook struct {
	enabled atomic.Bool
	eligible func(deviceID, channelID string) bool
	start func(deviceID, channelID, streamID string) error

	mu sync.Mutex
	inFlight map[string]struct{}
	recent map[string]time.Time
}

func newGBWHEPOnDemandHook(eligible func(string, string) bool, start func(string, string, string) error) *gbWHEPOnDemandHook {
	return &gbWHEPOnDemandHook{
		eligible: eligible,
		start: start,
		inFlight: make(map[string]struct{}),
		recent: make(map[string]time.Time),
	}
}

func (h *gbWHEPOnDemandHook) Name() string { return "nvr-gb28181-whep-on-demand" }

func (h *gbWHEPOnDemandHook) SetEnabled(enabled bool) {
	h.enabled.Store(enabled)
}

func splitGBWHEPStreamID(stream string) (deviceID, channelID string, ok bool) {
	deviceID, channelID, found := strings.Cut(stream, ":")
	if !found || !isGBSIPID(deviceID) || !isGBSIPID(channelID) {
		return "", "", false
	}
	return deviceID, channelID, true
}

func isGBSIPID(id string) bool {
	if len(id) != 20 { return false }
	for i := 0; i < len(id); i++ {
		if id[i] < '0' || id[i] > '9' { return false }
	}
	return true
}

func (h *gbWHEPOnDemandHook) OnHookEvent(event lalmaxserver.HookEvent) error {
	if !h.enabled.Load() || event.Event != lalmaxserver.HookEventStreamNotFound {
		return nil
	}
	var payload struct {
		App string `json:"app"`
		Stream string `json:"stream"`
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode missing-stream hook: %w", err)
	}
	// Do not turn unrelated RTSP, RTMP, ZLM or arbitrary stream requests into SIP commands.
	if (payload.App != "live" && payload.App != "") || payload.Schema != "whep" {
		return nil
	}
	deviceID, channelID, ok := splitGBWHEPStreamID(payload.Stream)
	if !ok || h.eligible == nil || h.start == nil || !h.eligible(deviceID, channelID) {
		return nil
	}
	// Global concurrency ceiling and per-channel dedupe/cooldown. The hook is
	// processed asynchronously so the WHEP HTTP handler can wait for the stream.
	h.mu.Lock()
	now := time.Now()
	if _, busy := h.inFlight[payload.Stream]; busy {
		h.mu.Unlock()
		return nil
	}
	if last := h.recent[payload.Stream]; !last.IsZero() && now.Sub(last) < gbWHEPOnDemandCooldown {
		h.mu.Unlock()
		return nil
	}
	if len(h.inFlight) >= gbWHEPOnDemandMaxConcurrent {
		h.mu.Unlock()
		slog.Warn("GB28181 WHEP on-demand concurrency limit reached")
		return nil
	}
	if len(h.recent) > 512 {
		for stream, last := range h.recent {
			if now.Sub(last) > 2 * time.Minute {
				delete(h.recent, stream)
			}
		}
	}
	h.inFlight[payload.Stream] = struct{}{}
	h.recent[payload.Stream] = now
	h.mu.Unlock()

	go func() {
		defer func() {
			h.mu.Lock()
			delete(h.inFlight, payload.Stream)
			h.mu.Unlock()
		}()
		// The server is checked again inside start so a device which went
		// offline between the hook and the INVITE cannot be started.
		if !h.enabled.Load() { return }
		if err := h.start(deviceID, channelID, payload.Stream); err != nil {
			slog.Warn("GB28181 WHEP on-demand INVITE failed", "stream_id", payload.Stream, "error", err)
			return
		}
		slog.Info("GB28181 WHEP on-demand INVITE requested", "stream_id", payload.Stream)
	}()
	return nil
}
