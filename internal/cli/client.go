// Package cli provides the command-line interface for OpenLoadBalancer.
package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/openloadbalancer/olb/internal/admin"
)

// Client is an Admin API client.
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string // Bearer token for auth
	username   string // Basic auth
	password   string // Basic auth
}

// PoolInfo represents a backend pool with its backends.
type PoolInfo struct {
	Name      string        `json:"name"`
	Algorithm string        `json:"algorithm"`
	Backends  []BackendInfo `json:"backends"`
}

// BackendInfo represents a single backend server.
type BackendInfo struct {
	ID       string `json:"id"`
	Address  string `json:"address"`
	Weight   int    `json:"weight"`
	State    string `json:"state"`
	Healthy  bool   `json:"healthy"`
	Requests int64  `json:"requests"`
	Errors   int64  `json:"errors"`
}

// RouteInfo represents an HTTP route configuration.
type RouteInfo struct {
	Name        string            `json:"name"`
	Host        string            `json:"host,omitempty"`
	Path        string            `json:"path"`
	Methods     []string          `json:"methods,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	BackendPool string            `json:"backend_pool"`
	Priority    int               `json:"priority"`
}

// HealthStatusInfo represents the overall health status.
type HealthStatusInfo struct {
	Status    string                     `json:"status"`
	Backends  map[string]HealthCheckInfo `json:"backends"`
	Timestamp time.Time                  `json:"timestamp"`
}

// HealthCheckInfo represents health check status for a backend.
type HealthCheckInfo struct {
	Status    string    `json:"status"`
	LastCheck time.Time `json:"last_check"`
	Message   string    `json:"message,omitempty"`
}

// AddBackendRequest is used to add a new backend to a pool.
type AddBackendRequest struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	Weight  int    `json:"weight,omitempty"`
}

// NewClient creates a new admin API client.
// The baseURL should be the admin API endpoint (e.g., "http://localhost:8080").
func NewClient(baseURL string) *Client {
	// Ensure baseURL doesn't end with a trailing slash
	for len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SetToken sets bearer token for authentication.
func (c *Client) SetToken(token string) {
	c.token = token
	c.username = ""
	c.password = ""
}

// SetBasicAuth sets basic auth credentials.
func (c *Client) SetBasicAuth(username, password string) {
	c.username = username
	c.password = password
	c.token = ""
}

// doRequest creates and executes an HTTP request with authentication.
func (c *Client) doRequest(method, path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	url := c.baseURL + "/api/v1" + path
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	// Add authentication
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	} else if c.username != "" || c.password != "" {
		auth := base64.StdEncoding.EncodeToString([]byte(c.username + ":" + c.password))
		req.Header.Set("Authorization", "Basic "+auth)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

// get performs a GET request and decodes the response.
func (c *Client) get(path string, result any) error {
	resp, err := c.doRequest(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return c.handleResponse(resp, result)
}

// post performs a POST request and decodes the response.
func (c *Client) post(path string, body, result any) error {
	resp, err := c.doRequest(http.MethodPost, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return c.handleResponse(resp, result)
}

// delete performs a DELETE request.
func (c *Client) delete(path string) error {
	resp, err := c.doRequest(http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.decodeError(resp)
	}

	return nil
}

// handleResponse processes the HTTP response and decodes JSON.
func (c *Client) handleResponse(resp *http.Response, result any) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return c.decodeErrorFromBody(resp.StatusCode, body)
	}

	if result != nil && len(body) > 0 {
		if err := json.Unmarshal(body, result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

// decodeError decodes an error response from the server.
func (c *Client) decodeError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("HTTP %d: failed to read error response", resp.StatusCode)
	}
	return c.decodeErrorFromBody(resp.StatusCode, body)
}

// decodeErrorFromBody decodes an error from response body bytes.
func (c *Client) decodeErrorFromBody(statusCode int, body []byte) error {
	var apiResp admin.Response
	if err := json.Unmarshal(body, &apiResp); err == nil && apiResp.Error != nil {
		return fmt.Errorf("HTTP %d: %s - %s", statusCode, apiResp.Error.Code, apiResp.Error.Message)
	}
	return fmt.Errorf("HTTP %d: %s", statusCode, string(body))
}

// GetSystemInfo retrieves system information.
func (c *Client) GetSystemInfo() (*admin.SystemInfo, error) {
	var result admin.SystemInfo
	if err := c.get("/system/info", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetSystemHealth retrieves the system health status.
func (c *Client) GetSystemHealth() (*admin.HealthStatus, error) {
	var result admin.HealthStatus
	if err := c.get("/system/health", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ReloadConfig triggers a configuration reload.
func (c *Client) ReloadConfig() error {
	return c.post("/system/reload", nil, nil)
}

// ListBackends returns a list of all backend pool names.
func (c *Client) ListBackends() ([]string, error) {
	var result []string
	if err := c.get("/backends", &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetPool retrieves detailed information about a backend pool.
func (c *Client) GetPool(name string) (*PoolInfo, error) {
	var result PoolInfo
	if err := c.get("/backends/"+name, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// AddBackend adds a new backend to a pool.
func (c *Client) AddBackend(pool string, backend *AddBackendRequest) error {
	return c.post("/backends/"+pool+"/backends", backend, nil)
}

// RemoveBackend removes a backend from a pool.
func (c *Client) RemoveBackend(pool, backend string) error {
	return c.delete("/backends/" + pool + "/backends/" + backend)
}

// DrainBackend marks a backend as draining.
func (c *Client) DrainBackend(pool, backend string) error {
	return c.post("/backends/"+pool+"/backends/"+backend+"/drain", nil, nil)
}

// ListRoutes returns all configured routes.
func (c *Client) ListRoutes() ([]RouteInfo, error) {
	var result []RouteInfo
	if err := c.get("/routes", &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetHealthStatus retrieves detailed health check status for all backends.
func (c *Client) GetHealthStatus() (*HealthStatusInfo, error) {
	var result HealthStatusInfo
	if err := c.get("/health", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetMetricsJSON retrieves metrics in JSON format.
func (c *Client) GetMetricsJSON() (map[string]any, error) {
	var result map[string]any
	if err := c.get("/metrics", &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetMetricsPrometheus retrieves metrics in Prometheus format.
func (c *Client) GetMetricsPrometheus() (string, error) {
	resp, err := c.doRequest(http.MethodGet, "/metrics/prometheus", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", c.decodeError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	return string(body), nil
}
