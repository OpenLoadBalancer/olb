// Package cli provides the command-line interface for OpenLoadBalancer.
package cli

import (
	"encoding/json"
	"fmt"
	"github.com/openloadbalancer/olb/internal/admin"
	"net/http"
	"time"
)

// MetricsFetcher fetches metrics from the admin API.
type MetricsFetcher struct {
	apiAddr string
	client  *http.Client
}

// NewMetricsFetcher creates a new metrics fetcher.
func NewMetricsFetcher(apiAddr string) *MetricsFetcher {
	return &MetricsFetcher{
		apiAddr: apiAddr,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// FetchSystemInfo fetches system information.
func (f *MetricsFetcher) FetchSystemInfo() (*admin.SystemInfo, error) {
	url := fmt.Sprintf("http://%s/api/v1/system/info", f.apiAddr)
	resp, err := f.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result admin.Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if !result.Success {
		return nil, fmt.Errorf("API error: %s", result.Error.Message)
	}

	data, _ := json.Marshal(result.Data)
	var info admin.SystemInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// FetchBackends fetches backend information.
func (f *MetricsFetcher) FetchBackends() ([]admin.BackendPool, error) {
	url := fmt.Sprintf("http://%s/api/v1/backends", f.apiAddr)
	resp, err := f.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result admin.Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if !result.Success {
		return nil, fmt.Errorf("API error: %s", result.Error.Message)
	}

	data, _ := json.Marshal(result.Data)
	var pools []admin.BackendPool
	if err := json.Unmarshal(data, &pools); err != nil {
		return nil, err
	}
	return pools, nil
}

// FetchRoutes fetches route information.
func (f *MetricsFetcher) FetchRoutes() ([]admin.Route, error) {
	url := fmt.Sprintf("http://%s/api/v1/routes", f.apiAddr)
	resp, err := f.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result admin.Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if !result.Success {
		return nil, fmt.Errorf("API error: %s", result.Error.Message)
	}

	data, _ := json.Marshal(result.Data)
	var routes []admin.Route
	if err := json.Unmarshal(data, &routes); err != nil {
		return nil, err
	}
	return routes, nil
}

// FetchHealth fetches health status.
func (f *MetricsFetcher) FetchHealth() (*admin.HealthStatus, error) {
	url := fmt.Sprintf("http://%s/api/v1/system/health", f.apiAddr)
	resp, err := f.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result admin.Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if !result.Success {
		return nil, fmt.Errorf("API error: %s", result.Error.Message)
	}

	data, _ := json.Marshal(result.Data)
	var health admin.HealthStatus
	if err := json.Unmarshal(data, &health); err != nil {
		return nil, err
	}
	return &health, nil
}
