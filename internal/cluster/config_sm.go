// Package cluster provides distributed clustering and consensus using Raft.
// This file implements a Config State Machine for replicating configuration
// changes across cluster nodes via Raft consensus.
package cluster

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/openloadbalancer/olb/internal/config"
)

// ConfigCommandType represents the type of configuration change command.
type ConfigCommandType string

const (
	// CmdSetConfig replaces the entire configuration.
	CmdSetConfig ConfigCommandType = "set_config"

	// CmdUpdateBackend updates a single backend within a pool.
	CmdUpdateBackend ConfigCommandType = "update_backend"

	// CmdUpdateRoute updates a route within a listener.
	CmdUpdateRoute ConfigCommandType = "update_route"

	// CmdUpdateListener updates a listener configuration.
	CmdUpdateListener ConfigCommandType = "update_listener"

	// CmdDeleteBackend removes a backend from a pool.
	CmdDeleteBackend ConfigCommandType = "delete_backend"

	// WAF command types
	CmdWAFAddWhitelist    ConfigCommandType = "waf_add_whitelist"
	CmdWAFRemoveWhitelist ConfigCommandType = "waf_remove_whitelist"
	CmdWAFAddBlacklist    ConfigCommandType = "waf_add_blacklist"
	CmdWAFRemoveBlacklist ConfigCommandType = "waf_remove_blacklist"
	CmdWAFAddRateRule     ConfigCommandType = "waf_add_rate_rule"
	CmdWAFRemoveRateRule  ConfigCommandType = "waf_remove_rate_rule"
	CmdWAFSetMode         ConfigCommandType = "waf_set_mode"
	CmdWAFSyncCounters    ConfigCommandType = "waf_sync_counters"
)

// WAFIPACLPayload is the payload for WAF IP ACL commands.
type WAFIPACLPayload struct {
	CIDR    string `json:"cidr"`
	Reason  string `json:"reason"`
	Expires string `json:"expires,omitempty"` // ISO 8601
}

// WAFRateRulePayload is the payload for WAF rate limit rule commands.
type WAFRateRulePayload struct {
	ID           string   `json:"id"`
	Scope        string   `json:"scope"`
	Paths        []string `json:"paths,omitempty"`
	Limit        int      `json:"limit"`
	Window       string   `json:"window"`
	Burst        int      `json:"burst,omitempty"`
	Action       string   `json:"action,omitempty"`
	AutoBanAfter int      `json:"auto_ban_after,omitempty"`
}

// WAFModePayload is the payload for WAF mode change commands.
type WAFModePayload struct {
	Layer string `json:"layer,omitempty"` // "detection", "bot", "rate_limit", "all"
	Mode  string `json:"mode"`            // "enforce", "monitor", "disabled"
}

// ConfigCommand represents a configuration change command to be applied via Raft.
type ConfigCommand struct {
	// Type is the kind of configuration change.
	Type ConfigCommandType `json:"type"`

	// Payload is the JSON-encoded payload for the command.
	Payload json.RawMessage `json:"payload"`
}

// UpdateBackendPayload is the payload for CmdUpdateBackend.
type UpdateBackendPayload struct {
	Pool    string          `json:"pool"`
	Backend *config.Backend `json:"backend"`
}

// UpdateRoutePayload is the payload for CmdUpdateRoute.
type UpdateRoutePayload struct {
	Listener string        `json:"listener"`
	Route    *config.Route `json:"route"`
}

// UpdateListenerPayload is the payload for CmdUpdateListener.
type UpdateListenerPayload struct {
	Listener *config.Listener `json:"listener"`
}

// DeleteBackendPayload is the payload for CmdDeleteBackend.
type DeleteBackendPayload struct {
	Pool      string `json:"pool"`
	BackendID string `json:"backend_id"`
}

// ConfigStateMachine implements the StateMachine interface for configuration
// replication. It stores the current Config as the Raft state and applies
// configuration change commands proposed through the cluster.
type ConfigStateMachine struct {
	mu     sync.RWMutex
	config *config.Config

	// onConfigApplied is called after a config change is successfully committed.
	onConfigApplied func(*config.Config)

	// wg tracks in-flight callback goroutines so callers can wait for drain.
	wg sync.WaitGroup

	// onWAFCommand is called when a WAF-specific command is applied via Raft.
	// WAF middleware registers this callback to apply WAF state changes.
	onWAFCommand func(cmdType ConfigCommandType, payload json.RawMessage) error
}

// SetWAFCommandHandler registers a callback for WAF Raft commands.
func (sm *ConfigStateMachine) SetWAFCommandHandler(handler func(ConfigCommandType, json.RawMessage) error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.onWAFCommand = handler
}

// NewConfigStateMachine creates a new ConfigStateMachine with an optional
// initial configuration. If cfg is nil, a default empty config is used.
func NewConfigStateMachine(cfg *config.Config) *ConfigStateMachine {
	if cfg == nil {
		cfg = &config.Config{
			Version: "1",
		}
	}
	return &ConfigStateMachine{
		config: cfg,
	}
}

// Apply applies a serialized ConfigCommand to the state machine.
// It returns the resulting config as JSON bytes, or an error if the
// command is malformed or cannot be applied.
func (sm *ConfigStateMachine) Apply(command []byte) ([]byte, error) {
	var cmd ConfigCommand
	if err := json.Unmarshal(command, &cmd); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config command: %w", err)
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	var err error
	switch cmd.Type {
	case CmdSetConfig:
		err = sm.applySetConfig(cmd.Payload)
	case CmdUpdateBackend:
		err = sm.applyUpdateBackend(cmd.Payload)
	case CmdUpdateRoute:
		err = sm.applyUpdateRoute(cmd.Payload)
	case CmdUpdateListener:
		err = sm.applyUpdateListener(cmd.Payload)
	case CmdDeleteBackend:
		err = sm.applyDeleteBackend(cmd.Payload)
	case CmdWAFAddWhitelist, CmdWAFRemoveWhitelist,
		CmdWAFAddBlacklist, CmdWAFRemoveBlacklist,
		CmdWAFAddRateRule, CmdWAFRemoveRateRule,
		CmdWAFSetMode, CmdWAFSyncCounters:
		if sm.onWAFCommand != nil {
			err = sm.onWAFCommand(cmd.Type, cmd.Payload)
		}
	default:
		return nil, fmt.Errorf("unknown config command type: %s", cmd.Type)
	}

	if err != nil {
		return nil, err
	}

	// Notify callback
	if sm.onConfigApplied != nil {
		cfgCopy := sm.cloneConfigLocked()
		sm.wg.Add(1)
		go func() {
			defer sm.wg.Done()
			defer func() {
				if r := recover(); r != nil {
					slog.Error("config callback panic recovered", "error", r)
				}
			}()
			sm.onConfigApplied(cfgCopy)
		}()
	}

	result, err := json.Marshal(sm.config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal resulting config: %w", err)
	}
	return result, nil
}

// Snapshot serializes the current configuration state to bytes.
func (sm *ConfigStateMachine) Snapshot() ([]byte, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	data, err := json.Marshal(sm.config)
	if err != nil {
		return nil, fmt.Errorf("failed to snapshot config: %w", err)
	}
	return data, nil
}

// Restore replaces the current configuration state from a snapshot.
func (sm *ConfigStateMachine) Restore(snapshot []byte) error {
	var cfg config.Config
	if err := json.Unmarshal(snapshot, &cfg); err != nil {
		return fmt.Errorf("failed to restore config snapshot: %w", err)
	}

	sm.mu.Lock()
	sm.config = &cfg
	sm.mu.Unlock()

	return nil
}

// GetCurrentConfig returns a copy of the current configuration.
func (sm *ConfigStateMachine) GetCurrentConfig() *config.Config {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.cloneConfigLocked()
}

// OnConfigApplied sets a callback function that is invoked after each
// successful configuration change. The callback receives a copy of the
// new configuration.
func (sm *ConfigStateMachine) OnConfigApplied(fn func(*config.Config)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.onConfigApplied = fn
}

// WaitCallbacks blocks until all in-flight config callback goroutines finish.
func (sm *ConfigStateMachine) WaitCallbacks() {
	sm.wg.Wait()
}

// applySetConfig replaces the entire configuration.
func (sm *ConfigStateMachine) applySetConfig(payload json.RawMessage) error {
	var cfg config.Config
	if err := json.Unmarshal(payload, &cfg); err != nil {
		return fmt.Errorf("failed to unmarshal SetConfig payload: %w", err)
	}
	sm.config = &cfg
	return nil
}

// applyUpdateBackend updates or adds a backend in the specified pool.
func (sm *ConfigStateMachine) applyUpdateBackend(payload json.RawMessage) error {
	var p UpdateBackendPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("failed to unmarshal UpdateBackend payload: %w", err)
	}
	if p.Pool == "" {
		return fmt.Errorf("pool name is required for UpdateBackend")
	}
	if p.Backend == nil {
		return fmt.Errorf("backend is required for UpdateBackend")
	}

	// Find the pool
	for _, pool := range sm.config.Pools {
		if pool.Name == p.Pool {
			// Look for existing backend by ID
			for i, b := range pool.Backends {
				if b.ID == p.Backend.ID {
					pool.Backends[i] = p.Backend
					return nil
				}
			}
			// Backend not found, add it
			pool.Backends = append(pool.Backends, p.Backend)
			return nil
		}
	}

	return fmt.Errorf("pool %q not found", p.Pool)
}

// applyUpdateRoute updates or adds a route in the specified listener.
func (sm *ConfigStateMachine) applyUpdateRoute(payload json.RawMessage) error {
	var p UpdateRoutePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("failed to unmarshal UpdateRoute payload: %w", err)
	}
	if p.Listener == "" {
		return fmt.Errorf("listener name is required for UpdateRoute")
	}
	if p.Route == nil {
		return fmt.Errorf("route is required for UpdateRoute")
	}

	// Find the listener
	for _, listener := range sm.config.Listeners {
		if listener.Name == p.Listener {
			// Look for existing route by path+host combination
			for i, r := range listener.Routes {
				if r.Path == p.Route.Path && r.Host == p.Route.Host {
					listener.Routes[i] = p.Route
					return nil
				}
			}
			// Route not found, add it
			listener.Routes = append(listener.Routes, p.Route)
			return nil
		}
	}

	return fmt.Errorf("listener %q not found", p.Listener)
}

// applyUpdateListener updates or adds a listener.
func (sm *ConfigStateMachine) applyUpdateListener(payload json.RawMessage) error {
	var p UpdateListenerPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("failed to unmarshal UpdateListener payload: %w", err)
	}
	if p.Listener == nil {
		return fmt.Errorf("listener is required for UpdateListener")
	}
	if p.Listener.Name == "" {
		return fmt.Errorf("listener name is required for UpdateListener")
	}

	// Look for existing listener by name
	for i, l := range sm.config.Listeners {
		if l.Name == p.Listener.Name {
			sm.config.Listeners[i] = p.Listener
			return nil
		}
	}

	// Listener not found, add it
	sm.config.Listeners = append(sm.config.Listeners, p.Listener)
	return nil
}

// cloneConfigLocked creates a deep copy of the config. Caller must hold at least a read lock.
func (sm *ConfigStateMachine) cloneConfigLocked() *config.Config {
	data, err := json.Marshal(sm.config)
	if err != nil {
		// Should not happen with a valid config
		return sm.config
	}
	var clone config.Config
	if err := json.Unmarshal(data, &clone); err != nil {
		return sm.config
	}
	return &clone
}

// ProposeConfigChange proposes a configuration change through the Raft cluster.
// The command is serialized and submitted to the cluster leader for replication.
// Returns an error if the proposal fails or the node is not the leader.
func ProposeConfigChange(c *Cluster, cmd ConfigCommand) error {
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal config command: %w", err)
	}

	result, err := c.Propose(data)
	if err != nil {
		return fmt.Errorf("failed to propose config change: %w", err)
	}

	if result.Error != nil {
		return fmt.Errorf("config change rejected: %w", result.Error)
	}

	return nil
}

// NewSetConfigCommand creates a ConfigCommand that replaces the entire config.
func NewSetConfigCommand(cfg *config.Config) (ConfigCommand, error) {
	payload, err := json.Marshal(cfg)
	if err != nil {
		return ConfigCommand{}, fmt.Errorf("failed to marshal config: %w", err)
	}
	return ConfigCommand{
		Type:    CmdSetConfig,
		Payload: payload,
	}, nil
}

// NewUpdateBackendCommand creates a ConfigCommand that updates a backend in a pool.
func NewUpdateBackendCommand(pool string, backend *config.Backend) (ConfigCommand, error) {
	payload, err := json.Marshal(UpdateBackendPayload{
		Pool:    pool,
		Backend: backend,
	})
	if err != nil {
		return ConfigCommand{}, fmt.Errorf("failed to marshal update backend payload: %w", err)
	}
	return ConfigCommand{
		Type:    CmdUpdateBackend,
		Payload: payload,
	}, nil
}

// NewUpdateRouteCommand creates a ConfigCommand that updates a route in a listener.
func NewUpdateRouteCommand(listener string, route *config.Route) (ConfigCommand, error) {
	payload, err := json.Marshal(UpdateRoutePayload{
		Listener: listener,
		Route:    route,
	})
	if err != nil {
		return ConfigCommand{}, fmt.Errorf("failed to marshal update route payload: %w", err)
	}
	return ConfigCommand{
		Type:    CmdUpdateRoute,
		Payload: payload,
	}, nil
}

// NewUpdateListenerCommand creates a ConfigCommand that updates a listener.
func NewUpdateListenerCommand(listener *config.Listener) (ConfigCommand, error) {
	payload, err := json.Marshal(UpdateListenerPayload{
		Listener: listener,
	})
	if err != nil {
		return ConfigCommand{}, fmt.Errorf("failed to marshal update listener payload: %w", err)
	}
	return ConfigCommand{
		Type:    CmdUpdateListener,
		Payload: payload,
	}, nil
}

// applyDeleteBackend removes a backend from the specified pool.
func (sm *ConfigStateMachine) applyDeleteBackend(payload json.RawMessage) error {
	var p DeleteBackendPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("failed to unmarshal DeleteBackend payload: %w", err)
	}
	if p.Pool == "" {
		return fmt.Errorf("pool name is required for DeleteBackend")
	}
	if p.BackendID == "" {
		return fmt.Errorf("backend_id is required for DeleteBackend")
	}

	for _, pool := range sm.config.Pools {
		if pool.Name == p.Pool {
			for i, b := range pool.Backends {
				if b.ID == p.BackendID {
					pool.Backends = append(pool.Backends[:i], pool.Backends[i+1:]...)
					return nil
				}
			}
			return fmt.Errorf("backend %q not found in pool %q", p.BackendID, p.Pool)
		}
	}

	return fmt.Errorf("pool %q not found", p.Pool)
}

// NewDeleteBackendCommand creates a ConfigCommand that removes a backend from a pool.
func NewDeleteBackendCommand(pool, backendID string) (ConfigCommand, error) {
	payload, err := json.Marshal(DeleteBackendPayload{
		Pool:      pool,
		BackendID: backendID,
	})
	if err != nil {
		return ConfigCommand{}, fmt.Errorf("failed to marshal delete backend payload: %w", err)
	}
	return ConfigCommand{
		Type:    CmdDeleteBackend,
		Payload: payload,
	}, nil
}
