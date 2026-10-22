package tray

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/rainchan/webmux/internal/contract"
)

// SupportedContracts are the control contract versions this tray speaks.
var SupportedContracts = []int{contract.V}

// BuildInfo identifies one webmux build: its version and the control
// contract it speaks.
type BuildInfo struct {
	Version  string
	Contract int
}

func (b BuildInfo) String() string {
	version := b.Version
	if version == "" {
		version = "?"
	}
	return fmt.Sprintf("%s (contract v%d)", version, b.Contract)
}

// Compat classifies a server build against the tray.
type Compat string

const (
	CompatMatched           Compat = "matched"
	CompatServerOlder       Compat = "server-older"
	CompatServerNewer       Compat = "server-newer"
	CompatIncompatibleOlder Compat = "incompatible-older"
	CompatIncompatibleNewer Compat = "incompatible-newer"
	// CompatUnknown is compatible but not comparable, e.g. a dev build.
	CompatUnknown Compat = "unknown"
)

// Incompatible reports a contract the tray does not speak.
func (c Compat) Incompatible() bool {
	return c == CompatIncompatibleOlder || c == CompatIncompatibleNewer
}

// Older reports a server that an install of the tray's version upgrades.
func (c Compat) Older() bool {
	return c == CompatServerOlder || c == CompatIncompatibleOlder
}

// Newer reports a server newer than the tray.
func (c Compat) Newer() bool {
	return c == CompatServerNewer || c == CompatIncompatibleNewer
}

// Classify decides compatibility by the control contract and, when
// compatible, correspondence by the version number alone: builds of the same
// version from different commits match.
func Classify(tray, server BuildInfo) Compat {
	switch {
	case server.Contract < slices.Min(SupportedContracts):
		return CompatIncompatibleOlder
	case server.Contract > slices.Max(SupportedContracts):
		return CompatIncompatibleNewer
	}
	t, ok1 := semver(tray.Version)
	w, ok2 := semver(server.Version)
	if !ok1 || !ok2 {
		return CompatUnknown
	}
	for n := range t {
		if w[n] != t[n] {
			if w[n] < t[n] {
				return CompatServerOlder
			}
			return CompatServerNewer
		}
	}
	return CompatMatched
}

// ParseBuildInfo reads `webmux version --json`. Versions without --json
// print "webmux <version>"; they passed the local API probe, so they speak
// contract v1.
func ParseBuildInfo(out string) BuildInfo {
	var v contract.VersionV1
	if err := json.Unmarshal([]byte(out), &v); err == nil && v.V > 0 && v.Contract > 0 {
		return BuildInfo{Version: v.Version, Contract: v.Contract}
	}
	return BuildInfo{Version: ParseVersion(out), Contract: 1}
}

// ErrContractUnsupported matches a *ContractError with errors.Is.
var ErrContractUnsupported = errors.New("WSL 中的 webmux 使用托盘不支持的控制契约")

// ContractError is a status the tray cannot parse because the server speaks
// another contract. Version and Local are read best effort from the fields
// every contract version has had so far.
type ContractError struct {
	V       int
	Version string
	// Local is the reported local link, or "".
	Local string
}

func (e *ContractError) Error() string {
	return fmt.Sprintf("WSL 中的 webmux %s 使用状态契约 v%d，托盘不支持", e.Version, e.V)
}

func (e *ContractError) Is(target error) bool { return target == ErrContractUnsupported }

// errNoContract means the output is not a versioned status at all.
var errNoContract = errors.New("status without a contract version")

// decodeStatus parses a status from either path, rejecting contracts the
// tray does not speak before reading the remaining fields.
func decodeStatus(data []byte) (contract.StatusV1, error) {
	var head struct {
		V       int             `json:"v"`
		Version string          `json:"version"`
		URLs    json.RawMessage `json:"urls"`
	}
	if err := json.Unmarshal(data, &head); err != nil || head.V == 0 {
		return contract.StatusV1{}, errNoContract
	}
	if !slices.Contains(SupportedContracts, head.V) {
		var urls struct {
			Local string `json:"local"`
		}
		_ = json.Unmarshal(head.URLs, &urls)
		local := ""
		if strings.HasPrefix(urls.Local, "http://") {
			local = urls.Local
		}
		return contract.StatusV1{}, &ContractError{V: head.V, Version: head.Version, Local: local}
	}
	var status contract.StatusV1
	if err := json.Unmarshal(data, &status); err != nil {
		return contract.StatusV1{}, fmt.Errorf("status v%d: %w", head.V, err)
	}
	return status, nil
}
