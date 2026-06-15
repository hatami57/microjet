// Package versioninfo provides build-time version variables.
// Set these via -ldflags at build time:
//
//	go build -ldflags "-X github.com/hatami57/microjet/versioninfo.Version=1.2.3 \
//	    -X github.com/hatami57/microjet/versioninfo.CommitHash=$(git rev-parse --short HEAD) \
//	    -X github.com/hatami57/microjet/versioninfo.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
package versioninfo

import (
	"fmt"
	"io"
	"os"
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

// Info holds all version fields as a value type for structured access or logging.
type Info struct {
	Version    string `json:"version"`
	CommitHash string `json:"commit_hash"`
	BuildTime  string `json:"build_time"`
	GoVersion  string `json:"go_version"`
}

// Get returns the current build's version info.
func Get() Info {
	return Info{
		Version:    Version,
		CommitHash: CommitHash,
		BuildTime:  BuildTime,
		GoVersion:  GoVersion(),
	}
}

// Print writes version fields as key=value lines to w.
func (i Info) Print(w io.Writer) {
	fmt.Fprintf(w, "version=%s\ncommit=%s\nbuild_time=%s\ngo_version=%s\n",
		i.Version, i.CommitHash, i.BuildTime, i.GoVersion)
}

// PrintToStdout writes version fields as key=value lines to stdout.
func (i Info) PrintToStdout() { i.Print(os.Stdout) }

func Print() {
	fmt.Printf("\nversion=%s\ncommit=%s\nbuild_time=%s\ngo_version=%s\n\n", Version, CommitHash, BuildTime, GoVersion())
}
