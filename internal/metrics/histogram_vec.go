package metrics

import (
	"fmt"
	"strings"
	"sync"
)

// HistogramVec is a vector of histograms with labels.
type HistogramVec struct {
	name       string
	help       string
	labels     []string
	buckets    []float64
	histograms sync.Map // map[string]*Histogram
}

// NewHistogramVec creates a new histogram vector with default buckets.
func NewHistogramVec(name, help string, labels []string) *HistogramVec {
	return NewHistogramVecWithBuckets(name, help, labels, DefaultBuckets)
}

// NewHistogramVecWithBuckets creates a new histogram vector with custom buckets.
func NewHistogramVecWithBuckets(name, help string, labels []string, buckets []float64) *HistogramVec {
	return &HistogramVec{
		name:    name,
		help:    help,
		labels:  labels,
		buckets: buckets,
	}
}

// Name returns the histogram vector name.
func (hv *HistogramVec) Name() string {
	return hv.name
}

// Help returns the histogram vector help text.
func (hv *HistogramVec) Help() string {
	return hv.help
}

// Labels returns the label names.
func (hv *HistogramVec) Labels() []string {
	return hv.labels
}

// With returns a histogram for the given label values.
func (hv *HistogramVec) With(labelValues ...string) *Histogram {
	key := hv.makeKey(labelValues)

	if h, ok := hv.histograms.Load(key); ok {
		return h.(*Histogram)
	}

	h := NewHistogramWithBuckets(hv.name, hv.help, hv.buckets)
	actual, _ := hv.histograms.LoadOrStore(key, h)
	return actual.(*Histogram)
}

// WithLabels returns a histogram for the given label map.
func (hv *HistogramVec) WithLabels(labels map[string]string) *Histogram {
	return hv.With(hv.extractValues(labels)...)
}

// makeKey creates a key from label values.
func (hv *HistogramVec) makeKey(values []string) string {
	return strings.Join(values, "\x00")
}

// extractValues extracts label values from a map in order.
func (hv *HistogramVec) extractValues(labels map[string]string) []string {
	values := make([]string, len(hv.labels))
	for i, name := range hv.labels {
		if v, ok := labels[name]; ok {
			values[i] = v
		} else {
			values[i] = ""
		}
	}
	return values
}

// Delete removes a histogram with the given label values.
func (hv *HistogramVec) Delete(labelValues ...string) {
	key := hv.makeKey(labelValues)
	hv.histograms.Delete(key)
}

// Reset clears all histograms.
func (hv *HistogramVec) Reset() {
	hv.histograms = sync.Map{}
}

// Collect calls fn for each histogram in the vector.
func (hv *HistogramVec) Collect(fn func(labels map[string]string, h *Histogram)) {
	hv.histograms.Range(func(key, value any) bool {
		labels := hv.parseKey(key.(string))
		fn(labels, value.(*Histogram))
		return true
	})
}

// parseKey parses a key into label values.
func (hv *HistogramVec) parseKey(key string) map[string]string {
	values := strings.Split(key, "\x00")
	labels := make(map[string]string, len(hv.labels))
	for i, name := range hv.labels {
		if i < len(values) {
			labels[name] = values[i]
		} else {
			labels[name] = ""
		}
	}
	return labels
}

// Ensure all label values are provided.
func (hv *HistogramVec) validateValues(values []string) error {
	if len(values) != len(hv.labels) {
		return fmt.Errorf("expected %d label values, got %d", len(hv.labels), len(values))
	}
	return nil
}
