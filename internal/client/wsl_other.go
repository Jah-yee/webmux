//go:build !linux

package client

func wslVirtualInterfaces() map[string]bool { return nil }
