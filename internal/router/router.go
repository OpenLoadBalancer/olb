// Package router provides HTTP routing for OpenLoadBalancer.
package router

import (
	"net"
	"net/http"
	"strings"
	"sync"

	olbErrors "github.com/openloadbalancer/olb/pkg/errors"
)

// Route represents a single HTTP route configuration.
type Route struct {
	// Name is the unique identifier for this route
	Name string

	// Host is the hostname to match (optional, supports wildcards like *.example.com)
	Host string

	// Path is the URL path pattern to match
	// Supports static paths (/users), named parameters (/users/:id), and wildcards (/files/*path)
	Path string

	// Methods is a list of HTTP methods to match (optional, empty = all methods)
	Methods []string

	// Headers is a map of header name-value pairs that must match exactly (optional)
	Headers map[string]string

	// BackendPool is the name of the backend pool to route to
	BackendPool string

	// Priority determines route precedence when multiple routes match (higher = more specific)
	Priority int
}

// RouteMatch holds the result of a successful route match.
type RouteMatch struct {
	// Route is the matched route configuration
	Route *Route

	// Params contains captured path parameters (e.g., :id -> "123")
	Params map[string]string
}

// routeEntry holds routes that share the same path pattern.
type routeEntry struct {
	routes []*Route // All routes for this path (differentiated by method/headers)
}

// hostTrie holds the radix trie for a specific host pattern.
type hostTrie struct {
	host   string
	trie   *RadixTrie
	routes map[string]*routeEntry // map[path]routeEntry
}

// Router manages HTTP routing with support for host-based routing,
// path matching, method filtering, and header matching.
type Router struct {
	// exactHosts maps exact hostnames to their tries
	exactHosts map[string]*hostTrie

	// wildcardHosts maps wildcard patterns (*.example.com) to their tries
	// The key is stored without the "*." prefix (e.g., "example.com")
	wildcardHosts map[string]*hostTrie

	// defaultTrie is used when no host matches
	defaultTrie *hostTrie

	// routesByName provides O(1) lookup for route removal
	routesByName map[string]*Route

	// mu protects all internal maps
	mu sync.RWMutex
}

// NewRouter creates a new HTTP router.
func NewRouter() *Router {
	return &Router{
		exactHosts:    make(map[string]*hostTrie),
		wildcardHosts: make(map[string]*hostTrie),
		routesByName:  make(map[string]*Route),
	}
}

// AddRoute adds a route to the router.
// Returns an error if the route is invalid or a route with the same name already exists.
func (r *Router) AddRoute(route *Route) error {
	if route == nil {
		return olbErrors.ErrInvalidArg.WithContext("reason", "route is nil")
	}
	if route.Name == "" {
		return olbErrors.ErrInvalidArg.WithContext("reason", "route name is empty")
	}
	if route.Path == "" {
		return olbErrors.ErrInvalidArg.WithContext("reason", "route path is empty")
	}
	if route.BackendPool == "" {
		return olbErrors.ErrInvalidArg.WithContext("reason", "backend pool is empty")
	}

	// Ensure path starts with /
	if route.Path[0] != '/' {
		route.Path = "/" + route.Path
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check for duplicate route name
	if _, exists := r.routesByName[route.Name]; exists {
		return olbErrors.ErrAlreadyExist.WithContext("route_name", route.Name)
	}

	// Get or create the host trie
	ht := r.getOrCreateHostTrie(route.Host)

	// Add route to trie - use path as the key
	ht.trie.Insert(route.Path, route.Path)

	// Add to route entry for this path
	entry, exists := ht.routes[route.Path]
	if !exists {
		entry = &routeEntry{routes: make([]*Route, 0)}
		ht.routes[route.Path] = entry
	}
	entry.routes = append(entry.routes, route)

	// Store in name index
	r.routesByName[route.Name] = route

	return nil
}

// getOrCreateHostTrie returns the hostTrie for the given host, creating it if necessary.
// Must be called with lock held.
func (r *Router) getOrCreateHostTrie(host string) *hostTrie {
	if host == "" {
		if r.defaultTrie == nil {
			r.defaultTrie = &hostTrie{
				host:   "",
				trie:   NewRadixTrie(),
				routes: make(map[string]*routeEntry),
			}
		}
		return r.defaultTrie
	}

	// Check for wildcard pattern
	if strings.HasPrefix(host, "*.") {
		domain := host[2:] // Remove "*." prefix
		if ht, ok := r.wildcardHosts[domain]; ok {
			return ht
		}
		ht := &hostTrie{
			host:   host,
			trie:   NewRadixTrie(),
			routes: make(map[string]*routeEntry),
		}
		r.wildcardHosts[domain] = ht
		return ht
	}

	// Exact host match
	if ht, ok := r.exactHosts[host]; ok {
		return ht
	}
	ht := &hostTrie{
		host:   host,
		trie:   NewRadixTrie(),
		routes: make(map[string]*routeEntry),
	}
	r.exactHosts[host] = ht
	return ht
}

// Match finds the best matching route for the given HTTP request.
// Returns the RouteMatch and true if a match is found, nil and false otherwise.
func (r *Router) Match(req *http.Request) (*RouteMatch, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	host := req.Host
	if host == "" {
		host = req.URL.Host
	}

	// Strip port if present (handles IPv6 bracket notation)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	path := req.URL.Path

	// Try exact host first
	if ht, ok := r.exactHosts[host]; ok {
		if match := r.matchInHostTrie(ht, path, req); match != nil {
			return match, true
		}
	}

	// Try wildcard match (*.example.com matches api.example.com)
	// Find the longest matching wildcard suffix
	parts := strings.Split(host, ".")
	for i := 1; i < len(parts); i++ {
		suffix := strings.Join(parts[i:], ".")
		if ht, ok := r.wildcardHosts[suffix]; ok {
			if match := r.matchInHostTrie(ht, path, req); match != nil {
				return match, true
			}
		}
	}

	// Fall back to default if no host match
	if r.defaultTrie != nil {
		if match := r.matchInHostTrie(r.defaultTrie, path, req); match != nil {
			return match, true
		}
	}

	return nil, false
}

// matchInHostTrie tries to match a request in a specific host trie.
// Returns nil if no match found.
func (r *Router) matchInHostTrie(ht *hostTrie, path string, req *http.Request) *RouteMatch {
	// Try exact match first
	result, ok := ht.trie.Match(path)
	if !ok {
		// Try prefix match: walk up the path looking for a parent route
		// e.g., /api/users → try /api/users, /api, /
		tryPath := path
		for tryPath != "" {
			idx := strings.LastIndex(tryPath, "/")
			if idx <= 0 {
				// Try root "/"
				result, ok = ht.trie.Match("/")
				break
			}
			tryPath = tryPath[:idx]
			result, ok = ht.trie.Match(tryPath)
			if ok {
				break
			}
		}
		if !ok {
			return nil
		}
	}

	// Get the matched path pattern
	matchedPath, _ := result.Value.(string)

	// Get all routes for this path
	entry, exists := ht.routes[matchedPath]
	if !exists || len(entry.routes) == 0 {
		return nil
	}

	// Find the best matching route based on method and headers
	for _, route := range entry.routes {
		if r.matchesRoute(route, req) {
			return &RouteMatch{
				Route:  route,
				Params: result.Params,
			}
		}
	}

	return nil
}

// matchesRoute checks if a request matches the route's method and header constraints.
func (r *Router) matchesRoute(route *Route, req *http.Request) bool {
	// Check method filter
	if len(route.Methods) > 0 {
		methodMatch := false
		for _, m := range route.Methods {
			if m == req.Method {
				methodMatch = true
				break
			}
		}
		if !methodMatch {
			return false
		}
	}

	// Check header filters
	if len(route.Headers) > 0 {
		for key, value := range route.Headers {
			if req.Header.Get(key) != value {
				return false
			}
		}
	}

	return true
}

// RemoveRoute removes a route by name.
func (r *Router) RemoveRoute(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	route, ok := r.routesByName[name]
	if !ok {
		return
	}

	// Find the host trie
	var ht *hostTrie
	if route.Host == "" {
		ht = r.defaultTrie
	} else if strings.HasPrefix(route.Host, "*.") {
		domain := route.Host[2:]
		ht = r.wildcardHosts[domain]
	} else {
		ht = r.exactHosts[route.Host]
	}

	if ht != nil {
		// Remove from route entry
		if entry, exists := ht.routes[route.Path]; exists {
			for i, r := range entry.routes {
				if r.Name == name {
					entry.routes = append(entry.routes[:i], entry.routes[i+1:]...)
					break
				}
			}
			// If no more routes for this path, remove the entry and trie entry
			if len(entry.routes) == 0 {
				delete(ht.routes, route.Path)
				ht.trie.Delete(route.Path)
			}
		}
	}

	delete(r.routesByName, name)
}

// Swap atomically replaces the entire route table with the given routes.
// This is used for hot-reload without disrupting in-flight requests.
func (r *Router) Swap(routes []*Route) error {
	// Validate all routes first
	for _, route := range routes {
		if route == nil {
			return olbErrors.ErrInvalidArg.WithContext("reason", "route is nil")
		}
		if route.Name == "" {
			return olbErrors.ErrInvalidArg.WithContext("reason", "route name is empty")
		}
		if route.Path == "" {
			return olbErrors.ErrInvalidArg.WithContext("reason", "route path is empty")
		}
		if route.BackendPool == "" {
			return olbErrors.ErrInvalidArg.WithContext("reason", "backend pool is empty")
		}
		// Ensure path starts with /
		if route.Path[0] != '/' {
			route.Path = "/" + route.Path
		}
	}

	// Build new route tables
	newExactHosts := make(map[string]*hostTrie)
	newWildcardHosts := make(map[string]*hostTrie)
	var newDefaultTrie *hostTrie
	newRoutesByName := make(map[string]*Route)

	for _, route := range routes {
		// Get or create host trie
		var ht *hostTrie
		if route.Host == "" {
			if newDefaultTrie == nil {
				newDefaultTrie = &hostTrie{
					host:   "",
					trie:   NewRadixTrie(),
					routes: make(map[string]*routeEntry),
				}
			}
			ht = newDefaultTrie
		} else if strings.HasPrefix(route.Host, "*.") {
			domain := route.Host[2:]
			if h, ok := newWildcardHosts[domain]; ok {
				ht = h
			} else {
				ht = &hostTrie{
					host:   route.Host,
					trie:   NewRadixTrie(),
					routes: make(map[string]*routeEntry),
				}
				newWildcardHosts[domain] = ht
			}
		} else {
			if h, ok := newExactHosts[route.Host]; ok {
				ht = h
			} else {
				ht = &hostTrie{
					host:   route.Host,
					trie:   NewRadixTrie(),
					routes: make(map[string]*routeEntry),
				}
				newExactHosts[route.Host] = ht
			}
		}

		// Add route to trie
		ht.trie.Insert(route.Path, route.Path)

		// Add to route entry
		entry, exists := ht.routes[route.Path]
		if !exists {
			entry = &routeEntry{routes: make([]*Route, 0)}
			ht.routes[route.Path] = entry
		}
		entry.routes = append(entry.routes, route)

		newRoutesByName[route.Name] = route
	}

	// Atomic swap
	r.mu.Lock()
	defer r.mu.Unlock()

	r.exactHosts = newExactHosts
	r.wildcardHosts = newWildcardHosts
	r.defaultTrie = newDefaultTrie
	r.routesByName = newRoutesByName

	return nil
}

// Routes returns a slice of all configured routes.
func (r *Router) Routes() []*Route {
	r.mu.RLock()
	defer r.mu.RUnlock()

	routes := make([]*Route, 0, len(r.routesByName))
	for _, route := range r.routesByName {
		routes = append(routes, route)
	}
	return routes
}

// GetRoute returns a route by name, or nil if not found.
func (r *Router) GetRoute(name string) *Route {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.routesByName[name]
}

// RouteCount returns the total number of routes.
func (r *Router) RouteCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.routesByName)
}
