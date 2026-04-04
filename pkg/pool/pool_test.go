package pool

import (
	"bytes"
	"testing"
)

func TestBufferPool(t *testing.T) {
	pool := NewBufferPool()

	// Get a buffer
	buf1 := pool.Get()
	if buf1 == nil {
		t.Fatal("Get() returned nil")
	}

	// Write some data
	buf1.WriteString("test data")

	// Return it
	pool.Put(buf1)

	// Get another buffer - should be the same one (reset)
	buf2 := pool.Get()
	if buf2.Len() != 0 {
		t.Error("Buffer should be reset")
	}

	// Test with large buffer (should be dropped)
	largeBuf := bytes.NewBuffer(make([]byte, 0, 100000))
	pool.Put(largeBuf)
}

func TestBytePool(t *testing.T) {
	pool := NewBytePool(1024)

	// Get a slice
	slice1 := pool.Get()
	if len(slice1) != 1024 {
		t.Errorf("Expected length 1024, got %d", len(slice1))
	}

	// Modify it
	slice1[0] = 42

	// Return it
	pool.Put(slice1)

	// Get another slice
	slice2 := pool.Get()
	if len(slice2) != 1024 {
		t.Errorf("Expected length 1024, got %d", len(slice2))
	}

	// Wrong size should not be returned to pool
	wrongSize := make([]byte, 512)
	pool.Put(wrongSize)
}

func TestDefaultBufferPool(t *testing.T) {
	// Test global instance
	buf := DefaultBufferPool.Get()
	if buf == nil {
		t.Fatal("DefaultBufferPool.Get() returned nil")
	}
	DefaultBufferPool.Put(buf)
}

func BenchmarkBufferPoolGetPut(b *testing.B) {
	pool := NewBufferPool()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := pool.Get()
			buf.WriteString("test")
			pool.Put(buf)
		}
	})
}

func BenchmarkBufferPoolNoPool(b *testing.B) {
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := bytes.NewBuffer(make([]byte, 0, 4096))
			buf.WriteString("test")
		}
	})
}

func BenchmarkBytePoolGetPut(b *testing.B) {
	pool := NewBytePool(4096)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := pool.Get()
			buf[0] = 1
			pool.Put(buf)
		}
	})
}

func BenchmarkBytePoolNoPool(b *testing.B) {
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := make([]byte, 4096)
			buf[0] = 1
		}
	})
}
