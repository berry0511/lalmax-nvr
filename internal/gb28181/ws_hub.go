package gb28181

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type EventType string

const (
	EventDeviceOnline   EventType = "device.online"
	EventDeviceOffline  EventType = "device.offline"
	EventChannelUpdate  EventType = "channel.update"
	EventChannelDelete  EventType = "channel.delete"
	EventChannelOffline EventType = "channel.offline"
)

type Event struct {
	Type      EventType              `json:"type"`
	Data      map[string]interface{} `json:"data"`
	Timestamp int64                  `json:"timestamp"`
}

type Client struct {
	hub  *WSHub
	conn *websocket.Conn
	send chan []byte
}

type WSHub struct {
	clients    map[*Client]bool
	broadcast  chan Event
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
}

func NewWSHub() *WSHub {
	ctx, cancel := context.WithCancel(context.Background())
	return &WSHub{
		ctx: ctx,
		cancel: cancel,
		clients:    make(map[*Client]bool),
		broadcast:  make(chan Event, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *WSHub) Stop() { h.cancel() }

func (h *WSHub) Run() {
	defer func() {
		h.mu.Lock()
		for client := range h.clients {
			delete(h.clients, client)
			close(client.send)
			client.conn.Close()
		}
		h.mu.Unlock()
	}()
	for {
		select {
		case <-h.ctx.Done():
			return
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()

		case event := <-h.broadcast:
			data, err := json.Marshal(event)
			if err != nil {
				slog.Error("failed to marshal event", "error", err)
				continue
			}

			h.mu.Lock()
			for client := range h.clients {
				select {
				case client.send <- data:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.Unlock()
		}
	}
}

func (h *WSHub) Broadcast(event Event) {
	event.Timestamp = time.Now().UnixMilli()
	select {
	case <-h.ctx.Done():
		return
	case h.broadcast <- event:
	default:
		slog.Warn("GB28181 websocket event queue full: broadcast dropped", "event", event.Type)
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (h *WSHub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("failed to upgrade websocket", "error", err)
		return
	}

	client := &Client{
		hub:  h,
		conn: conn,
		send: make(chan []byte, 256),
	}

	select {
	case <-h.ctx.Done():
		conn.Close()
		return
	case h.register <- client:
	}

	go client.writePump()
	go client.readPump()
}

func (c *Client) readPump() {
	defer func() {
		select {
		case <-c.hub.ctx.Done():
		case c.hub.unregister <- c:
		}
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
