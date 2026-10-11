// Package version holds build-time identification for this binary. Both
// variables default to placeholders and are overridden at build time via
// linker flags, e.g.:
//
//	go build -ldflags "-X .../internal/version.Version=v1.2.3 -X .../internal/version.Commit=$(git rev-parse HEAD)" ./cmd/calendar
//
// They are recorded in the YAML configuration file (see
// config.Config.WrittenBy) every time it is saved, so a future config
// migration can tell which binary last wrote a given file.
package version

// Version is a human-readable release identifier (e.g. a git tag like
// "v1.2.3", or "unstable"/"dev" for untagged builds). Set via -ldflags at
// build time; defaults to "dev" for local/unflagged builds.
var Version = "dev"

// Commit is the full git commit SHA1 the binary was built from. Set via
// -ldflags at build time; defaults to "unknown" when not supplied.
var Commit = "unknown"
