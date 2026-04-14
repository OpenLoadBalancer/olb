package l7

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/openloadbalancer/olb/internal/backend"
)

// grpcFrameHeaderPool reuses 5-byte header buffers across parseGRPCFrame calls.
var grpcFrameHeaderPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 5)
		return &buf
	},
}

// IsGRPCRequest checks if the request is a gRPC request.
func IsGRPCRequest(r *http.Request) bool {
	contentType := r.Header.Get("Content-Type")
	return strings.HasPrefix(contentType, "application/grpc")
}

// GRPCConfig configures gRPC proxy behavior.
type GRPCConfig struct {
	// EnableGRPC enables gRPC proxying.
	EnableGRPC bool

	// MaxMessageSize is the maximum gRPC message size.
	MaxMessageSize int

	// Timeout is the default timeout for gRPC calls.
	Timeout time.Duration

	// EnableGRPWeb enables gRPC-Web support.
	EnableGRPCWeb bool
}

// DefaultGRPCConfig returns a default gRPC configuration.
func DefaultGRPCConfig() *GRPCConfig {
	return &GRPCConfig{
		EnableGRPC:     true,
		MaxMessageSize: 4 * 1024 * 1024, // 4MB
		Timeout:        30 * time.Second,
		EnableGRPCWeb:  true,
	}
}

// GRPCHandler handles gRPC proxying.
type GRPCHandler struct {
	config      *GRPCConfig
	transport   http.RoundTripper
	trustedNets []*net.IPNet
}

// SetTrustedNets sets the trusted proxy networks for XFF handling.
func (h *GRPCHandler) SetTrustedNets(nets []*net.IPNet) {
	h.trustedNets = nets
}

// NewGRPCHandler creates a new gRPC handler.
func NewGRPCHandler(config *GRPCConfig) *GRPCHandler {
	if config == nil {
		config = DefaultGRPCConfig()
	}

	return &GRPCHandler{
		config:    config,
		transport: createGRPCTransport(),
	}
}

// createGRPCTransport creates an HTTP/2 transport for gRPC.
func createGRPCTransport() http.RoundTripper {
	// For gRPC, we need HTTP/2 support
	// The transport should support h2c (HTTP/2 without TLS)
	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// Allow HTTP/2 without TLS (h2c)
		// This is done by not setting ForceAttemptHTTP2
	}
}

// HandleGRPC handles a gRPC request.
func (gh *GRPCHandler) HandleGRPC(w http.ResponseWriter, r *http.Request, b *backend.Backend) error {
	if !gh.config.EnableGRPC {
		return errors.New("gRPC disabled")
	}

	// Acquire connection slot
	if !b.AcquireConn() {
		return errors.New("backend at max connections")
	}
	defer b.ReleaseConn()

	// Prepare outbound request
	outReq, err := gh.prepareGRPCRequest(r, b)
	if err != nil {
		return err
	}

	// Set timeout
	ctx := outReq.Context()
	if gh.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, gh.config.Timeout)
		defer cancel()
		outReq = outReq.WithContext(ctx)
	}

	// Execute request
	resp, err := gh.transport.RoundTrip(outReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Copy response headers (excluding trailers)
	copyGRPCHeaders(w.Header(), resp.Header)

	// Write status code
	w.WriteHeader(resp.StatusCode)

	// Copy response body (bounded to prevent DoS from unbounded backend response)
	maxRespSize := int64(gh.config.MaxMessageSize)
	if maxRespSize <= 0 {
		maxRespSize = 4 * 1024 * 1024 // 4MB default
	}
	_, err = io.Copy(w, io.LimitReader(resp.Body, maxRespSize))
	if err != nil {
		return err
	}

	// Copy trailers if present
	if trailers := resp.Trailer; len(trailers) > 0 {
		for key, values := range trailers {
			for _, value := range values {
				w.Header().Add(http.TrailerPrefix+key, value)
			}
		}
	}

	return nil
}

// prepareGRPCRequest creates the outbound gRPC request.
func (gh *GRPCHandler) prepareGRPCRequest(r *http.Request, b *backend.Backend) (*http.Request, error) {
	// Clone the request
	outReq := r.Clone(r.Context())

	// Set the URL to point to the backend
	backendURL, err := url.Parse("http://" + b.Address)
	if err != nil {
		return nil, fmt.Errorf("invalid backend address: %w", err)
	}

	outReq.URL.Scheme = backendURL.Scheme
	outReq.URL.Host = backendURL.Host
	outReq.Host = r.Host
	outReq.RequestURI = ""

	// Set X-Forwarded-For
	clientIP := trustedClientIP(r, gh.trustedNets)
	if prior := outReq.Header.Get("X-Forwarded-For"); prior != "" {
		outReq.Header.Set("X-Forwarded-For", prior+", "+clientIP)
	} else {
		outReq.Header.Set("X-Forwarded-For", clientIP)
	}

	// Set X-Real-IP
	outReq.Header.Set("X-Real-IP", clientIP)

	// Set X-Forwarded-Proto
	proto := "http"
	if r.TLS != nil {
		proto = "https"
	}
	outReq.Header.Set("X-Forwarded-Proto", proto)

	// Ensure HTTP/2 for gRPC
	// gRPC requires HTTP/2
	outReq.Proto = "HTTP/2.0"
	outReq.ProtoMajor = 2
	outReq.ProtoMinor = 0

	return outReq, nil
}

// copyGRPCHeaders copies headers from source to destination, excluding hop-by-hop and trailers.
func copyGRPCHeaders(dst, src http.Header) {
	for key, values := range src {
		// Skip hop-by-hop headers
		if isHopByHopHeader(key) {
			continue
		}
		// Skip trailer headers (they'll be handled separately)
		if strings.HasPrefix(key, "Trailer") {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

// IsGRPCWebRequest checks if the request is a gRPC-Web request.
func IsGRPCWebRequest(r *http.Request) bool {
	contentType := r.Header.Get("Content-Type")
	return strings.HasPrefix(contentType, "application/grpc-web")
}

// isGRPCWebTextRequest checks if the request uses base64-encoded gRPC-Web text mode.
func isGRPCWebTextRequest(r *http.Request) bool {
	contentType := r.Header.Get("Content-Type")
	return strings.HasPrefix(contentType, "application/grpc-web-text")
}

// grpcWebResponseContentType maps a gRPC-Web request Content-Type to the
// appropriate response Content-Type.
func grpcWebResponseContentType(requestContentType string) string {
	if strings.HasPrefix(requestContentType, "application/grpc-web-text") {
		return "application/grpc-web-text+proto"
	}
	return "application/grpc-web+proto"
}

// encodeTrailersAsGRPCWebFrame encodes HTTP trailers into a gRPC-Web trailer
// frame. The format is: 1 byte flag (0x80), 4 bytes big-endian length, then
// the trailer key-value pairs as "Key: Value\r\n".
// If trailers is empty, it synthesizes "grpc-status: 0".
func encodeTrailersAsGRPCWebFrame(trailers http.Header) []byte {
	// Estimate trailer size: most trailers are small (~64 bytes)
	estimated := 64
	for key, values := range trailers {
		estimated += len(key)*len(values) + 4*len(values)
	}
	if estimated < 32 {
		estimated = 32
	}

	var buf []byte
	// Build trailer data directly into a byte slice
	trailerBuf := make([]byte, 0, estimated)
	for key, values := range trailers {
		for _, value := range values {
			trailerBuf = append(trailerBuf, key...)
			trailerBuf = append(trailerBuf, ": "...)
			trailerBuf = append(trailerBuf, value...)
			trailerBuf = append(trailerBuf, "\r\n"...)
		}
	}
	if len(trailerBuf) == 0 {
		trailerBuf = append(trailerBuf, "grpc-status: 0\r\n"...)
	}

	if len(trailerBuf) > math.MaxUint32 {
		trailerBuf = trailerBuf[:math.MaxUint32]
	}
	buf = make([]byte, 5+len(trailerBuf))
	buf[0] = 0x80 // trailer flag
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(trailerBuf)))
	copy(buf[5:], trailerBuf)
	return buf
}

// GRPCWebHandler handles gRPC-Web proxying (converts gRPC-Web to gRPC).
type GRPCWebHandler struct {
	grpcHandler *GRPCHandler
}

// NewGRPCWebHandler creates a new gRPC-Web handler.
func NewGRPCWebHandler(grpcHandler *GRPCHandler) *GRPCWebHandler {
	return &GRPCWebHandler{
		grpcHandler: grpcHandler,
	}
}

// HandleGRPCWeb handles a gRPC-Web request by translating between gRPC-Web
// and native gRPC protocols. It:
//  1. Optionally decodes the base64 request body (grpc-web-text mode)
//  2. Translates Content-Type to application/grpc
//  3. Forwards to the backend via the gRPC transport
//  4. Collects the response body and HTTP trailers
//  5. Encodes trailers as a final gRPC-Web trailer frame
//  6. Optionally base64-encodes the response (grpc-web-text mode)
func (gwh *GRPCWebHandler) HandleGRPCWeb(w http.ResponseWriter, r *http.Request, b *backend.Backend) error {
	if !gwh.grpcHandler.config.EnableGRPCWeb {
		return errors.New("gRPC-Web disabled")
	}

	isTextMode := isGRPCWebTextRequest(r)
	originalContentType := r.Header.Get("Content-Type")

	// Phase 1: Decode request body if grpc-web-text
	maxReqSize := gwh.grpcHandler.config.MaxMessageSize
	if maxReqSize == 0 {
		maxReqSize = 4 * 1024 * 1024
	}

	var requestBody []byte
	if isTextMode {
		raw, err := io.ReadAll(io.LimitReader(r.Body, int64(maxReqSize)+1))
		if err != nil {
			return fmt.Errorf("reading gRPC-Web request body: %w", err)
		}
		if len(raw) > maxReqSize {
			return fmt.Errorf("gRPC-Web request body exceeds maximum size (%d bytes)", maxReqSize)
		}
		requestBody, err = base64.StdEncoding.DecodeString(string(raw))
		if err != nil {
			return fmt.Errorf("decoding base64 gRPC-Web request: %w", err)
		}
	} else {
		var err error
		requestBody, err = io.ReadAll(io.LimitReader(r.Body, int64(maxReqSize)+1))
		if err != nil {
			return fmt.Errorf("reading gRPC-Web request body: %w", err)
		}
		if len(requestBody) > maxReqSize {
			return fmt.Errorf("gRPC-Web request body exceeds maximum size (%d bytes)", maxReqSize)
		}
	}
	r.Body.Close()

	// Phase 2: Acquire connection slot
	if !b.AcquireConn() {
		return errors.New("backend at max connections")
	}
	defer b.ReleaseConn()

	// Phase 3: Prepare outbound request with translated Content-Type
	outReq, err := gwh.prepareGRPCWebRequest(r, b, requestBody)
	if err != nil {
		return err
	}

	// Phase 4: Set timeout
	ctx := outReq.Context()
	if gwh.grpcHandler.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, gwh.grpcHandler.config.Timeout)
		defer cancel()
		outReq = outReq.WithContext(ctx)
	}

	// Phase 5: Execute request via transport
	resp, err := gwh.grpcHandler.transport.RoundTrip(outReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Phase 6: Read response body
	maxSize := gwh.grpcHandler.config.MaxMessageSize
	if maxSize == 0 {
		maxSize = 4 * 1024 * 1024
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxSize)+1))
	if err != nil {
		return fmt.Errorf("reading gRPC response body: %w", err)
	}

	// Phase 7: Collect trailers from both Trailer map and headers
	mergedTrailers := http.Header{}
	// Some backends send grpc-status as a regular header
	for _, key := range []string{"Grpc-Status", "Grpc-Message"} {
		if v := resp.Header.Get(key); v != "" {
			mergedTrailers.Set(key, v)
		}
	}
	// Also check the Trailer map (populated by Go's HTTP client for HTTP/2 trailers)
	for key, values := range resp.Trailer {
		for _, value := range values {
			mergedTrailers.Add(key, value)
		}
	}

	// Phase 8: Encode trailers as gRPC-Web trailer frame
	trailerFrame := encodeTrailersAsGRPCWebFrame(mergedTrailers)
	responseBody = append(responseBody, trailerFrame...)

	// Phase 9: Encode response for grpc-web-text if needed
	var finalBody []byte
	if isTextMode {
		finalBody = []byte(base64.StdEncoding.EncodeToString(responseBody))
	} else {
		finalBody = responseBody
	}

	// Phase 10: Write response
	w.Header().Set("Content-Type", grpcWebResponseContentType(originalContentType))
	// Copy relevant response headers, excluding trailers and grpc-specific ones
	for key, values := range resp.Header {
		lower := strings.ToLower(key)
		if lower == "content-type" || lower == "trailer" || lower == "grpc-status" || lower == "grpc-message" {
			continue
		}
		if isHopByHopHeader(key) {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	// Remove Content-Length since we modified the body
	w.Header().Del("Content-Length")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(finalBody)

	return nil
}

// prepareGRPCWebRequest creates the outbound gRPC request from a gRPC-Web request.
func (gwh *GRPCWebHandler) prepareGRPCWebRequest(r *http.Request, b *backend.Backend, body []byte) (*http.Request, error) {
	// Clone the original request
	outReq := r.Clone(r.Context())
	outReq.Body = io.NopCloser(bytes.NewReader(body))
	outReq.ContentLength = int64(len(body))

	// Translate Content-Type from grpc-web to grpc
	outReq.Header.Set("Content-Type", "application/grpc")

	// Set the URL to point to the backend
	backendURL, err := url.Parse("http://" + b.Address)
	if err != nil {
		return nil, fmt.Errorf("invalid backend address: %w", err)
	}

	outReq.URL.Scheme = backendURL.Scheme
	outReq.URL.Host = backendURL.Host
	outReq.Host = r.Host
	outReq.RequestURI = ""

	// Set X-Forwarded headers
	clientIP := trustedClientIP(r, gwh.grpcHandler.trustedNets)
	if prior := outReq.Header.Get("X-Forwarded-For"); prior != "" {
		outReq.Header.Set("X-Forwarded-For", prior+", "+clientIP)
	} else {
		outReq.Header.Set("X-Forwarded-For", clientIP)
	}
	outReq.Header.Set("X-Real-IP", clientIP)

	proto := "http"
	if r.TLS != nil {
		proto = "https"
	}
	outReq.Header.Set("X-Forwarded-Proto", proto)

	// Ensure HTTP/2 for gRPC
	outReq.Proto = "HTTP/2.0"
	outReq.ProtoMajor = 2
	outReq.ProtoMinor = 0

	return outReq, nil
}

// GRPCStatus represents a gRPC status code.
type GRPCStatus int

const (
	// GRPCStatusOK indicates success.
	GRPCStatusOK GRPCStatus = 0
	// GRPCStatusCancelled indicates the operation was cancelled.
	GRPCStatusCancelled GRPCStatus = 1
	// GRPCStatusUnknown indicates an unknown error.
	GRPCStatusUnknown GRPCStatus = 2
	// GRPCStatusInvalidArgument indicates an invalid argument.
	GRPCStatusInvalidArgument GRPCStatus = 3
	// GRPCStatusDeadlineExceeded indicates the deadline was exceeded.
	GRPCStatusDeadlineExceeded GRPCStatus = 4
	// GRPCStatusNotFound indicates the requested entity was not found.
	GRPCStatusNotFound GRPCStatus = 5
	// GRPCStatusAlreadyExists indicates the entity already exists.
	GRPCStatusAlreadyExists GRPCStatus = 6
	// GRPCStatusPermissionDenied indicates permission denied.
	GRPCStatusPermissionDenied GRPCStatus = 7
	// GRPCStatusResourceExhausted indicates resource exhaustion.
	GRPCStatusResourceExhausted GRPCStatus = 8
	// GRPCStatusFailedPrecondition indicates a failed precondition.
	GRPCStatusFailedPrecondition GRPCStatus = 9
	// GRPCStatusAborted indicates the operation was aborted.
	GRPCStatusAborted GRPCStatus = 10
	// GRPCStatusOutOfRange indicates the value is out of range.
	GRPCStatusOutOfRange GRPCStatus = 11
	// GRPCStatusUnimplemented indicates the operation is unimplemented.
	GRPCStatusUnimplemented GRPCStatus = 12
	// GRPCStatusInternal indicates an internal error.
	GRPCStatusInternal GRPCStatus = 13
	// GRPCStatusUnavailable indicates the service is unavailable.
	GRPCStatusUnavailable GRPCStatus = 14
	// GRPCStatusDataLoss indicates data loss.
	GRPCStatusDataLoss GRPCStatus = 15
	// GRPCStatusUnauthenticated indicates the caller is unauthenticated.
	GRPCStatusUnauthenticated GRPCStatus = 16
)

// HTTPStatusToGRPCStatus converts an HTTP status code to a gRPC status code.
func HTTPStatusToGRPCStatus(httpStatus int) GRPCStatus {
	switch httpStatus {
	case http.StatusOK:
		return GRPCStatusOK
	case http.StatusBadRequest:
		return GRPCStatusInvalidArgument
	case http.StatusUnauthorized:
		return GRPCStatusUnauthenticated
	case http.StatusForbidden:
		return GRPCStatusPermissionDenied
	case http.StatusNotFound:
		return GRPCStatusNotFound
	case http.StatusTooManyRequests:
		return GRPCStatusResourceExhausted
	case http.StatusInternalServerError:
		return GRPCStatusInternal
	case http.StatusNotImplemented:
		return GRPCStatusUnimplemented
	case http.StatusBadGateway:
		return GRPCStatusUnavailable
	case http.StatusServiceUnavailable:
		return GRPCStatusUnavailable
	case http.StatusGatewayTimeout:
		return GRPCStatusDeadlineExceeded
	default:
		return GRPCStatusUnknown
	}
}

// GRPCStatusToHTTPStatus converts a gRPC status code to an HTTP status code.
func GRPCStatusToHTTPStatus(grpcStatus GRPCStatus) int {
	switch grpcStatus {
	case GRPCStatusOK:
		return http.StatusOK
	case GRPCStatusCancelled:
		return 499 // Client Closed Request (nginx convention)
	case GRPCStatusUnknown:
		return http.StatusInternalServerError
	case GRPCStatusInvalidArgument:
		return http.StatusBadRequest
	case GRPCStatusDeadlineExceeded:
		return http.StatusGatewayTimeout
	case GRPCStatusNotFound:
		return http.StatusNotFound
	case GRPCStatusAlreadyExists:
		return http.StatusConflict
	case GRPCStatusPermissionDenied:
		return http.StatusForbidden
	case GRPCStatusResourceExhausted:
		return http.StatusTooManyRequests
	case GRPCStatusFailedPrecondition:
		return http.StatusPreconditionFailed
	case GRPCStatusAborted:
		return 409 // Conflict
	case GRPCStatusOutOfRange:
		return http.StatusBadRequest
	case GRPCStatusUnimplemented:
		return http.StatusNotImplemented
	case GRPCStatusInternal:
		return http.StatusInternalServerError
	case GRPCStatusUnavailable:
		return http.StatusServiceUnavailable
	case GRPCStatusDataLoss:
		return http.StatusInternalServerError
	case GRPCStatusUnauthenticated:
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}

// gRPCFrame represents a gRPC frame header.
type gRPCFrame struct {
	Compressed bool
	Length     uint32
	Data       []byte
}

// parseGRPCFrame parses a gRPC frame from the reader.
func parseGRPCFrame(r io.Reader) (*gRPCFrame, error) {
	// gRPC frame format:
	// 1 byte: flags (compressed flag)
	// 4 bytes: message length (big-endian)
	// N bytes: message data

	bufPtr := grpcFrameHeaderPool.Get().(*[]byte)
	buf := *bufPtr
	defer grpcFrameHeaderPool.Put(bufPtr)

	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}

	compressed := buf[0] == 1
	length := binary.BigEndian.Uint32(buf[1:5])

	// Guard against unbounded allocation from malicious frame headers.
	// gRPC's default max receive size is 4MB; we enforce the same cap.
	const maxGRPCFrameSize = 4 * 1024 * 1024
	if length > maxGRPCFrameSize {
		return nil, fmt.Errorf("grpc frame length %d exceeds maximum allowed size %d", length, maxGRPCFrameSize)
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}

	return &gRPCFrame{
		Compressed: compressed,
		Length:     length,
		Data:       data,
	}, nil
}

// writeGRPCFrame writes a gRPC frame to the writer.
func writeGRPCFrame(w io.Writer, frame *gRPCFrame) error {
	// Write header directly to avoid combined buffer allocation
	header := [5]byte{}
	if frame.Compressed {
		header[0] = 1
	}
	binary.BigEndian.PutUint32(header[1:5], frame.Length)

	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if len(frame.Data) > 0 {
		_, err := w.Write(frame.Data)
		return err
	}
	return nil
}
