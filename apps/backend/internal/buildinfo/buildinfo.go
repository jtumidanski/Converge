// Package buildinfo exposes the version compiled into the binary.
package buildinfo

// Version is overridden at link time with
// -ldflags "-X github.com/jtumidanski/converge/internal/buildinfo.Version=<version>".
var Version = "dev"
