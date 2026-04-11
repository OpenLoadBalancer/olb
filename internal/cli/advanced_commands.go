// Package cli provides advanced command-line interface commands for OpenLoadBalancer.
package cli

import (
	"net/http"
	"strconv"
)

func (c *Client) Post(path string, body, result any) error {
	return c.post(path, body, result)
}

// Helper method for Client.delete (exported for use in commands)
func (c *Client) Delete(path string) error {
	return c.delete(path)
}

// Helper method for Client.get (exported for use in commands)
func (c *Client) Get(path string, result any) error {
	return c.get(path, result)
}

// Helper method for Client.doRequest (exported for use in commands)
func (c *Client) DoRequest(method, path string, body any) (*http.Response, error) {
	return c.doRequest(method, path, body)
}

// Helper function to parse int with default
func parseIntDefault(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return i
}
