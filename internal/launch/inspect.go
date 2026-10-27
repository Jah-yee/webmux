package launch

import "github.com/rainchan/webmux/internal/statefile"

func Inspect(dir string) error {
	_, err := statefile.ReadSnapshot[storedFile](dir, statefile.Launch, func(f storedFile) error { _, err := validateStored(f); return err })
	return err
}
