package streamhistory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lalmax-pro/lalmax-nvr/internal/media"
	"github.com/lalmax-pro/lalmax-nvr/internal/storage"
	lalmaxserver "github.com/q191201771/lalmax/server"
)

var logger = slog.Default().With("component", "stream-history")

// Manager subscribes to lalmax pub start/stop events and records stream history.
type Manager struct {
	db                storage.StreamHistoryRepository
	engine            media.Engine
	cancelFunc        context.CancelFunc
	mu                sync.Mutex
	iptvSessions      map[string]string // stream ID -> currently active media history session
	iptvPullErrors    map[string]string // stream ID -> most recent sanitized upstream pull error
	activeSessions    map[string]struct{}
	hookRegistered    bool
	historyReconciled bool
}

func NewManager(db storage.StreamHistoryRepository, engine media.Engine) *Manager {
	return &Manager{
		db:             db,
		engine:         engine,
		iptvSessions:   make(map[string]string),
		iptvPullErrors: make(map[string]string),
		activeSessions: make(map[string]struct{}),
	}
}

// RecordHLSPullSessionState closes an active media-history interval when the
// native HLS pull task ends. Starting a task alone is not an online media
// session; media history begins on the stream-active event.
func (m *Manager) RecordHLSPullSessionState(streamID string, online bool, at time.Time, offlineReason, offlineDetail string) {
	if m == nil || m.db == nil || streamID == "" {
		return
	}
	if !online && at.IsZero() {
		at = time.Now()
	}
	m.mu.Lock()
	if online {
		delete(m.iptvPullErrors, streamID)
		m.mu.Unlock()
		return
	}
	sessionID, exists := m.iptvSessions[streamID]
	delete(m.iptvSessions, streamID)
	delete(m.iptvPullErrors, streamID)
	m.mu.Unlock()
	if exists {
		if offlineReason == "" {
			offlineReason = "unknown"
		}
		if err := m.db.FinishStreamHistory(context.Background(), streamID, sessionID, at, offlineReason, offlineDetail, 0, 0); err != nil {
			logger.Warn("failed to finish IPTV pull history", "stream_id", streamID, "error", err)
		}
		m.forgetActive(streamID, sessionID, "HLS_PULL")
	}
}

// RecordHLSPullAttemptError retains the latest sanitized upstream failure so
// a media-stop event can explain why the IPTV stream became idle.
func (m *Manager) RecordHLSPullAttemptError(streamID, detail string) {
	if m == nil || streamID == "" || strings.TrimSpace(detail) == "" {
		return
	}
	m.mu.Lock()
	m.iptvPullErrors[streamID] = strings.TrimSpace(detail)
	m.mu.Unlock()
}

func (m *Manager) RecordMediaState(streamID string, active bool, at time.Time) {
	if m == nil || m.db == nil || streamID == "" {
		return
	}
	if isIPTVStreamID(streamID) {
		if active {
			m.startIPTVMediaHistory(streamID, at)
			return
		}
		m.finishIPTVMediaHistory(streamID, at)
		return
	}
	if err := m.db.RecordStreamHistoryMediaState(context.Background(), streamID, at, active); err != nil {
		logger.Warn("failed to update stream media state", "stream_id", streamID, "active", active, "error", err)
	}
}

func (m *Manager) startIPTVMediaHistory(streamID string, at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	m.mu.Lock()
	if _, exists := m.iptvSessions[streamID]; exists {
		delete(m.iptvPullErrors, streamID)
		m.mu.Unlock()
		if err := m.db.RecordStreamHistoryMediaState(context.Background(), streamID, at, true); err != nil {
			logger.Warn("failed to update IPTV media state", "stream_id", streamID, "active", true, "error", err)
		}
		return
	}
	sessionID := fmt.Sprintf("hls-pull-%s-%d", streamID, at.UnixNano())
	m.iptvSessions[streamID] = sessionID
	delete(m.iptvPullErrors, streamID)
	m.mu.Unlock()

	history := &storage.StreamHistory{
		StreamID:  streamID,
		AppName:   "live",
		Protocol:  "HLS_PULL",
		SessionID: sessionID,
		StartedAt: at,
	}
	if err := m.insertStartOnce(history); err != nil {
		logger.Error("failed to insert IPTV media history", "stream_id", streamID, "error", err)
		m.mu.Lock()
		if m.iptvSessions[streamID] == sessionID {
			delete(m.iptvSessions, streamID)
		}
		m.mu.Unlock()
		return
	}
	if err := m.db.RecordStreamHistoryMediaState(context.Background(), streamID, at, true); err != nil {
		logger.Warn("failed to update IPTV media state", "stream_id", streamID, "active", true, "error", err)
	}
}

func (m *Manager) finishIPTVMediaHistory(streamID string, at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	m.mu.Lock()
	sessionID, exists := m.iptvSessions[streamID]
	delete(m.iptvSessions, streamID)
	pullError := m.iptvPullErrors[streamID]
	delete(m.iptvPullErrors, streamID)
	m.mu.Unlock()
	if !exists {
		return
	}
	reason := "media_stopped"
	detail := "Media engine reported that the stream group stopped."
	if pullError != "" {
		reason = "upstream_error"
		detail = pullError
	}
	if err := m.db.FinishStreamHistory(context.Background(), streamID, sessionID, at, reason, detail, 0, 0); err != nil {
		logger.Warn("failed to finish IPTV media history", "stream_id", streamID, "error", err)
	}
	m.forgetActive(streamID, sessionID, "HLS_PULL")
}

// Start begins listening for pub events and recording history.
func (m *Manager) Start(ctx context.Context) error {
	if err := m.Reconcile(ctx); err != nil {
		return fmt.Errorf("finalize previous stream history sessions: %w", err)
	}
	m.mu.Lock()
	hookRegistered := m.hookRegistered
	m.mu.Unlock()
	if hookRegistered {
		// The embedded engine already delivers the same lifecycle through its
		// hook plugin. Subscribing to SSE too would insert every session twice.
		return nil
	}
	if m.engine == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	m.cancelFunc = cancel

	// Run in goroutine to avoid blocking startup (lalmax may not be ready yet)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			events, err := m.engine.SubscribeEvents(ctx, media.EventFilter{
				Types: []media.EventType{
					media.EventPublisherStarted,
					media.EventPublisherStopped,
					media.EventRelayPullStarted,
					media.EventRelayPullStopped,
					media.EventStreamActive,
					media.EventStreamStopped,
				},
			})
			if err != nil {
				logger.Warn("failed to subscribe to stream events, retrying in 5s", "error", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
				}
				continue
			}

			m.run(ctx, events)
			// If events channel closed, retry after a delay
			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Second):
			}
		}
	}()
	return nil
}

// Reconcile closes rows left open by the previous process. Call it before
// source recovery starts so restored tasks are not mistaken for stale rows.
func (m *Manager) Reconcile(ctx context.Context) error {
	if m == nil || m.db == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.historyReconciled {
		return nil
	}
	if err := m.db.FinalizeOpenStreamHistories(ctx, time.Now(), "process_restarted"); err != nil {
		return err
	}
	m.historyReconciled = true
	return nil
}

func (m *Manager) Stop() {
	if m.cancelFunc != nil {
		m.cancelFunc()
	}
}

func (m *Manager) run(ctx context.Context, events <-chan media.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			switch ev.Type {
			case media.EventPublisherStarted:
				m.handlePubStart(ev)
			case media.EventPublisherStopped:
				m.handlePubStop(ev, "publisher_stopped")
			case media.EventRelayPullStarted:
				m.handlePubStart(ev)
			case media.EventRelayPullStopped:
				m.handlePubStop(ev, "relay_pull_stopped")
			case media.EventStreamActive:
				m.RecordMediaState(ev.StreamID, true, ev.At)
			case media.EventStreamStopped:
				m.RecordMediaState(ev.StreamID, false, ev.At)
			}
		}
	}
}

type pubStartPayload struct {
	SessionId  string `json:"session_id"`
	Protocol   string `json:"protocol"`
	RemoteAddr string `json:"remote_addr"`
	AppName    string `json:"app_name"`
	StreamName string `json:"stream_name"`
}

type pubStopPayload struct {
	SessionId     string `json:"session_id"`
	Protocol      string `json:"protocol"`
	RemoteAddr    string `json:"remote_addr"`
	AppName       string `json:"app_name"`
	StreamName    string `json:"stream_name"`
	ReadBytesSum  uint64 `json:"read_bytes_sum"`
	WroteBytesSum uint64 `json:"wrote_bytes_sum"`
	Reason        string `json:"reason"`
	Error         string `json:"error"`
	Message       string `json:"message"`
}

func (m *Manager) handlePubStart(ev media.Event) {
	var payload pubStartPayload
	if err := json.Unmarshal(ev.Raw, &payload); err != nil {
		logger.Warn("failed to parse pub_start payload", "error", err)
		return
	}
	if ev.StreamID == "" {
		ev.StreamID = payload.StreamName
	}
	if ev.Protocol == "" {
		ev.Protocol = payload.Protocol
	}
	if ev.SessionID == "" {
		ev.SessionID = payload.SessionId
	}
	if isInternalHLSHistory(ev.StreamID, ev.Protocol) {
		return
	}

	h := &storage.StreamHistory{
		StreamID:   ev.StreamID,
		AppName:    ev.AppName,
		Protocol:   ev.Protocol,
		RemoteAddr: payload.RemoteAddr,
		SessionID:  ev.SessionID,
		StartedAt:  ev.At,
	}

	if err := m.insertStartOnce(h); err != nil {
		logger.Error("failed to insert stream history", "stream_id", ev.StreamID, "error", err)
		return
	}
	logger.Info("stream history recorded", "stream_id", ev.StreamID, "protocol", ev.Protocol, "remote", payload.RemoteAddr)
}

func (m *Manager) handlePubStop(ev media.Event, defaultReason string) {
	var payload pubStopPayload
	if err := json.Unmarshal(ev.Raw, &payload); err != nil {
		// If we can't parse the payload, still try to finish with zero bytes
		payload = pubStopPayload{}
	}

	endedAt := ev.At
	if endedAt.IsZero() {
		endedAt = time.Now()
	}
	if payload.SessionId != "" {
		ev.SessionID = payload.SessionId
	}
	if payload.StreamName != "" {
		ev.StreamID = payload.StreamName
	}
	if ev.Protocol == "" {
		ev.Protocol = payload.Protocol
	}
	if isInternalHLSHistory(ev.StreamID, ev.Protocol) {
		return
	}
	reason, detail := classifyStopReason(payload, defaultReason)

	if err := m.db.FinishStreamHistory(context.Background(), ev.StreamID, ev.SessionID, endedAt, reason, detail, payload.ReadBytesSum, payload.WroteBytesSum); err != nil {
		logger.Warn("failed to finish stream history", "session_id", ev.SessionID, "error", err)
	}
	m.forgetActive(ev.StreamID, ev.SessionID, ev.Protocol)
}

func isInternalHLSHistory(streamID, protocol string) bool {
	if !strings.EqualFold(strings.TrimSpace(protocol), "customize") {
		return false
	}
	streamID = strings.ToLower(strings.TrimSpace(streamID))
	return strings.HasPrefix(streamID, "iptv_") || strings.HasPrefix(streamID, "emby_")
}

func isIPTVStreamID(streamID string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(streamID)), "iptv_")
}

func classifyStopReason(payload pubStopPayload, fallback string) (string, string) {
	if payload.Reason != "" {
		return payload.Reason, payload.Message
	}
	if payload.Error != "" {
		return "upstream_error", payload.Error
	}
	if payload.Message != "" {
		return "upstream_error", payload.Message
	}
	if fallback == "" {
		fallback = "unknown"
	}
	return fallback, ""
}

func historySessionKey(streamID, sessionID, protocol string) string {
	return streamID + "\x00" + sessionID + "\x00" + protocol
}

func (m *Manager) insertStartOnce(history *storage.StreamHistory) error {
	key := historySessionKey(history.StreamID, history.SessionID, history.Protocol)
	if history.SessionID != "" {
		m.mu.Lock()
		if _, exists := m.activeSessions[key]; exists {
			m.mu.Unlock()
			return nil
		}
		m.activeSessions[key] = struct{}{}
		m.mu.Unlock()
	}
	if err := m.db.InsertStreamHistory(context.Background(), history); err != nil {
		if history.SessionID != "" {
			m.mu.Lock()
			delete(m.activeSessions, key)
			m.mu.Unlock()
		}
		return err
	}
	return nil
}

func (m *Manager) forgetActive(streamID, sessionID, protocol string) {
	if sessionID == "" {
		return
	}
	m.mu.Lock()
	delete(m.activeSessions, historySessionKey(streamID, sessionID, protocol))
	m.mu.Unlock()
}

// RegisterAsHookPlugin registers with lalmax's hook plugin system as a fallback event source.
// This provides more detailed payload (RemoteAddr, bytes) compared to the SSE-based events.
type HookRegistrar interface {
	RegisterHookPlugin(lalmaxserver.HookPlugin, lalmaxserver.HookPluginOptions) (func(), error)
}

func (m *Manager) RegisterAsHookPlugin(server HookRegistrar) (func(), error) {
	plugin := &hookPlugin{mgr: m}
	cancel, err := server.RegisterHookPlugin(plugin, lalmaxserver.HookPluginOptions{
		BufferSize: 128,
		Filter: lalmaxserver.NewHookEventFilter("", "", "", []string{
			lalmaxserver.HookEventPubStart,
			lalmaxserver.HookEventPubStop,
			lalmaxserver.HookEventRelayPullStart,
			lalmaxserver.HookEventRelayPullStop,
			lalmaxserver.HookEventStreamActive,
			lalmaxserver.HookEventGroupStop,
		}),
	})
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.hookRegistered = true
	m.mu.Unlock()
	return cancel, nil
}

type hookPlugin struct {
	mgr *Manager
}

func (p *hookPlugin) Name() string { return "stream-history" }

func (p *hookPlugin) OnHookEvent(event lalmaxserver.HookEvent) error {
	switch event.Event {
	case lalmaxserver.HookEventPubStart:
		p.mgr.handlePubStart(hookStartEvent(event))
	case lalmaxserver.HookEventPubStop:
		p.mgr.handlePubStop(hookStopEvent(media.EventPublisherStopped, event), "publisher_stopped")
	case lalmaxserver.HookEventRelayPullStart:
		p.mgr.handlePubStart(hookStartEvent(event))
	case lalmaxserver.HookEventRelayPullStop:
		p.mgr.handlePubStop(hookStopEvent(media.EventRelayPullStopped, event), "relay_pull_stopped")
	case lalmaxserver.HookEventStreamActive:
		p.mgr.RecordMediaState(hookGroupStreamID(event.Payload), true, hookEventAt(event))
	case lalmaxserver.HookEventGroupStop:
		p.mgr.RecordMediaState(hookGroupStreamID(event.Payload), false, hookEventAt(event))
	}
	return nil
}

func hookStartEvent(hookEvent lalmaxserver.HookEvent) media.Event {
	var payload pubStartPayload
	_ = json.Unmarshal(hookEvent.Payload, &payload)
	return media.Event{
		Type: media.EventPublisherStarted, StreamID: payload.StreamName,
		AppName: payload.AppName, Protocol: payload.Protocol,
		SessionID: payload.SessionId, At: hookEventAt(hookEvent), Raw: hookEvent.Payload,
	}
}

func hookStopEvent(eventType media.EventType, hookEvent lalmaxserver.HookEvent) media.Event {
	var payload pubStopPayload
	_ = json.Unmarshal(hookEvent.Payload, &payload)
	return media.Event{
		Type: eventType, StreamID: payload.StreamName,
		AppName: payload.AppName, Protocol: payload.Protocol,
		SessionID: payload.SessionId, At: hookEventAt(hookEvent), Raw: hookEvent.Payload,
	}
}

func hookGroupStreamID(raw []byte) string {
	var payload struct {
		StreamName string `json:"stream_name"`
	}
	_ = json.Unmarshal(raw, &payload)
	return payload.StreamName
}

func hookEventAt(event lalmaxserver.HookEvent) time.Time {
	if at, err := time.Parse(time.RFC3339Nano, event.Timestamp); err == nil {
		return at
	}
	return time.Now()
}
