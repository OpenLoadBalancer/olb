package waf

import (
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

// WAFEvent represents a structured WAF decision event.
type WAFEvent struct {
	Timestamp  time.Time `json:"timestamp"`
	RequestID  string    `json:"request_id"`
	RemoteIP   string    `json:"remote_ip"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	UserAgent  string    `json:"user_agent"`
	Layer      string    `json:"layer"`  // "ip_acl", "rate_limit", "sanitizer", "detection", "bot", "response"
	Action     string    `json:"action"` // "allow", "block", "challenge", "log", "bypass"
	TotalScore int       `json:"total_score"`
	Findings   []Finding `json:"findings,omitempty"`
	LatencyNS  int64     `json:"latency_ns"`
	NodeID     string    `json:"node_id"`
	Message    string    `json:"message,omitempty"`
}

// EventLogger handles structured WAF event logging.
type EventLogger struct {
	mu         sync.Mutex
	writer     io.Writer
	nodeID     string
	logAllowed bool
	logBlocked bool
	logBody    bool
	analytics  *Analytics
	metrics    *WAFMetrics
}

// EventLoggerConfig configures the event logger.
type EventLoggerConfig struct {
	Writer     io.Writer
	NodeID     string
	LogAllowed bool
	LogBlocked bool
	LogBody    bool
	Analytics  *Analytics
	Metrics    *WAFMetrics
}

// NewEventLogger creates a new EventLogger.
func NewEventLogger(cfg EventLoggerConfig) *EventLogger {
	w := cfg.Writer
	if w == nil {
		w = os.Stderr
	}
	return &EventLogger{
		writer:     w,
		nodeID:     cfg.NodeID,
		logAllowed: cfg.LogAllowed,
		logBlocked: cfg.LogBlocked,
		analytics:  cfg.Analytics,
		metrics:    cfg.Metrics,
	}
}

// LogEvent logs a WAF event.
func (el *EventLogger) LogEvent(evt *WAFEvent) {
	if evt == nil {
		return
	}

	evt.NodeID = el.nodeID
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}

	// Update analytics
	if el.analytics != nil {
		el.analytics.Record(evt)
	}

	// Update Prometheus metrics
	if el.metrics != nil {
		el.metrics.RecordRequest(evt.Action)
		if evt.Action == "block" {
			el.metrics.RecordBlock(evt.Layer)
		}
		for _, f := range evt.Findings {
			el.metrics.RecordDetectorHit(f.Detector)
		}
		if evt.LatencyNS > 0 {
			el.metrics.RecordLatency(evt.Layer, float64(evt.LatencyNS)/1e9)
		}
	}

	// Check if we should log this event
	switch evt.Action {
	case "allow", "bypass":
		if !el.logAllowed {
			return
		}
	case "block", "challenge":
		if !el.logBlocked {
			return
		}
	}

	data, err := json.Marshal(evt)
	if err != nil {
		return
	}

	el.mu.Lock()
	el.writer.Write(append(data, '\n'))
	el.mu.Unlock()
}

// NewEvent creates a WAFEvent from a RequestContext.
func NewEvent(ctx *RequestContext, layer, action string) *WAFEvent {
	evt := &WAFEvent{
		Timestamp: time.Now(),
		Layer:     layer,
		Action:    action,
	}
	if ctx != nil {
		evt.RemoteIP = ctx.RemoteIP
		evt.Method = ctx.Method
		evt.Path = ctx.Path
		if ctx.Request != nil {
			evt.UserAgent = ctx.Request.UserAgent()
			evt.RequestID = ctx.Request.Header.Get("X-Request-ID")
		}
	}
	return evt
}
