// Package pathutil holds path helpers shared by the server, its
// configuration and the local clients.
package pathutil

import (
	"os"
	"path/filepath"
	"strings"
)

// Expand expands a leading "~/" and makes the path absolute.
func Expand(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return filepath.Abs(path)
}
