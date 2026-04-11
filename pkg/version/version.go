// Package version provides build-time version information for OpenLoadBalancer.
// Values are injected at build time using ldflags.
package version

import "runtime"

var (
	// Version is the semantic version of the build.
	// Injected at build time: -X github.com/openloadbalancer/olb/pkg/version.Version=0.1.0
	Version = "dev"

	// Commit is the full git commit SHA.
	// Injected at build time: -X github.com/openloadbalancer/olb/pkg/version.Commit=$(git rev-parse HEAD)
	Commit = "unknown"

	// ShortCommit is the abbreviated git commit SHA (7 characters).
	// Injected at build time: -X github.com/openloadbalancer/olb/pkg/version.ShortCommit=$(git rev-parse --short HEAD)
	ShortCommit = "unknown"

	// Date is the build timestamp in RFC3339 format.
	// Injected at build time: -X github.com/openloadbalancer/olb/pkg/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
	Date = "unknown"

	// GoVersion is the Go version used to build the binary.
	GoVersion = runtime.Version()

	// Platform is the OS/architecture combination.
	Platform = runtime.GOOS + "/" + runtime.GOARCH
)

// Info returns a map of all version information.
func Info() map[string]string {
	return map[string]string{
		"version":      Version,
		"commit":       Commit,
		"short_commit": ShortCommit,
		"date":         Date,
		"go_version":   GoVersion,
		"platform":     Platform,
	}
}

// String returns a formatted version string.
func String() string {
	return Version + " (" + ShortCommit + ", " + Date + ", " + GoVersion + ", " + Platform + ")"
}
