// Package client runs outside the server.
package client

import (
	_ "example.com/fixture/internal/api" // want R2
	_ "example.com/fixture/internal/shared"
	_ "example.com/fixture/internal/statefile"
	_ "example.com/fixture/internal/svcout" // want R1
)
