package metrics

import (
	"sync/atomic"
)

// Counter is an atomic int64 counter.
type Counter struct {
	value atomic.Int64
	name  string
	help  string
}

// NewCounter creates a new counter.
func NewCounter(name, help string) *Counter {
	return &Counter{
		name: name,
		help: help,
	}
}

// Name returns the counter name.
func (c *Counter) Name() string {
	return c.name
}

// Help returns the counter help text.
func (c *Counter) Help() string {
	return c.help
}

// Inc increments the counter by 1.
func (c *Counter) Inc() {
	c.value.Add(1)
}

// Add adds n to the counter.
func (c *Counter) Add(n int64) {
	c.value.Add(n)
}

// Get returns the current counter value.
func (c *Counter) Get() int64 {
	return c.value.Load()
}

// Reset resets the counter to 0.
func (c *Counter) Reset() {
	c.value.Store(0)
}
