package event

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lalmax-pro/lalmax-nvr/internal/model"
)

type recordingEventRepository struct {
	events []model.Event
}

func (r *recordingEventRepository) InsertEvent(_ context.Context, event model.Event) (int64, error) {
	r.events = append(r.events, event)
	return int64(len(r.events)), nil
}

func TestArchiverHandlesAIDetectionAndMQTTTrigger(t *testing.T) {
	repo := &recordingEventRepository{}
	archiver := &Archiver{db: repo}
	started := time.Unix(1_700_000_000, 0).UTC()

	aiDetection := AIDetection{
		CameraID: "cam-1", PTS: 42, Timestamp: started.Unix(),
		Detections: []AIDetectionObject{
			{Label: "person", Confidence: 0.9},
			{Label: "person", Confidence: 0.8},
			{Label: "car", Confidence: 0.7},
		},
	}
	archiver.handle(context.Background(), Event{Topic: TopicAIDetection, Data: aiDetection})
	// Repeated frames with the same camera and detected classes are debounced.
	aiDetection.PTS++
	archiver.handle(context.Background(), Event{Topic: TopicAIDetection, Data: aiDetection})
	archiver.handle(context.Background(), Event{Topic: TopicMQTTTrigger, Data: MQTTTrigger{
		CameraID: "cam-2", Action: "stop", At: started,
	}})

	if len(repo.events) != 2 {
		t.Fatalf("expected 2 archived events, got %d", len(repo.events))
	}
	aiEvent := repo.events[0]
	if aiEvent.Source != model.EventSourceAI || aiEvent.Type != "ai_detection" || aiEvent.CameraID != "cam-1" {
		t.Fatalf("unexpected AI event: %+v", aiEvent)
	}
	if aiEvent.Message != "AI detected: car x1, person x2" || !aiEvent.StartedAt.Equal(started) {
		t.Fatalf("unexpected AI event summary/time: %+v", aiEvent)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(aiEvent.Metadata), &metadata); err != nil || metadata["pts"] != float64(42) {
		t.Fatalf("invalid AI metadata %q: %v", aiEvent.Metadata, err)
	}
	mqttEvent := repo.events[1]
	if mqttEvent.Source != model.EventSourceMQTT || mqttEvent.Type != "mqtt_trigger" || mqttEvent.CameraID != "cam-2" {
		t.Fatalf("unexpected MQTT event: %+v", mqttEvent)
	}
	if mqttEvent.Message != "MQTT action: stop" || !mqttEvent.StartedAt.Equal(started) {
		t.Fatalf("unexpected MQTT event summary/time: %+v", mqttEvent)
	}
}

func TestArchiverSkipsEmptyAIDetections(t *testing.T) {
	repo := &recordingEventRepository{}
	archiver := &Archiver{db: repo}
	archiver.handle(context.Background(), Event{Topic: TopicAIDetection, Data: AIDetection{CameraID: "cam-1"}})
	if len(repo.events) != 0 {
		t.Fatalf("expected empty AI detection to be ignored, archived %d events", len(repo.events))
	}
}
