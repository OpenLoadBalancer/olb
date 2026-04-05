package rewrite

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRewrite_Disabled(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = false

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/original" {
			t.Errorf("Path should not change when disabled, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/original", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should have been called")
	}
}

func TestRewrite_SimpleReplacement(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{
		{
			Pattern:     "^/old/(.*)$",
			Replacement: "/new/$1",
			Flag:        "last",
		},
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/new/path" {
			t.Errorf("Expected path /new/path, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/old/path", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestRewrite_NoMatch(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{
		{
			Pattern:     "^/api/(.*)$",
			Replacement: "/v1/$1",
			Flag:        "last",
		},
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/other/path" {
			t.Errorf("Path should not change when no match, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/other/path", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
}

func TestRewrite_PermanentRedirect(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{
		{
			Pattern:     "^/old$",
			Replacement: "/new",
			Flag:        "permanent",
		},
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for redirect")
	}))

	req := httptest.NewRequest("GET", "/old", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("Expected status %d, got %d", http.StatusMovedPermanently, rec.Code)
	}

	location := rec.Header().Get("Location")
	if location != "/new" {
		t.Errorf("Expected Location /new, got %s", location)
	}
}

func TestRewrite_TemporaryRedirect(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{
		{
			Pattern:     "^/temp$",
			Replacement: "/redirected",
			Flag:        "redirect",
		},
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for redirect")
	}))

	req := httptest.NewRequest("GET", "/temp", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("Expected status %d, got %d", http.StatusFound, rec.Code)
	}
}

func TestRewrite_BreakFlag(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{
		{
			Pattern:     "^/internal/(.*)$",
			Replacement: "/private/$1",
			Flag:        "break",
		},
		{
			Pattern:     "^/private/(.*)$",
			Replacement: "/public/$1",
			Flag:        "last",
		},
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should be /private/test, not /public/test because break stops processing
		if r.URL.Path != "/private/test" {
			t.Errorf("Expected path /private/test (break flag), got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/internal/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
}

func TestRewrite_ChainRules(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{
		{
			Pattern:     "^/a/(.*)$",
			Replacement: "/b/$1",
			Flag:        "last",
		},
		{
			Pattern:     "^/b/(.*)$",
			Replacement: "/c/$1",
			Flag:        "last",
		},
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// First rule changes /a/x to /b/x, then second changes /b/x to /c/x
		if r.URL.Path != "/c/test" {
			t.Errorf("Expected path /c/test, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/a/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
}

func TestRewrite_ExcludePath(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{
		{
			Pattern:     "^/api/(.*)$",
			Replacement: "/v2/$1",
			Flag:        "last",
		},
	}
	config.ExcludePaths = []string{"/api/internal"}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Excluded path should not be rewritten
		if r.URL.Path != "/api/internal/secret" {
			t.Errorf("Excluded path should not change, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/internal/secret", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
}

func TestRewrite_InvalidRegex(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{
		{
			Pattern:     "[invalid(",
			Replacement: "/test",
			Flag:        "last",
		},
	}

	_, err := New(config)
	if err == nil {
		t.Error("Expected error for invalid regex")
	}
}

func TestRewrite_WithQueryString(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{
		{
			Pattern:     "^/search$",
			Replacement: "/new-search",
			Flag:        "last",
		},
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/new-search" {
			t.Errorf("Expected path /new-search, got %s", r.URL.Path)
		}
		if r.URL.RawQuery != "q=test" {
			t.Errorf("Query string should be preserved, got %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/search?q=test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
}

func TestRewrite_CaptureGroups(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{
		{
			Pattern:     "^/users/(\\d+)/posts/(\\d+)$",
			Replacement: "/posts/$2?user=$1",
			Flag:        "last",
		},
	}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The replacement puts query params in the path, which gets parsed
		if r.URL.Path != "/posts/456" && r.URL.Path != "/posts/456?user=123" {
			t.Errorf("Expected path /posts/456, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/users/123/posts/456", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != false {
		t.Error("Default Enabled should be false")
	}
}

func TestMiddleware_Priority(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Priority() != 135 {
		t.Errorf("Expected priority 135, got %d", mw.Priority())
	}
}

func TestMiddleware_Name(t *testing.T) {
	config := DefaultConfig()
	mw, _ := New(config)

	if mw.Name() != "rewrite" {
		t.Errorf("Expected name 'rewrite', got '%s'", mw.Name())
	}
}

func TestCommonRewriteRules(t *testing.T) {
	// Test that common rules exist
	if _, ok := CommonRewriteRules["old_to_new"]; !ok {
		t.Error("CommonRewriteRules should contain old_to_new")
	}
	if _, ok := CommonRewriteRules["http_to_https"]; !ok {
		t.Error("CommonRewriteRules should contain http_to_https")
	}
	if _, ok := CommonRewriteRules["trailing_slash"]; !ok {
		t.Error("CommonRewriteRules should contain trailing_slash")
	}
}

func TestRewrite_CommonRuleOldToNew(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{CommonRewriteRules["old_to_new"]}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for redirect")
	}))

	req := httptest.NewRequest("GET", "/old/page", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("Expected status %d, got %d", http.StatusMovedPermanently, rec.Code)
	}

	location := rec.Header().Get("Location")
	if location != "/new/page" {
		t.Errorf("Expected Location /new/page, got %s", location)
	}
}

func TestRewrite_TrailingSlash(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{CommonRewriteRules["trailing_slash"]}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for redirect")
	}))

	req := httptest.NewRequest("GET", "/path", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("Expected status %d, got %d", http.StatusMovedPermanently, rec.Code)
	}

	location := rec.Header().Get("Location")
	if location != "/path/" {
		t.Errorf("Expected Location /path/, got %s", location)
	}
}

func TestRewrite_RemoveTrailingSlash(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{CommonRewriteRules["remove_trailing_slash"]}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for redirect")
	}))

	req := httptest.NewRequest("GET", "/path/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("Expected status %d, got %d", http.StatusMovedPermanently, rec.Code)
	}

	location := rec.Header().Get("Location")
	if location != "/path" {
		t.Errorf("Expected Location /path, got %s", location)
	}
}

func TestRewrite_EmptyRules(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{}

	mw, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	// Should return next handler directly when no rules
	if len(mw.rules) != 0 {
		t.Error("Should have no compiled rules")
	}

	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should have been called")
	}
}
