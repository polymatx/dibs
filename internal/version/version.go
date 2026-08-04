// Package version holds build metadata injected at release time.
package version

// Set via -ldflags at build time; defaults describe a source build.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)
