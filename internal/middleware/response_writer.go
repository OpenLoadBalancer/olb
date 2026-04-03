package middleware

import (
	"bufio"
	"net"
	"net/http"
	"sync"
)

// ResponseWriter extends http.ResponseWriter with additional capabilities.
type ResponseWriter interface {
	http.ResponseWriter

	// Status returns the HTTP status code that was written.
	// Returns 200 if WriteHeader has not been called.
	Status() int

	// BytesWritten returns the total number of bytes written.
	BytesWritten() int64

	// Written returns true if WriteHeader has been called.
	Written() bool
}

// responseWriter wraps http.ResponseWriter to capture metadata.
type responseWriter struct {
	http.ResponseWriter
	status       int
	bytesWritten int64
	written      bool
}

// pool for recycling responseWriter objects.
var responseWriterPool = sync.Pool{
	New: func() any {
		return &responseWriter{}
	},
}

// NewResponseWriter wraps an http.ResponseWriter with metadata capture.
func NewResponseWriter(w http.ResponseWriter) ResponseWriter {
	rw := responseWriterPool.Get().(*responseWriter)
	rw.ResponseWriter = w
	rw.status = http.StatusOK
	rw.bytesWritten = 0
	rw.written = false
	return rw
}

// WriteHeader captures the status code and delegates to the underlying writer.
func (rw *responseWriter) WriteHeader(status int) {
	if rw.written {
		return
	}
	rw.status = status
	rw.written = true
	rw.ResponseWriter.WriteHeader(status)
}

// Write captures the byte count and delegates to the underlying writer.
// If WriteHeader has not been called, it implicitly writes a 200 status.
func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += int64(n)
	return n, err
}

// Status returns the HTTP status code.
// Returns 200 if WriteHeader has not been called.
func (rw *responseWriter) Status() int {
	return rw.status
}

// BytesWritten returns the total number of bytes written.
func (rw *responseWriter) BytesWritten() int64 {
	return rw.bytesWritten
}

// Written returns true if WriteHeader has been called.
func (rw *responseWriter) Written() bool {
	return rw.written
}

// Release returns the responseWriter to the pool for reuse.
// This should be called when the request is complete.
func (rw *responseWriter) Release() {
	rw.ResponseWriter = nil
	responseWriterPool.Put(rw)
}

// Hijack implements http.Hijacker if the underlying writer supports it.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

// Flush implements http.Flusher if the underlying writer supports it.
func (rw *responseWriter) Flush() {
	flusher, ok := rw.ResponseWriter.(http.Flusher)
	if ok {
		flusher.Flush()
	}
}

// Push implements http.Pusher if the underlying writer supports it (HTTP/2).
func (rw *responseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := rw.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

// Unwrap returns the underlying ResponseWriter.
// This allows middleware to access the original writer if needed.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// Ensure responseWriter implements all optional interfaces.
var (
	_ http.Hijacker = (*responseWriter)(nil)
	_ http.Flusher  = (*responseWriter)(nil)
	_ http.Pusher   = (*responseWriter)(nil)
)
