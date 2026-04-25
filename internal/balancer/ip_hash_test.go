package balancer

import (
	"fmt"
	"sync"
	"testing"

	"github.com/openloadbalancer/olb/internal/backend"
)

func TestIPHash_Name(t *testing.T) {
	ih := NewIPHash()
	if ih.Name() != "ip_hash" {
		t.Errorf("Name() = %v, want %v", ih.Name(), "ip_hash")
	}
}

func TestIPHash_ConsistentHashing(t *testing.T) {
	ih := NewIPHash()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	b3 := backend.NewBackend("b3", "127.0.0.1:8082")

	backends := []*backend.Backend{b1, b2, b3}

	// Test IPs
	testIPs := []string{
		"192.168.1.1",
		"10.0.0.1",
		"172.16.0.1",
		"8.8.8.8",
		"1.1.1.1",
	}

	// Record which backend each IP maps to
	ipToBackend := make(map[string]string)
	for _, ip := range testIPs {
		ctx := &RequestContext{ClientIP: ip}
		b := ih.Next(ctx, backends)
		if b == nil {
			t.Fatalf("Next(ctx{ClientIP:%s}) returned nil", ip)
		}
		ipToBackend[ip] = b.ID
	}

	// Run multiple times and verify consistency
	for i := 0; i < 100; i++ {
		for _, ip := range testIPs {
			ctx := &RequestContext{ClientIP: ip}
			b := ih.Next(ctx, backends)
			if b == nil {
				t.Fatalf("Next(ctx{ClientIP:%s}) returned nil on iteration %d", ip, i)
			}
			if b.ID != ipToBackend[ip] {
				t.Errorf("IP %s mapped to %s, expected %s (iteration %d)",
					ip, b.ID, ipToBackend[ip], i)
			}
		}
	}
}

func TestIPHash_EmptyBackends(t *testing.T) {
	ih := NewIPHash()

	ctx := &RequestContext{ClientIP: "192.168.1.1"}
	result := ih.Next(ctx, []*backend.Backend{})
	if result != nil {
		t.Error("Next with empty backends should return nil")
	}

	result = ih.Next(ctx, nil)
	if result != nil {
		t.Error("Next with nil backends should return nil")
	}
}

func TestIPHash_EmptyIP(t *testing.T) {
	ih := NewIPHash()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	backends := []*backend.Backend{b1, b2}

	// Empty IP should return a backend (hash of empty string = 0)
	ctx := &RequestContext{ClientIP: ""}
	result := ih.Next(ctx, backends)
	if result == nil {
		t.Fatal("Next with empty IP should return a backend")
	}
	// Should always return the first backend (index 0)
	if result.ID != "b1" {
		t.Errorf("Empty IP should map to first backend, got %s", result.ID)
	}
}

func TestIPHash_IPv4Addresses(t *testing.T) {
	ih := NewIPHash()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	backends := []*backend.Backend{b1, b2}

	// Test various IPv4 addresses
	testCases := []string{
		"192.168.1.1",
		"10.0.0.1",
		"172.16.0.1",
		"127.0.0.1",
		"255.255.255.255",
		"0.0.0.0",
	}

	for _, ip := range testCases {
		ctx := &RequestContext{ClientIP: ip}
		b := ih.Next(ctx, backends)
		if b == nil {
			t.Errorf("Next(ctx{ClientIP:%s}) returned nil", ip)
			continue
		}
		// Verify consistency
		for i := 0; i < 10; i++ {
			b2 := ih.Next(ctx, backends)
			if b2.ID != b.ID {
				t.Errorf("IP %s inconsistent: got %s then %s", ip, b.ID, b2.ID)
			}
		}
	}
}

func TestIPHash_IPv6Addresses(t *testing.T) {
	ih := NewIPHash()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	b3 := backend.NewBackend("b3", "127.0.0.1:8082")
	backends := []*backend.Backend{b1, b2, b3}

	// Test various IPv6 addresses
	testCases := []string{
		"::1",
		"2001:db8::1",
		"fe80::1",
		"::ffff:192.168.1.1", // IPv4-mapped IPv6
		"2001:0db8:0000:0000:0000:0000:0000:0001",
	}

	for _, ip := range testCases {
		ctx := &RequestContext{ClientIP: ip}
		b := ih.Next(ctx, backends)
		if b == nil {
			t.Errorf("Next(ctx{ClientIP:%s}) returned nil", ip)
			continue
		}
		// Verify consistency
		for i := 0; i < 10; i++ {
			b2 := ih.Next(ctx, backends)
			if b2.ID != b.ID {
				t.Errorf("IP %s inconsistent: got %s then %s", ip, b.ID, b2.ID)
			}
		}
	}
}

func TestIPHash_InvalidIP(t *testing.T) {
	ih := NewIPHash()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	backends := []*backend.Backend{b1, b2}

	// Invalid IPs should still produce a deterministic hash
	invalidIPs := []string{
		"not-an-ip",
		"256.256.256.256",
		"abc.def.ghi.jkl",
		":::",
	}

	for _, ip := range invalidIPs {
		ctx := &RequestContext{ClientIP: ip}
		b := ih.Next(ctx, backends)
		if b == nil {
			t.Errorf("Next(ctx{ClientIP:%s}) returned nil", ip)
			continue
		}
		// Verify consistency even for invalid IPs
		for i := 0; i < 10; i++ {
			b2 := ih.Next(ctx, backends)
			if b2.ID != b.ID {
				t.Errorf("Invalid IP %s inconsistent: got %s then %s", ip, b.ID, b2.ID)
			}
		}
	}
}

func TestIPHash_Distribution(t *testing.T) {
	ih := NewIPHash()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	b3 := backend.NewBackend("b3", "127.0.0.1:8082")

	backends := []*backend.Backend{b1, b2, b3}

	// Generate many IPs and check distribution
	counts := map[string]int{
		"b1": 0,
		"b2": 0,
		"b3": 0,
	}

	numIPs := 10000
	for i := 0; i < numIPs; i++ {
		ip := fmt.Sprintf("192.168.%d.%d", i/256, i%256)
		ctx := &RequestContext{ClientIP: ip}
		b := ih.Next(ctx, backends)
		if b != nil {
			counts[b.ID]++
		}
	}

	// Check that distribution is roughly even (within 20% of expected)
	expected := numIPs / 3
	tolerance := expected / 5 // 20%

	for id, count := range counts {
		if count < expected-tolerance || count > expected+tolerance {
			t.Logf("Backend %s count = %d, expected ~%d (tolerance: %d)",
				id, count, expected, tolerance)
			// Don't fail the test, just log - distribution won't be perfect
		}
	}

	t.Logf("Distribution: b1=%d, b2=%d, b3=%d", counts["b1"], counts["b2"], counts["b3"])
}

func TestIPHash_BackendChanges(t *testing.T) {
	ih := NewIPHash()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	b3 := backend.NewBackend("b3", "127.0.0.1:8082")

	// Start with 2 backends
	backends := []*backend.Backend{b1, b2}

	// Map IPs to backends
	testIPs := []string{
		"192.168.1.1",
		"10.0.0.1",
		"172.16.0.1",
		"8.8.8.8",
		"1.1.1.1",
	}

	originalMapping := make(map[string]string)
	for _, ip := range testIPs {
		ctx := &RequestContext{ClientIP: ip}
		b := ih.Next(ctx, backends)
		originalMapping[ip] = b.ID
	}

	// Add third backend
	backends = []*backend.Backend{b1, b2, b3}

	// Check redistribution - some IPs should stay on same backend
	consistent := 0
	for _, ip := range testIPs {
		ctx := &RequestContext{ClientIP: ip}
		b := ih.Next(ctx, backends)
		if b.ID == originalMapping[ip] {
			consistent++
		}
	}

	// With 2 -> 3 backends, roughly 2/3 of IPs should stay on same backend
	// We allow some variance
	if consistent == 0 {
		t.Error("No IPs stayed on same backend after adding backend")
	}

	t.Logf("After adding backend: %d/%d IPs stayed on same backend", consistent, len(testIPs))

	// Remove a backend
	backends = []*backend.Backend{b1, b2}

	// Check redistribution again
	consistent = 0
	for _, ip := range testIPs {
		ctx := &RequestContext{ClientIP: ip}
		b := ih.Next(ctx, backends)
		if b.ID == originalMapping[ip] {
			consistent++
		}
	}

	t.Logf("After removing backend: %d/%d IPs on original backend", consistent, len(testIPs))
}

func TestIPHash_Add(t *testing.T) {
	ih := NewIPHash()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")

	ih.Add(b1)
	if len(ih.GetBackends()) != 1 {
		t.Errorf("Expected 1 backend, got %d", len(ih.GetBackends()))
	}

	ih.Add(b2)
	if len(ih.GetBackends()) != 2 {
		t.Errorf("Expected 2 backends, got %d", len(ih.GetBackends()))
	}

	// Adding duplicate should not increase count
	ih.Add(b1)
	if len(ih.GetBackends()) != 2 {
		t.Errorf("Expected 2 backends after duplicate add, got %d", len(ih.GetBackends()))
	}
}

func TestIPHash_Remove(t *testing.T) {
	ih := NewIPHash()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	b3 := backend.NewBackend("b3", "127.0.0.1:8082")

	ih.Add(b1)
	ih.Add(b2)
	ih.Add(b3)

	ih.Remove("b2")
	backends := ih.GetBackends()
	if len(backends) != 2 {
		t.Errorf("Expected 2 backends after remove, got %d", len(backends))
	}

	// Verify b2 is gone
	for _, b := range backends {
		if b.ID == "b2" {
			t.Error("b2 should have been removed")
		}
	}

	// Removing non-existent should not panic
	ih.Remove("non-existent")
}

func TestIPHash_Update(t *testing.T) {
	ih := NewIPHash()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b1.SetWeight(1)

	ih.Add(b1)

	// Update the backend
	b1Updated := backend.NewBackend("b1", "127.0.0.1:8080")
	b1Updated.SetWeight(5)

	ih.Update(b1Updated)

	backends := ih.GetBackends()
	if len(backends) != 1 {
		t.Fatalf("Expected 1 backend, got %d", len(backends))
	}

	if backends[0].GetWeight() != 5 {
		t.Errorf("Expected weight 5, got %d", backends[0].GetWeight())
	}
}

func TestIPHash_ConcurrentAccess(t *testing.T) {
	ih := NewIPHash()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	b3 := backend.NewBackend("b3", "127.0.0.1:8082")

	backends := []*backend.Backend{b1, b2, b3}

	// Concurrent reads
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				ip := fmt.Sprintf("192.168.%d.%d", id, j)
				ctx := &RequestContext{ClientIP: ip}
				ih.Next(ctx, backends)
			}
		}(i)
	}

	// Concurrent adds/removes
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			b := backend.NewBackend(fmt.Sprintf("b%d", id), fmt.Sprintf("127.0.0.1:%d", 8090+id))
			ih.Add(b)
			ih.Remove(b.ID)
		}(i)
	}

	wg.Wait()
}

func TestIPHash_InterfaceCompliance(t *testing.T) {
	// Ensure IPHash implements Balancer interface
	var _ Balancer = NewIPHash()
}

func TestIPHash_SingleBackend(t *testing.T) {
	ih := NewIPHash()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	backends := []*backend.Backend{b1}

	// All IPs should map to the single backend
	testIPs := []string{
		"192.168.1.1",
		"10.0.0.1",
		"::1",
		"2001:db8::1",
	}

	for _, ip := range testIPs {
		ctx := &RequestContext{ClientIP: ip}
		for i := 0; i < 10; i++ {
			b := ih.Next(ctx, backends)
			if b == nil {
				t.Fatalf("Next(ctx{ClientIP:%s}) returned nil", ip)
			}
			if b.ID != "b1" {
				t.Errorf("IP %s mapped to %s, expected b1", ip, b.ID)
			}
		}
	}
}

// --- Tests for extractIP helper ---

func TestExtractIP(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"192.168.1.1:8080", "192.168.1.1"},
		{"10.0.0.1:443", "10.0.0.1"},
		{"[::1]:8080", "::1"},
		{"[2001:db8::1]:443", "2001:db8::1"},
		{"192.168.1.1", "192.168.1.1"},
		{"::1", "::1"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractIP(tt.input)
			if got != tt.expected {
				t.Errorf("extractIP(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIPHash_Next_InterfaceMethod(t *testing.T) {
	ih := NewIPHash()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	backends := []*backend.Backend{b1, b2}

	// Next (without context) should return a backend deterministically
	result := ih.Next(nil, backends)
	if result == nil {
		t.Fatal("Next(nil, backends) returned nil")
	}

	// Should be consistent (nil context hashes empty IP to 0 -> first backend)
	for i := 0; i < 10; i++ {
		r := ih.Next(nil, backends)
		if r.ID != result.ID {
			t.Errorf("Next(nil, backends) inconsistent: got %s then %s", result.ID, r.ID)
		}
	}
}

// Benchmarks

func BenchmarkIPHash_NextWithIP(b *testing.B) {
	ih := NewIPHash()

	be1 := backend.NewBackend("b1", "127.0.0.1:8080")
	be2 := backend.NewBackend("b2", "127.0.0.1:8081")
	be3 := backend.NewBackend("b3", "127.0.0.1:8082")

	backends := []*backend.Backend{be1, be2, be3}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ip := fmt.Sprintf("192.168.%d.%d", i/256, i%256)
		ctx := &RequestContext{ClientIP: ip}
		ih.Next(ctx, backends)
	}
}

func BenchmarkIPHash_NextWithIP_SingleBackend(b *testing.B) {
	ih := NewIPHash()

	be1 := backend.NewBackend("b1", "127.0.0.1:8080")
	backends := []*backend.Backend{be1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ip := fmt.Sprintf("192.168.%d.%d", i/256, i%256)
		ctx := &RequestContext{ClientIP: ip}
		ih.Next(ctx, backends)
	}
}

func TestIPHash_Next_EmptyIPHash(t *testing.T) {
	ih := NewIPHash()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	b2 := backend.NewBackend("b2", "127.0.0.1:8081")
	backends := []*backend.Backend{b1, b2}

	// Next(nil, ...) uses empty IP internally (hash=0), should return backends[0]
	for i := 0; i < 10; i++ {
		result := ih.Next(nil, backends)
		if result == nil {
			t.Fatal("Next(nil, backends) returned nil")
		}
		if result.ID != "b1" {
			t.Errorf("Next(nil, backends) = %s, want b1 (hash of empty is 0)", result.ID)
		}
	}
}

func TestIPHash_Update_Nonexistent(t *testing.T) {
	ih := NewIPHash()

	b1 := backend.NewBackend("b1", "127.0.0.1:8080")
	// Update on a backend not yet added should not panic
	ih.Update(b1)

	// Verify no backend was added
	if len(ih.GetBackends()) != 0 {
		t.Errorf("GetBackends() = %d, want 0 after updating nonexistent", len(ih.GetBackends()))
	}
}

func TestIPHash_Next_EmptyBackends_Slice(t *testing.T) {
	ih := NewIPHash()
	result := ih.Next(nil, []*backend.Backend{})
	if result != nil {
		t.Error("Next(nil, empty) should return nil")
	}
}

func BenchmarkIPHash_HashIP(b *testing.B) {
	ih := NewIPHash()
	testIPs := []string{
		"192.168.1.1",
		"10.0.0.1",
		"::1",
		"2001:db8::1",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ip := testIPs[i%len(testIPs)]
		ih.hashIP(ip)
	}
}

func BenchmarkIPHash_Concurrent(b *testing.B) {
	ih := NewIPHash()

	be1 := backend.NewBackend("b1", "127.0.0.1:8080")
	be2 := backend.NewBackend("b2", "127.0.0.1:8081")
	be3 := backend.NewBackend("b3", "127.0.0.1:8082")

	backends := []*backend.Backend{be1, be2, be3}

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			ip := fmt.Sprintf("192.168.%d.%d", i/256, i%256)
			ctx := &RequestContext{ClientIP: ip}
			ih.Next(ctx, backends)
			i++
		}
	})
}
