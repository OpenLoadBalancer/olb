// Package middleware provides HTTP middleware infrastructure for OpenLoadBalancer.
package middleware

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

// defaultCompressibleTypes contains the default list of content types that should be compressed.
var defaultCompressibleTypes = []string{
	"text/",
	"application/json",
	"application/javascript",
	"application/xml",
	"application/rss+xml",
	"application/atom+xml",
	"image/svg+xml",
}

// CompressionConfig configures the compression middleware.
type CompressionConfig struct {
	MinSize       int      // Minimum response size to compress (default: 1024 bytes)
	Level         int      // gzip compression level (-2 to 9, default: -1 for default)
	ContentTypes  []string // Content types to compress (default: common text types)
	ExcludePaths  []string // Path prefixes to exclude
	ExcludeAgents []string // User-Agent substrings to exclude
	MaxBufferSize int      // Maximum response body to buffer before bypassing compression (default: 8MB)
}

// CompressionMiddleware implements gzip/deflate response compression.
type CompressionMiddleware struct {
	config          CompressionConfig
	allowedTypes    map[string]bool
	lowercaseAgents []string // pre-computed lowercase agent strings
}

// compressWriter wraps http.ResponseWriter to buffer and optionally compress response.
type compressWriter struct {
	http.ResponseWriter
	config       *CompressionConfig
	allowedTypes map[string]bool // pre-computed content type lookup
	encoding     string
	buffer       *bytes.Buffer
	writer       io.WriteCloser
	minSize      int
	wroteHeader  bool
	status       int
}

// gzipWriterPool pools gzip.Writer instances for reuse.
var gzipWriterPool = sync.Pool{
	New: func() any {
		// Create with default compression level, we'll reset with the correct level
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
		return w
	},
}

// flateWriterPool pools flate.Writer instances for reuse.
var flateWriterPool = sync.Pool{
	New: func() any {
		// Create with default compression level
		w, _ := flate.NewWriter(io.Discard, flate.DefaultCompression)
		return w
	},
}

// bufferPool pools bytes.Buffer instances for buffering response content.
var bufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

// NewCompressionMiddleware creates a new compression middleware.
func NewCompressionMiddleware(config CompressionConfig) (*CompressionMiddleware, error) {
	// Set defaults
	if config.MinSize <= 0 {
		config.MinSize = 1024
	}
	if config.Level == 0 {
		config.Level = gzip.DefaultCompression // -1
	}
	if config.MaxBufferSize <= 0 {
		config.MaxBufferSize = 8 * 1024 * 1024 // 8MB
	}
	if len(config.ContentTypes) == 0 {
		config.ContentTypes = defaultCompressibleTypes
	}

	// Build allowed types map for fast lookup
	allowedTypes := make(map[string]bool, len(config.ContentTypes))
	for _, ct := range config.ContentTypes {
		allowedTypes[strings.ToLower(ct)] = true
	}

	// Pre-compute lowercase exclude agents for fast per-request lookup
	lowercaseAgents := make([]string, len(config.ExcludeAgents))
	for i, agent := range config.ExcludeAgents {
		lowercaseAgents[i] = strings.ToLower(agent)
	}

	return &CompressionMiddleware{
		config:          config,
		allowedTypes:    allowedTypes,
		lowercaseAgents: lowercaseAgents,
	}, nil
}

// Name returns the middleware name.
func (m *CompressionMiddleware) Name() string {
	return "compression"
}

// Priority returns the middleware priority.
func (m *CompressionMiddleware) Priority() int {
	return PriorityCompress
}

// Wrap wraps the next handler with compression.
func (m *CompressionMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always set Vary header so caches know the response may differ by Accept-Encoding
		w.Header().Add("Vary", "Accept-Encoding")

		// Check if compression should be applied
		encoding := m.selectEncoding(r)
		if encoding == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Check exclusions
		if !m.shouldCompress(r, w) {
			next.ServeHTTP(w, r)
			return
		}

		// Create compression writer
		cw := &compressWriter{
			ResponseWriter: w,
			config:         &m.config,
			allowedTypes:   m.allowedTypes,
			encoding:       encoding,
			buffer:         bufferPool.Get().(*bytes.Buffer),
			minSize:        m.config.MinSize,
			status:         http.StatusOK,
		}
		cw.buffer.Reset()

		// Call next handler
		next.ServeHTTP(cw, r)

		// Close the compression writer to finalize
		if err := cw.Close(); err != nil {
			cw.WriteHeader(http.StatusInternalServerError)
		}
	})
}

// shouldCompress determines if the response should be compressed based on various criteria.
func (m *CompressionMiddleware) shouldCompress(r *http.Request, _ http.ResponseWriter) bool {
	// Check excluded paths
	path := r.URL.Path
	for _, prefix := range m.config.ExcludePaths {
		if strings.HasPrefix(path, prefix) && (len(path) == len(prefix) || path[len(prefix)] == '/' || prefix[len(prefix)-1] == '/') {
			return false
		}
	}

	// Check excluded user agents
	userAgent := strings.ToLower(r.UserAgent())
	for _, agent := range m.lowercaseAgents {
		if strings.Contains(userAgent, agent) {
			return false
		}
	}

	// Check for Range request (don't compress partial content)
	if r.Header.Get("Range") != "" {
		return false
	}

	return true
}

// selectEncoding selects the best encoding from the Accept-Encoding header.
// Returns "gzip", "deflate", or "" if no supported encoding.
func (m *CompressionMiddleware) selectEncoding(r *http.Request) string {
	acceptEncoding := strings.ToLower(r.Header.Get("Accept-Encoding"))
	if acceptEncoding == "" {
		return ""
	}

	// Check for gzip first (preferred)
	if strings.Contains(acceptEncoding, "gzip") {
		return "gzip"
	}

	// Check for deflate
	if strings.Contains(acceptEncoding, "deflate") {
		return "deflate"
	}

	return ""
}

// isCompressibleContentType checks if the content type should be compressed.
func (m *CompressionMiddleware) isCompressibleContentType(contentType string) bool {
	if contentType == "" {
		return false
	}

	contentType = strings.ToLower(contentType)

	// Check exact match
	if m.allowedTypes[contentType] {
		return true
	}

	// Check prefix matches (e.g., "text/" matches "text/html")
	for ct := range m.allowedTypes {
		if strings.HasSuffix(ct, "/") && strings.HasPrefix(contentType, ct) {
			return true
		}
	}

	return false
}

// WriteHeader captures the status code.
func (cw *compressWriter) WriteHeader(status int) {
	if cw.wroteHeader {
		return
	}
	cw.status = status
	cw.wroteHeader = true
}

// Write buffers the response content.
func (cw *compressWriter) Write(p []byte) (int, error) {
	if !cw.wroteHeader {
		cw.WriteHeader(http.StatusOK)
	}

	// If we've already started compression, write directly
	if cw.writer != nil {
		return cw.writer.Write(p)
	}

	// Check max buffer size to prevent unbounded memory growth
	if cw.buffer.Len()+len(p) > cw.config.MaxBufferSize {
		// Exceeded max buffer size - flush uncompressed and pass through
		if err := cw.flushUncompressed(); err != nil {
			return 0, err
		}
		return cw.ResponseWriter.Write(p)
	}

	// Buffer the content
	n, err := cw.buffer.Write(p)

	// If buffer exceeds minSize, start compression
	if cw.buffer.Len() >= cw.minSize {
		if err := cw.startCompression(); err != nil {
			return n, err
		}
	}

	return n, err
}

// startCompression initializes the compression writer and flushes the buffer.
func (cw *compressWriter) startCompression() error {
	// Check if response already has Content-Encoding (already compressed)
	if cw.ResponseWriter.Header().Get("Content-Encoding") != "" {
		return cw.flushUncompressed()
	}

	// Check if we should compress based on content type
	contentType := cw.ResponseWriter.Header().Get("Content-Type")
	if !cw.isContentTypeCompressible(contentType) {
		// Don't compress, flush buffer as-is
		return cw.flushUncompressed()
	}

	// Remove Content-Length if set (it will be wrong after compression)
	cw.ResponseWriter.Header().Del("Content-Length")

	// Set Content-Encoding header
	cw.ResponseWriter.Header().Set("Content-Encoding", cw.encoding)

	// Write the status header
	cw.ResponseWriter.WriteHeader(cw.status)

	// Create the compression writer based on encoding
	var err error
	switch cw.encoding {
	case "gzip":
		cw.writer, err = cw.getGzipWriter()
	case "deflate":
		cw.writer, err = cw.getFlateWriter()
	default:
		return cw.flushUncompressed()
	}

	if err != nil {
		return cw.flushUncompressed()
	}

	// Flush buffered content through compressor
	if cw.buffer.Len() > 0 {
		_, err = cw.writer.Write(cw.buffer.Bytes())
		if err != nil {
			return err
		}
	}

	return nil
}

// isContentTypeCompressible checks if the content type should be compressed.
func (cw *compressWriter) isContentTypeCompressible(contentType string) bool {
	if contentType == "" {
		return false
	}

	contentType = strings.ToLower(contentType)

	// Check exact match
	if cw.allowedTypes[contentType] {
		return true
	}

	// Check prefix matches (e.g., "text/" matches "text/html")
	for ct := range cw.allowedTypes {
		if strings.HasSuffix(ct, "/") && strings.HasPrefix(contentType, ct) {
			return true
		}
	}

	return false
}

// flushUncompressed writes the buffered content without compression.
func (cw *compressWriter) flushUncompressed() error {
	cw.ResponseWriter.WriteHeader(cw.status)
	if cw.buffer.Len() > 0 {
		_, err := cw.ResponseWriter.Write(cw.buffer.Bytes())
		return err
	}
	return nil
}

// getGzipWriter gets a gzip writer from the pool.
func (cw *compressWriter) getGzipWriter() (io.WriteCloser, error) {
	w := gzipWriterPool.Get().(*gzip.Writer)
	w.Reset(cw.ResponseWriter)
	return &gzipPooledWriter{writer: w}, nil
}

// getFlateWriter gets a flate writer from the pool.
func (cw *compressWriter) getFlateWriter() (io.WriteCloser, error) {
	w := flateWriterPool.Get().(*flate.Writer)
	w.Reset(cw.ResponseWriter)
	return &flatePooledWriter{writer: w}, nil
}

// Close finalizes the compression and returns resources to pools.
func (cw *compressWriter) Close() error {
	defer func() {
		// Return buffer to pool
		if cw.buffer != nil {
			cw.buffer.Reset()
			bufferPool.Put(cw.buffer)
			cw.buffer = nil
		}
	}()

	// If we never started compression
	if cw.writer == nil {
		// Check if we should compress based on accumulated content
		if cw.buffer.Len() >= cw.minSize {
			if err := cw.startCompression(); err != nil {
				return cw.flushUncompressed()
			}
		} else {
			// Too small, write uncompressed
			return cw.flushUncompressed()
		}
	}

	// Close the compression writer
	if cw.writer != nil {
		if err := cw.writer.Close(); err != nil {
			return err
		}
	}

	return nil
}

// Header returns the header map.
func (cw *compressWriter) Header() http.Header {
	return cw.ResponseWriter.Header()
}

// gzipPooledWriter wraps a pooled gzip.Writer to handle pool returns.
type gzipPooledWriter struct {
	writer *gzip.Writer
}

func (w *gzipPooledWriter) Write(p []byte) (int, error) {
	return w.writer.Write(p)
}

func (w *gzipPooledWriter) Close() error {
	err := w.writer.Close()
	w.writer.Reset(io.Discard)
	gzipWriterPool.Put(w.writer)
	return err
}

// flatePooledWriter wraps a pooled flate.Writer to handle pool returns.
type flatePooledWriter struct {
	writer *flate.Writer
}

func (w *flatePooledWriter) Write(p []byte) (int, error) {
	return w.writer.Write(p)
}

func (w *flatePooledWriter) Close() error {
	if err := w.writer.Close(); err != nil {
		w.writer.Reset(io.Discard)
		flateWriterPool.Put(w.writer)
		return err
	}
	w.writer.Reset(io.Discard)
	flateWriterPool.Put(w.writer)
	return nil
}

// Ensure compressWriter implements http.Flusher
func (cw *compressWriter) Flush() {
	if cw.writer != nil {
		// Flush the compression writer if possible
		if flusher, ok := cw.writer.(interface{ Flush() error }); ok {
			flusher.Flush()
		}
	}
	if flusher, ok := cw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
