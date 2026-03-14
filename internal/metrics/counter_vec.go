package metrics

import (
	"fmt"
	"strings"
	"sync"
)

// CounterVec is a vector of counters with labels.
type CounterVec struct {
	name    string
	help    string
	labels  []string
	counters sync.Map // map[string]*Counter
}

// NewCounterVec creates a new counter vector.
func NewCounterVec(name, help string, labels []string) *CounterVec {
	return &CounterVec{
		name:   name,
		help:   help,
		labels: labels,
	}
}

// Name returns the counter vector name.
func (cv *CounterVec) Name() string {
	return cv.name
}

// Help returns the counter vector help text.
func (cv *CounterVec) Help() string {
	return cv.help
}

// Labels returns the label names.
func (cv *CounterVec) Labels() []string {
	return cv.labels
}

// With returns a counter for the given label values.
func (cv *CounterVec) With(labelValues ...string) *Counter {
	key := cv.makeKey(labelValues)

	if c, ok := cv.counters.Load(key); ok {
		return c.(*Counter)
	}

	c := &Counter{
		name: cv.name,
		help: cv.help,
	}
	actual, _ := cv.counters.LoadOrStore(key, c)
	return actual.(*Counter)
}

// WithLabels returns a counter for the given label map.
func (cv *CounterVec) WithLabels(labels map[string]string) *Counter {
	return cv.With(cv.extractValues(labels)...)
}

// makeKey creates a key from label values.
func (cv *CounterVec) makeKey(values []string) string {
	return strings.Join(values, "\x00")
}

// extractValues extracts label values from a map in order.
func (cv *CounterVec) extractValues(labels map[string]string) []string {
	values := make([]string, len(cv.labels))
	for i, name := range cv.labels {
		if v, ok := labels[name]; ok {
			values[i] = v
		} else {
			values[i] = ""
		}
	}
	return values
}

// Delete removes a counter with the given label values.
func (cv *CounterVec) Delete(labelValues ...string) {
	key := cv.makeKey(labelValues)
	cv.counters.Delete(key)
}

// Reset clears all counters.
func (cv *CounterVec) Reset() {
	cv.counters = sync.Map{}
}

// Collect calls fn for each counter in the vector.
func (cv *CounterVec) Collect(fn func(labels map[string]string, c *Counter)) {
	cv.counters.Range(func(key, value interface{}) bool {
		labels := cv.parseKey(key.(string))
		fn(labels, value.(*Counter))
		return true
	})
}

// parseKey parses a key into label values.
func (cv *CounterVec) parseKey(key string) map[string]string {
	values := strings.Split(key, "\x00")
	labels := make(map[string]string, len(cv.labels))
	for i, name := range cv.labels {
		if i < len(values) {
			labels[name] = values[i]
		} else {
			labels[name] = ""
		}
	}
	return labels
}

// Ensure all label values are provided.
func (cv *CounterVec) validateValues(values []string) error {
	if len(values) != len(cv.labels) {
		return fmt.Errorf("expected %d label values, got %d", len(cv.labels), len(values))
	}
	return nil
}
