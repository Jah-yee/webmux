// Package ctrl is a controller exercising R3, R4 and R5.
package ctrl

import (
	fsys "os"
	_ "os/exec" // want R3
	_ "syscall" // want R3

	_ "example.com/fixture/internal/statefile" // want R3
	s "example.com/fixture/internal/svc"
	_ "github.com/creack/pty" // want R3
	_ "golang.org/x/sys/unix" // want R3
)

type Handler struct{ Service *s.Service }

func (h Handler) Run() error {
	_, _ = fsys.ReadDir(".") // want R4
	stat := fsys.Stat        // want R4
	_, _ = stat(".")
	_ = s.Validate("x") // want R5
	_ = s.New()         // want R5
	check := s.Validate // want R5
	_ = check("x")
	_ = s.Generic(1) // want R5
	_ = s.Pure("x")
	pure := s.Pure
	_ = pure("x")
	s.NoReason() // want R5
	s.Save()     // want R5
	// Compliant: methods, types, sentinels, constants and non-filesystem os.
	_ = h.Service.Do("x")
	_ = s.Report{}.Devices
	_, _ = fsys.Hostname()
	_ = fsys.PathSeparator
	return s.ErrNotFound
}
