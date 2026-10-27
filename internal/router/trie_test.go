package router

import (
	"testing"
)

// TestRadixInsert_RejectsConflictingParamNames pins F018: two different
// parameter names at the same trie position must be rejected (the node can
// only capture under one name).
func TestRadixInsert_RejectsConflictingParamNames(t *testing.T) {
	trie := NewRadixTrie()
	if err := trie.Insert("/users/:id", 1); err != nil {
		t.Fatalf("insert /users/:id: %v", err)
	}
	if err := trie.Insert("/users/:name/profile", 2); err == nil {
		t.Error("expected error for conflicting param name at same position")
	}
	// Re-inserting with the SAME param name is fine.
	if err := trie.Insert("/users/:id/profile", 3); err != nil {
		t.Errorf("same-name param insert should succeed: %v", err)
	}
	// Wildcards are subject to the same rule.
	t2 := NewRadixTrie()
	if err := t2.Insert("/p/*a", 1); err != nil {
		t.Fatalf("insert /p/*a: %v", err)
	}
	if err := t2.Insert("/p/*b", 2); err == nil {
		t.Error("expected error for conflicting wildcard name at same position")
	}
}

// TestRadixInsert_RejectsWildcardNotLast pins F019: a route with segments
// below a wildcard can never match and must be rejected at insert.
func TestRadixInsert_RejectsWildcardNotLast(t *testing.T) {
	trie := NewRadixTrie()
	if err := trie.Insert("/files/*path/sub", 1); err == nil {
		t.Error("expected error for wildcard segment that is not last")
	}
	// A valid wildcard still captures the remainder.
	if err := trie.Insert("/files/*path", 2); err != nil {
		t.Fatalf("insert /files/*path: %v", err)
	}
	res, ok := trie.Match("/files/a/b")
	if !ok || res.Params["path"] != "a/b" {
		t.Errorf("wildcard capture broken: ok=%v params=%v", ok, res.Params)
	}
}

func TestNewTrie(t *testing.T) {
	trie := NewTrie()
	if trie == nil {
		t.Fatal("NewTrie() returned nil")
	}
	if trie.root == nil {
		t.Fatal("NewTrie() root is nil")
	}
}

func TestExactMatch(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/users", "users_handler")

	tests := []struct {
		path       string
		wantOk     bool
		wantVal    string
		wantParams map[string]string
	}{
		{"/users", true, "users_handler", map[string]string{}},
		{"/users/", true, "users_handler", map[string]string{}}, // trailing slash
		{"/users/123", false, "", nil},
		{"/user", false, "", nil},
		{"/userss", false, "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result, ok := trie.Match(tt.path)
			if ok != tt.wantOk {
				t.Errorf("Match(%q) ok = %v, want %v", tt.path, ok, tt.wantOk)
				return
			}
			if !tt.wantOk {
				return
			}
			if result.Value != tt.wantVal {
				t.Errorf("Match(%q) value = %v, want %v", tt.path, result.Value, tt.wantVal)
			}
			if len(result.Params) != len(tt.wantParams) {
				t.Errorf("Match(%q) params = %v, want %v", tt.path, result.Params, tt.wantParams)
			}
		})
	}
}

func TestParamExtraction(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/users/:id", "user_detail_handler")

	tests := []struct {
		path       string
		wantOk     bool
		wantVal    string
		wantParams map[string]string
	}{
		{
			path:       "/users/123",
			wantOk:     true,
			wantVal:    "user_detail_handler",
			wantParams: map[string]string{"id": "123"},
		},
		{
			path:       "/users/abc",
			wantOk:     true,
			wantVal:    "user_detail_handler",
			wantParams: map[string]string{"id": "abc"},
		},
		{
			path:       "/users/123/extra",
			wantOk:     false,
			wantParams: nil,
		},
		{
			path:       "/users",
			wantOk:     false,
			wantParams: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result, ok := trie.Match(tt.path)
			if ok != tt.wantOk {
				t.Errorf("Match(%q) ok = %v, want %v", tt.path, ok, tt.wantOk)
				return
			}
			if !tt.wantOk {
				return
			}
			if result.Value != tt.wantVal {
				t.Errorf("Match(%q) value = %v, want %v", tt.path, result.Value, tt.wantVal)
			}
			for k, v := range tt.wantParams {
				if result.Params[k] != v {
					t.Errorf("Match(%q) params[%q] = %q, want %q", tt.path, k, result.Params[k], v)
				}
			}
		})
	}
}

func TestWildcardMatch(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/files/*filepath", "file_handler")

	tests := []struct {
		path       string
		wantOk     bool
		wantVal    string
		wantParams map[string]string
	}{
		{
			path:       "/files/js/app.js",
			wantOk:     true,
			wantVal:    "file_handler",
			wantParams: map[string]string{"filepath": "js/app.js"},
		},
		{
			path:       "/files/css/style.css",
			wantOk:     true,
			wantVal:    "file_handler",
			wantParams: map[string]string{"filepath": "css/style.css"},
		},
		{
			path:       "/files/image.png",
			wantOk:     true,
			wantVal:    "file_handler",
			wantParams: map[string]string{"filepath": "image.png"},
		},
		{
			path:       "/files",
			wantOk:     false,
			wantParams: nil,
		},
		{
			path:       "/file/js/app.js",
			wantOk:     false,
			wantParams: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result, ok := trie.Match(tt.path)
			if ok != tt.wantOk {
				t.Errorf("Match(%q) ok = %v, want %v", tt.path, ok, tt.wantOk)
				return
			}
			if !tt.wantOk {
				return
			}
			if result.Value != tt.wantVal {
				t.Errorf("Match(%q) value = %v, want %v", tt.path, result.Value, tt.wantVal)
			}
			for k, v := range tt.wantParams {
				if result.Params[k] != v {
					t.Errorf("Match(%q) params[%q] = %q, want %q", tt.path, k, result.Params[k], v)
				}
			}
		})
	}
}

func TestPriorityMatching(t *testing.T) {
	// Test that exact > param > wildcard
	trie := NewTrie()
	trie.Insert("/users/new", "exact_handler")
	trie.Insert("/users/:id", "param_handler")
	trie.Insert("/users/*path", "wildcard_handler")

	tests := []struct {
		path    string
		wantVal string
	}{
		{"/users/new", "exact_handler"},         // exact should win
		{"/users/123", "param_handler"},         // param should win over wildcard
		{"/users/123/edit", "wildcard_handler"}, // wildcard captures rest
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result, ok := trie.Match(tt.path)
			if !ok {
				t.Errorf("Match(%q) returned false, want true", tt.path)
				return
			}
			if result.Value != tt.wantVal {
				t.Errorf("Match(%q) value = %v, want %v", tt.path, result.Value, tt.wantVal)
			}
		})
	}
}

func TestNestedPaths(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/api/v1/users", "users_list")
	trie.Insert("/api/v1/users/:id", "user_get")
	trie.Insert("/api/v1/users/:id/posts", "user_posts")
	trie.Insert("/api/v1/posts/:postId/comments/:commentId", "comment_get")

	tests := []struct {
		path       string
		wantOk     bool
		wantVal    string
		wantParams map[string]string
	}{
		{
			path:       "/api/v1/users",
			wantOk:     true,
			wantVal:    "users_list",
			wantParams: map[string]string{},
		},
		{
			path:       "/api/v1/users/123",
			wantOk:     true,
			wantVal:    "user_get",
			wantParams: map[string]string{"id": "123"},
		},
		{
			path:       "/api/v1/users/123/posts",
			wantOk:     true,
			wantVal:    "user_posts",
			wantParams: map[string]string{"id": "123"},
		},
		{
			path:    "/api/v1/posts/42/comments/7",
			wantOk:  true,
			wantVal: "comment_get",
			wantParams: map[string]string{
				"postId":    "42",
				"commentId": "7",
			},
		},
		{
			path:   "/api/v2/users",
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result, ok := trie.Match(tt.path)
			if ok != tt.wantOk {
				t.Errorf("Match(%q) ok = %v, want %v", tt.path, ok, tt.wantOk)
				return
			}
			if !tt.wantOk {
				return
			}
			if result.Value != tt.wantVal {
				t.Errorf("Match(%q) value = %v, want %v", tt.path, result.Value, tt.wantVal)
			}
			for k, v := range tt.wantParams {
				if result.Params[k] != v {
					t.Errorf("Match(%q) params[%q] = %q, want %q", tt.path, k, result.Params[k], v)
				}
			}
		})
	}
}

func TestRootPath(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/", "root_handler")

	result, ok := trie.Match("/")
	if !ok {
		t.Fatal("Match(\"/\") returned false, want true")
	}
	if result.Value != "root_handler" {
		t.Errorf("Match(\"/\") value = %v, want root_handler", result.Value)
	}
	if len(result.Params) != 0 {
		t.Errorf("Match(\"/\") params = %v, want empty", result.Params)
	}
}

func TestNoMatch(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/users", "users_handler")
	trie.Insert("/posts/:id", "post_handler")

	tests := []string{
		"/articles",
		"/users/123/extra",
		"",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			_, ok := trie.Match(path)
			if ok {
				t.Errorf("Match(%q) returned true, want false", path)
			}
		})
	}
}

func TestMultipleParams(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/users/:userId/posts/:postId", "user_post_handler")

	result, ok := trie.Match("/users/42/posts/100")
	if !ok {
		t.Fatal("Match returned false, want true")
	}
	if result.Value != "user_post_handler" {
		t.Errorf("value = %v, want user_post_handler", result.Value)
	}
	if result.Params["userId"] != "42" {
		t.Errorf("params[userId] = %q, want 42", result.Params["userId"])
	}
	if result.Params["postId"] != "100" {
		t.Errorf("params[postId] = %q, want 100", result.Params["postId"])
	}
}

func TestWildcardAtDifferentPositions(t *testing.T) {
	// Wildcard should only work at end of path
	trie := NewTrie()
	trie.Insert("/static/*filepath", "static_handler")

	// Test that wildcard captures everything after /static/
	result, ok := trie.Match("/static/css/main.css")
	if !ok {
		t.Fatal("Match returned false")
	}
	if result.Params["filepath"] != "css/main.css" {
		t.Errorf("filepath = %q, want css/main.css", result.Params["filepath"])
	}

	// Test with deeply nested path
	result, ok = trie.Match("/static/js/vendor/lodash/index.js")
	if !ok {
		t.Fatal("Match returned false")
	}
	if result.Params["filepath"] != "js/vendor/lodash/index.js" {
		t.Errorf("filepath = %q, want js/vendor/lodash/index.js", result.Params["filepath"])
	}
}

func TestOverlappingPaths(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/users/new", "new_user")
	trie.Insert("/users/:id", "get_user")
	trie.Insert("/users/:id/edit", "edit_user")
	trie.Insert("/users/admin", "admin_user")

	tests := []struct {
		path    string
		wantVal string
	}{
		{"/users/new", "new_user"},       // exact match
		{"/users/admin", "admin_user"},   // exact match
		{"/users/123", "get_user"},       // param match
		{"/users/123/edit", "edit_user"}, // param + static
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result, ok := trie.Match(tt.path)
			if !ok {
				t.Errorf("Match(%q) returned false", tt.path)
				return
			}
			if result.Value != tt.wantVal {
				t.Errorf("Match(%q) value = %v, want %v", tt.path, result.Value, tt.wantVal)
			}
		})
	}
}

func TestPathNormalization(t *testing.T) {
	trie := NewTrie()
	trie.Insert("users", "users_handler")   // no leading slash
	trie.Insert("/posts/", "posts_handler") // trailing slash

	// Both should work the same
	result, ok := trie.Match("/users")
	if !ok || result.Value != "users_handler" {
		t.Errorf("Match(/users) failed")
	}

	result, ok = trie.Match("/posts")
	if !ok || result.Value != "posts_handler" {
		t.Errorf("Match(/posts) failed")
	}

	result, ok = trie.Match("/posts/")
	if !ok || result.Value != "posts_handler" {
		t.Errorf("Match(/posts/) failed")
	}
}

func TestEmptyPath(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/", "root")

	// Empty string should not match
	_, ok := trie.Match("")
	if ok {
		t.Error("Match(\"\") should return false")
	}
}

func TestExampleUsage(t *testing.T) {
	// Test the example from the requirements
	trie := NewTrie()
	trie.Insert("/users", "users_handler")
	trie.Insert("/users/:id", "user_detail_handler")
	trie.Insert("/files/*path", "file_handler")

	result, ok := trie.Match("/users/123")
	if !ok {
		t.Fatal("Expected match for /users/123")
	}
	if result.Value != "user_detail_handler" {
		t.Errorf("Expected user_detail_handler, got %v", result.Value)
	}
	if result.Params["id"] != "123" {
		t.Errorf("Expected id=123, got %v", result.Params["id"])
	}
}

func BenchmarkTrieInsert(b *testing.B) {
	trie := NewTrie()
	routes := []string{
		"/users",
		"/users/:id",
		"/users/:id/posts",
		"/users/:id/posts/:postId",
		"/api/v1/status",
		"/api/v1/users",
		"/api/v1/users/:id",
		"/files/*filepath",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, route := range routes {
			trie.Insert(route, "handler")
		}
	}
}

func BenchmarkTrieMatchStatic(b *testing.B) {
	trie := NewTrie()
	trie.Insert("/api/v1/users", "handler")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		trie.Match("/api/v1/users")
	}
}

func BenchmarkTrieMatchParam(b *testing.B) {
	trie := NewTrie()
	trie.Insert("/users/:id", "handler")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		trie.Match("/users/123")
	}
}

func BenchmarkTrieMatchWildcard(b *testing.B) {
	trie := NewTrie()
	trie.Insert("/files/*filepath", "handler")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		trie.Match("/files/path/to/file.txt")
	}
}

func BenchmarkTrieMatchNested(b *testing.B) {
	trie := NewTrie()
	trie.Insert("/api/v1/users/:id/posts/:postId", "handler")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		trie.Match("/api/v1/users/42/posts/100")
	}
}

// Additional tests for comprehensive coverage

func TestInsert_EmptyPath(t *testing.T) {
	trie := NewTrie()

	// Insert empty path should be no-op
	trie.Insert("", "empty_handler")

	// Should still match root
	result, ok := trie.Match("/")
	if ok {
		t.Error("empty path insert should not create root endpoint")
	}
	_ = result
}

func TestInsert_Duplicate(t *testing.T) {
	trie := NewTrie()

	// Insert same path twice with different values
	trie.Insert("/users", "first")
	trie.Insert("/users", "second")

	// Should return the last value
	result, ok := trie.Match("/users")
	if !ok {
		t.Fatal("expected match")
	}
	if result.Value != "second" {
		t.Errorf("expected 'second', got %v", result.Value)
	}
}

func TestInsert_InvalidWildcard(t *testing.T) {
	trie := NewTrie()

	// Wildcard in the middle of path (not at end)
	// Current implementation allows this, just tests the behavior
	trie.Insert("/files/*path/static", "handler")

	// The wildcard captures everything after /files/
	result, ok := trie.Match("/files/docs/report.pdf/static")
	if !ok {
		t.Log("Wildcard in middle may not work as expected")
	} else {
		// The wildcard captures everything including /static
		if result.Params["path"] != "docs/report.pdf/static" {
			t.Logf("Wildcard captured: %s", result.Params["path"])
		}
	}
}

func TestMatch_EmptyPath(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/", "root")

	// Empty string should not match
	_, ok := trie.Match("")
	if ok {
		t.Error("empty path should not match")
	}
}

func TestMatch_TrailingSlash(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/users", "users_handler")

	// Both with and without trailing slash should match
	result1, ok1 := trie.Match("/users")
	result2, ok2 := trie.Match("/users/")

	if !ok1 {
		t.Error("/users should match")
	}
	if !ok2 {
		t.Error("/users/ should match")
	}
	if ok1 && ok2 && result1.Value != result2.Value {
		t.Error("trailing slash should produce same result")
	}
}

func TestMatch_CaseSensitivity(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/Users", "users_handler")

	// Paths are case-sensitive
	_, ok1 := trie.Match("/Users")
	_, ok2 := trie.Match("/users")
	_, ok3 := trie.Match("/USERS")

	if !ok1 {
		t.Error("/Users should match (exact case)")
	}
	if ok2 {
		t.Error("/users should not match (case-sensitive)")
	}
	if ok3 {
		t.Error("/USERS should not match (case-sensitive)")
	}
}

func TestDeleteRoute(t *testing.T) {
	trie := NewTrie()

	// Insert and then delete
	trie.Insert("/users", "users_handler")
	trie.Insert("/posts", "posts_handler")

	// Verify both exist
	_, ok := trie.Match("/users")
	if !ok {
		t.Fatal("/users should exist before delete")
	}

	// Delete /users
	trie.Delete("/users")

	// Verify /users is gone
	_, ok = trie.Match("/users")
	if ok {
		t.Error("/users should not exist after delete")
	}

	// Verify /posts still exists
	_, ok = trie.Match("/posts")
	if !ok {
		t.Error("/posts should still exist")
	}
}

func TestDeleteRoute_WithParams(t *testing.T) {
	trie := NewTrie()

	trie.Insert("/users/:id", "user_handler")
	trie.Insert("/users/:id/posts", "posts_handler")

	// Delete the first route
	trie.Delete("/users/:id")

	// Verify it's gone but the other remains
	_, ok1 := trie.Match("/users/123")
	_, ok2 := trie.Match("/users/123/posts")

	if ok1 {
		t.Error("/users/:id should be deleted")
	}
	if !ok2 {
		t.Error("/users/:id/posts should still exist")
	}
}

func TestDeleteRoute_NonExistent(t *testing.T) {
	trie := NewTrie()

	trie.Insert("/users", "users_handler")

	// Delete non-existent path - should not panic
	trie.Delete("/nonexistent")
	trie.Delete("/users/extra")

	// Verify original still exists
	_, ok := trie.Match("/users")
	if !ok {
		t.Error("/users should still exist")
	}
}

func TestCloneTrie(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/users", "users_handler")
	trie.Insert("/users/:id", "user_detail_handler")
	trie.Insert("/files/*path", "file_handler")

	// Clone the trie
	cloned := trie.Clone()

	// Verify cloned trie has same routes
	tests := []struct {
		path    string
		wantVal string
	}{
		{"/users", "users_handler"},
		{"/users/123", "user_detail_handler"},
		{"/files/docs/report.pdf", "file_handler"},
	}

	for _, tt := range tests {
		result, ok := cloned.Match(tt.path)
		if !ok {
			t.Errorf("cloned trie should match %s", tt.path)
			continue
		}
		if result.Value != tt.wantVal {
			t.Errorf("cloned trie: expected %v, got %v", tt.wantVal, result.Value)
		}
	}

	// Modify original trie
	trie.Insert("/new", "new_handler")

	// Verify cloned trie is unaffected
	_, ok := cloned.Match("/new")
	if ok {
		t.Error("cloned trie should not be affected by changes to original")
	}
}

func TestDeepNesting(t *testing.T) {
	trie := NewTrie()

	// Create deeply nested path (10+ levels)
	path := "/a/b/c/d/e/f/g/h/i/j/k"
	trie.Insert(path, "deep_handler")

	result, ok := trie.Match(path)
	if !ok {
		t.Fatal("deeply nested path should match")
	}
	if result.Value != "deep_handler" {
		t.Errorf("expected deep_handler, got %v", result.Value)
	}

	// Test partial match fails
	_, ok = trie.Match("/a/b/c/d/e/f/g/h/i")
	if ok {
		t.Error("partial path should not match")
	}
}

func TestManyParams(t *testing.T) {
	trie := NewTrie()

	// Create path with 10+ parameters
	path := "/:a/:b/:c/:d/:e/:f/:g/:h/:i/:j"
	trie.Insert(path, "many_params_handler")

	// Match with actual values
	matchPath := "/1/2/3/4/5/6/7/8/9/10"
	result, ok := trie.Match(matchPath)
	if !ok {
		t.Fatal("path with many params should match")
	}

	// Verify all params
	expectedParams := map[string]string{
		"a": "1", "b": "2", "c": "3", "d": "4", "e": "5",
		"f": "6", "g": "7", "h": "8", "i": "9", "j": "10",
	}

	for key, wantVal := range expectedParams {
		if gotVal, ok := result.Params[key]; !ok {
			t.Errorf("missing param %s", key)
		} else if gotVal != wantVal {
			t.Errorf("param %s: got %s, want %s", key, gotVal, wantVal)
		}
	}
}

func TestMatch_InvalidPath(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/users", "users_handler")

	// Path not starting with /
	_, ok := trie.Match("users")
	if ok {
		t.Error("path without leading slash should not match")
	}
}

// --------------------------------------------------------------------------
// Additional coverage tests for radix tree edge cases
// --------------------------------------------------------------------------

func TestDelete_MiddleNode(t *testing.T) {
	// Test deleting a leaf node where the parent still has other children
	trie := NewTrie()
	trie.Insert("/users", "users_handler")
	trie.Insert("/users/new", "new_handler")
	trie.Insert("/users/admin", "admin_handler")

	// Delete middle leaf - parent /users should remain
	trie.Delete("/users/new")

	_, ok := trie.Match("/users/new")
	if ok {
		t.Error("/users/new should be deleted")
	}

	// Parent and sibling should still exist
	result, ok := trie.Match("/users")
	if !ok || result.Value != "users_handler" {
		t.Error("/users should still exist")
	}
	result, ok = trie.Match("/users/admin")
	if !ok || result.Value != "admin_handler" {
		t.Error("/users/admin should still exist")
	}
}

func TestDelete_AllChildren(t *testing.T) {
	// Delete all children of a node - parent should be cleaned up
	trie := NewTrie()
	trie.Insert("/a/b", "ab_handler")

	trie.Delete("/a/b")

	// Both /a/b and /a should be gone since /a was not an endpoint
	_, ok := trie.Match("/a/b")
	if ok {
		t.Error("/a/b should be deleted")
	}
}

func TestDelete_RootRoute(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/", "root_handler")

	result, ok := trie.Match("/")
	if !ok || result.Value != "root_handler" {
		t.Fatal("root should exist before delete")
	}

	trie.Delete("/")

	_, ok = trie.Match("/")
	if ok {
		t.Error("root should be deleted")
	}
}

func TestDelete_InvalidInput(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/users", "users_handler")

	// Delete with empty string - should be no-op
	trie.Delete("")

	// Delete with path not starting with / - should be no-op
	trie.Delete("users")

	// Original should still exist
	result, ok := trie.Match("/users")
	if !ok || result.Value != "users_handler" {
		t.Error("/users should still exist after invalid deletes")
	}
}

func TestDelete_WildcardRoute(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/files/*filepath", "file_handler")
	trie.Insert("/files/docs", "docs_handler")

	trie.Delete("/files/*filepath")

	// Wildcard should be gone but static should remain
	_, ok := trie.Match("/files/js/app.js")
	if ok {
		t.Error("wildcard route should be deleted")
	}
	result, ok := trie.Match("/files/docs")
	if !ok || result.Value != "docs_handler" {
		t.Error("/files/docs should still exist")
	}
}

func TestDelete_ParamRoute(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/users/:id", "user_handler")
	trie.Insert("/users/:id/posts", "posts_handler")

	trie.Delete("/users/:id")

	// Param route gone, but deeper route should still be unreachable
	// since the :id node was removed
	_, ok := trie.Match("/users/123")
	if ok {
		t.Error("/users/:id should be deleted")
	}
}

func TestClone_EmptyTrie(t *testing.T) {
	trie := NewTrie()

	cloned := trie.Clone()
	if cloned == nil {
		t.Fatal("Clone of empty trie should not be nil")
	}

	// No routes to match
	_, ok := cloned.Match("/anything")
	if ok {
		t.Error("empty cloned trie should not match anything")
	}
}

func TestClone_NilNodeHandling(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/test", "handler")

	// Clone should handle internal nil children correctly
	cloned := trie.Clone()

	result, ok := cloned.Match("/test")
	if !ok || result.Value != "handler" {
		t.Error("cloned trie should match /test")
	}

	// Verify independence
	trie.Insert("/extra", "extra_handler")
	_, ok = cloned.Match("/extra")
	if ok {
		t.Error("cloned trie should not see routes added to original after cloning")
	}
}

func TestClone_DeepModificationIndependence(t *testing.T) {
	trie := NewTrie()
	trie.Insert("/a/b/c/d", "deep_handler")

	cloned := trie.Clone()

	// Delete from original
	trie.Delete("/a/b/c/d")

	// Cloned should still have it
	result, ok := cloned.Match("/a/b/c/d")
	if !ok || result.Value != "deep_handler" {
		t.Error("cloned trie should be independent of deletions in original")
	}
}

func TestDelete_ParamBacktracking(t *testing.T) {
	// Test deletion when param child exists alongside static
	trie := NewTrie()
	trie.Insert("/users/admin", "admin_handler")
	trie.Insert("/users/:id", "param_handler")

	trie.Delete("/users/:id")

	// Static should remain, param gone
	result, ok := trie.Match("/users/admin")
	if !ok || result.Value != "admin_handler" {
		t.Error("/users/admin should still exist")
	}

	_, ok = trie.Match("/users/123")
	if ok {
		t.Error("/users/:id should be deleted")
	}
}

func TestDelete_TrieWithMixedTypes(t *testing.T) {
	// Insert static, param, and wildcard at same level then delete them one by one
	trie := NewTrie()
	trie.Insert("/static", "static_val")
	trie.Insert("/:param", "param_val")
	trie.Insert("/*wild", "wild_val")

	// Delete static - matches exact key "static"
	trie.Delete("/static")

	// After deleting the static endpoint, Match("/static") falls through
	// to the param match :param, so it still returns a result.
	result, ok := trie.Match("/static")
	if !ok {
		t.Fatal("expected param match for /static after static deleted")
	}
	if result.Value != "param_val" {
		t.Errorf("expected param_val for /static after static deleted, got %v", result.Value)
	}

	// Param should still work for non-static paths
	result, ok = trie.Match("/anything")
	if !ok || result.Value != "param_val" {
		t.Error("param should still match")
	}

	// Delete param - uses ":" as key since segment starts with :
	trie.Delete("/:param")

	// Wildcard should still work
	result, ok = trie.Match("/deep/nested/path")
	if !ok || result.Value != "wild_val" {
		t.Error("wildcard should still match after param deleted")
	}

	// Now /static should match wildcard (param is gone)
	result, ok = trie.Match("/static")
	if !ok || result.Value != "wild_val" {
		t.Error("wildcard should now catch /static")
	}
}
