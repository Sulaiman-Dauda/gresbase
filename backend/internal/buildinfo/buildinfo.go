// Package buildinfo holds the build-stamped version metadata. It is the single
// source of truth for the version string — set at build time via -ldflags and
// read by the CLI, the health/metrics endpoints, and the generated OpenAPI spec.
package buildinfo

var (
	// Version is the Gresbase release version. Overridden at build time with
	// -ldflags "-X github.com/gresbase/gresbase/internal/buildinfo.Version=...".
	Version = "1.0.0"
	// BuildTime is the UTC build timestamp (set via ldflags).
	BuildTime = "dev"
	// GitCommit is the short commit hash (set via ldflags).
	GitCommit = "unknown"
)
