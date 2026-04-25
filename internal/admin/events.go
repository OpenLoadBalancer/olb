package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// maxEventSubscribers limits the number of concurrent SSE subscribers
// to prevent unbounded resource exhaustion.
const maxEventSubscribers = 100

// sseKeepAliveInterval is the idle timeout for SSE connections.
// If no events are sent within this period, a keepalive comment is sent
// to detect dead connections and prevent intermediate proxies from closing
// the connection.
const sseKeepAliveInterval = 5 * time.Minute

// eventBus broadcasts EventItems to connected SSE subscribers.
type eventBus struct {
	mu          sync.RWMutex
	subscribers map[chan EventItem]struct{}
}

// newEventBus creates a new event bus.
func newEventBus() *eventBus {
	return &eventBus{
		subscribers: make(map[chan EventItem]struct{}),
	}
}

// subscribe creates and registers a new subscriber channel.
// If the subscriber limit has been reached, it returns a closed channel
// so the caller receives EOF immediately.
func (eb *eventBus) subscribe() chan EventItem {
	ch := make(chan EventItem, 16)
	eb.mu.Lock()
	if len(eb.subscribers) >= maxEventSubscribers {
		eb.mu.Unlock()
		close(ch)
		return ch
	}
	eb.subscribers[ch] = struct{}{}
	eb.mu.Unlock()
	return ch
}

// unsubscribe removes a subscriber channel and drains it.
func (eb *eventBus) unsubscribe(ch chan EventItem) {
	eb.mu.Lock()
	delete(eb.subscribers, ch)
	eb.mu.Unlock()
	// Drain remaining messages to prevent goroutine leak
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// Publish sends an event to all connected subscribers.
// Non-blocking: if a subscriber's buffer is full, the event is dropped
// for that subscriber to prevent slow clients from blocking the publisher.
func (eb *eventBus) Publish(event EventItem) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	for ch := range eb.subscribers {
		select {
		case ch <- event:
		default:
			// Subscriber buffer full — drop event to avoid blocking
		}
	}
}

// SubscriberCount returns the number of connected SSE clients.
func (eb *eventBus) SubscriberCount() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return len(eb.subscribers)
}

// streamEvents handles GET /api/v1/events/stream (SSE).
// Clients receive real-time events via Server-Sent Events.
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	if s.eventBus == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "event streaming not available")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "streaming not supported")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Subscribe to events
	ch := s.eventBus.subscribe()

	// If the channel is already closed, the subscriber limit was reached.
	select {
	case <-ch:
		writeError(w, http.StatusServiceUnavailable, "TOO_MANY_SUBSCRIBERS",
			"maximum number of SSE subscribers reached")
		return
	default:
	}

	defer s.eventBus.unsubscribe(ch)

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	// Set up keepalive ticker to detect dead connections
	keepalive := time.NewTicker(sseKeepAliveInterval)
	defer keepalive.Stop()

	// Stream events until client disconnects
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: event\ndata: %s\n\n", data)
			flusher.Flush()
		case <-keepalive.C:
			// Send SSE keepalive comment; if the write fails the client has disconnected.
			if _, err := fmt.Fprintf(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// PublishEvent is a public method to push an event to all SSE subscribers.
func (s *Server) PublishEvent(event EventItem) {
	if s.eventBus != nil {
		s.eventBus.Publish(event)
	}
}
