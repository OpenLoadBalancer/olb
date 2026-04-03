// Package router provides HTTP routing for OpenLoadBalancer.
package router

import (
	"strings"
	"sync"
)

// radixNode represents a node in the path trie.
// This is a segment-based trie where each node represents a path segment.
type radixNode struct {
	// segment is the path segment (e.g., "users", ":id", "*path")
	segment string

	// children maps segment keys to child nodes
	// For static segments, key is the segment itself
	// For parameters, key is ":"
	// For wildcards, key is "*"
	children map[string]*radixNode

	// isEndpoint indicates if this node is a valid route endpoint
	isEndpoint bool

	// value is the stored data for this route
	value any

	// paramName is set if this node is a parameter
	paramName string

	// isWildcard indicates if this node is a wildcard
	isWildcard bool
}

// RadixTrie is a trie for efficient path matching with parameter support.
// It supports:
//   - Static paths: /users/profile
//   - Named parameters: /users/:id
//   - Wildcard parameters: /files/*path
//
// The trie is safe for concurrent reads but not concurrent writes.
type RadixTrie struct {
	root *radixNode
	mu   sync.RWMutex
}

// NewTrie creates a new empty radix tree.
// This is an alias for NewRadixTrie for API consistency.
func NewTrie() *RadixTrie {
	return NewRadixTrie()
}

// NewRadixTrie creates a new empty radix tree.
func NewRadixTrie() *RadixTrie {
	return &RadixTrie{
		root: &radixNode{
			segment:  "",
			children: make(map[string]*radixNode),
		},
	}
}

// Insert adds a route to the trie with the given value.
func (t *RadixTrie) Insert(path string, value any) {
	if path == "" {
		return
	}

	// Ensure path starts with /
	if path[0] != '/' {
		path = "/" + path
	}

	// Normalize path: remove trailing slash except for root
	if len(path) > 1 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	segments := splitPath(path)
	t.insertSegments(t.root, segments, 0, value)
}

func (t *RadixTrie) insertSegments(node *radixNode, segments []string, index int, value any) {
	if index >= len(segments) {
		node.isEndpoint = true
		node.value = value
		return
	}

	segment := segments[index]
	isLast := index == len(segments)-1

	// Determine the key for this segment
	var key string
	var paramName string
	var isWildcard bool

	if len(segment) > 0 && segment[0] == ':' {
		key = ":"
		paramName = segment[1:]
	} else if len(segment) > 0 && segment[0] == '*' {
		key = "*"
		paramName = segment[1:]
		if paramName == "" {
			paramName = "path"
		}
		isWildcard = true
	} else {
		key = segment
	}

	child, exists := node.children[key]
	if !exists {
		child = &radixNode{
			segment:    segment,
			children:   make(map[string]*radixNode),
			paramName:  paramName,
			isWildcard: isWildcard,
		}
		node.children[key] = child
	}

	if isLast {
		child.isEndpoint = true
		child.value = value
	} else {
		t.insertSegments(child, segments, index+1, value)
	}
}

// MatchResult holds the result of a path match.
type MatchResult struct {
	// Value is the stored route handler/data.
	Value any

	// Params contains captured path parameters (e.g., :id -> "123")
	Params map[string]string
}

// Match finds the best matching route for the given path.
// Returns the MatchResult and true if a match is found, false otherwise.
func (t *RadixTrie) Match(path string) (*MatchResult, bool) {
	if path == "" || path[0] != '/' {
		return nil, false
	}

	// Normalize path: remove trailing slash except for root
	if len(path) > 1 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	segments := splitPath(path)
	params := make(map[string]string)

	node := t.matchSegments(t.root, segments, 0, params)
	if node == nil || !node.isEndpoint {
		return nil, false
	}

	return &MatchResult{
		Value:  node.value,
		Params: params,
	}, true
}

func (t *RadixTrie) matchSegments(node *radixNode, segments []string, index int, params map[string]string) *radixNode {
	if index >= len(segments) {
		if node.isEndpoint {
			return node
		}
		return nil
	}

	segment := segments[index]

	// Try exact match first (highest priority)
	if child, ok := node.children[segment]; ok {
		if result := t.matchSegments(child, segments, index+1, params); result != nil {
			return result
		}
	}

	// Try parameter match (:name)
	if child, ok := node.children[":"]; ok {
		params[child.paramName] = segment
		if result := t.matchSegments(child, segments, index+1, params); result != nil {
			return result
		}
		// Backtrack: remove param if this path didn't work
		delete(params, child.paramName)
	}

	// Try wildcard match (*name) - matches everything remaining
	if child, ok := node.children["*"]; ok && child.isWildcard {
		// Wildcard captures all remaining segments
		remaining := strings.Join(segments[index:], "/")
		params[child.paramName] = remaining
		if child.isEndpoint {
			return child
		}
	}

	return nil
}

// splitPath splits a path into segments, removing the leading empty segment from root.
func splitPath(path string) []string {
	if path == "/" {
		return nil
	}

	// Remove leading slash and split
	if path[0] == '/' {
		path = path[1:]
	}

	parts := strings.Split(path, "/")
	var segments []string
	for _, part := range parts {
		if part != "" {
			segments = append(segments, part)
		}
	}
	return segments
}

// Delete removes a route from the trie.
func (t *RadixTrie) Delete(path string) {
	if path == "" || path[0] != '/' {
		return
	}

	// Normalize path
	if len(path) > 1 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	segments := splitPath(path)
	t.deleteSegments(t.root, segments, 0)
}

func (t *RadixTrie) deleteSegments(node *radixNode, segments []string, index int) bool {
	if index >= len(segments) {
		if node.isEndpoint {
			node.isEndpoint = false
			node.value = nil
		}
		return len(node.children) == 0
	}

	segment := segments[index]
	var key string
	if len(segment) > 0 && segment[0] == ':' {
		key = ":"
	} else if len(segment) > 0 && segment[0] == '*' {
		key = "*"
	} else {
		key = segment
	}

	child, ok := node.children[key]
	if !ok {
		return false
	}

	shouldDelete := t.deleteSegments(child, segments, index+1)
	if shouldDelete {
		delete(node.children, key)
	}

	return !node.isEndpoint && len(node.children) == 0
}

// Clone creates a deep copy of the trie.
func (t *RadixTrie) Clone() *RadixTrie {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return &RadixTrie{
		root: t.cloneNode(t.root),
	}
}

func (t *RadixTrie) cloneNode(node *radixNode) *radixNode {
	if node == nil {
		return nil
	}

	clone := &radixNode{
		segment:    node.segment,
		isEndpoint: node.isEndpoint,
		value:      node.value,
		paramName:  node.paramName,
		isWildcard: node.isWildcard,
		children:   make(map[string]*radixNode, len(node.children)),
	}

	for k, v := range node.children {
		clone.children[k] = t.cloneNode(v)
	}

	return clone
}
