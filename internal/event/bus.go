package event

import (
	"context"
	"errors"
	"sync"
)

// Topic constants for event types.
const (
	TopicSegmentCompleted    = "segment.completed"
	TopicRecorderReconnected = "recorder.reconnected"
	TopicAIDetection         = "ai.detection"
	TopicMQTTTrigger         = "mqtt.trigger"
)

var (
	ErrDuplicateSubscriber = errors.New("subscriber already registered for this topic")
	ErrInvalidSubscriber = errors.New("event subscriber channel must be non-nil and buffered")
)

// subscriber holds a channel and its mutex.
type subscriber struct {
	ch     chan Event
	mu     sync.Mutex // protects send vs close race
	closed bool
}

// EventBus is a lightweight pub/sub system with ring-buffer overflow.
// Per-topic subscribers get buffered channels; when full, the oldest
// event is dropped to make room — never blocks the publisher.
type EventBus struct {
	mu          sync.RWMutex
	bufferSize  int
	subscribers map[string][]*subscriber
}

// NewEventBus creates an EventBus with the given buffer size.
// If bufferSize <= 0, defaults to 64.
func NewEventBus(bufferSize int) *EventBus {
	if bufferSize <= 0 {
		bufferSize = 64
	}
	return &EventBus{
		bufferSize:  bufferSize,
		subscribers: make(map[string][]*subscriber),
	}
}

// Subscribe registers a caller-owned buffered channel for a topic.
// bufferSize is retained for API compatibility, but capacity is determined
// by the supplied channel. An unbuffered channel cannot support drop-oldest
// delivery without blocking the media/control publisher.
func (b *EventBus) Subscribe(topic string, ch chan Event, bufferSize int) error {
	if ch == nil || cap(ch) == 0 {
		return ErrInvalidSubscriber
	}
	_ = bufferSize // Deprecated: callers own channel capacity.
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.subscribers[topic] {
		if s.ch == ch && !s.closed {
			return ErrDuplicateSubscriber
		}
	}
	b.subscribers[topic] = append(b.subscribers[topic], &subscriber{ch: ch})
	return nil
}

// Unsubscribe removes only the requested subscriber. It never closes the
// caller's channel or evicts other subscribers from the same topic.
func (b *EventBus) Unsubscribe(topic string, ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subscribers[topic]
	if len(subs) == 0 {
		return
	}
	remaining := make([]*subscriber, 0, len(subs))
	for _, s := range subs {
		if s.ch != ch {
			remaining = append(remaining, s)
			continue
		}
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
	}
	if len(remaining) == 0 {
		delete(b.subscribers, topic)
		return
	}
	b.subscribers[topic] = remaining
}

// Publish sends an event to all subscribers of the given topic.
// Respects context cancellation. Never blocks on any single subscriber.
func (b *EventBus) Publish(ctx context.Context, topic string, data interface{}) {
	evt := Event{Topic: topic, Data: data}

	b.mu.RLock()
	subs := make([]*subscriber, len(b.subscribers[topic]))
	copy(subs, b.subscribers[topic])
	b.mu.RUnlock()

	for _, s := range subs {
		select {
		case <-ctx.Done():
			return
		default:
		}

		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			continue
		}
		// Ring-buffer overflow: if channel full, drain one (oldest) then send.
		if len(s.ch) == cap(s.ch) {
			<-s.ch // drop oldest
		}
		s.ch <- evt
		s.mu.Unlock()
	}
}
