// Package pool provides object pools for reducing allocations.
package pool

import (
	"bytes"
	"sync"
)

// BufferPool is a pool of reusable bytes.Buffer objects.
// It helps reduce GC pressure in high-throughput scenarios.
type BufferPool struct {
	pool sync.Pool
}

// NewBufferPool creates a new buffer pool.
func NewBufferPool() *BufferPool {
	return &BufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				return bytes.NewBuffer(make([]byte, 0, 4096))
			},
		},
	}
}

// Get retrieves a buffer from the pool.
// The buffer is reset and ready for use.
func (p *BufferPool) Get() *bytes.Buffer {
	buf := p.pool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// Put returns a buffer to the pool.
func (p *BufferPool) Put(buf *bytes.Buffer) {
	// Limit max capacity to prevent memory bloat
	if buf.Cap() > 65536 {
		return // Drop large buffers
	}
	p.pool.Put(buf)
}

// DefaultBufferPool is the global buffer pool instance.
var DefaultBufferPool = NewBufferPool()

// BytePool is a pool of reusable byte slices.
type BytePool struct {
	pool sync.Pool
	size int
}

// NewBytePool creates a new byte slice pool with fixed size.
func NewBytePool(size int) *BytePool {
	return &BytePool{
		pool: sync.Pool{
			New: func() interface{} {
				return make([]byte, size)
			},
		},
		size: size,
	}
}

// Get retrieves a byte slice from the pool.
func (p *BytePool) Get() []byte {
	return p.pool.Get().([]byte)
}

// Put returns a byte slice to the pool.
func (p *BytePool) Put(buf []byte) {
	if len(buf) != p.size {
		return // Only return correct size
	}
	p.pool.Put(buf)
}

// Common byte pool sizes
var (
	// BytePool4K for small buffers (headers, small requests)
	BytePool4K = NewBytePool(4096)

	// BytePool16K for medium buffers
	BytePool16K = NewBytePool(16384)

	// BytePool64K for large buffers
	BytePool64K = NewBytePool(65536)
)
