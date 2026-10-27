package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

var testModTime = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

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

func TestHandlerServeHTTP(t *testing.T) {
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
		{"serve index at root", "/", http.StatusOK, "<!DOCTYPE html>", "text/html; charset=utf-8"},
		{"serve index.html", "/index.html", http.StatusOK, "<!DOCTYPE html>", "text/html; charset=utf-8"},
		{"serve css", "/css/test.css", http.StatusOK, "body { color: red; }", "text/css; charset=utf-8"},
		{"serve js", "/js/test.js", http.StatusOK, "console.log", "application/javascript; charset=utf-8"},
		{"SPA fallback", "/dashboard", http.StatusOK, "<!DOCTYPE html>", "text/html; charset=utf-8"},
		{"SPA nested route", "/backends/123", http.StatusOK, "<!DOCTYPE html>", "text/html; charset=utf-8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %v, want %v", rec.Code, tt.wantStatus)
			}
			body, _ := io.ReadAll(rec.Body)
			if !strings.Contains(string(body), tt.wantContent) {
				t.Errorf("body missing %q", tt.wantContent)
			}
			if ct := rec.Header().Get("Content-Type"); tt.wantContentType != "" && ct != tt.wantContentType {
				t.Errorf("Content-Type = %v, want %v", ct, tt.wantContentType)
			}
		})
	}
}

func TestServeHTTP_SPAFallback(t *testing.T) {
	testFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>spa</html>"), ModTime: testModTime},
	}
	handler := NewHandlerWithFS(http.FS(testFS))
	for _, p := range []string{"/dashboard", "/backends/1", "/routes/new"} {
		t.Run(p, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("expected 200 for %s, got %d", p, rec.Code)
			}
			body, _ := io.ReadAll(rec.Body)
			if !strings.Contains(string(body), "spa") {
				t.Errorf("expected SPA fallback for %s", p)
			}
		})
	}
}

func TestServeHTTP_PathTraversal(t *testing.T) {
	testFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html></html>"), ModTime: testModTime},
		"secret.txt": &fstest.MapFile{Data: []byte("secret"), ModTime: testModTime},
	}
	handler := NewHandlerWithFS(http.FS(testFS))
	req := httptest.NewRequest(http.MethodGet, "/../secret.txt", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Body)
	if strings.Contains(string(body), "secret") {
		t.Error("Path traversal vulnerability: secret file was exposed")
	}
}

func TestServeHTTP_Directory(t *testing.T) {
	testFS := fstest.MapFS{
		"index.html":        &fstest.MapFile{Data: []byte("<html>root</html>"), ModTime: testModTime},
		"subdir/index.html": &fstest.MapFile{Data: []byte("<html>sub</html>"), ModTime: testModTime},
	}
	handler := NewHandlerWithFS(http.FS(testFS))
	req := httptest.NewRequest(http.MethodGet, "/subdir/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestServeIndex_NoIndexFile(t *testing.T) {
	handler := NewHandlerWithFS(http.FS(fstest.MapFS{}))
	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "web ui not available") {
		t.Errorf("expected 'web ui not available', got %q", string(body))
	}
}

func TestServeFile_CSSCaching(t *testing.T) {
	testFS := fstest.MapFS{
		"css/style.css": &fstest.MapFile{Data: []byte("body{color:red}"), ModTime: testModTime},
		"index.html":    &fstest.MapFile{Data: []byte("<html></html>"), ModTime: testModTime},
	}
	handler := NewHandlerWithFS(http.FS(testFS))
	req := httptest.NewRequest(http.MethodGet, "/css/style.css", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("expected immutable cache for CSS, got %q", cc)
	}
}

func TestServeFile_JSCaching(t *testing.T) {
	testFS := fstest.MapFS{
		"js/app.js":  &fstest.MapFile{Data: []byte("console.log('hello');"), ModTime: testModTime},
		"index.html": &fstest.MapFile{Data: []byte("<html></html>"), ModTime: testModTime},
	}
	handler := NewHandlerWithFS(http.FS(testFS))
	req := httptest.NewRequest(http.MethodGet, "/js/app.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("expected immutable cache for JS, got %q", cc)
	}
}

func TestServeFile_HTMLNoCaching(t *testing.T) {
	testFS := fstest.MapFS{
		"page.html":  &fstest.MapFile{Data: []byte("<html>page</html>"), ModTime: testModTime},
		"index.html": &fstest.MapFile{Data: []byte("<html></html>"), ModTime: testModTime},
	}
	handler := NewHandlerWithFS(http.FS(testFS))
	req := httptest.NewRequest(http.MethodGet, "/page.html", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("expected 'no-cache' for HTML, got %q", cc)
	}
}

func TestServeFile_AssetsPathCaching(t *testing.T) {
	testFS := fstest.MapFS{
		"assets/logo.png": &fstest.MapFile{Data: []byte("PNG"), ModTime: testModTime},
		"index.html":      &fstest.MapFile{Data: []byte("<html></html>"), ModTime: testModTime},
	}
	handler := NewHandlerWithFS(http.FS(testFS))
	req := httptest.NewRequest(http.MethodGet, "/assets/logo.png", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("expected immutable cache for /assets/, got %q", cc)
	}
}

func TestRegisterRoutes(t *testing.T) {
	testFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html></html>"), ModTime: testModTime},
	}
	handler := NewHandlerWithFS(http.FS(testFS))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, "/ui")
	req := httptest.NewRequest(http.MethodGet, "/ui/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestRegisterRoutes_CustomPrefix(t *testing.T) {
	testFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>custom</html>"), ModTime: testModTime},
	}
	handler := NewHandlerWithFS(http.FS(testFS))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, "/admin")
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

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
			if got := getContentType(tt.filepath); got != tt.want {
				t.Errorf("getContentType(%q) = %q, want %q", tt.filepath, got, tt.want)
			}
		})
	}
}

// --- Error path tests with custom filesystems ---

// statFailFile implements http.File where Stat always fails.
type statFailFile struct {
	data []byte
}

func (f *statFailFile) Read(p []byte) (int, error) {
	n := copy(p, f.data)
	f.data = f.data[n:]
	if len(f.data) == 0 {
		return n, io.EOF
	}
	return n, nil
}
func (f *statFailFile) Seek(offset int64, whence int) (int64, error) { return 0, nil }
func (f *statFailFile) Close() error                                 { return nil }
func (f *statFailFile) Readdir(count int) ([]os.FileInfo, error)     { return nil, nil }
func (f *statFailFile) Stat() (os.FileInfo, error)                   { return nil, os.ErrPermission }

// statFailFS opens files successfully but Stat always fails.
type statFailFS struct{}

func (fs *statFailFS) Open(name string) (http.File, error) {
	return &statFailFile{data: []byte("<html>test</html>")}, nil
}

func TestServeHTTP_StatFailure(t *testing.T) {
	handler := NewHandlerWithFS(&statFailFS{})
	req := httptest.NewRequest(http.MethodGet, "/test.html", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	// Open succeeds, Stat fails -> serveIndex -> Open succeeds, Stat fails -> 500
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// openFailFS fails on a specific Open call number.
type openFailFS struct {
	call int
	fail int
}

func (fs *openFailFS) Open(name string) (http.File, error) {
	fs.call++
	if fs.call == fs.fail {
		return nil, os.ErrNotExist
	}
	return &statFailFile{data: []byte("<html>test</html>")}, nil
}

func TestServeIndex_OpenFailure(t *testing.T) {
	// ServeHTTP: Open succeeds (call 1), Stat fails -> serveIndex
	// serveIndex: Open index.html fails (call 2)
	fs := &openFailFS{fail: 2}
	handler := NewHandlerWithFS(fs)
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// normalFile implements http.File with a working Stat.
type normalFile struct {
	data    []byte
	modTime time.Time
	name    string
}

func (f *normalFile) Read(p []byte) (int, error) {
	n := copy(p, f.data)
	f.data = f.data[n:]
	if len(f.data) == 0 {
		return n, io.EOF
	}
	return n, nil
}
func (f *normalFile) Seek(offset int64, whence int) (int64, error) { return 0, nil }
func (f *normalFile) Close() error                                 { return nil }
func (f *normalFile) Readdir(count int) ([]os.FileInfo, error)     { return nil, nil }
func (f *normalFile) Stat() (os.FileInfo, error) {
	return &fileInfo{name: f.name, size: int64(len(f.data)), modTime: f.modTime}, nil
}

type fileInfo struct {
	name    string
	size    int64
	modTime time.Time
}

func (i *fileInfo) Name() string       { return i.name }
func (i *fileInfo) Size() int64        { return i.size }
func (i *fileInfo) Mode() os.FileMode  { return 0644 }
func (i *fileInfo) ModTime() time.Time { return i.modTime }
func (i *fileInfo) IsDir() bool        { return false }
func (i *fileInfo) Sys() interface{}   { return nil }

// serveFileFailOpenFS returns file from ServeHTTP but serveFile re-open fails.
type serveFileFailOpenFS struct{ opens int }

func (fs *serveFileFailOpenFS) Open(name string) (http.File, error) {
	fs.opens++
	if fs.opens == 1 {
		return &normalFile{data: []byte("ok"), name: "test.html", modTime: testModTime}, nil
	}
	return nil, os.ErrNotExist
}

func TestServeFile_OpenFailure(t *testing.T) {
	handler := NewHandlerWithFS(&serveFileFailOpenFS{})
	req := httptest.NewRequest(http.MethodGet, "/test.html", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	// Open succeeds (call 1), Stat succeeds, serveFile re-opens (call 2) fails -> 404
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// serveFileStatFailFS: ServeHTTP opens+stats OK, serveFile re-opens and Stat fails.
type serveFileStatFailFS struct{ opens int }

func (fs *serveFileStatFailFS) Open(name string) (http.File, error) {
	fs.opens++
	if fs.opens == 1 {
		return &normalFile{data: []byte("ok"), name: "test.html", modTime: testModTime}, nil
	}
	return &statFailFile{data: []byte("ok")}, nil
}

func TestServeFile_StatFailure(t *testing.T) {
	handler := NewHandlerWithFS(&serveFileStatFailFS{})
	req := httptest.NewRequest(http.MethodGet, "/test.html", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	// serveFile re-opens -> Stat fails -> 404
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// dirInfo implements os.FileInfo for a directory.
type dirInfo struct{}

func (i *dirInfo) Name() string       { return "subdir" }
func (i *dirInfo) Size() int64        { return 0 }
func (i *dirInfo) Mode() os.FileMode  { return 0755 | os.ModeDir }
func (i *dirInfo) ModTime() time.Time { return testModTime }
func (i *dirInfo) IsDir() bool        { return true }
func (i *dirInfo) Sys() interface{}   { return nil }

// dirFileTest implements http.File that reports as a directory via Stat.
type dirFileTest struct{}

func (f *dirFileTest) Read(p []byte) (int, error)                   { return 0, io.EOF }
func (f *dirFileTest) Seek(offset int64, whence int) (int64, error) { return 0, nil }
func (f *dirFileTest) Close() error                                 { return nil }
func (f *dirFileTest) Readdir(count int) ([]os.FileInfo, error)     { return nil, nil }
func (f *dirFileTest) Stat() (os.FileInfo, error)                   { return &dirInfo{}, nil }

// dirNoIndexFS returns a directory for /subdir but no index.html inside.
type dirNoIndexFS struct{ opens int }

func (fs *dirNoIndexFS) Open(name string) (http.File, error) {
	fs.opens++
	if name == "subdir" || name == "subdir/" {
		return &dirFileTest{}, nil
	}
	if name == "subdir/index.html" {
		return nil, os.ErrNotExist
	}
	if name == "index.html" {
		return &statFailFile{data: []byte("<html>fallback</html>")}, nil
	}
	return nil, os.ErrNotExist
}

func TestServeHTTP_DirWithoutIndexHTML_FallsBack(t *testing.T) {
	handler := NewHandlerWithFS(&dirNoIndexFS{})
	req := httptest.NewRequest(http.MethodGet, "/subdir/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	// dir detected -> tries subdir/index.html -> fails -> serveIndex -> Open succeeds, Stat fails -> 500
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}
