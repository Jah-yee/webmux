package main

import (
	"log/slog"
	"os"
)

// redirectOutput points fd 1 and 2 at path so logs and runtime panics
// survive the launcher closing its pipes. An existing file is kept as
// path+".1", replacing an older one.
func redirectOutput(path string) error {
	f, err := createFresh(path, ".1", os.O_APPEND)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	for _, fd := range []int{1, 2} {
		if err := dupTo(int(f.Fd()), fd); err != nil {
			return err
		}
	}
	// Rebuild the default handler on the redirected stderr.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	return nil
}
