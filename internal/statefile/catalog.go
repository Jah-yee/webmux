package statefile

// ID is a registered persistence capability, not a caller-supplied path.
type ID string

const (
	Launch             ID = "launch"
	Repositories       ID = "repositories"
	Tunnel             ID = "tunnel"
	TailscaleSettings  ID = "tailscale-settings"
	TailscaleOwnership ID = "tailscale-ownership"
	Push               ID = "push"
	Runtime            ID = "runtime"
	Settings           ID = "settings"
)

// Descriptor is the single runtime source of storage paths and policy.
// Domain validation remains with the owning service.
type Descriptor struct {
	ID                              ID
	Path, Owner, Failure, Sensitive string
	Version                         int
	VersionKey                      string
	MaxBytes                        int64
	Web                             bool
	Legacy                          string
}

func (d Descriptor) LockPath() string { return d.Path + ".lock" }

var catalog = []Descriptor{
	{Launch, "launch-configs.json", "internal/launch", "unavailable", "command/env", 2, "v", 4 << 20, true, ""},
	{Repositories, "repositories.json", "internal/workspace", "unavailable", "paths", 1, "version", 4 << 20, true, ""},
	{Tunnel, "tunnel-settings.json", "internal/tunnelsettings", "fail_closed", "token", 1, "v", 64 << 10, true, "tunnel.json"},
	{TailscaleSettings, "tailscale-settings.json", "internal/tailscalesettings", "fail_closed", "none", 1, "v", 64 << 10, true, ""},
	{TailscaleOwnership, "tailscale-ownership.json", "cmd/webmux", "block_start", "target", 1, "v", 64 << 10, false, "tailscale.json"},
	{Push, "push.json", "internal/push", "unavailable", "vapid/subscriptions", 1, "v", 1 << 20, true, ""},
	{Runtime, "run/server.json", "internal/runtime", "local_api_unavailable", "fingerprint", 1, "v", 64 << 10, false, ""},
	{Settings, "settings.json", "internal/settings", "unavailable", "commands", 1, "v", 4 << 20, true, ""},
}

func Catalog() []Descriptor { return append([]Descriptor(nil), catalog...) }
func Describe(id ID) Descriptor {
	for _, d := range catalog {
		if d.ID == id {
			return d
		}
	}
	return Descriptor{}
}
