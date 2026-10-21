//go:build darwin

package main

import "golang.org/x/sys/unix"

func dupTo(from, to int) error { return unix.Dup2(from, to) }
