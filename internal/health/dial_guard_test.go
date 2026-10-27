package health

import (
	"strings"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

// The SSRF guard used to look only at the configured host string, and only
// for HTTP checks. A hostname that resolves (or rebinds) to a metadata or
// link-local address passed, and TCP/gRPC checks were never guarded. The
// guard now runs on the resolved address at connect time for every check.
func TestDialGuard_BlocksResolvedInternalAddresses(t *testing.T) {
	for _, addr := range []string{
		"169.254.169.254:80",
		"169.254.10.10:443",
		"[fe80::1]:80",
		"[fd00:ec2::254]:80",
		"100.100.100.200:80",
		"0.0.0.0:80",
	} {
		if err := guardHealthDial("tcp", addr, nil); err == nil {
			t.Errorf("guardHealthDial(%q) = nil, want SSRF rejection", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:80", "10.0.0.5:8080", "[::1]:80", "203.0.113.7:443"} {
		if err := guardHealthDial("tcp", addr, nil); err != nil {
			t.Errorf("guardHealthDial(%q) = %v, want allowed", addr, err)
		}
	}
}

func TestCheckTCP_RejectsMetadataAddress(t *testing.T) {
	c := NewChecker()
	b := backend.NewBackend("meta", "169.254.169.254:80")
	start := time.Now()
	res := c.checkTCP(b, &Check{Type: "tcp", Timeout: 2 * time.Second})
	if res.Healthy || res.Error == nil || !strings.Contains(res.Error.Error(), "SSRF") {
		t.Fatalf("checkTCP = %+v, want an SSRF rejection", res)
	}
	if time.Since(start) > time.Second {
		t.Errorf("rejection took %v; it should fail before connecting", time.Since(start))
	}
}
