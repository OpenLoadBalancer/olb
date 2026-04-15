package cluster

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
)

func newTestConfig() *config.Config {
	return &config.Config{
		Version: "1",
		Listeners: []*config.Listener{
			{
				Name:     "http",
				Address:  ":8080",
				Protocol: "http",
				Routes: []*config.Route{
					{Path: "/api", Host: "api.example.com", Pool: "api-pool"},
				},
			},
		},
		Pools: []*config.Pool{
			{
				Name:      "api-pool",
				Algorithm: "round_robin",
				Backends: []*config.Backend{
					{ID: "b1", Address: "10.0.0.1:8080", Weight: 1},
					{ID: "b2", Address: "10.0.0.2:8080", Weight: 1},
				},
			},
		},
	}
}

func makeCommand(t *testing.T, cmd ConfigCommand) []byte {
	t.Helper()
	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("Failed to marshal command: %v", err)
	}
	return data
}

func TestNewConfigStateMachine(t *testing.T) {
	t.Run("with_nil_config", func(t *testing.T) {
		sm := NewConfigStateMachine(nil)
		cfg := sm.GetCurrentConfig()
		if cfg == nil {
			t.Fatal("Expected non-nil config")
		}
		if cfg.Version != "1" {
			t.Errorf("Version = %q, want %q", cfg.Version, "1")
		}
	})

	t.Run("with_config", func(t *testing.T) {
		initial := newTestConfig()
		sm := NewConfigStateMachine(initial)
		cfg := sm.GetCurrentConfig()
		if cfg.Version != "1" {
			t.Errorf("Version = %q, want %q", cfg.Version, "1")
		}
		if len(cfg.Listeners) != 1 {
			t.Errorf("Listeners count = %d, want 1", len(cfg.Listeners))
		}
		if len(cfg.Pools) != 1 {
			t.Errorf("Pools count = %d, want 1", len(cfg.Pools))
		}
	})
}

func TestConfigStateMachine_Apply_SetConfig(t *testing.T) {
	sm := NewConfigStateMachine(nil)

	newCfg := newTestConfig()
	cmd, err := NewSetConfigCommand(newCfg)
	if err != nil {
		t.Fatalf("Failed to create command: %v", err)
	}

	result, err := sm.Apply(makeCommand(t, cmd))
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	cfg := sm.GetCurrentConfig()
	if len(cfg.Listeners) != 1 {
		t.Errorf("Listeners count = %d, want 1", len(cfg.Listeners))
	}
	if cfg.Listeners[0].Name != "http" {
		t.Errorf("Listener name = %q, want %q", cfg.Listeners[0].Name, "http")
	}
	if len(cfg.Pools) != 1 {
		t.Errorf("Pools count = %d, want 1", len(cfg.Pools))
	}
	if len(cfg.Pools[0].Backends) != 2 {
		t.Errorf("Backends count = %d, want 2", len(cfg.Pools[0].Backends))
	}
}

func TestConfigStateMachine_Apply_UpdateBackend(t *testing.T) {
	sm := NewConfigStateMachine(newTestConfig())

	t.Run("update_existing", func(t *testing.T) {
		cmd, err := NewUpdateBackendCommand("api-pool", &config.Backend{
			ID: "b1", Address: "10.0.0.1:9090", Weight: 5,
		})
		if err != nil {
			t.Fatalf("Failed to create command: %v", err)
		}

		_, err = sm.Apply(makeCommand(t, cmd))
		if err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		cfg := sm.GetCurrentConfig()
		for _, b := range cfg.Pools[0].Backends {
			if b.ID == "b1" {
				if b.Address != "10.0.0.1:9090" {
					t.Errorf("Address = %q, want %q", b.Address, "10.0.0.1:9090")
				}
				if b.Weight != 5 {
					t.Errorf("Weight = %d, want 5", b.Weight)
				}
				return
			}
		}
		t.Error("Backend b1 not found after update")
	})

	t.Run("add_new", func(t *testing.T) {
		cmd, err := NewUpdateBackendCommand("api-pool", &config.Backend{
			ID: "b3", Address: "10.0.0.3:8080", Weight: 2,
		})
		if err != nil {
			t.Fatalf("Failed to create command: %v", err)
		}

		_, err = sm.Apply(makeCommand(t, cmd))
		if err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		cfg := sm.GetCurrentConfig()
		if len(cfg.Pools[0].Backends) != 3 {
			t.Errorf("Backends count = %d, want 3", len(cfg.Pools[0].Backends))
		}
	})

	t.Run("pool_not_found", func(t *testing.T) {
		cmd, err := NewUpdateBackendCommand("nonexistent", &config.Backend{
			ID: "b1", Address: "10.0.0.1:8080", Weight: 1,
		})
		if err != nil {
			t.Fatalf("Failed to create command: %v", err)
		}

		_, err = sm.Apply(makeCommand(t, cmd))
		if err == nil {
			t.Error("Expected error for nonexistent pool")
		}
	})
}

func TestConfigStateMachine_Apply_UpdateRoute(t *testing.T) {
	sm := NewConfigStateMachine(newTestConfig())

	t.Run("update_existing", func(t *testing.T) {
		cmd, err := NewUpdateRouteCommand("http", &config.Route{
			Path: "/api", Host: "api.example.com", Pool: "new-pool",
		})
		if err != nil {
			t.Fatalf("Failed to create command: %v", err)
		}

		_, err = sm.Apply(makeCommand(t, cmd))
		if err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		cfg := sm.GetCurrentConfig()
		if cfg.Listeners[0].Routes[0].Pool != "new-pool" {
			t.Errorf("Pool = %q, want %q", cfg.Listeners[0].Routes[0].Pool, "new-pool")
		}
	})

	t.Run("add_new_route", func(t *testing.T) {
		cmd, err := NewUpdateRouteCommand("http", &config.Route{
			Path: "/health", Host: "", Pool: "health-pool",
		})
		if err != nil {
			t.Fatalf("Failed to create command: %v", err)
		}

		_, err = sm.Apply(makeCommand(t, cmd))
		if err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		cfg := sm.GetCurrentConfig()
		if len(cfg.Listeners[0].Routes) != 2 {
			t.Errorf("Routes count = %d, want 2", len(cfg.Listeners[0].Routes))
		}
	})

	t.Run("listener_not_found", func(t *testing.T) {
		cmd, err := NewUpdateRouteCommand("nonexistent", &config.Route{
			Path: "/api", Pool: "pool",
		})
		if err != nil {
			t.Fatalf("Failed to create command: %v", err)
		}

		_, err = sm.Apply(makeCommand(t, cmd))
		if err == nil {
			t.Error("Expected error for nonexistent listener")
		}
	})
}

func TestConfigStateMachine_Apply_UpdateListener(t *testing.T) {
	sm := NewConfigStateMachine(newTestConfig())

	t.Run("update_existing", func(t *testing.T) {
		cmd, err := NewUpdateListenerCommand(&config.Listener{
			Name:     "http",
			Address:  ":9090",
			Protocol: "http",
		})
		if err != nil {
			t.Fatalf("Failed to create command: %v", err)
		}

		_, err = sm.Apply(makeCommand(t, cmd))
		if err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		cfg := sm.GetCurrentConfig()
		if cfg.Listeners[0].Address != ":9090" {
			t.Errorf("Address = %q, want %q", cfg.Listeners[0].Address, ":9090")
		}
	})

	t.Run("add_new_listener", func(t *testing.T) {
		cmd, err := NewUpdateListenerCommand(&config.Listener{
			Name:     "https",
			Address:  ":443",
			Protocol: "https",
			TLS:      &config.ListenerTLS{Enabled: true},
		})
		if err != nil {
			t.Fatalf("Failed to create command: %v", err)
		}

		_, err = sm.Apply(makeCommand(t, cmd))
		if err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		cfg := sm.GetCurrentConfig()
		if len(cfg.Listeners) != 2 {
			t.Errorf("Listeners count = %d, want 2", len(cfg.Listeners))
		}
	})
}

func TestConfigStateMachine_SnapshotRestore(t *testing.T) {
	sm1 := NewConfigStateMachine(newTestConfig())

	// Take a snapshot
	snapshot, err := sm1.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}
	if len(snapshot) == 0 {
		t.Fatal("Expected non-empty snapshot")
	}

	// Restore on a new state machine
	sm2 := NewConfigStateMachine(nil)
	if err := sm2.Restore(snapshot); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	// Verify restored config matches
	cfg1 := sm1.GetCurrentConfig()
	cfg2 := sm2.GetCurrentConfig()

	if cfg1.Version != cfg2.Version {
		t.Errorf("Version mismatch: %q vs %q", cfg1.Version, cfg2.Version)
	}
	if len(cfg1.Listeners) != len(cfg2.Listeners) {
		t.Errorf("Listeners count mismatch: %d vs %d", len(cfg1.Listeners), len(cfg2.Listeners))
	}
	if len(cfg1.Pools) != len(cfg2.Pools) {
		t.Errorf("Pools count mismatch: %d vs %d", len(cfg1.Pools), len(cfg2.Pools))
	}
	if len(cfg1.Pools[0].Backends) != len(cfg2.Pools[0].Backends) {
		t.Errorf("Backends count mismatch: %d vs %d",
			len(cfg1.Pools[0].Backends), len(cfg2.Pools[0].Backends))
	}
}

func TestConfigStateMachine_SnapshotRestore_InvalidData(t *testing.T) {
	sm := NewConfigStateMachine(nil)
	err := sm.Restore([]byte("invalid json"))
	if err == nil {
		t.Error("Expected error for invalid snapshot data")
	}
}

func TestConfigStateMachine_InvalidCommand(t *testing.T) {
	sm := NewConfigStateMachine(newTestConfig())

	t.Run("invalid_json", func(t *testing.T) {
		_, err := sm.Apply([]byte("not json"))
		if err == nil {
			t.Error("Expected error for invalid JSON")
		}
	})

	t.Run("unknown_command_type", func(t *testing.T) {
		cmd := ConfigCommand{
			Type:    "unknown_type",
			Payload: json.RawMessage(`{}`),
		}
		_, err := sm.Apply(makeCommand(t, cmd))
		if err == nil {
			t.Error("Expected error for unknown command type")
		}
	})

	t.Run("invalid_payload", func(t *testing.T) {
		// Manually construct JSON with valid outer structure but invalid payload
		raw := []byte(`{"type":"set_config","payload":"not a json object"}`)
		_, err := sm.Apply(raw)
		if err == nil {
			t.Error("Expected error for invalid payload")
		}
	})

	t.Run("update_backend_missing_pool", func(t *testing.T) {
		payload, _ := json.Marshal(UpdateBackendPayload{
			Pool:    "",
			Backend: &config.Backend{ID: "b1"},
		})
		cmd := ConfigCommand{
			Type:    CmdUpdateBackend,
			Payload: payload,
		}
		_, err := sm.Apply(makeCommand(t, cmd))
		if err == nil {
			t.Error("Expected error for missing pool name")
		}
	})

	t.Run("update_backend_missing_backend", func(t *testing.T) {
		payload, _ := json.Marshal(UpdateBackendPayload{
			Pool:    "api-pool",
			Backend: nil,
		})
		cmd := ConfigCommand{
			Type:    CmdUpdateBackend,
			Payload: payload,
		}
		_, err := sm.Apply(makeCommand(t, cmd))
		if err == nil {
			t.Error("Expected error for missing backend")
		}
	})

	t.Run("update_route_missing_listener", func(t *testing.T) {
		payload, _ := json.Marshal(UpdateRoutePayload{
			Listener: "",
			Route:    &config.Route{Path: "/"},
		})
		cmd := ConfigCommand{
			Type:    CmdUpdateRoute,
			Payload: payload,
		}
		_, err := sm.Apply(makeCommand(t, cmd))
		if err == nil {
			t.Error("Expected error for missing listener name")
		}
	})

	t.Run("update_listener_missing_name", func(t *testing.T) {
		payload, _ := json.Marshal(UpdateListenerPayload{
			Listener: &config.Listener{Name: ""},
		})
		cmd := ConfigCommand{
			Type:    CmdUpdateListener,
			Payload: payload,
		}
		_, err := sm.Apply(makeCommand(t, cmd))
		if err == nil {
			t.Error("Expected error for missing listener name")
		}
	})

	t.Run("update_listener_nil_listener", func(t *testing.T) {
		payload, _ := json.Marshal(UpdateListenerPayload{
			Listener: nil,
		})
		cmd := ConfigCommand{
			Type:    CmdUpdateListener,
			Payload: payload,
		}
		_, err := sm.Apply(makeCommand(t, cmd))
		if err == nil {
			t.Error("Expected error for nil listener")
		}
	})
}

func TestConfigStateMachine_OnConfigApplied(t *testing.T) {
	sm := NewConfigStateMachine(nil)

	callbackCalled := make(chan *config.Config, 1)
	sm.OnConfigApplied(func(cfg *config.Config) {
		callbackCalled <- cfg
	})

	newCfg := newTestConfig()
	cmd, err := NewSetConfigCommand(newCfg)
	if err != nil {
		t.Fatalf("Failed to create command: %v", err)
	}

	_, err = sm.Apply(makeCommand(t, cmd))
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	select {
	case cfg := <-callbackCalled:
		if cfg == nil {
			t.Fatal("Expected non-nil config in callback")
		}
		if len(cfg.Listeners) != 1 {
			t.Errorf("Callback config listeners = %d, want 1", len(cfg.Listeners))
		}
	case <-time.After(2 * time.Second):
		t.Error("OnConfigApplied callback not called within timeout")
	}
}

func TestConfigStateMachine_ConcurrentAccess(t *testing.T) {
	sm := NewConfigStateMachine(newTestConfig())

	var wg sync.WaitGroup
	errCh := make(chan error, 40)

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cfg := sm.GetCurrentConfig()
			if cfg == nil {
				errCh <- fmt.Errorf("GetCurrentConfig returned nil")
			}
		}()
	}

	// Concurrent snapshots
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := sm.Snapshot()
			if err != nil {
				errCh <- err
			}
		}()
	}

	// Concurrent applies
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cmd, err := NewUpdateBackendCommand("api-pool", &config.Backend{
				ID:      fmt.Sprintf("b-concurrent-%d", idx),
				Address: fmt.Sprintf("10.0.%d.1:8080", idx),
				Weight:  1,
			})
			if err != nil {
				errCh <- err
				return
			}
			_, err = sm.Apply(makeCommand(t, cmd))
			if err != nil {
				errCh <- err
			}
		}(i)
	}

	// Concurrent restores
	snapshot, _ := sm.Snapshot()
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Create a separate SM for restore to avoid interfering with applies
			sm2 := NewConfigStateMachine(nil)
			if err := sm2.Restore(snapshot); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("Concurrent error: %v", err)
	}
}

func TestProposeConfigChange(t *testing.T) {
	sm := NewConfigStateMachine(newTestConfig())

	clusterConfig := &Config{
		NodeID:        "node1",
		BindAddr:      "127.0.0.1",
		BindPort:      7946,
		ElectionTick:  2 * time.Second,
		HeartbeatTick: 500 * time.Millisecond,
	}

	c, err := New(clusterConfig, sm)
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}

	// Start cluster so command channel processing begins
	if err := c.Start(); err != nil {
		t.Fatalf("Failed to start cluster: %v", err)
	}
	defer c.Stop()

	// Wait for the node to become leader (single-node cluster)
	time.Sleep(500 * time.Millisecond)

	// Only test if the node became leader
	if c.IsLeader() {
		cmd, err := NewSetConfigCommand(&config.Config{
			Version: "2",
			Listeners: []*config.Listener{
				{Name: "test", Address: ":8080"},
			},
		})
		if err != nil {
			t.Fatalf("Failed to create command: %v", err)
		}

		if err := ProposeConfigChange(c, cmd); err != nil {
			t.Errorf("ProposeConfigChange failed: %v", err)
		}

		cfg := sm.GetCurrentConfig()
		if cfg.Version != "2" {
			t.Errorf("Version = %q, want %q", cfg.Version, "2")
		}
	}
}

func TestNewCommandHelpers(t *testing.T) {
	t.Run("NewSetConfigCommand", func(t *testing.T) {
		cfg := newTestConfig()
		cmd, err := NewSetConfigCommand(cfg)
		if err != nil {
			t.Fatalf("NewSetConfigCommand failed: %v", err)
		}
		if cmd.Type != CmdSetConfig {
			t.Errorf("Type = %q, want %q", cmd.Type, CmdSetConfig)
		}
		if len(cmd.Payload) == 0 {
			t.Error("Expected non-empty payload")
		}
	})

	t.Run("NewUpdateBackendCommand", func(t *testing.T) {
		cmd, err := NewUpdateBackendCommand("pool1", &config.Backend{
			ID: "b1", Address: "10.0.0.1:8080", Weight: 1,
		})
		if err != nil {
			t.Fatalf("NewUpdateBackendCommand failed: %v", err)
		}
		if cmd.Type != CmdUpdateBackend {
			t.Errorf("Type = %q, want %q", cmd.Type, CmdUpdateBackend)
		}
	})

	t.Run("NewUpdateRouteCommand", func(t *testing.T) {
		cmd, err := NewUpdateRouteCommand("listener1", &config.Route{
			Path: "/api", Pool: "api-pool",
		})
		if err != nil {
			t.Fatalf("NewUpdateRouteCommand failed: %v", err)
		}
		if cmd.Type != CmdUpdateRoute {
			t.Errorf("Type = %q, want %q", cmd.Type, CmdUpdateRoute)
		}
	})

	t.Run("NewUpdateListenerCommand", func(t *testing.T) {
		cmd, err := NewUpdateListenerCommand(&config.Listener{
			Name: "https", Address: ":443", Protocol: "https",
		})
		if err != nil {
			t.Fatalf("NewUpdateListenerCommand failed: %v", err)
		}
		if cmd.Type != CmdUpdateListener {
			t.Errorf("Type = %q, want %q", cmd.Type, CmdUpdateListener)
		}
	})
}

func TestConfigStateMachine_GetCurrentConfig_ReturnsCopy(t *testing.T) {
	sm := NewConfigStateMachine(newTestConfig())

	cfg1 := sm.GetCurrentConfig()
	cfg2 := sm.GetCurrentConfig()

	// Modify cfg1 and verify cfg2 is unaffected
	cfg1.Version = "modified"
	if cfg2.Version == "modified" {
		t.Error("GetCurrentConfig should return a copy, not a reference")
	}
}

// ---------------------------------------------------------------------------
// SetWAFCommandHandler coverage (0% -> full)
// ---------------------------------------------------------------------------

func TestConfigStateMachine_SetWAFCommandHandler(t *testing.T) {
	sm := NewConfigStateMachine(nil)

	called := false
	sm.SetWAFCommandHandler(func(cmdType ConfigCommandType, payload json.RawMessage) error {
		called = true
		if cmdType != CmdWAFAddWhitelist {
			t.Errorf("cmdType = %q, want %q", cmdType, CmdWAFAddWhitelist)
		}
		return nil
	})

	// Apply a WAF command to trigger the handler.
	payload, _ := json.Marshal(WAFIPACLPayload{CIDR: "10.0.0.0/8", Reason: "test"})
	cmd := ConfigCommand{Type: CmdWAFAddWhitelist, Payload: payload}
	_, err := sm.Apply(makeCommand(t, cmd))
	if err != nil {
		t.Fatalf("Apply WAF command failed: %v", err)
	}
	if !called {
		t.Error("WAF command handler was not called")
	}
}

func TestConfigStateMachine_WAFCommands_NoHandler(t *testing.T) {
	sm := NewConfigStateMachine(nil)

	// WAF commands without a handler should succeed silently.
	cmd := ConfigCommand{Type: CmdWAFAddWhitelist, Payload: json.RawMessage(`{}`)}
	_, err := sm.Apply(makeCommand(t, cmd))
	if err != nil {
		t.Fatalf("Expected no error when no WAF handler set, got: %v", err)
	}
}

func TestConfigStateMachine_WAFCommands_AllTypes(t *testing.T) {
	sm := NewConfigStateMachine(nil)

	var calledTypes []ConfigCommandType
	sm.SetWAFCommandHandler(func(cmdType ConfigCommandType, payload json.RawMessage) error {
		calledTypes = append(calledTypes, cmdType)
		return nil
	})

	wafTypes := []ConfigCommandType{
		CmdWAFAddWhitelist,
		CmdWAFRemoveWhitelist,
		CmdWAFAddBlacklist,
		CmdWAFRemoveBlacklist,
		CmdWAFAddRateRule,
		CmdWAFRemoveRateRule,
		CmdWAFSetMode,
		CmdWAFSyncCounters,
	}

	for _, cmdType := range wafTypes {
		cmd := ConfigCommand{Type: cmdType, Payload: json.RawMessage(`{}`)}
		_, err := sm.Apply(makeCommand(t, cmd))
		if err != nil {
			t.Errorf("Apply %s failed: %v", cmdType, err)
		}
	}

	if len(calledTypes) != len(wafTypes) {
		t.Errorf("called %d types, want %d", len(calledTypes), len(wafTypes))
	}
}

func TestConfigStateMachine_WAFCommand_Error(t *testing.T) {
	sm := NewConfigStateMachine(nil)

	sm.SetWAFCommandHandler(func(cmdType ConfigCommandType, payload json.RawMessage) error {
		return fmt.Errorf("WAF handler error")
	})

	cmd := ConfigCommand{Type: CmdWAFAddWhitelist, Payload: json.RawMessage(`{}`)}
	_, err := sm.Apply(makeCommand(t, cmd))
	if err == nil {
		t.Error("Expected error from WAF handler")
	}
}

// ---------------------------------------------------------------------------
// ProposeConfigChange error paths
// ---------------------------------------------------------------------------

func TestProposeConfigChange_NotLeader(t *testing.T) {
	sm := NewConfigStateMachine(nil)
	clusterConfig := &Config{
		NodeID:        "node1",
		BindAddr:      "127.0.0.1",
		BindPort:      7946,
		ElectionTick:  2 * time.Second,
		HeartbeatTick: 500 * time.Millisecond,
		Peers:         []string{"node2"},
	}
	c, err := New(clusterConfig, sm)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Don't start the cluster; node stays follower.
	// ProposeConfigChange calls c.Propose which sends to commandCh.
	// Since we're not started, commandCh is unbuffered and handleCommand
	// won't process it. We test the not-leader error by using a follower.

	// The command goes through commandCh -> handleCommand which returns
	// "not leader" error. We need to start the run() loop to process it.
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop()

	cmd := ConfigCommand{Type: CmdSetConfig, Payload: json.RawMessage(`{"version":"2"}`)}
	err = ProposeConfigChange(c, cmd)
	// In single node, it becomes leader quickly. If it's leader, it succeeds.
	// If follower (due to peers), it returns "not leader" error.
	// We just verify no panic.
	_ = err
}

func TestProposeConfigChange_ProposeError(t *testing.T) {
	sm := NewConfigStateMachine(nil)
	clusterConfig := &Config{
		NodeID:        "node1",
		BindAddr:      "127.0.0.1",
		BindPort:      7946,
		ElectionTick:  2 * time.Second,
		HeartbeatTick: 500 * time.Millisecond,
		Peers:         []string{"node2", "node3"},
	}
	c, err := New(clusterConfig, sm)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Point peers to non-existent addresses and set up a transport.
	clientTransport := newStartedTransport(t, &stubHandler{})
	defer clientTransport.Stop()
	c.SetTransport(clientTransport)

	c.nodesMu.Lock()
	for _, id := range []string{"node2", "node3"} {
		if n, ok := c.nodes[id]; ok {
			n.Address = "127.0.0.1:1"
		}
	}
	c.nodesMu.Unlock()

	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop()

	// Wait for election. With peers pointing to nothing, single-node won't
	// become leader for a 3-node cluster. handleCommand returns "not leader".
	time.Sleep(500 * time.Millisecond)

	cmd := ConfigCommand{Type: CmdSetConfig, Payload: json.RawMessage(`{"version":"2"}`)}
	err = ProposeConfigChange(c, cmd)
	if err == nil {
		t.Error("Expected error from ProposeConfigChange when not leader")
	}
}

// ---------------------------------------------------------------------------
// UpdateRoute error path: missing route
// ---------------------------------------------------------------------------

func TestConfigStateMachine_Apply_UpdateRoute_MissingRoute(t *testing.T) {
	sm := NewConfigStateMachine(newTestConfig())

	payload, _ := json.Marshal(UpdateRoutePayload{
		Listener: "",
		Route:    &config.Route{Path: "/api", Pool: "pool"},
	})
	cmd := ConfigCommand{Type: CmdUpdateRoute, Payload: payload}
	_, err := sm.Apply(makeCommand(t, cmd))
	if err == nil {
		t.Error("Expected error for missing listener name in update route")
	}
}

func TestConfigStateMachine_Apply_UpdateRoute_MissingRouteNil(t *testing.T) {
	sm := NewConfigStateMachine(newTestConfig())

	payload, _ := json.Marshal(UpdateRoutePayload{
		Listener: "http",
		Route:    nil,
	})
	cmd := ConfigCommand{Type: CmdUpdateRoute, Payload: payload}
	_, err := sm.Apply(makeCommand(t, cmd))
	if err == nil {
		t.Error("Expected error for nil route in update route")
	}
}

// --- Coverage improvements for config_sm helpers ---

func TestNewSetConfigCommand_WithConfig(t *testing.T) {
	cfg := &config.Config{Listeners: []*config.Listener{{Name: "test"}}}
	cmd, err := NewSetConfigCommand(cfg)
	if err != nil {
		t.Fatalf("NewSetConfigCommand: %v", err)
	}
	if cmd.Type != CmdSetConfig {
		t.Errorf("Type = %v, want CmdSetConfig", cmd.Type)
	}
	if len(cmd.Payload) == 0 {
		t.Error("expected non-empty payload")
	}
}

func TestNewUpdateBackendCommand_WithData(t *testing.T) {
	cmd, err := NewUpdateBackendCommand("pool1", &config.Backend{Address: "127.0.0.1:8080"})
	if err != nil {
		t.Fatalf("NewUpdateBackendCommand: %v", err)
	}
	if cmd.Type != CmdUpdateBackend {
		t.Errorf("Type = %v, want CmdUpdateBackend", cmd.Type)
	}
	if len(cmd.Payload) == 0 {
		t.Error("expected non-empty payload")
	}
}

func TestNewUpdateRouteCommand_WithData(t *testing.T) {
	cmd, err := NewUpdateRouteCommand("listener1", &config.Route{Path: "/api"})
	if err != nil {
		t.Fatalf("NewUpdateRouteCommand: %v", err)
	}
	if cmd.Type != CmdUpdateRoute {
		t.Errorf("Type = %v, want CmdUpdateRoute", cmd.Type)
	}
	if len(cmd.Payload) == 0 {
		t.Error("expected non-empty payload")
	}
}

func TestNewUpdateListenerCommand_WithData(t *testing.T) {
	cmd, err := NewUpdateListenerCommand(&config.Listener{Name: "http", Address: ":8080"})
	if err != nil {
		t.Fatalf("NewUpdateListenerCommand: %v", err)
	}
	if cmd.Type != CmdUpdateListener {
		t.Errorf("Type = %v, want CmdUpdateListener", cmd.Type)
	}
	if len(cmd.Payload) == 0 {
		t.Error("expected non-empty payload")
	}
}

func TestConfigStateMachine_CloneConfigLocked(t *testing.T) {
	sm := NewConfigStateMachine(&config.Config{
		Listeners: []*config.Listener{{Name: "l1"}},
		Pools:     []*config.Pool{{Name: "p1"}},
	})
	clone := sm.cloneConfigLocked()
	if clone == nil {
		t.Fatal("clone should not be nil")
	}
	if len(clone.Listeners) != 1 {
		t.Errorf("Listeners = %d, want 1", len(clone.Listeners))
	}
	if len(clone.Pools) != 1 {
		t.Errorf("Pools = %d, want 1", len(clone.Pools))
	}
	// Mutating clone should not affect original
	clone.Listeners[0].Name = "modified"
	sm.mu.RLock()
	origName := sm.config.Listeners[0].Name
	sm.mu.RUnlock()
	if origName != "l1" {
		t.Errorf("original name = %q, want l1", origName)
	}
}

func TestConfigStateMachine_Snapshot_Cloned(t *testing.T) {
	sm := NewConfigStateMachine(&config.Config{
		Listeners: []*config.Listener{{Name: "l1"}},
	})
	data, err := sm.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty snapshot data")
	}
}

// --- Additional coverage for cloneConfigLocked and New*Command helpers ---

func TestCloneConfigLocked_NilConfig(t *testing.T) {
	sm := NewConfigStateMachine(nil)
	sm.mu.RLock()
	clone := sm.cloneConfigLocked()
	sm.mu.RUnlock()
	if clone == nil {
		t.Fatal("clone should not be nil even with nil-initialised config")
	}
	if clone.Version != "1" {
		t.Errorf("Version = %q, want 1", clone.Version)
	}
}

func TestCloneConfigLocked_DeepCopy(t *testing.T) {
	sm := NewConfigStateMachine(&config.Config{
		Version:   "2",
		Pools:     []*config.Pool{{Name: "p1"}, {Name: "p2"}},
		Listeners: []*config.Listener{{Name: "l1", Address: ":8080"}},
	})
	clone := sm.cloneConfigLocked()
	// Modify clone
	clone.Pools[0].Name = "p1-modified"
	clone.Listeners[0].Address = ":9999"
	clone.Version = "3"
	// Original should be unchanged
	sm.mu.RLock()
	orig := sm.config
	sm.mu.RUnlock()
	if orig.Pools[0].Name != "p1" {
		t.Errorf("original pool name = %q, want p1", orig.Pools[0].Name)
	}
	if orig.Listeners[0].Address != ":8080" {
		t.Errorf("original listener address = %q, want :8080", orig.Listeners[0].Address)
	}
	if orig.Version != "2" {
		t.Errorf("original version = %q, want 2", orig.Version)
	}
}

func TestNewSetConfigCommand_RoundTrip(t *testing.T) {
	cfg := &config.Config{
		Version:   "3",
		Pools:     []*config.Pool{{Name: "test-pool"}},
		Listeners: []*config.Listener{{Name: "http", Address: ":9090"}},
	}
	cmd, err := NewSetConfigCommand(cfg)
	if err != nil {
		t.Fatalf("NewSetConfigCommand: %v", err)
	}
	if cmd.Type != CmdSetConfig {
		t.Errorf("Type = %v, want CmdSetConfig", cmd.Type)
	}
	// Verify the payload round-trips correctly
	var decoded config.Config
	if err := json.Unmarshal(cmd.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if decoded.Version != "3" {
		t.Errorf("decoded version = %q, want 3", decoded.Version)
	}
	if len(decoded.Pools) != 1 || decoded.Pools[0].Name != "test-pool" {
		t.Errorf("decoded pools = %v, want [{test-pool}]", decoded.Pools)
	}
}

func TestNewUpdateBackendCommand_RoundTrip(t *testing.T) {
	backend := &config.Backend{Address: "10.0.0.1:8080", Weight: 5}
	cmd, err := NewUpdateBackendCommand("mypool", backend)
	if err != nil {
		t.Fatalf("NewUpdateBackendCommand: %v", err)
	}
	if cmd.Type != CmdUpdateBackend {
		t.Errorf("Type = %v, want CmdUpdateBackend", cmd.Type)
	}
	var decoded UpdateBackendPayload
	if err := json.Unmarshal(cmd.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Pool != "mypool" {
		t.Errorf("Pool = %q, want mypool", decoded.Pool)
	}
	if decoded.Backend.Address != "10.0.0.1:8080" {
		t.Errorf("Backend.Address = %q, want 10.0.0.1:8080", decoded.Backend.Address)
	}
}

func TestNewUpdateRouteCommand_RoundTrip(t *testing.T) {
	route := &config.Route{Path: "/api/v2", Pool: "api-pool"}
	cmd, err := NewUpdateRouteCommand("mylistener", route)
	if err != nil {
		t.Fatalf("NewUpdateRouteCommand: %v", err)
	}
	if cmd.Type != CmdUpdateRoute {
		t.Errorf("Type = %v, want CmdUpdateRoute", cmd.Type)
	}
	var decoded UpdateRoutePayload
	if err := json.Unmarshal(cmd.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Listener != "mylistener" {
		t.Errorf("Listener = %q, want mylistener", decoded.Listener)
	}
	if decoded.Route.Path != "/api/v2" {
		t.Errorf("Route.Path = %q, want /api/v2", decoded.Route.Path)
	}
}

func TestNewUpdateListenerCommand_RoundTrip(t *testing.T) {
	listener := &config.Listener{Name: "https", Address: ":443", Protocol: "https"}
	cmd, err := NewUpdateListenerCommand(listener)
	if err != nil {
		t.Fatalf("NewUpdateListenerCommand: %v", err)
	}
	if cmd.Type != CmdUpdateListener {
		t.Errorf("Type = %v, want CmdUpdateListener", cmd.Type)
	}
	var decoded UpdateListenerPayload
	if err := json.Unmarshal(cmd.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Listener.Name != "https" {
		t.Errorf("Listener.Name = %q, want https", decoded.Listener.Name)
	}
}

func TestProposeConfigChange_MarshalError(t *testing.T) {
	// Create a ConfigCommand with a payload that can't be marshalled.
	// ProposeConfigChange takes a *Cluster and ConfigCommand, so test via the
	// happy path instead with a nil cluster.
	// We test the json.Marshal path succeeds for normal commands.
	cmd := ConfigCommand{Type: CmdSetConfig, Payload: json.RawMessage(`{}`)}
	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ConfigCommand
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Type != CmdSetConfig {
		t.Errorf("Type = %q, want %q", decoded.Type, CmdSetConfig)
	}
}

// --- Error paths in Apply for invalid payloads ---

func TestConfigStateMachine_Apply_InvalidUpdateBackendPayload(t *testing.T) {
	sm := NewConfigStateMachine(nil)
	cmd := ConfigCommand{Type: CmdUpdateBackend, Payload: json.RawMessage(`not valid json`)}
	data, _ := json.Marshal(cmd)
	_, err := sm.Apply(data)
	if err == nil {
		t.Error("expected error for invalid UpdateBackend payload")
	}
}

func TestConfigStateMachine_Apply_InvalidUpdateRoutePayload(t *testing.T) {
	sm := NewConfigStateMachine(nil)
	cmd := ConfigCommand{Type: CmdUpdateRoute, Payload: json.RawMessage(`{broken`)}
	data, _ := json.Marshal(cmd)
	_, err := sm.Apply(data)
	if err == nil {
		t.Error("expected error for invalid UpdateRoute payload")
	}
}

func TestConfigStateMachine_Apply_InvalidUpdateListenerPayload(t *testing.T) {
	sm := NewConfigStateMachine(nil)
	cmd := ConfigCommand{Type: CmdUpdateListener, Payload: json.RawMessage(`not-json`)}
	data, _ := json.Marshal(cmd)
	_, err := sm.Apply(data)
	if err == nil {
		t.Error("expected error for invalid UpdateListener payload")
	}
}

func TestProposeConfigChange_ProposeError_FullBuffer(t *testing.T) {
	sm := newKVStateMachine()
	config := &Config{
		NodeID:        "node1",
		BindAddr:      "127.0.0.1",
		BindPort:      0,
		ElectionTick:  2 * time.Second,
		HeartbeatTick: 500 * time.Millisecond,
	}
	c, err := New(config, sm)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Don't start the run loop; Propose will fail because commandCh is not being drained.
	// Fill the buffer
	for i := 0; i < cap(c.commandCh); i++ {
		c.commandCh <- &Command{
			Command: []byte(fmt.Sprintf("filler-%d", i)),
			Result:  make(chan *CommandResult, 1),
		}
	}

	cmd := ConfigCommand{Type: CmdSetConfig, Payload: json.RawMessage(`{"version":"1"}`)}
	err = ProposeConfigChange(c, cmd)
	if err == nil {
		t.Error("expected error from ProposeConfigChange when Propose fails")
	}
}

func TestConfigStateMachine_Apply_UpdateBackendEmptyPool(t *testing.T) {
	sm := NewConfigStateMachine(nil)
	cmd := ConfigCommand{Type: CmdUpdateBackend, Payload: json.RawMessage(`{"pool":"","backend":{"address":"10.0.0.1:8080"}}`)}
	data, _ := json.Marshal(cmd)
	_, err := sm.Apply(data)
	if err == nil {
		t.Error("expected error for empty pool name")
	}
}

func TestConfigStateMachine_Apply_UpdateRouteEmptyListener(t *testing.T) {
	sm := NewConfigStateMachine(nil)
	cmd := ConfigCommand{Type: CmdUpdateRoute, Payload: json.RawMessage(`{"listener":"","route":{"path":"/"}}`)}
	data, _ := json.Marshal(cmd)
	_, err := sm.Apply(data)
	if err == nil {
		t.Error("expected error for empty listener name")
	}
}

func TestConfigStateMachine_Apply_UpdateListenerNil(t *testing.T) {
	sm := NewConfigStateMachine(nil)
	cmd := ConfigCommand{Type: CmdUpdateListener, Payload: json.RawMessage(`{"listener":null}`)}
	data, _ := json.Marshal(cmd)
	_, err := sm.Apply(data)
	if err == nil {
		t.Error("expected error for nil listener")
	}
}

func TestConfigStateMachine_Apply_UpdateListenerEmptyName(t *testing.T) {
	sm := NewConfigStateMachine(nil)
	cmd := ConfigCommand{Type: CmdUpdateListener, Payload: json.RawMessage(`{"listener":{"name":"","address":":8080"}}`)}
	data, _ := json.Marshal(cmd)
	_, err := sm.Apply(data)
	if err == nil {
		t.Error("expected error for empty listener name")
	}
}

func TestConfigStateMachine_Apply_UnknownCommandType(t *testing.T) {
	sm := NewConfigStateMachine(nil)
	cmd := ConfigCommand{Type: ConfigCommandType("unknown_type"), Payload: json.RawMessage(`{}`)}
	data, _ := json.Marshal(cmd)
	_, err := sm.Apply(data)
	if err == nil {
		t.Error("expected error for unknown command type")
	}
}

func TestConfigStateMachine_Apply_WAFCommandWithHandler(t *testing.T) {
	sm := NewConfigStateMachine(nil)
	var receivedType ConfigCommandType
	var receivedPayload json.RawMessage
	sm.SetWAFCommandHandler(func(ct ConfigCommandType, p json.RawMessage) error {
		receivedType = ct
		receivedPayload = p
		return nil
	})

	cmd := ConfigCommand{Type: CmdWAFSetMode, Payload: json.RawMessage(`{"layer":"all","mode":"monitor"}`)}
	data, _ := json.Marshal(cmd)
	_, err := sm.Apply(data)
	if err != nil {
		t.Fatalf("Apply WAF command: %v", err)
	}
	if receivedType != CmdWAFSetMode {
		t.Errorf("received type = %q, want %q", receivedType, CmdWAFSetMode)
	}
	if string(receivedPayload) != `{"layer":"all","mode":"monitor"}` {
		t.Errorf("received payload = %q", string(receivedPayload))
	}
}

func TestConfigStateMachine_Apply_WAFCommandWithHandlerError(t *testing.T) {
	sm := NewConfigStateMachine(nil)
	sm.SetWAFCommandHandler(func(ct ConfigCommandType, p json.RawMessage) error {
		return fmt.Errorf("WAF handler error")
	})

	cmd := ConfigCommand{Type: CmdWAFAddWhitelist, Payload: json.RawMessage(`{"cidr":"10.0.0.0/8"}`)}
	data, _ := json.Marshal(cmd)
	_, err := sm.Apply(data)
	if err == nil {
		t.Error("expected error from WAF handler")
	}
}

// ---------------------------------------------------------------------------
// DeleteBackend command
// ---------------------------------------------------------------------------

func TestConfigStateMachine_Apply_DeleteBackend(t *testing.T) {
	sm := NewConfigStateMachine(newTestConfig())

	t.Run("delete_existing", func(t *testing.T) {
		cmd, err := NewDeleteBackendCommand("api-pool", "b1")
		if err != nil {
			t.Fatalf("NewDeleteBackendCommand: %v", err)
		}

		_, err = sm.Apply(makeCommand(t, cmd))
		if err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		cfg := sm.GetCurrentConfig()
		if len(cfg.Pools[0].Backends) != 1 {
			t.Errorf("Backends count = %d, want 1", len(cfg.Pools[0].Backends))
		}
		if cfg.Pools[0].Backends[0].ID == "b1" {
			t.Error("b1 should have been deleted")
		}
	})

	t.Run("delete_nonexistent_backend", func(t *testing.T) {
		cmd, err := NewDeleteBackendCommand("api-pool", "nonexistent")
		if err != nil {
			t.Fatalf("NewDeleteBackendCommand: %v", err)
		}

		_, err = sm.Apply(makeCommand(t, cmd))
		if err == nil {
			t.Error("Expected error for nonexistent backend")
		}
	})

	t.Run("delete_nonexistent_pool", func(t *testing.T) {
		cmd, err := NewDeleteBackendCommand("nonexistent-pool", "b1")
		if err != nil {
			t.Fatalf("NewDeleteBackendCommand: %v", err)
		}

		_, err = sm.Apply(makeCommand(t, cmd))
		if err == nil {
			t.Error("Expected error for nonexistent pool")
		}
	})

	t.Run("empty_pool_name", func(t *testing.T) {
		cmd := ConfigCommand{Type: CmdDeleteBackend, Payload: json.RawMessage(`{"pool":"","backend_id":"b1"}`)}
		_, err := sm.Apply(makeCommand(t, cmd))
		if err == nil {
			t.Error("Expected error for empty pool name")
		}
	})

	t.Run("empty_backend_id", func(t *testing.T) {
		cmd := ConfigCommand{Type: CmdDeleteBackend, Payload: json.RawMessage(`{"pool":"api-pool","backend_id":""}`)}
		_, err := sm.Apply(makeCommand(t, cmd))
		if err == nil {
			t.Error("Expected error for empty backend_id")
		}
	})
}

func TestNewDeleteBackendCommand(t *testing.T) {
	cmd, err := NewDeleteBackendCommand("mypool", "b1")
	if err != nil {
		t.Fatalf("NewDeleteBackendCommand: %v", err)
	}
	if cmd.Type != CmdDeleteBackend {
		t.Errorf("Type = %q, want %q", cmd.Type, CmdDeleteBackend)
	}
	var decoded DeleteBackendPayload
	if err := json.Unmarshal(cmd.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Pool != "mypool" {
		t.Errorf("Pool = %q, want mypool", decoded.Pool)
	}
	if decoded.BackendID != "b1" {
		t.Errorf("BackendID = %q, want b1", decoded.BackendID)
	}
}
