package metrics

import (
	"fmt"
	"io"
	"strings"
)

// PrometheusHandler handles Prometheus exposition format output.
type PrometheusHandler struct {
	registry *Registry
}

// NewPrometheusHandler creates a new Prometheus handler.
func NewPrometheusHandler(registry *Registry) *PrometheusHandler {
	if registry == nil {
		registry = DefaultRegistry
	}
	return &PrometheusHandler{registry: registry}
}

// WriteTo writes Prometheus format metrics to w.
func (h *PrometheusHandler) WriteTo(w io.Writer) error {
	// Collect and write all metrics
	h.registry.Collect(
		func(name string, c *Counter) {
			h.writeCounter(w, name, c)
		},
		func(name string, g *Gauge) {
			h.writeGauge(w, name, g)
		},
		func(name string, hist *Histogram) {
			h.writeHistogram(w, name, hist)
		},
		func(name string, cv *CounterVec) {
			h.writeCounterVec(w, name, cv)
		},
		func(name string, gv *GaugeVec) {
			h.writeGaugeVec(w, name, gv)
		},
		func(name string, hv *HistogramVec) {
			h.writeHistogramVec(w, name, hv)
		},
	)
	return nil
}

// writeCounter writes a counter in Prometheus format.
func (h *PrometheusHandler) writeCounter(w io.Writer, name string, c *Counter) {
	if c.Help() != "" {
		fmt.Fprintf(w, "# HELP %s %s\n", name, escapeHelp(c.Help()))
	}
	fmt.Fprintf(w, "# TYPE %s counter\n", name)
	fmt.Fprintf(w, "%s %d\n", name, c.Get())
}

// writeGauge writes a gauge in Prometheus format.
func (h *PrometheusHandler) writeGauge(w io.Writer, name string, g *Gauge) {
	if g.Help() != "" {
		fmt.Fprintf(w, "# HELP %s %s\n", name, escapeHelp(g.Help()))
	}
	fmt.Fprintf(w, "# TYPE %s gauge\n", name)
	fmt.Fprintf(w, "%s %g\n", name, g.Get())
}

// writeHistogram writes a histogram in Prometheus format.
func (h *PrometheusHandler) writeHistogram(w io.Writer, name string, hist *Histogram) {
	if hist.Help() != "" {
		fmt.Fprintf(w, "# HELP %s %s\n", name, escapeHelp(hist.Help()))
	}
	fmt.Fprintf(w, "# TYPE %s histogram\n", name)

	snap := hist.Snapshot()

	// Write buckets
	for i, bound := range snap.Bounds {
		fmt.Fprintf(w, "%s_bucket{le=\"%g\"} %d\n", name, bound, snap.Buckets[i])
	}
	// +Inf bucket
	fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", name, snap.Buckets[len(snap.Buckets)-1])

	// Sum and count
	fmt.Fprintf(w, "%s_sum %g\n", name, snap.Sum)
	fmt.Fprintf(w, "%s_count %d\n", name, snap.Count)
}

// writeCounterVec writes a counter vector in Prometheus format.
func (h *PrometheusHandler) writeCounterVec(w io.Writer, name string, cv *CounterVec) {
	if cv.Help() != "" {
		fmt.Fprintf(w, "# HELP %s %s\n", name, escapeHelp(cv.Help()))
	}
	fmt.Fprintf(w, "# TYPE %s counter\n", name)

	cv.Collect(func(labels map[string]string, c *Counter) {
		labelStr := formatLabels(labels)
		fmt.Fprintf(w, "%s%s %d\n", name, labelStr, c.Get())
	})
}

// writeGaugeVec writes a gauge vector in Prometheus format.
func (h *PrometheusHandler) writeGaugeVec(w io.Writer, name string, gv *GaugeVec) {
	if gv.Help() != "" {
		fmt.Fprintf(w, "# HELP %s %s\n", name, escapeHelp(gv.Help()))
	}
	fmt.Fprintf(w, "# TYPE %s gauge\n", name)

	gv.Collect(func(labels map[string]string, g *Gauge) {
		labelStr := formatLabels(labels)
		fmt.Fprintf(w, "%s%s %g\n", name, labelStr, g.Get())
	})
}

// writeHistogramVec writes a histogram vector in Prometheus format.
func (h *PrometheusHandler) writeHistogramVec(w io.Writer, name string, hv *HistogramVec) {
	if hv.Help() != "" {
		fmt.Fprintf(w, "# HELP %s %s\n", name, escapeHelp(hv.Help()))
	}
	fmt.Fprintf(w, "# TYPE %s histogram\n", name)

	hv.Collect(func(labels map[string]string, hist *Histogram) {
		snap := hist.Snapshot()
		labelStr := formatLabels(labels)

		// Write buckets
		for i, bound := range snap.Bounds {
			fmt.Fprintf(w, "%s_bucket%s{le=\"%g\"} %d\n", name, labelStr, bound, snap.Buckets[i])
		}
		// +Inf bucket
		fmt.Fprintf(w, "%s_bucket%s{le=\"+Inf\"} %d\n", name, labelStr, snap.Buckets[len(snap.Buckets)-1])

		// Sum and count
		fmt.Fprintf(w, "%s_sum%s %g\n", name, labelStr, snap.Sum)
		fmt.Fprintf(w, "%s_count%s %d\n", name, labelStr, snap.Count)
	})
}

// escapeHelp escapes special characters in help text.
func escapeHelp(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

// formatLabels formats labels as a Prometheus label string.
func formatLabels(labels map[string]string) string {
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
		b.WriteByte('"')
		b.WriteString(escapeLabel(v))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

// escapeLabel escapes special characters in label values.
func escapeLabel(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}
