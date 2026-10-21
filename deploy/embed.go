// Package deploy embeds the example configuration used by `webmux config init`.
package deploy

import _ "embed"

// ConfigExample is deploy/config.example.toml.
//
//go:embed config.example.toml
var ConfigExample string
