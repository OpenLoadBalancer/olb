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
			t.Error("Expected non-nil config in callback")
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
