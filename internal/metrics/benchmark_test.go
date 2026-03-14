package metrics

import (
	"testing"
)

func BenchmarkCounter_Inc(b *testing.B) {
	c := NewCounter("bench", "bench")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Inc()
	}
}

func BenchmarkCounter_Inc_Parallel(b *testing.B) {
	c := NewCounter("bench", "bench")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Inc()
		}
	})
}

func BenchmarkGauge_Set(b *testing.B) {
	g := NewGauge("bench", "bench")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Set(float64(i))
	}
}

func BenchmarkGauge_Add(b *testing.B) {
	g := NewGauge("bench", "bench")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Add(1)
	}
}

func BenchmarkGauge_Add_Parallel(b *testing.B) {
	g := NewGauge("bench", "bench")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			g.Add(1)
		}
	})
}

func BenchmarkHistogram_Observe(b *testing.B) {
	h := NewHistogram("bench", "bench")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Observe(float64(i % 100))
	}
}

func BenchmarkHistogram_Observe_Parallel(b *testing.B) {
	h := NewHistogram("bench", "bench")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			h.Observe(float64(i % 100))
			i++
		}
	})
}
