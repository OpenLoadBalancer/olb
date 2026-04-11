package backend

import (
	"sync"

	"github.com/openloadbalancer/olb/pkg/errors"
)

// PoolManager manages multiple backend pools with thread-safe operations.
type PoolManager struct {
	mu    sync.RWMutex
	pools map[string]*Pool
}

// NewPoolManager creates a new PoolManager.
func NewPoolManager() *PoolManager {
	return &PoolManager{
		pools: make(map[string]*Pool),
	}
}

// AddPool adds a pool to the manager.
// Returns ErrAlreadyExist if a pool with the same name already exists.
func (pm *PoolManager) AddPool(pool *Pool) error {
	if pool == nil {
		return errors.ErrInvalidArg.WithContext("reason", "pool is nil")
	}
	if pool.Name == "" {
		return errors.ErrInvalidArg.WithContext("reason", "pool name is empty")
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.pools[pool.Name]; exists {
		return errors.ErrAlreadyExist.WithContext("pool_name", pool.Name)
	}

	pm.pools[pool.Name] = pool
	return nil
}

// RemovePool removes a pool from the manager.
// Returns ErrPoolNotFound if the pool doesn't exist.
func (pm *PoolManager) RemovePool(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.pools[name]; !exists {
		return errors.ErrPoolNotFound.WithContext("pool_name", name)
	}

	delete(pm.pools, name)
	return nil
}

// GetPool returns a pool by name.
// Returns nil if not found.
func (pm *PoolManager) GetPool(name string) *Pool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.pools[name]
}

// GetAllPools returns a slice of all pools.
func (pm *PoolManager) GetAllPools() []*Pool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make([]*Pool, 0, len(pm.pools))
	for _, p := range pm.pools {
		result = append(result, p)
	}
	return result
}

// GetBackend retrieves a backend from a specific pool.
// Returns nil if the pool or backend doesn't exist.
func (pm *PoolManager) GetBackend(poolName, backendID string) *Backend {
	pool := pm.GetPool(poolName)
	if pool == nil {
		return nil
	}
	return pool.GetBackend(backendID)
}

// GetBackendAcrossPools searches for a backend across all pools.
// Returns the backend and the pool name it was found in.
// Returns nil and empty string if not found.
func (pm *PoolManager) GetBackendAcrossPools(backendID string) (*Backend, string) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for poolName, pool := range pm.pools {
		if b := pool.GetBackend(backendID); b != nil {
			return b, poolName
		}
	}
	return nil, ""
}

// PoolExists returns true if a pool with the given name exists.
func (pm *PoolManager) PoolExists(name string) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	_, exists := pm.pools[name]
	return exists
}

// BackendCount returns the total number of backends across all pools.
func (pm *PoolManager) BackendCount() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	count := 0
	for _, pool := range pm.pools {
		count += pool.BackendCount()
	}
	return count
}

// HealthyBackendCount returns the total number of healthy backends across all pools.
func (pm *PoolManager) HealthyBackendCount() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	count := 0
	for _, pool := range pm.pools {
		count += pool.HealthyCount()
	}
	return count
}

// BackendStats returns aggregated statistics for all pools.
func (pm *PoolManager) BackendStats() map[string]PoolStats {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make(map[string]PoolStats, len(pm.pools))
	for name, pool := range pm.pools {
		result[name] = pool.Stats()
	}
	return result
}

// Snapshot creates a consistent snapshot of all pools.
// Useful for config reloads or debugging.
func (pm *PoolManager) Snapshot() map[string]*Pool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make(map[string]*Pool, len(pm.pools))
	for name, pool := range pm.pools {
		result[name] = pool.Clone()
	}
	return result
}

// Clear removes all pools from the manager.
func (pm *PoolManager) Clear() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.pools = make(map[string]*Pool)
}

// PoolCount returns the number of pools.
func (pm *PoolManager) PoolCount() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.pools)
}

// GetBackendByAddress searches for a backend across all pools by its address.
// Returns the backend if found, nil otherwise.
func (pm *PoolManager) GetBackendByAddress(addr string) *Backend {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, pool := range pm.pools {
		for _, b := range pool.Backends {
			if b.Address == addr {
				return b
			}
		}
	}
	return nil
}
