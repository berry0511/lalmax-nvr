package event

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lalmax-pro/lalmax-nvr/internal/model"
	"github.com/lalmax-pro/lalmax-nvr/internal/storage"
)

// Archiver persists runtime EventBus messages into the unified product event table.
type Archiver struct {
	bus    *EventBus
	db     storage.EventRepository
	mu     sync.Mutex
	lastAI map[string]time.Time
}

const aiEventRepeatInterval = 30 * time.Second

func NewArchiver(bus *EventBus, db storage.EventRepository) *Archiver {
	if bus == nil || db == nil {
		return nil
	}
	return &Archiver{bus: bus, db: db}
}

func (a *Archiver) Start(ctx context.Context) {
	if a == nil {
		return
	}
	topics := []string{TopicRecorderReconnected, TopicAIDetection, TopicMQTTTrigger}
	ch := make(chan Event, 128)
	subscribed := make([]string, 0, len(topics))
	for _, topic := range topics {
		if err := a.bus.Subscribe(topic, ch, 128); err != nil {
			slog.Warn("event archiver subscribe failed", "topic", topic, "error", err)
			continue
		}
		subscribed = append(subscribed, topic)
	}
	defer func() {
		for _, topic := range subscribed {
			a.bus.Unsubscribe(topic, ch)
		}
	}()
	if len(subscribed) == 0 {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-ch:
			a.handle(ctx, evt)
		}
	}
}

func (a *Archiver) handle(ctx context.Context, evt Event) {
	switch data := evt.Data.(type) {
	case RecorderReconnected:
		a.handleRecorderReconnected(ctx, data)
	case AIDetection:
		a.handleAIDetection(ctx, data)
	case MQTTTrigger:
		a.handleMQTTTrigger(ctx, data)
	default:
		slog.Debug("event archiver ignored unsupported event", "topic", evt.Topic)
	}
}

func (a *Archiver) handleAIDetection(ctx context.Context, ev AIDetection) {
	if ev.CameraID == "" || len(ev.Detections) == 0 {
		return
	}
	labels := make(map[string]int)
	for _, detection := range ev.Detections {
		label := strings.TrimSpace(detection.Label)
		if label == "" {
			label = "object"
		}
		labels[label]++
	}
	orderedLabels := make([]string, 0, len(labels))
	for label := range labels {
		orderedLabels = append(orderedLabels, label)
	}
	sort.Strings(orderedLabels)
	parts := make([]string, 0, len(orderedLabels))
	for _, label := range orderedLabels {
		parts = append(parts, label+" x"+strconv.Itoa(labels[label]))
	}
	// Webhook detectors can report the same objects every frame. Keep the event
	// timeline useful by recording an unchanged camera/class set at most every
	// 30 seconds; changed classes still produce an event immediately.
	key := ev.CameraID + "\x00" + strings.Join(orderedLabels, "\x00")
	now := time.Now().UTC()
	a.mu.Lock()
	if a.lastAI == nil {
		a.lastAI = make(map[string]time.Time)
	}
	if last, ok := a.lastAI[key]; ok && now.Sub(last) < aiEventRepeatInterval {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	metadata, _ := json.Marshal(map[string]any{"pts": ev.PTS, "detections": ev.Detections})
	startedAt := now
	if ev.Timestamp > 0 {
		if ev.Timestamp >= 1_000_000_000_000 {
			startedAt = time.UnixMilli(ev.Timestamp).UTC()
		} else {
			startedAt = time.Unix(ev.Timestamp, 0).UTC()
		}
	}
	_, err := a.db.InsertEvent(ctx, model.Event{
		CameraID: ev.CameraID, Source: model.EventSourceAI, Type: "ai_detection",
		Severity: model.EventSeverityInfo, Status: model.EventStatusOpen,
		Message: "AI detected: " + strings.Join(parts, ", "), Metadata: string(metadata),
		StartedAt: startedAt, CreatedAt: now,
	})
	if err != nil {
		slog.Warn("failed to archive AI detection", "camera_id", ev.CameraID, "error", err)
		return
	}
	a.mu.Lock()
	a.lastAI[key] = now
	for existingKey, seenAt := range a.lastAI {
		if now.Sub(seenAt) > 5*time.Minute {
			delete(a.lastAI, existingKey)
		}
	}
	a.mu.Unlock()
}

func (a *Archiver) handleMQTTTrigger(ctx context.Context, ev MQTTTrigger) {
	if ev.CameraID == "" || ev.Action == "" {
		return
	}
	at := ev.At.UTC()
	if at.IsZero() {
		at = time.Now().UTC()
	}
	metadata, _ := json.Marshal(map[string]string{"action": ev.Action})
	_, err := a.db.InsertEvent(ctx, model.Event{
		CameraID: ev.CameraID, Source: model.EventSourceMQTT, Type: "mqtt_trigger",
		Severity: model.EventSeverityInfo, Status: model.EventStatusOpen,
		Message: "MQTT action: " + ev.Action, Metadata: string(metadata),
		StartedAt: at, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		slog.Warn("failed to archive MQTT trigger", "camera_id", ev.CameraID, "error", err)
	}
}

func (a *Archiver) handleRecorderReconnected(ctx context.Context, ev RecorderReconnected) {
	startedAt := parseEventTime(ev.ReconnectedAt)
	metadata, _ := json.Marshal(map[string]any{
		"disconnected_at": ev.DisconnectedAt,
		"reconnected_at":  ev.ReconnectedAt,
		"downtime":        ev.Downtime,
		"retry_count":     ev.RetryCount,
		"gap_reason":      ev.GapReason,
	})

	_, err := a.db.InsertEvent(ctx, model.Event{
		CameraID:    ev.CameraID,
		Source:      model.EventSourceRecorder,
		Type:        "recorder_reconnected",
		Severity:    model.EventSeverityWarning,
		Status:      model.EventStatusOpen,
		Message:     "Recorder reconnected after " + ev.Downtime,
		Metadata:    string(metadata),
		RecordingID: ev.RecordingID,
		StartedAt:   startedAt,
		CreatedAt:   startedAt,
	})
	if err != nil {
		slog.Warn("failed to archive recorder reconnected event", "camera_id", ev.CameraID, "error", err)
	}
}

func parseEventTime(value string) time.Time {
	if value == "" {
		return time.Now().UTC()
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC()
		}
	}
	return time.Now().UTC()
}
