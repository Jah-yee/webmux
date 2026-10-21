//go:build darwin

package session

// macOS does not expose other processes' environments.
func hasSessionMarker(int) bool { return false }
