// Package versioninfo provides build-time version variables.
// Set these via -ldflags at build time:
//
//	go build -ldflags "-X github.com/hatami57/microjet/versioninfo.Version=1.2.3 \
//	    -X github.com/hatami57/microjet/versioninfo.CommitHash=$(git rev-parse --short HEAD) \
//	    -X github.com/hatami57/microjet/versioninfo.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
package versioninfo

import "fmt"

var (
	Version    = "dev"
	CommitHash = "unknown"
	BuildTime  = "unknown"
	GoVersion  = "unknown"
)

func String() string {
	return fmt.Sprintf("%s (%s) built %s with %s", Version, CommitHash, BuildTime, GoVersion)
}
