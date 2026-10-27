package push

import "github.com/rainchan/webmux/internal/statefile"

func Inspect(dir string) error {
	_, err := statefile.ReadSnapshot[storedFile](dir, statefile.Push, func(f storedFile) error { return new(Store).loadValue(f) })
	return err
}
