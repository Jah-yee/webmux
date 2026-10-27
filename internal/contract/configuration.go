package contract

const PathConfiguration = "/local/v1/configuration"

type ConfigField struct {
	Value  any    `json:"value"`
	Source string `json:"source"`
}
type StoreHealth struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	Status     string `json:"status"`
	Phase      string `json:"phase,omitempty"`
	ObservedAt string `json:"observedAt"`
	Lock       string `json:"lock"`
}
type ConfigInstance struct {
	PID       int    `json:"pid"`
	Identity  string `json:"identity"`
	StartedAt string `json:"startedAt"`
}
type Configuration struct {
	ConfigV1
	View       string                 `json:"view"`
	ObservedAt string                 `json:"observedAt"`
	Instance   *ConfigInstance        `json:"instance,omitempty"`
	Fields     map[string]ConfigField `json:"fields"`
	Stores     []StoreHealth          `json:"stores"`
}
