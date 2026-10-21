package attach

import "errors"

// ErrNotTerminal means standard input or output is not an interactive
// terminal, so a raw attachment is impossible.
var ErrNotTerminal = errors.New("standard input and output must be a terminal")
