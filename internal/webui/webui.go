// Package webui provides the Web UI for OpenLoadBalancer.
package webui

import (
	"embed"
	"net/http"
	"path"
	"strings"
)

//go:embed all:assets
//go:embed index.html
//go:embed favicon.svg
var distFS embed.FS

// Handler serves the Web UI static files and SPA fallback.
type Handler struct {
	static http.FileSystem
}

// NewHandler creates a new Web UI handler.
func NewHandler() (*Handler, error) {
	return &Handler{
		static: http.FS(distFS),
	}, nil
}

// NewHandlerWithFS creates a handler with a custom filesystem (for testing).
func NewHandlerWithFS(filesystem http.FileSystem) *Handler {
	return &Handler{
		static: filesystem,
	}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Get raw path and check for path traversal attempts before cleaning
	rawPath := r.URL.Path

	// Prevent path traversal - reject paths containing ..
	if strings.Contains(rawPath, "..") {
		h.serveIndex(w, r)
		return
	}

	// Clean the path
	cleanPath := path.Clean(rawPath)

	// Ensure path starts with /
	if !strings.HasPrefix(cleanPath, "/") {
		cleanPath = "/" + cleanPath
	}

	// Remove leading / for filesystem lookup
	fsPath := strings.TrimPrefix(cleanPath, "/")

	// Try to serve the file
	file, err := h.static.Open(fsPath)
	if err != nil {
		// File not found, serve index.html for SPA routing
		h.serveIndex(w, r)
		return
	}
	defer file.Close()

	// Check if it's a directory
	stat, err := file.Stat()
	if err != nil {
		h.serveIndex(w, r)
		return
	}

	if stat.IsDir() {
		h.serveIndex(w, r)
		return
	}

	// Serve the file
	h.serveFile(w, r, fsPath)
}

// serveFile serves a specific file from the static filesystem.
func (h *Handler) serveFile(w http.ResponseWriter, r *http.Request, filepath string) {
	file, err := h.static.Open(filepath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Set appropriate content type based on extension
	contentType := getContentType(filepath)
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}

	// Set caching headers for static assets
	if strings.HasPrefix(filepath, "assets/") || strings.HasSuffix(filepath, ".css") || strings.HasSuffix(filepath, ".js") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else if strings.HasSuffix(filepath, ".html") {
		w.Header().Set("Cache-Control", "no-cache")
	}

	http.ServeContent(w, r, filepath, stat.ModTime(), file)
}

// serveIndex serves the index.html for SPA routing.
func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")

	file, err := h.static.Open("index.html")
	if err != nil {
		http.Error(w, "index.html not found: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		http.Error(w, "failed to stat index.html", http.StatusInternalServerError)
		return
	}

	http.ServeContent(w, r, "index.html", stat.ModTime(), file)
}

// getContentType returns the MIME type for a given file path.
func getContentType(filepath string) string {
	ext := strings.ToLower(path.Ext(filepath))
	switch ext {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".otf":
		return "font/otf"
	case ".eot":
		return "application/vnd.ms-fontobject"
	default:
		return ""
	}
}

// RegisterRoutes registers the Web UI routes with the provided mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, prefix string) {
	// Serve static files and SPA
	mux.Handle(prefix+"/", http.StripPrefix(prefix, h))
}
