module example.com/fixture

go 1.26.0

require (
	github.com/coder/websocket v0.0.0
	github.com/creack/pty v0.0.0
	golang.org/x/sys v0.0.0
)

replace (
	github.com/coder/websocket => ./stubs/websocket
	github.com/creack/pty => ./stubs/pty
	golang.org/x/sys => ./stubs/sys
)
