package metrics

import (
	"fmt"
	"sync"
)

// Registry is a metrics registry for pre-registered metrics.
type Registry struct {
	counters   sync.Map // map[string]*Counter
	gauges     sync.Map // map[string]*Gauge
	histograms sync.Map // map[string]*Histogram
	counterVecs sync.Map // map[string]*CounterVec
	gaugeVecs   sync.Map // map[string]*GaugeVec
	histogramVecs sync.Map // map[string]*HistogramVec
}

// NewRegistry creates a new metrics registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// RegisterCounter registers a counter.
func (r *Registry) RegisterCounter(c *Counter) error {
	name := c.Name()
	if _, loaded := r.counters.LoadOrStore(name, c); loaded {
		return fmt.Errorf("counter %s already registered", name)
	}
	return nil
}

// RegisterGauge registers a gauge.
func (r *Registry) RegisterGauge(g *Gauge) error {
	name := g.Name()
	if _, loaded := r.gauges.LoadOrStore(name, g); loaded {
		return fmt.Errorf("gauge %s already registered", name)
	}
	return nil
}

// RegisterHistogram registers a histogram.
func (r *Registry) RegisterHistogram(h *Histogram) error {
	name := h.Name()
	if _, loaded := r.histograms.LoadOrStore(name, h); loaded {
		return fmt.Errorf("histogram %s already registered", name)
	}
	return nil
}

// RegisterCounterVec registers a counter vector.
func (r *Registry) RegisterCounterVec(cv *CounterVec) error {
	name := cv.Name()
	if _, loaded := r.counterVecs.LoadOrStore(name, cv); loaded {
		return fmt.Errorf("counter vec %s already registered", name)
	}
	return nil
}

// RegisterGaugeVec registers a gauge vector.
func (r *Registry) RegisterGaugeVec(gv *GaugeVec) error {
	name := gv.Name()
	if _, loaded := r.gaugeVecs.LoadOrStore(name, gv); loaded {
		return fmt.Errorf("gauge vec %s already registered", name)
	}
	return nil
}

// RegisterHistogramVec registers a histogram vector.
func (r *Registry) RegisterHistogramVec(hv *HistogramVec) error {
	name := hv.Name()
	if _, loaded := r.histogramVecs.LoadOrStore(name, hv); loaded {
		return fmt.Errorf("histogram vec %s already registered", name)
	}
	return nil
}

// GetCounter returns a registered counter.
func (r *Registry) GetCounter(name string) *Counter {
	if c, ok := r.counters.Load(name); ok {
		return c.(*Counter)
	}
	return nil
}

// GetGauge returns a registered gauge.
func (r *Registry) GetGauge(name string) *Gauge {
	if g, ok := r.gauges.Load(name); ok {
		return g.(*Gauge)
	}
	return nil
}

// GetHistogram returns a registered histogram.
func (r *Registry) GetHistogram(name string) *Histogram {
	if h, ok := r.histograms.Load(name); ok {
		return h.(*Histogram)
	}
	return nil
}

// GetCounterVec returns a registered counter vector.
func (r *Registry) GetCounterVec(name string) *CounterVec {
	if cv, ok := r.counterVecs.Load(name); ok {
		return cv.(*CounterVec)
	}
	return nil
}

// GetGaugeVec returns a registered gauge vector.
func (r *Registry) GetGaugeVec(name string) *GaugeVec {
	if gv, ok := r.gaugeVecs.Load(name); ok {
		return gv.(*GaugeVec)
	}
	return nil
}

// GetHistogramVec returns a registered histogram vector.
func (r *Registry) GetHistogramVec(name string) *HistogramVec {
	if hv, ok := r.histogramVecs.Load(name); ok {
		return hv.(*HistogramVec)
	}
	return nil
}

// Unregister removes a metric from the registry.
func (r *Registry) Unregister(name string) {
	r.counters.Delete(name)
	r.gauges.Delete(name)
	r.histograms.Delete(name)
	r.counterVecs.Delete(name)
	r.gaugeVecs.Delete(name)
	r.histogramVecs.Delete(name)
}

// Collect calls the given functions for all registered metrics.
func (r *Registry) Collect(
	counterFn func(name string, c *Counter),
	gaugeFn func(name string, g *Gauge),
	histogramFn func(name string, h *Histogram),
	counterVecFn func(name string, cv *CounterVec),
	gaugeVecFn func(name string, gv *GaugeVec),
	histogramVecFn func(name string, hv *HistogramVec),
) {
	r.counters.Range(func(key, value interface{}) bool {
		counterFn(key.(string), value.(*Counter))
		return true
	})
	r.gauges.Range(func(key, value interface{}) bool {
		gaugeFn(key.(string), value.(*Gauge))
		return true
	})
	r.histograms.Range(func(key, value interface{}) bool {
		histogramFn(key.(string), value.(*Histogram))
		return true
	})
	r.counterVecs.Range(func(key, value interface{}) bool {
		counterVecFn(key.(string), value.(*CounterVec))
		return true
	})
	r.gaugeVecs.Range(func(key, value interface{}) bool {
		gaugeVecFn(key.(string), value.(*GaugeVec))
		return true
	})
	r.histogramVecs.Range(func(key, value interface{}) bool {
		histogramVecFn(key.(string), value.(*HistogramVec))
		return true
	})
}

// Reset clears all registered metrics.
func (r *Registry) Reset() {
	r.counters = sync.Map{}
	r.gauges = sync.Map{}
	r.histograms = sync.Map{}
	r.counterVecs = sync.Map{}
	r.gaugeVecs = sync.Map{}
	r.histogramVecs = sync.Map{}
}

// DefaultRegistry is the default global registry.
var DefaultRegistry = NewRegistry()

// RegisterCounter registers a counter to the default registry.
func RegisterCounter(c *Counter) error {
	return DefaultRegistry.RegisterCounter(c)
}

// RegisterGauge registers a gauge to the default registry.
func RegisterGauge(g *Gauge) error {
	return DefaultRegistry.RegisterGauge(g)
}

// RegisterHistogram registers a histogram to the default registry.
func RegisterHistogram(h *Histogram) error {
	return DefaultRegistry.RegisterHistogram(h)
}

// RegisterCounterVec registers a counter vector to the default registry.
func RegisterCounterVec(cv *CounterVec) error {
	return DefaultRegistry.RegisterCounterVec(cv)
}

// RegisterGaugeVec registers a gauge vector to the default registry.
func RegisterGaugeVec(gv *GaugeVec) error {
	return DefaultRegistry.RegisterGaugeVec(gv)
}

// RegisterHistogramVec registers a histogram vector to the default registry.
func RegisterHistogramVec(hv *HistogramVec) error {
	return DefaultRegistry.RegisterHistogramVec(hv)
}
