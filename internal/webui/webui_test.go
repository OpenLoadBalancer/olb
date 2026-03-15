package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// TestNewHandler tests the creation of a new handler.
func TestNewHandler(t *testing.T) {
	handler, err := NewHandler()
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	if handler == nil {
		t.Fatal("NewHandler() returned nil handler")
	}
	if handler.static == nil {
		t.Fatal("handler.static is nil")
	}
}

// TestHandlerServeHTTP tests the HTTP handler.
func TestHandlerServeHTTP(t *testing.T) {
	// Create a test filesystem
	testFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data:    []byte("<!DOCTYPE html><html><head><title>Test</title></head><body>Test</body></html>"),
			Mode:    0644,
			ModTime: testModTime,
		},
		"css/test.css": &fstest.MapFile{
			Data:    []byte("body { color: red; }"),
			Mode:    0644,
			ModTime: testModTime,
		},
		"js/test.js": &fstest.MapFile{
			Data:    []byte("console.log('test');"),
			Mode:    0644,
			ModTime: testModTime,
		},
	}

	handler := NewHandlerWithFS(http.FS(testFS))

	tests := []struct {
		name            string
		path            string
		wantStatus      int
		wantContent     string
		wantContentType string
	}{
		{
			name:            "serve index.html at root",
			path:            "/",
			wantStatus:      http.StatusOK,
			wantContent:     "<!DOCTYPE html>",
			wantContentType: "text/html; charset=utf-8",
		},
		{
			name:            "serve index.html",
			path:            "/index.html",
			wantStatus:      http.StatusOK,
			wantContent:     "<!DOCTYPE html>",
			wantContentType: "text/html; charset=utf-8",
		},
		{
			name:            "serve css file",
			path:            "/css/test.css",
			wantStatus:      http.StatusOK,
			wantContent:     "body { color: red; }",
			wantContentType: "text/css; charset=utf-8",
		},
		{
			name:            "serve js file",
			path:            "/js/test.js",
			wantStatus:      http.StatusOK,
			wantContent:     "console.log",
			wantContentType: "application/javascript; charset=utf-8",
		},
		{
			name:            "SPA fallback for unknown route",
			path:            "/dashboard",
			wantStatus:      http.StatusOK,
			wantContent:     "<!DOCTYPE html>",
			wantContentType: "text/html; charset=utf-8",
		},
		{
			name:            "SPA fallback for nested route",
			path:            "/backends/123",
			wantStatus:      http.StatusOK,
			wantContent:     "<!DOCTYPE html>",
			wantContentType: "text/html; charset=utf-8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("ServeHTTP() status = %v, want %v", rec.Code, tt.wantStatus)
			}

			body, _ := io.ReadAll(rec.Body)
			if !strings.Contains(string(body), tt.wantContent) {
				t.Errorf("ServeHTTP() body = %v, want containing %v", string(body), tt.wantContent)
			}

			contentType := rec.Header().Get("Content-Type")
			if tt.wantContentType != "" && contentType != tt.wantContentType {
				t.Errorf("ServeHTTP() Content-Type = %v, want %v", contentType, tt.wantContentType)
			}
		})
	}
}

// TestHandlerServeHTTPWithStaticPrefix tests paths with /static prefix.
func TestHandlerServeHTTPWithStaticPrefix(t *testing.T) {
	testFS := fstest.MapFS{
		"css/design.css": &fstest.MapFile{
			Data:    []byte("/* design system */"),
			Mode:    0644,
			ModTime: testModTime,
		},
	}

	handler := NewHandlerWithFS(http.FS(testFS))

	req := httptest.NewRequest(http.MethodGet, "/static/css/design.css", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("ServeHTTP() status = %v, want %v", rec.Code, http.StatusOK)
	}

	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "design system") {
		t.Errorf("ServeHTTP() body = %v, want containing 'design system'", string(body))
	}
}

// TestGetContentType tests the content type detection.
func TestGetContentType(t *testing.T) {
	tests := []struct {
		filepath string
		want     string
	}{
		{"test.html", "text/html; charset=utf-8"},
		{"test.css", "text/css; charset=utf-8"},
		{"test.js", "application/javascript; charset=utf-8"},
		{"test.json", "application/json"},
		{"test.png", "image/png"},
		{"test.jpg", "image/jpeg"},
		{"test.jpeg", "image/jpeg"},
		{"test.gif", "image/gif"},
		{"test.svg", "image/svg+xml"},
		{"test.ico", "image/x-icon"},
		{"test.woff", "font/woff"},
		{"test.woff2", "font/woff2"},
		{"test.ttf", "font/ttf"},
		{"test.otf", "font/otf"},
		{"test.eot", "application/vnd.ms-fontobject"},
		{"test.unknown", ""},
		{"test", ""},
	}

	for _, tt := range tests {
		t.Run(tt.filepath, func(t *testing.T) {
			got := getContentType(tt.filepath)
			if got != tt.want {
				t.Errorf("getContentType(%q) = %q, want %q", tt.filepath, got, tt.want)
			}
		})
	}
}

// TestRegisterRoutes tests route registration.
func TestRegisterRoutes(t *testing.T) {
	testFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data:    []byte("<html></html>"),
			Mode:    0644,
			ModTime: testModTime,
		},
	}

	handler := NewHandlerWithFS(http.FS(testFS))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, "/ui")

	// Test registered route
	req := httptest.NewRequest(http.MethodGet, "/ui/", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("ServeHTTP() status = %v, want %v", rec.Code, http.StatusOK)
	}
}

// TestHandlerCacheHeaders tests cache control headers.
func TestHandlerCacheHeaders(t *testing.T) {
	testFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data:    []byte("<html></html>"),
			Mode:    0644,
			ModTime: testModTime,
		},
		"css/test.css": &fstest.MapFile{
			Data:    []byte("body{}"),
			Mode:    0644,
			ModTime: testModTime,
		},
		"js/test.js": &fstest.MapFile{
			Data:    []byte("test"),
			Mode:    0644,
			ModTime: testModTime,
		},
	}

	handler := NewHandlerWithFS(http.FS(testFS))

	tests := []struct {
		name      string
		path      string
		wantCache string
	}{
		{
			name:      "html no cache",
			path:      "/index.html",
			wantCache: "no-cache",
		},
		{
			name:      "css long cache",
			path:      "/css/test.css",
			wantCache: "public, max-age=31536000, immutable",
		},
		{
			name:      "js long cache",
			path:      "/js/test.js",
			wantCache: "public, max-age=31536000, immutable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			cacheControl := rec.Header().Get("Cache-Control")
			if cacheControl != tt.wantCache {
				t.Errorf("Cache-Control = %v, want %v", cacheControl, tt.wantCache)
			}
		})
	}
}

// TestHandlerWithRealFS tests with actual embedded filesystem.
func TestHandlerWithRealFS(t *testing.T) {
	handler, err := NewHandler()
	if err != nil {
		t.Skipf("Embedded filesystem not available: %v", err)
	}

	// Test that index.html is served
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("ServeHTTP() status = %v, want %v", rec.Code, http.StatusOK)
	}

	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "OpenLoadBalancer") {
		t.Error("Response does not contain expected content")
	}
}

// BenchmarkHandler benchmarks the handler.
func BenchmarkHandler(b *testing.B) {
	testFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data:    []byte("<!DOCTYPE html><html><head><title>Test</title></head><body>Test</body></html>"),
			Mode:    0644,
			ModTime: testModTime,
		},
	}

	handler := NewHandlerWithFS(http.FS(testFS))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

// testModTime is a fixed modification time for test files.
var testModTime = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// tempDirFS is a filesystem wrapper for temp directories.
type tempDirFS struct {
	root string
}

func (t *tempDirFS) Open(name string) (http.File, error) {
	return nil, nil
}

// TestNewHandlerWithFS tests creating handler with custom filesystem.
func TestNewHandlerWithFS(t *testing.T) {
	testFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data:    []byte("<html>test</html>"),
			Mode:    0644,
			ModTime: testModTime,
		},
	}

	handler := NewHandlerWithFS(http.FS(testFS))
	if handler == nil {
		t.Fatal("NewHandlerWithFS() returned nil")
	}
	if handler.static == nil {
		t.Fatal("handler.static is nil")
	}
}

// TestHandlerMethods tests handler with different HTTP methods.
func TestHandlerMethods(t *testing.T) {
	testFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data:    []byte("<html></html>"),
			Mode:    0644,
			ModTime: testModTime,
		},
	}

	handler := NewHandlerWithFS(http.FS(testFS))

	methods := []string{http.MethodGet, http.MethodHead}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/", nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("ServeHTTP() status = %v, want %v", rec.Code, http.StatusOK)
			}
		})
	}
}

// TestHandlerPathTraversal tests path traversal protection.
func TestHandlerPathTraversal(t *testing.T) {
	testFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data:    []byte("<html></html>"),
			Mode:    0644,
			ModTime: testModTime,
		},
		"secret.txt": &fstest.MapFile{
			Data:    []byte("secret"),
			Mode:    0644,
			ModTime: testModTime,
		},
	}

	handler := NewHandlerWithFS(http.FS(testFS))

	// Path traversal attempt should fall back to index.html
	req := httptest.NewRequest(http.MethodGet, "/../secret.txt", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should serve index.html (SPA fallback) not the secret file
	body, _ := io.ReadAll(rec.Body)
	if strings.Contains(string(body), "secret") {
		t.Error("Path traversal vulnerability: secret file was exposed")
	}
}

// TestServeIndex_NoIndexFile tests serveIndex when index.html doesn't exist.
func TestServeIndex_NoIndexFile(t *testing.T) {
	emptyFS := fstest.MapFS{}
	handler := NewHandlerWithFS(http.FS(emptyFS))

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "index.html not found") {
		t.Errorf("expected 'index.html not found' error, got %q", string(body))
	}
}

// TestServeHTTP_DirectoryWithIndex tests serving a directory containing index.html.
func TestServeHTTP_DirectoryWithIndex(t *testing.T) {
	testFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data:    []byte("<html>root</html>"),
			Mode:    0644,
			ModTime: testModTime,
		},
		"subdir/index.html": &fstest.MapFile{
			Data:    []byte("<html>sub</html>"),
			Mode:    0644,
			ModTime: testModTime,
		},
	}

	handler := NewHandlerWithFS(http.FS(testFS))

	req := httptest.NewRequest(http.MethodGet, "/subdir/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should serve the subdir/index.html or fallback to root index.html
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

// TestServeHTTP_DirectoryWithoutIndex tests serving a directory without index.html.
func TestServeHTTP_DirectoryWithoutIndex(t *testing.T) {
	testFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data:    []byte("<html>root</html>"),
			Mode:    0644,
			ModTime: testModTime,
		},
		"subdir/other.txt": &fstest.MapFile{
			Data:    []byte("other"),
			Mode:    0644,
			ModTime: testModTime,
		},
	}

	handler := NewHandlerWithFS(http.FS(testFS))

	// Request subdir which has no index.html
	req := httptest.NewRequest(http.MethodGet, "/subdir/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should fall back to root index.html
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

// TestServeFile_CSSCaching tests caching headers for CSS files.
func TestServeFile_CSSCaching(t *testing.T) {
	testFS := fstest.MapFS{
		"css/style.css": &fstest.MapFile{
			Data:    []byte("body { color: red; }"),
			Mode:    0644,
			ModTime: testModTime,
		},
		"index.html": &fstest.MapFile{
			Data:    []byte("<html></html>"),
			Mode:    0644,
			ModTime: testModTime,
		},
	}

	handler := NewHandlerWithFS(http.FS(testFS))

	req := httptest.NewRequest(http.MethodGet, "/css/style.css", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	cacheControl := rec.Header().Get("Cache-Control")
	if !strings.Contains(cacheControl, "immutable") {
		t.Errorf("expected immutable cache control for CSS, got %q", cacheControl)
	}
}

// TestServeFile_JSCaching tests caching headers for JS files.
func TestServeFile_JSCaching(t *testing.T) {
	testFS := fstest.MapFS{
		"js/app.js": &fstest.MapFile{
			Data:    []byte("console.log('hello');"),
			Mode:    0644,
			ModTime: testModTime,
		},
		"index.html": &fstest.MapFile{
			Data:    []byte("<html></html>"),
			Mode:    0644,
			ModTime: testModTime,
		},
	}

	handler := NewHandlerWithFS(http.FS(testFS))

	req := httptest.NewRequest(http.MethodGet, "/js/app.js", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	cacheControl := rec.Header().Get("Cache-Control")
	if !strings.Contains(cacheControl, "immutable") {
		t.Errorf("expected immutable cache control for JS, got %q", cacheControl)
	}
}

// TestServeFile_HTMLNoCaching tests that HTML files have no-cache headers.
func TestServeFile_HTMLNoCaching(t *testing.T) {
	testFS := fstest.MapFS{
		"page.html": &fstest.MapFile{
			Data:    []byte("<html>page</html>"),
			Mode:    0644,
			ModTime: testModTime,
		},
		"index.html": &fstest.MapFile{
			Data:    []byte("<html></html>"),
			Mode:    0644,
			ModTime: testModTime,
		},
	}

	handler := NewHandlerWithFS(http.FS(testFS))

	req := httptest.NewRequest(http.MethodGet, "/page.html", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	cacheControl := rec.Header().Get("Cache-Control")
	if cacheControl != "no-cache" {
		t.Errorf("expected 'no-cache' for HTML, got %q", cacheControl)
	}
}

// TestServeHTTP_PathWithoutLeadingSlash tests path normalization.
func TestServeHTTP_PathWithoutLeadingSlash(t *testing.T) {
	testFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data:    []byte("<html></html>"),
			Mode:    0644,
			ModTime: testModTime,
		},
		"test.txt": &fstest.MapFile{
			Data:    []byte("test content"),
			Mode:    0644,
			ModTime: testModTime,
		},
	}

	handler := NewHandlerWithFS(http.FS(testFS))

	req := httptest.NewRequest(http.MethodGet, "/test.txt", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

// TestGetContentType_AdditionalTypes tests content type detection for more types.
func TestGetContentType_AdditionalTypes(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/font.woff", "font/woff"},
		{"/font.woff2", "font/woff2"},
		{"/font.ttf", "font/ttf"},
		{"/font.otf", "font/otf"},
		{"/font.eot", "application/vnd.ms-fontobject"},
		{"/image.gif", "image/gif"},
		{"/image.svg", "image/svg+xml"},
		{"/data.json", "application/json"},
		{"/image.jpeg", "image/jpeg"},
		{"/unknown.xyz", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := getContentType(tt.path)
			if got != tt.expected {
				t.Errorf("getContentType(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}
