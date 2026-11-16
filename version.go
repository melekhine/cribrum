package cribrum

import "fmt"

var (
	// Version represents the semantic version tag from git (falls back to "dev").
	Version = "dev"
	// GitHash represents the short or full commit hash.
	GitHash = "unknown"
	// BuildTime represents the timestamp when the binary was compiled.
	BuildTime = "unknown"
)

// BuildInfo returns a formatted string with version, commit hash, and build time.
func BuildInfo() string {
	return fmt.Sprintf("cribrum %s (commit: %s, built: %s)", Version, GitHash, BuildTime)
}
