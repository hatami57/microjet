// Package versioninfo provides build-time version variables.
// Set these via -ldflags at build time:
//
//	go build -ldflags "-X github.com/hatami57/microjet/versioninfo.Version=1.2.3 \
//	    -X github.com/hatami57/microjet/versioninfo.CommitHash=$(git rev-parse --short HEAD) \
//	    -X github.com/hatami57/microjet/versioninfo.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
package versioninfo

import (
	"fmt"
	"runtime"
)

var (
	Version    = "dev"
	CommitHash = "unknown"
	BuildTime  = "unknown"
)

// GoVersion reports the Go toolchain that built the binary. It is authoritative
// at runtime, so unlike Version/CommitHash/BuildTime it is not set via ldflags.
func GoVersion() string { return runtime.Version() }

func String() string {
	return fmt.Sprintf("%s (%s) built %s with %s", Version, CommitHash, BuildTime, GoVersion())
}
