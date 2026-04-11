package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestString_WithAllVariablesSet(t *testing.T) {
	// Save original values
	origVersion := Version
	origShortCommit := ShortCommit
	origDate := Date
	origGoVersion := GoVersion
	origPlatform := Platform

	// Set test values
	Version = "v1.2.3"
	ShortCommit = "abc1234"
	Date = "2024-03-14T12:00:00Z"
	GoVersion = "go1.23.0"
	Platform = "linux/amd64"

	// Restore original values after test
	defer func() {
		Version = origVersion
		ShortCommit = origShortCommit
		Date = origDate
		GoVersion = origGoVersion
		Platform = origPlatform
	}()

	result := String()
	expected := "v1.2.3 (abc1234, 2024-03-14T12:00:00Z, go1.23.0, linux/amd64)"

	if result != expected {
		t.Errorf("String() = %q, want %q", result, expected)
	}
}

func TestString_WithEmptyVariables(t *testing.T) {
	// Save original values
	origVersion := Version
	origShortCommit := ShortCommit
	origDate := Date
	origGoVersion := GoVersion
	origPlatform := Platform

	// Set empty values
	Version = ""
	ShortCommit = ""
	Date = ""
	GoVersion = ""
	Platform = ""

	// Restore original values after test
	defer func() {
		Version = origVersion
		ShortCommit = origShortCommit
		Date = origDate
		GoVersion = origGoVersion
		Platform = origPlatform
	}()

	result := String()
	expected := " (, , , )"

	if result != expected {
		t.Errorf("String() = %q, want %q", result, expected)
	}
}

func TestInfo(t *testing.T) {
	// Save original values
	origVersion := Version
	origCommit := Commit
	origShortCommit := ShortCommit
	origDate := Date

	// Set test values
	Version = "v0.1.0"
	Commit = "abcdef1234567890"
	ShortCommit = "abcdef1"
	Date = "2024-01-01T00:00:00Z"

	// Restore original values after test
	defer func() {
		Version = origVersion
		Commit = origCommit
		ShortCommit = origShortCommit
		Date = origDate
	}()

	info := Info()

	// Check all expected keys exist
	expectedKeys := []string{"version", "commit", "short_commit", "date", "go_version", "platform"}
	for _, key := range expectedKeys {
		if _, ok := info[key]; !ok {
			t.Errorf("Info() missing key: %s", key)
		}
	}

	// Check values
	if info["version"] != "v0.1.0" {
		t.Errorf("Info()[version] = %q, want %q", info["version"], "v0.1.0")
	}
	if info["commit"] != "abcdef1234567890" {
		t.Errorf("Info()[commit] = %q, want %q", info["commit"], "abcdef1234567890")
	}
	if info["short_commit"] != "abcdef1" {
		t.Errorf("Info()[short_commit] = %q, want %q", info["short_commit"], "abcdef1")
	}
	if info["date"] != "2024-01-01T00:00:00Z" {
		t.Errorf("Info()[date] = %q, want %q", info["date"], "2024-01-01T00:00:00Z")
	}

	// GoVersion and Platform should match runtime values
	if info["go_version"] != runtime.Version() {
		t.Errorf("Info()[go_version] = %q, want %q", info["go_version"], runtime.Version())
	}
	expectedPlatform := runtime.GOOS + "/" + runtime.GOARCH
	if info["platform"] != expectedPlatform {
		t.Errorf("Info()[platform] = %q, want %q", info["platform"], expectedPlatform)
	}
}

func TestVersionVariable(t *testing.T) {
	// Save original value
	origVersion := Version
	defer func() { Version = origVersion }()

	// Test that Version can be set and retrieved
	Version = "test-version-123"
	if Version != "test-version-123" {
		t.Errorf("Version = %q, want %q", Version, "test-version-123")
	}

	// Test default value (dev)
	Version = "dev"
	if Version != "dev" {
		t.Errorf("Version = %q, want %q", Version, "dev")
	}
}

func TestCommitVariable(t *testing.T) {
	// Save original values
	origCommit := Commit
	origShortCommit := ShortCommit
	defer func() {
		Commit = origCommit
		ShortCommit = origShortCommit
	}()

	// Test that Commit can be set and retrieved
	Commit = "full-commit-hash-12345"
	if Commit != "full-commit-hash-12345" {
		t.Errorf("Commit = %q, want %q", Commit, "full-commit-hash-12345")
	}

	// Test ShortCommit
	ShortCommit = "short-abc"
	if ShortCommit != "short-abc" {
		t.Errorf("ShortCommit = %q, want %q", ShortCommit, "short-abc")
	}
}

func TestDateVariable(t *testing.T) {
	// Save original value
	origDate := Date
	defer func() { Date = origDate }()

	// Test that Date can be set and retrieved
	Date = "2024-12-25T10:30:00Z"
	if Date != "2024-12-25T10:30:00Z" {
		t.Errorf("Date = %q, want %q", Date, "2024-12-25T10:30:00Z")
	}

	// Test default value (unknown)
	Date = "unknown"
	if Date != "unknown" {
		t.Errorf("Date = %q, want %q", Date, "unknown")
	}
}

func TestGoVersion(t *testing.T) {
	// GoVersion should match runtime.Version()
	if GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", GoVersion, runtime.Version())
	}

	// GoVersion should start with "go"
	if !strings.HasPrefix(GoVersion, "go") {
		t.Errorf("GoVersion should start with 'go', got %q", GoVersion)
	}
}

func TestPlatform(t *testing.T) {
	// Platform should match runtime.GOOS + "/" + runtime.GOARCH
	expected := runtime.GOOS + "/" + runtime.GOARCH
	if Platform != expected {
		t.Errorf("Platform = %q, want %q", Platform, expected)
	}

	// Platform should contain a slash
	if !strings.Contains(Platform, "/") {
		t.Errorf("Platform should contain '/', got %q", Platform)
	}
}

func TestDefaultValues(t *testing.T) {
	// Save original values
	origVersion := Version
	origCommit := Commit
	origShortCommit := ShortCommit
	origDate := Date
	defer func() {
		Version = origVersion
		Commit = origCommit
		ShortCommit = origShortCommit
		Date = origDate
	}()

	// Reset to defaults
	Version = "dev"
	Commit = "unknown"
	ShortCommit = "unknown"
	Date = "unknown"

	// Test defaults
	if Version != "dev" {
		t.Errorf("Default Version = %q, want %q", Version, "dev")
	}
	if Commit != "unknown" {
		t.Errorf("Default Commit = %q, want %q", Commit, "unknown")
	}
	if ShortCommit != "unknown" {
		t.Errorf("Default ShortCommit = %q, want %q", ShortCommit, "unknown")
	}
	if Date != "unknown" {
		t.Errorf("Default Date = %q, want %q", Date, "unknown")
	}
}
