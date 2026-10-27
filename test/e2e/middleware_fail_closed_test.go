package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/engine"
)

// An enabled security middleware whose config its constructor rejects must
// stop the engine from serving, not be dropped from the chain. Basic auth
// rejects plaintext passwords unless hash: plain is set — a typo there used
// to leave the listener open with no authentication at all.
func TestE2E_MisconfiguredBasicAuthFailsClosed(t *testing.T) {
	backend := startBackendWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "secret backend content")
	})
	proxyPH := reservePort(t)
	adminPH := reservePort(t)

	yaml := fmt.Sprintf(`admin:
  address: "127.0.0.1:%d"
listeners:
  - name: http
    address: "127.0.0.1:%d"
    protocol: http
    routes:
      - path: /
        pool: p
pools:
  - name: p
    algorithm: round_robin
    backends:
      - address: "%s"
middleware:
  basic_auth:
    enabled: true
    users:
      admin: "not-a-hash"
`, adminPH.Port(), proxyPH.Port(), backend)
	path := filepath.Join(t.TempDir(), "olb.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		return // rejected at load: fails closed
	}
	eng, err := engine.New(cfg, "")
	if err != nil {
		return // rejected at construction: fails closed
	}
	if err := startEngineWithPorts(eng, proxyPH, adminPH); err != nil {
		return // rejected at start: fails closed
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})

	addr := cfg.Listeners[0].Address
	waitForReady(t, addr, 5*time.Second)
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("basic auth was enabled but misconfigured, and an unauthenticated request got 200: the middleware was silently dropped")
	}
}
