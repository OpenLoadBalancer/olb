package profiling

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Coverage: pprofTokenMiddleware + stringsEqualConstTime
// ---------------------------------------------------------------------------

func TestPprofTokenMiddleware_ValidToken(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	handler := pprofTokenMiddleware(inner, "secret-token")

	// Request with correct Bearer token.
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for valid token", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "ok")
	}
}

func TestPprofTokenMiddleware_InvalidToken(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not be called for invalid token")
	})

	handler := pprofTokenMiddleware(inner, "secret-token")

	// Request with wrong token.
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for invalid token", rec.Code)
	}
}

func TestPprofTokenMiddleware_MissingHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not be called for missing header")
	})

	handler := pprofTokenMiddleware(inner, "secret-token")

	// Request with no Authorization header.
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for missing header", rec.Code)
	}
}

func TestPprofTokenMiddleware_WrongScheme(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not be called for wrong scheme")
	})

	handler := pprofTokenMiddleware(inner, "secret-token")

	// Request with Basic auth instead of Bearer.
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	req.Header.Set("Authorization", "Basic secret-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for wrong scheme", rec.Code)
	}
}

func TestStringsEqualConstTime(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{"equal", "hello", "hello", true},
		{"not equal", "hello", "world", false},
		{"empty equal", "", "", true},
		{"different length", "a", "ab", false},
		{"prefix mismatch", "prefix-long", "prefix-short", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringsEqualConstTime(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("stringsEqualConstTime(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Coverage: Apply with Token (Bearer auth middleware wired through Apply)
// ---------------------------------------------------------------------------

func TestApply_WithPprofAndToken(t *testing.T) {
	cfg := ProfileConfig{
		EnablePprof: true,
		PprofAddr:   "127.0.0.1:0",
		Token:       "test-bearer-token",
	}

	cleanup, err := Apply(cfg)
	if err != nil {
		t.Fatalf("Apply with token: %v", err)
	}

	// The server needs a moment to start.
	// We cannot easily get the address since Apply does not expose it,
	// but we verify Apply did not error and the cleanup works.
	cleanup()
}

// ---------------------------------------------------------------------------
// Coverage: Apply with pprof bound to non-localhost address (warning path)
// ---------------------------------------------------------------------------

func TestApply_WithPprofNonLocalhostAddr(t *testing.T) {
	cfg := ProfileConfig{
		EnablePprof: true,
		PprofAddr:   "0.0.0.0:0",
	}

	cleanup, err := Apply(cfg)
	if err != nil {
		t.Fatalf("Apply with non-localhost addr: %v", err)
	}
	cleanup()
}

// ---------------------------------------------------------------------------
// Coverage: Apply with pprof + token on non-localhost (both warning paths)
// ---------------------------------------------------------------------------

func TestApply_WithPprofTokenAndNonLocalhost(t *testing.T) {
	cfg := ProfileConfig{
		EnablePprof: true,
		PprofAddr:   "0.0.0.0:0",
		Token:       "my-secret",
	}

	cleanup, err := Apply(cfg)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	cleanup()
}

// ---------------------------------------------------------------------------
// Coverage: Verify token middleware end-to-end via httptest.Server
// ---------------------------------------------------------------------------

func TestPprofTokenMiddleware_Integration(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	var handler http.Handler = mux
	handler = pprofTokenMiddleware(handler, "mytoken")

	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Without token: should be 401.
	resp, err := http.Get(srv.URL + "/debug/pprof/")
	if err != nil {
		t.Fatalf("GET without token: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", resp.StatusCode)
	}

	// With correct token: should be 200.
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/debug/pprof/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer mytoken")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET with token: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("with token: status = %d, want 200", resp.StatusCode)
	}

	// With wrong token: should be 401.
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/debug/pprof/", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET with wrong token: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want 401", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Coverage: Apply with only pprof (no CPU, no mem, no block, no mutex)
// ---------------------------------------------------------------------------

func TestApply_PprofOnly(t *testing.T) {
	cfg := ProfileConfig{
		EnablePprof: true,
		PprofAddr:   "127.0.0.1:0",
	}

	cleanup, err := Apply(cfg)
	if err != nil {
		t.Fatalf("Apply pprof-only: %v", err)
	}
	cleanup()
}

// ---------------------------------------------------------------------------
// Coverage: Apply with pprof bound to IPv6 localhost
// ---------------------------------------------------------------------------

func TestApply_WithPprofIPv6Localhost(t *testing.T) {
	cfg := ProfileConfig{
		EnablePprof: true,
		PprofAddr:   "[::1]:0",
	}

	cleanup, err := Apply(cfg)
	if err != nil {
		t.Fatalf("Apply with IPv6 localhost: %v", err)
	}
	cleanup()
}

// ---------------------------------------------------------------------------
// Coverage: WriteMemProfile WriteTo error (read-only file)
// ---------------------------------------------------------------------------

func TestWriteMemProfile_WriteToError(t *testing.T) {
	t.Run("readonly_directory", func(t *testing.T) {
		dir := t.TempDir()
		// Use a path inside a non-existent directory tree to trigger create error.
		badPath := dir + "/no/such/subdir/mem.prof"
		err := WriteMemProfile(badPath)
		if err == nil {
			t.Fatal("expected error for unwritable path")
		}
		if !strings.Contains(err.Error(), "create mem profile") {
			t.Errorf("error %q should mention 'create mem profile'", err.Error())
		}
	})
}

// ---------------------------------------------------------------------------
// Coverage: WriteAllocProfile WriteTo error
// ---------------------------------------------------------------------------

func TestWriteAllocProfile_WriteToError(t *testing.T) {
	t.Run("readonly_directory", func(t *testing.T) {
		dir := t.TempDir()
		badPath := dir + "/no/such/subdir/allocs.prof"
		err := WriteAllocProfile(badPath)
		if err == nil {
			t.Fatal("expected error for unwritable path")
		}
		if !strings.Contains(err.Error(), "create alloc profile") {
			t.Errorf("error %q should mention 'create alloc profile'", err.Error())
		}
	})
}
