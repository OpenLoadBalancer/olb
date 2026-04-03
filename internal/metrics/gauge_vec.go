package metrics

import (
	"fmt"
	"strings"
	"sync"
)

// GaugeVec is a vector of gauges with labels.
type GaugeVec struct {
	name   string
	help   string
	labels []string
	gauges sync.Map // map[string]*Gauge
}

// NewGaugeVec creates a new gauge vector.
func NewGaugeVec(name, help string, labels []string) *GaugeVec {
	return &GaugeVec{
		name:   name,
		help:   help,
		labels: labels,
	}
}

// Name returns the gauge vector name.
func (gv *GaugeVec) Name() string {
	return gv.name
}

// Help returns the gauge vector help text.
func (gv *GaugeVec) Help() string {
	return gv.help
}

// Labels returns the label names.
func (gv *GaugeVec) Labels() []string {
	return gv.labels
}

// With returns a gauge for the given label values.
func (gv *GaugeVec) With(labelValues ...string) *Gauge {
	key := gv.makeKey(labelValues)

	if g, ok := gv.gauges.Load(key); ok {
		return g.(*Gauge)
	}

	g := &Gauge{
		name: gv.name,
		help: gv.help,
	}
	actual, _ := gv.gauges.LoadOrStore(key, g)
	return actual.(*Gauge)
}

// WithLabels returns a gauge for the given label map.
func (gv *GaugeVec) WithLabels(labels map[string]string) *Gauge {
	return gv.With(gv.extractValues(labels)...)
}

// makeKey creates a key from label values.
func (gv *GaugeVec) makeKey(values []string) string {
	return strings.Join(values, "\x00")
}

// extractValues extracts label values from a map in order.
func (gv *GaugeVec) extractValues(labels map[string]string) []string {
	values := make([]string, len(gv.labels))
	for i, name := range gv.labels {
		if v, ok := labels[name]; ok {
			values[i] = v
		} else {
			values[i] = ""
		}
	}
	return values
}

// Delete removes a gauge with the given label values.
func (gv *GaugeVec) Delete(labelValues ...string) {
	key := gv.makeKey(labelValues)
	gv.gauges.Delete(key)
}

// Reset clears all gauges.
func (gv *GaugeVec) Reset() {
	gv.gauges = sync.Map{}
}

// Collect calls fn for each gauge in the vector.
func (gv *GaugeVec) Collect(fn func(labels map[string]string, g *Gauge)) {
	gv.gauges.Range(func(key, value any) bool {
		labels := gv.parseKey(key.(string))
		fn(labels, value.(*Gauge))
		return true
	})
}

// parseKey parses a key into label values.
func (gv *GaugeVec) parseKey(key string) map[string]string {
	values := strings.Split(key, "\x00")
	labels := make(map[string]string, len(gv.labels))
	for i, name := range gv.labels {
		if i < len(values) {
			labels[name] = values[i]
		} else {
			labels[name] = ""
		}
	}
	return labels
}

// Ensure all label values are provided.
func (gv *GaugeVec) validateValues(values []string) error {
	if len(values) != len(gv.labels) {
		return fmt.Errorf("expected %d label values, got %d", len(gv.labels), len(values))
	}
	return nil
}
