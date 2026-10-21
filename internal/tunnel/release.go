// Package tunnel locates, downloads, and runs cloudflared to expose webmux
// through a Cloudflare Tunnel.
package tunnel

// Version is the cloudflared release downloaded when no cloudflared is
// configured or on PATH. Upgrade it together with the digests below.
const Version = "2026.10.2"

// asset describes one platform's release download. sha256 is the GitHub
// release asset digest; binarySHA256 is the extracted executable, used to
// verify the cache (identical to sha256 for bare binaries).
type asset struct {
	name         string
	sha256       string
	binarySHA256 string
	archive      bool // .tgz containing a top-level "cloudflared"
}

var assets = map[string]asset{
	"linux/amd64": {
		name:         "cloudflared-linux-amd64",
		sha256:       "35fbdd2beb378436c7da377d7358cea8e33f9c94a6705d1d8036bf9b0d6d8005",
		binarySHA256: "35fbdd2beb378436c7da377d7358cea8e33f9c94a6705d1d8036bf9b0d6d8005",
	},
	"linux/arm64": {
		name:         "cloudflared-linux-arm64",
		sha256:       "b7e55b4d9702c2ab5f8348f780bb1db044527fc555367714c78e0595750a4665",
		binarySHA256: "b7e55b4d9702c2ab5f8348f780bb1db044527fc555367714c78e0595750a4665",
	},
	"darwin/amd64": {
		name:         "cloudflared-darwin-amd64.tgz",
		sha256:       "b64efd5a2772fc124b855f60eb3d33ba1a4f2ba67b19ddc58d32b705d42b82c9",
		binarySHA256: "4be2a4c0b76f6be4a84bea590743e85041949c294efcee4de86a140a9f11869a",
		archive:      true,
	},
	"darwin/arm64": {
		name:         "cloudflared-darwin-arm64.tgz",
		sha256:       "529a07089ac2d46762578389230fa148e5cb6ce2689200fc006956864042d17a",
		binarySHA256: "823f993ef362fba5e0e288db6f3452d8de34291632971c7a40b79548420e9a18",
		archive:      true,
	},
}

// DefaultMirrors are gh-proxy style prefixes tried, in order, after a direct
// GitHub download fails. Each receives "<prefix>/https://github.com/...".
// Both served the pinned linux-amd64 asset intact from mainland China at
// 1.8-2.5 MB/s on 2026-10-20; ghproxy.net managed only ~10 KB/s and was dropped.
var DefaultMirrors = []string{"https://ghfast.top", "https://gh-proxy.com"}
