package ctrl

import (
	. "example.com/fixture/internal/svc"
	. "os"
)

func dotted() { _, _ = ReadFile("x") } // want R4

func dottedServices() {
	_ = Pure("x")
	_ = Validate("x") // want R5
	_ = New()         // want R5
}
