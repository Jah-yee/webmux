package statefile

import "github.com/rainchan/webmux/internal/jsonutil"

// Decode shares the strict JSON codec with HTTP boundaries.
func Decode(data []byte, out any) error { return jsonutil.Decode(data, out) }
