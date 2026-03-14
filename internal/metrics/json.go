package metrics

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// JSONHandler handles JSON format metrics output.
type JSONHandler struct {
	registry *Registry
}

// NewJSONHandler creates a new JSON handler.
func NewJSONHandler(registry *Registry) *JSONHandler {
	if registry == nil {
		registry = DefaultRegistry
	}
	return &JSONHandler{registry: registry}
}

// WriteTo writes JSON format metrics to w.
func (h *JSONHandler) WriteTo(w io.Writer) error {
	metrics := make(map[string]interface{})

	// Collect all metrics
	h.registry.Collect(
		func(name string, c *Counter) {
			metrics[name] = map[string]interface{}{
				"type":  "counter",
				"help":  c.Help(),
				"value": c.Get(),
			}
		},
		func(name string, g *Gauge) {
			metrics[name] = map[string]interface{}{
				"type":  "gauge",
				"help":  g.Help(),
				"value": g.Get(),
			}
		},
		func(name string, hist *Histogram) {
			snap := hist.Snapshot()
			buckets := make(map[string]int64)
			for i, bound := range snap.Bounds {
				key := formatFloat(bound)
				buckets[key] = snap.Buckets[i]
			}
			// +Inf bucket
			buckets["+Inf"] = snap.Buckets[len(snap.Buckets)-1]

			metrics[name] = map[string]interface{}{
				"type":    "histogram",
				"help":    hist.Help(),
				"count":   snap.Count,
				"sum":     snap.Sum,
				"buckets": buckets,
			}
		},
		func(name string, cv *CounterVec) {
			vectors := make(map[string]int64)
			cv.Collect(func(labels map[string]string, c *Counter) {
				key := formatLabelMap(labels)
				vectors[key] = c.Get()
			})
			metrics[name] = map[string]interface{}{
				"type":   "counter_vec",
				"help":   cv.Help(),
				"labels": cv.Labels(),
				"values": vectors,
			}
		},
		func(name string, gv *GaugeVec) {
			vectors := make(map[string]float64)
			gv.Collect(func(labels map[string]string, g *Gauge) {
				key := formatLabelMap(labels)
				vectors[key] = g.Get()
			})
			metrics[name] = map[string]interface{}{
				"type":   "gauge_vec",
				"help":   gv.Help(),
				"labels": gv.Labels(),
				"values": vectors,
			}
		},
		func(name string, hv *HistogramVec) {
			vectors := make(map[string]interface{})
			hv.Collect(func(labels map[string]string, hist *Histogram) {
				key := formatLabelMap(labels)
				snap := hist.Snapshot()
				buckets := make(map[string]int64)
				for i, bound := range snap.Bounds {
					buckets[formatFloat(bound)] = snap.Buckets[i]
				}
				buckets["+Inf"] = snap.Buckets[len(snap.Buckets)-1]

				vectors[key] = map[string]interface{}{
					"count":   snap.Count,
					"sum":     snap.Sum,
					"buckets": buckets,
				}
			})
			metrics[name] = map[string]interface{}{
				"type":   "histogram_vec",
				"help":   hv.Help(),
				"labels": hv.Labels(),
				"values": vectors,
			}
		},
	)

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(metrics)
}

// formatFloat formats a float as a compact string.
func formatFloat(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%g", f)
}

// formatLabelMap formats a label map as a string.
func formatLabelMap(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteByte('{')
	first := true
	for k, v := range labels {
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v)
	}
	b.WriteByte('}')
	return b.String()
}
