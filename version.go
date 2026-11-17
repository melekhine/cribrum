package cribrum

import (
	"fmt"
	"runtime/debug"
)

// Build metadata. Release builds may override these via
// -ldflags "-X github.com/melekhine/cribrum.Version=...". When left at their
// defaults, BuildInfo falls back to what the Go toolchain embeds itself.
var (
	// Version is the semantic version tag (falls back to the module version or "dev").
	Version = "dev"
	// GitHash is the commit hash (falls back to vcs.revision).
	GitHash = "unknown"
	// BuildTime is the commit or build timestamp (falls back to vcs.time).
	BuildTime = "unknown"
)

// BuildInfo returns a formatted string with version, commit hash, and build time.
func BuildInfo() string {
	version, hash, built := Version, GitHash, BuildTime

	if bi, ok := debug.ReadBuildInfo(); ok {
		if version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			version = bi.Main.Version
		}
		var dirty bool
		for _, kv := range bi.Settings {
			switch kv.Key {
			case "vcs.revision":
				if hash == "unknown" {
					hash = kv.Value[:min(len(kv.Value), 12)]
				}
			case "vcs.time":
				if built == "unknown" {
					built = kv.Value
				}
			case "vcs.modified":
				dirty = kv.Value == "true"
			}
		}
		if dirty && GitHash == "unknown" && hash != "unknown" {
			hash += "-dirty"
		}
	}

	return fmt.Sprintf("cribrum %s (commit: %s, built: %s)", version, hash, built)
}
