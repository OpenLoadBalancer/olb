package admin

import "testing"

// Without authentication the admin API must only listen on loopback. The
// check exempted an empty host, so ":8080" — which listens on every
// interface, and was also the default admin address — started an
// unauthenticated admin API reachable from the network.
func TestNewServer_NoAuthRequiresLoopback(t *testing.T) {
	for _, addr := range []string{":8080", "0.0.0.0:8080", "[::]:8080", "10.0.0.5:8080"} {
		if _, err := NewServer(&Config{Address: addr}); err == nil {
			t.Errorf("NewServer(%q) without auth succeeded; want refusal", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080", "127.0.0.1:0"} {
		if _, err := NewServer(&Config{Address: addr}); err != nil {
			t.Errorf("NewServer(%q) without auth: %v, want allowed", addr, err)
		}
	}
}
