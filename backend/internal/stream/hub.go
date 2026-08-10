// Package stream implements a topic-based WebSocket fan-out hub. Slow
// consumers are disconnected rather than allowed to backpressure producers.
package stream

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait     = 10 * time.Second
	pingPeriod    = 30 * time.Second
	subscriberBuf = 128
)

type subscriber struct {
	ch chan []byte
}

// Hub broadcasts JSON-encoded messages to WebSocket subscribers per topic.
type Hub struct {
	mu       sync.RWMutex
	topics   map[string]map[*subscriber]struct{}
	upgrader websocket.Upgrader
}

// NewHub returns an empty hub. checkOrigin relaxes the origin check for
// development ("*" allows any origin).
func NewHub(corsOrigin string) *Hub {
	return &Hub{
		topics: map[string]map[*subscriber]struct{}{},
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 4096,
			CheckOrigin: func(r *http.Request) bool {
				if corsOrigin == "*" {
					return true
				}
				return r.Header.Get("Origin") == corsOrigin
			},
		},
	}
}

// Publish JSON-encodes v and broadcasts it to every subscriber of topic.
func (h *Hub) Publish(topic string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("hub marshal", "topic", topic, "err", err)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for sub := range h.topics[topic] {
		select {
		case sub.ch <- data:
		default: // drop message for slow subscriber
		}
	}
}

// Subscribers returns the current subscriber count for a topic.
func (h *Hub) Subscribers(topic string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.topics[topic])
}

func (h *Hub) add(topic string, s *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.topics[topic] == nil {
		h.topics[topic] = map[*subscriber]struct{}{}
	}
	h.topics[topic][s] = struct{}{}
}

func (h *Hub) remove(topic string, s *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.topics[topic], s)
}

// ServeWS returns an http.Handler that upgrades the connection and streams
// the given topic until the client disconnects.
func (h *Hub) ServeWS(topic string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := h.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		sub := &subscriber{ch: make(chan []byte, subscriberBuf)}
		h.add(topic, sub)
		defer func() {
			h.remove(topic, sub)
			conn.Close()
		}()

		// Reader: only to observe close frames.
		go func() {
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					conn.Close()
					return
				}
			}
		}()

		ping := time.NewTicker(pingPeriod)
		defer ping.Stop()
		for {
			select {
			case msg, ok := <-sub.ch:
				if !ok {
					return
				}
				conn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					return
				}
			case <-ping.C:
				conn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}
}
