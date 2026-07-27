// Package version holds the build version for outline-gate.
// Override at link time:
//
//	go build -ldflags="-X github.com/unhexx/outline-gate/internal/version.Version=0.4.0"
package version

import "strings"

// Version is the release version without a leading "v" (semver).
// Default is updated for each release; Docker/Makefile set it via -ldflags.
var Version = "0.4.0"

// String returns a display form with a leading "v" (e.g. "v0.4.0").
func String() string {
	v := strings.TrimSpace(Version)
	if v == "" {
		return "vdev"
	}
	if strings.HasPrefix(v, "v") || strings.HasPrefix(v, "V") {
		return "v" + strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	}
	return "v" + v
}
