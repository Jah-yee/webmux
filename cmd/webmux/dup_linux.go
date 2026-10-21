//go:build linux

package main

import "golang.org/x/sys/unix"

// dupTo uses dup3 because linux/arm64 has no dup2 system call.
func dupTo(from, to int) error { return unix.Dup3(from, to, 0) }
