// Package balancer provides load balancing algorithms for OpenLoadBalancer.
package balancer

import (
	"github.com/openloadbalancer/olb/internal/backend"
)

// Balancer is the interface for load balancing algorithms.
type Balancer interface {
	// Name returns the name of the balancer algorithm.
	Name() string

	// Next selects the next backend from the provided list.
	// Returns nil if no backend is available.
	Next(backends []*backend.Backend) *backend.Backend

	// Add adds a backend to the balancer.
	Add(backend *backend.Backend)

	// Remove removes a backend from the balancer by ID.
	Remove(id string)

	// Update updates a backend's state in the balancer.
	Update(backend *backend.Backend)
}

// Factory is a function that creates a new Balancer instance.
type Factory func() Balancer

var (
	// registry maps algorithm names to their factory functions.
	registry = make(map[string]Factory)
)

// Register registers a balancer factory with the given name.
// Panics if the name is already registered.
func Register(name string, factory Factory) {
	if _, exists := registry[name]; exists {
		panic("balancer: " + name + " already registered")
	}
	registry[name] = factory
}

// Get returns a balancer factory by name.
// Returns nil if the balancer is not registered.
func Get(name string) Factory {
	return registry[name]
}

// New creates a new Balancer instance by name.
// Returns nil if the balancer is not registered.
func New(name string) Balancer {
	if factory := Get(name); factory != nil {
		return factory()
	}
	return nil
}

// Names returns a list of registered balancer names.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}

func init() {
	// Register built-in balancers
	Register("round_robin", func() Balancer { return NewRoundRobin() })
	Register("rr", func() Balancer { return NewRoundRobin() }) // alias
	Register("weighted_round_robin", func() Balancer { return NewWeightedRoundRobin() })
	Register("wrr", func() Balancer { return NewWeightedRoundRobin() }) // alias
	Register("ip_hash", func() Balancer { return NewIPHash() })
	Register("iphash", func() Balancer { return NewIPHash() }) // alias
	Register("least_connections", func() Balancer { return NewLeastConnections() })
	Register("lc", func() Balancer { return NewLeastConnections() }) // alias
	Register("weighted_least_connections", func() Balancer { return NewWeightedLeastConnections() })
	Register("wlc", func() Balancer { return NewWeightedLeastConnections() }) // alias
	Register("random", func() Balancer { return NewRandom() })
	Register("weighted_random", func() Balancer { return NewWeightedRandom() })
	Register("wrandom", func() Balancer { return NewWeightedRandom() }) // alias
	Register("consistent_hash", func() Balancer { return NewConsistentHash(DefaultVirtualNodes) })
	Register("ch", func() Balancer { return NewConsistentHash(DefaultVirtualNodes) })     // alias
	Register("ketama", func() Balancer { return NewConsistentHash(DefaultVirtualNodes) }) // alias
	Register("power_of_two", func() Balancer { return NewPowerOfTwo() })
	Register("p2c", func() Balancer { return NewPowerOfTwo() }) // alias
	Register("least_response_time", func() Balancer { return NewLeastResponseTime() })
	Register("lrt", func() Balancer { return NewLeastResponseTime() }) // alias
	Register("weighted_least_response_time", func() Balancer { return NewWeightedLeastResponseTime() })
	Register("wlrt", func() Balancer { return NewWeightedLeastResponseTime() }) // alias
	Register("maglev", func() Balancer { return NewMaglev() })
	Register("ring_hash", func() Balancer { return NewRingHash() })
	Register("ringhash", func() Balancer { return NewRingHash() }) // alias
	Register("rendezvous", func() Balancer { return NewRendezvousHash() })
	Register("rendezvous_hash", func() Balancer { return NewRendezvousHash() }) // alias
	Register("peak_ewma", func() Balancer { return NewPeakEWMA() })
	Register("pewma", func() Balancer { return NewPeakEWMA() }) // alias
	Register("sticky", func() Balancer { return NewSticky(NewRoundRobin(), nil) })
}
