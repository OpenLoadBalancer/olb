package metrics

import "testing"

func TestRegistry_VecOrRegister(t *testing.T) {
	r := NewRegistry()

	cv := NewCounterVec("c_total", "c", []string{"k"})
	if got := r.CounterVecOrRegister(cv); got != cv {
		t.Fatal("first CounterVecOrRegister should register and return its argument")
	}
	if got := r.CounterVecOrRegister(NewCounterVec("c_total", "c", []string{"k"})); got != cv {
		t.Error("second CounterVecOrRegister should return the registered vector")
	}

	gv := NewGaugeVec("g", "g", []string{"k"})
	if r.GaugeVecOrRegister(gv) != gv || r.GaugeVecOrRegister(NewGaugeVec("g", "g", []string{"k"})) != gv {
		t.Error("GaugeVecOrRegister did not keep the first registered vector")
	}

	hv := NewHistogramVec("h", "h", []string{"k"})
	if r.HistogramVecOrRegister(hv) != hv || r.HistogramVecOrRegister(NewHistogramVec("h", "h", []string{"k"})) != hv {
		t.Error("HistogramVecOrRegister did not keep the first registered vector")
	}
}
