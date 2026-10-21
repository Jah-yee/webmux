//go:build linux

package session

import (
	"bytes"
	"os"
	"strconv"
)

func hasSessionMarker(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		return false
	}
	for entry := range bytes.SplitSeq(data, []byte{0}) {
		if bytes.HasPrefix(entry, []byte("WEBMUX_SESSION_ID=")) {
			return true
		}
	}
	return false
}
