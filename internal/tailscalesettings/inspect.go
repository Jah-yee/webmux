package tailscalesettings

import (
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/statefile"
)

func Inspect(dir string, defaults config.TailscaleConfig) (config.TailscaleConfig, bool, error) {
	f, err := statefile.ReadSnapshot[storedFile](dir, statefile.TailscaleSettings, func(f storedFile) error { _, err := decodeStored(f, defaults); return err })
	if err != nil {
		return defaults, false, err
	}
	v, err := decodeStored(f, defaults)
	return v, err == nil, err
}
