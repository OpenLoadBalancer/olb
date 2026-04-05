// Package rewrite provides URL rewriting middleware.
package rewrite

import (
	"net/http"
	"regexp"
	"strings"
)

// Rule represents a single rewrite rule.
type Rule struct {
	Pattern     string // Regex pattern to match
	Replacement string // Replacement string (can use $1, $2, etc.)
	Flag        string // Flag: "last", "break", "redirect", "permanent"
}

// Config configures rewrite middleware.
type Config struct {
	Enabled      bool     // Enable rewriting
	Rules        []Rule   // Rewrite rules (applied in order)
	ExcludePaths []string // Paths to exclude from rewriting
}

// DefaultConfig returns default rewrite configuration.
func DefaultConfig() Config {
	return Config{
		Enabled: false,
	}
}

// compiledRule represents a compiled rewrite rule.
type compiledRule struct {
	pattern     *regexp.Regexp
	replacement string
	flag        string
}

// Middleware provides URL rewriting functionality.
type Middleware struct {
	config Config
	rules  []compiledRule
}

// New creates a new rewrite middleware.
func New(config Config) (*Middleware, error) {
	if !config.Enabled || len(config.Rules) == 0 {
		return &Middleware{config: config}, nil
	}

	// Compile all rules
	var compiled []compiledRule
	for _, rule := range config.Rules {
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, compiledRule{
			pattern:     re,
			replacement: rule.Replacement,
			flag:        rule.Flag,
		})
	}

	return &Middleware{
		config: config,
		rules:  compiled,
	}, nil
}

// Name returns the middleware name.
func (m *Middleware) Name() string {
	return "rewrite"
}

// Priority returns the middleware priority.
func (m *Middleware) Priority() int {
	return 135 // After Strip Prefix (140), before WAF (100) - actually should be early
}

// Wrap wraps the handler with URL rewriting.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	if !m.config.Enabled || len(m.rules) == 0 {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check excluded paths
		for _, path := range m.config.ExcludePaths {
			if strings.HasPrefix(r.URL.Path, path) {
				next.ServeHTTP(w, r)
				return
			}
		}

		originalPath := r.URL.Path
		originalRawPath := r.URL.RawPath

		// Apply rewrite rules
		for _, rule := range m.rules {
			newPath, matched := m.applyRule(originalPath, rule)
			if matched {
				switch rule.flag {
				case "redirect", "temp":
					http.Redirect(w, r, newPath, http.StatusFound)
					return
				case "permanent":
					http.Redirect(w, r, newPath, http.StatusMovedPermanently)
					return
				case "break":
					// Rewrite and stop processing, but don't change URL visible to client
					r.URL.Path = newPath
					r.URL.RawPath = ""
					r.RequestURI = newPath
					if r.URL.RawQuery != "" {
						r.RequestURI += "?" + r.URL.RawQuery
					}
					next.ServeHTTP(w, r)
					return
				case "last", "":
					// Rewrite and continue to next handler
					originalPath = newPath
					originalRawPath = ""
					// Continue to next rule
				}
			}
		}

		// Apply final rewrite if path changed
		if originalPath != r.URL.Path {
			r.URL.Path = originalPath
			r.URL.RawPath = originalRawPath
			r.RequestURI = originalPath
			if r.URL.RawQuery != "" {
				r.RequestURI += "?" + r.URL.RawQuery
			}
		}

		next.ServeHTTP(w, r)
	})
}

// applyRule applies a single rewrite rule.
func (m *Middleware) applyRule(path string, rule compiledRule) (string, bool) {
	// Check if pattern matches
	if !rule.pattern.MatchString(path) {
		return path, false
	}

	// Apply replacement
	result := rule.pattern.ReplaceAllString(path, rule.replacement)

	return result, true
}

// CommonRewriteRules provides commonly used rewrite rules.
var CommonRewriteRules = map[string]Rule{
	"old_to_new": {
		Pattern:     "^/old/(.*)$",
		Replacement: "/new/$1",
		Flag:        "permanent",
	},
	"http_to_https": {
		Pattern:     "^/insecure/(.*)$",
		Replacement: "/secure/$1",
		Flag:        "permanent",
	},
	"trailing_slash": {
		Pattern:     "^(.*[^/])$",
		Replacement: "$1/",
		Flag:        "permanent",
	},
	"remove_trailing_slash": {
		Pattern:     "^(.*)/$",
		Replacement: "$1",
		Flag:        "permanent",
	},
}
