package middleware

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewCompressionMiddleware(t *testing.T) {
	tests := []struct {
		name    string
		config  CompressionConfig
		wantErr bool
	}{
		{
			name:    "default config",
			config:  CompressionConfig{},
			wantErr: false,
		},
		{
			name: "custom config",
			config: CompressionConfig{
				MinSize:      512,
				Level:        gzip.BestCompression,
				ContentTypes: []string{"text/html", "application/json"},
				ExcludePaths: []string{"/api/health"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw, err := NewCompressionMiddleware(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewCompressionMiddleware() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if mw == nil {
				t.Error("NewCompressionMiddleware() returned nil")
			}
		})
	}
}

func TestCompressionMiddleware_Name(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{})
	if mw.Name() != "compression" {
		t.Errorf("Name() = %v, want %v", mw.Name(), "compression")
	}
}

func TestCompressionMiddleware_Priority(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{})
	if mw.Priority() != PriorityCompress {
		t.Errorf("Priority() = %v, want %v", mw.Priority(), PriorityCompress)
	}
}

func TestCompressionMiddleware_GzipEncoding(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{
		MinSize:      10,
		ContentTypes: []string{"text/plain"},
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Hello, World! This is a test message that is long enough to compress."))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Check Content-Encoding header
	if rr.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("Content-Encoding = %v, want gzip", rr.Header().Get("Content-Encoding"))
	}

	// Check Vary header
	if !strings.Contains(rr.Header().Get("Vary"), "Accept-Encoding") {
		t.Errorf("Vary header = %v, should contain Accept-Encoding", rr.Header().Get("Vary"))
	}

	// Verify it's actually gzip compressed
	body := rr.Body.Bytes()
	if len(body) == 0 {
		t.Fatal("Response body is empty")
	}

	// Try to decompress
	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to decompress: %v", err)
	}

	expected := "Hello, World! This is a test message that is long enough to compress."
	if string(decompressed) != expected {
		t.Errorf("Decompressed content = %v, want %v", string(decompressed), expected)
	}
}

func TestCompressionMiddleware_DeflateEncoding(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{
		MinSize:      10,
		ContentTypes: []string{"text/plain"},
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Hello, World! This is a test message that is long enough to compress."))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "deflate")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Check Content-Encoding header
	if rr.Header().Get("Content-Encoding") != "deflate" {
		t.Errorf("Content-Encoding = %v, want deflate", rr.Header().Get("Content-Encoding"))
	}

	// Verify it's actually deflate compressed
	body := rr.Body.Bytes()
	if len(body) == 0 {
		t.Fatal("Response body is empty")
	}

	// Try to decompress
	reader := flate.NewReader(bytes.NewReader(body))
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to decompress: %v", err)
	}

	expected := "Hello, World! This is a test message that is long enough to compress."
	if string(decompressed) != expected {
		t.Errorf("Decompressed content = %v, want %v", string(decompressed), expected)
	}
}

func TestCompressionMiddleware_NoAcceptEncoding(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{
		MinSize:      10,
		ContentTypes: []string{"text/plain"},
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Hello, World! This is a test message that is long enough to compress."))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No Accept-Encoding header
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should not be compressed
	if rr.Header().Get("Content-Encoding") != "" {
		t.Errorf("Content-Encoding = %v, want empty", rr.Header().Get("Content-Encoding"))
	}

	body := rr.Body.String()
	expected := "Hello, World! This is a test message that is long enough to compress."
	if body != expected {
		t.Errorf("Body = %v, want %v", body, expected)
	}
}

func TestCompressionMiddleware_ResponseTooSmall(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{
		MinSize:      100,
		ContentTypes: []string{"text/plain"},
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Short"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should not be compressed (too small)
	if rr.Header().Get("Content-Encoding") != "" {
		t.Errorf("Content-Encoding = %v, want empty", rr.Header().Get("Content-Encoding"))
	}

	body := rr.Body.String()
	expected := "Short"
	if body != expected {
		t.Errorf("Body = %v, want %v", body, expected)
	}
}

func TestCompressionMiddleware_ExcludedContentType(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{
		MinSize:      10,
		ContentTypes: []string{"text/plain"}, // Only text/plain, not text/html
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body>Hello, World! This is a test message that is long enough.</body></html>"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should not be compressed (content type not in list)
	if rr.Header().Get("Content-Encoding") != "" {
		t.Errorf("Content-Encoding = %v, want empty", rr.Header().Get("Content-Encoding"))
	}
}

func TestCompressionMiddleware_ExcludedPath(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{
		MinSize:      10,
		ContentTypes: []string{"text/plain"},
		ExcludePaths: []string{"/api/"},
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Hello, World! This is a test message that is long enough to compress."))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should not be compressed (path excluded)
	if rr.Header().Get("Content-Encoding") != "" {
		t.Errorf("Content-Encoding = %v, want empty", rr.Header().Get("Content-Encoding"))
	}
}

func TestCompressionMiddleware_ExcludedUserAgent(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{
		MinSize:       10,
		ContentTypes:  []string{"text/plain"},
		ExcludeAgents: []string{"bot", "crawler"},
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Hello, World! This is a test message that is long enough to compress."))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", "MyBot/1.0 Crawler")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should not be compressed (user agent excluded)
	if rr.Header().Get("Content-Encoding") != "" {
		t.Errorf("Content-Encoding = %v, want empty", rr.Header().Get("Content-Encoding"))
	}
}

func TestCompressionMiddleware_RangeRequest(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{
		MinSize:      10,
		ContentTypes: []string{"text/plain"},
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Hello, World! This is a test message that is long enough to compress."))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Range", "bytes=0-10")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should not be compressed (range request)
	if rr.Header().Get("Content-Encoding") != "" {
		t.Errorf("Content-Encoding = %v, want empty", rr.Header().Get("Content-Encoding"))
	}
}

func TestCompressionMiddleware_AlreadyCompressed(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{
		MinSize:      10,
		ContentTypes: []string{"text/plain"},
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "br") // Already compressed with brotli
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Hello, World! This is a test message that is long enough to compress."))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should keep original encoding
	if rr.Header().Get("Content-Encoding") != "br" {
		t.Errorf("Content-Encoding = %v, want br", rr.Header().Get("Content-Encoding"))
	}
}

func TestCompressionMiddleware_LargeResponse(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{
		MinSize:      100,
		ContentTypes: []string{"text/plain"},
	})

	// Create a large response
	largeContent := strings.Repeat("Hello, World! ", 1000) // ~14KB

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(largeContent))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should be compressed
	if rr.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("Content-Encoding = %v, want gzip", rr.Header().Get("Content-Encoding"))
	}

	// Verify compressed size is smaller than original
	if rr.Body.Len() >= len(largeContent) {
		t.Errorf("Compressed size %d should be smaller than original %d", rr.Body.Len(), len(largeContent))
	}

	// Decompress and verify content
	reader, err := gzip.NewReader(bytes.NewReader(rr.Body.Bytes()))
	if err != nil {
		t.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to decompress: %v", err)
	}

	if string(decompressed) != largeContent {
		t.Error("Decompressed content does not match original")
	}
}

func TestCompressionMiddleware_DefaultContentTypes(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{
		MinSize: 10,
		// No ContentTypes specified, should use defaults
	})

	tests := []struct {
		contentType    string
		shouldCompress bool
	}{
		{"text/html", true},
		{"text/plain", true},
		{"text/css", true},
		{"application/json", true},
		{"application/javascript", true},
		{"application/xml", true},
		{"application/rss+xml", true},
		{"application/atom+xml", true},
		{"image/svg+xml", true},
		{"image/png", false},
		{"application/octet-stream", false},
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.Write([]byte("Hello, World! This is a test message that is long enough to compress."))
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Accept-Encoding", "gzip")
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			isCompressed := rr.Header().Get("Content-Encoding") == "gzip"
			if isCompressed != tt.shouldCompress {
				t.Errorf("Content-Encoding = %v, should compress = %v", rr.Header().Get("Content-Encoding"), tt.shouldCompress)
			}
		})
	}
}

func TestCompressionMiddleware_GzipPreferredOverDeflate(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{
		MinSize:      10,
		ContentTypes: []string{"text/plain"},
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Hello, World! This is a test message that is long enough to compress."))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// Both gzip and deflate are acceptable
	req.Header.Set("Accept-Encoding", "deflate, gzip")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should prefer gzip
	if rr.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("Content-Encoding = %v, want gzip (preferred over deflate)", rr.Header().Get("Content-Encoding"))
	}
}

func TestCompressionMiddleware_WriteHeaderMultipleTimes(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{
		MinSize:      10,
		ContentTypes: []string{"text/plain"},
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.WriteHeader(http.StatusNotFound) // Should be ignored
		w.Write([]byte("Hello, World! This is a test message that is long enough to compress."))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should use first status code
	if rr.Code != http.StatusOK {
		t.Errorf("Status = %v, want %v", rr.Code, http.StatusOK)
	}
}

func TestCompressionMiddleware_EmptyResponse(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{
		MinSize:      10,
		ContentTypes: []string{"text/plain"},
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusNoContent)
		// No body
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should not compress empty response
	if rr.Header().Get("Content-Encoding") != "" {
		t.Errorf("Content-Encoding = %v, want empty", rr.Header().Get("Content-Encoding"))
	}
}

func TestCompressionMiddleware_StatusCodePreserved(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{
		MinSize:      10,
		ContentTypes: []string{"text/plain"},
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Hello, World! This is a test message that is long enough to compress."))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Status = %v, want %v", rr.Code, http.StatusNotFound)
	}

	if rr.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("Content-Encoding = %v, want gzip", rr.Header().Get("Content-Encoding"))
	}
}

func TestCompressionMiddleware_IsCompressibleContentType(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{
		ContentTypes: []string{"text/", "application/json", "image/svg+xml"},
	})

	tests := []struct {
		contentType string
		expected    bool
	}{
		{"", false},
		{"text/html", true},
		{"text/plain", true},
		{"text/css", true},
		{"application/json", true},
		{"image/svg+xml", true},
		{"image/png", false},
		{"application/octet-stream", false},
		{"video/mp4", false},
		{"TEXT/HTML", true},        // case insensitive
		{"Application/JSON", true}, // case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			result := mw.isCompressibleContentType(tt.contentType)
			if result != tt.expected {
				t.Errorf("isCompressibleContentType(%q) = %v, want %v", tt.contentType, result, tt.expected)
			}
		})
	}
}

func TestCompressionMiddleware_Flush(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{
		MinSize:      10,
		ContentTypes: []string{"text/plain"},
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		// Write enough to trigger compression
		w.Write([]byte(strings.Repeat("Hello, World! ", 100)))
		// Call Flush via the http.Flusher interface
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Verify the response is still valid gzip after flush
	if rr.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("Content-Encoding = %v, want gzip", rr.Header().Get("Content-Encoding"))
	}

	body := rr.Body.Bytes()
	if len(body) == 0 {
		t.Fatal("Response body is empty")
	}

	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer reader.Close()

	_, err = io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to decompress after flush: %v", err)
	}
}

func TestCompressionMiddleware_NoContentLengthHeader(t *testing.T) {
	mw, _ := NewCompressionMiddleware(CompressionConfig{
		MinSize:      10,
		ContentTypes: []string{"text/plain"},
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "1000") // This should be removed
		w.Write([]byte("Hello, World! This is a test message that is long enough to compress."))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Content-Length should be removed since compression changes the length
	if rr.Header().Get("Content-Length") != "" {
		t.Errorf("Content-Length = %v, should be removed", rr.Header().Get("Content-Length"))
	}
}
