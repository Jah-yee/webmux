package tray

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rainchan/webmux/internal/fetch"
	"github.com/rainchan/webmux/internal/wslclient"
)

// Release digests of the Linux server built alongside this tray, set with
// -ldflags -X by `make release`. Empty in development builds.
var (
	linuxAMD64SHA256 string
	linuxARM64SHA256 string
)

// ReleaseRepo is the GitHub owner/name serving release assets.
var ReleaseRepo = "mzdluo123/webmux"

var (
	// ErrInstallUnsupported means this tray carries no server digests.
	ErrInstallUnsupported = errors.New("此版本托盘不支持安装 webmux")
	// ErrNoWindowsFiles means the distribution cannot see Windows drives.
	ErrNoWindowsFiles = errors.New("WSL 无法访问 Windows 文件（automount 已关闭？）")
)

// BuiltinDigests returns the server digests by WSL architecture name
// ("amd64", "arm64"), or nil for a development build.
func BuiltinDigests() map[string]string {
	if linuxAMD64SHA256 == "" || linuxARM64SHA256 == "" {
		return nil
	}
	return map[string]string{"amd64": linuxAMD64SHA256, "arm64": linuxARM64SHA256}
}

// Installer puts the release server matching the tray version into WSL.
type Installer struct {
	Runner  Runner
	Target  Target
	Version string // tray version; the release tag is "v"+Version
	// Dir holds verified downloads on the Windows side.
	Dir        string
	Digests    map[string]string // by architecture; nil means unsupported
	Downloader fetch.Downloader
	Logf       func(format string, args ...any)
}

// Install downloads, verifies and installs the server, then checks that the
// installed file reports the tray version. The existing file in WSL is
// untouched unless a verified download is ready.
func (i Installer) Install(ctx context.Context) error {
	if i.Digests == nil || !releaseVersion(i.Version) {
		return ErrInstallUnsupported
	}
	arch, err := i.arch(ctx)
	if err != nil {
		return err
	}
	digest, ok := i.Digests[arch]
	if !ok {
		return fmt.Errorf("不支持的 WSL 架构 %s", arch)
	}
	name := "webmux-linux-" + arch
	local := filepath.Join(i.Dir, "webmux-"+i.Version+"-linux-"+arch)
	if sum, err := fetch.FileSHA256(local); err != nil || sum != digest {
		if err := os.MkdirAll(i.Dir, 0o700); err != nil {
			return fmt.Errorf("下载目录：%w", err)
		}
		asset := fetch.Asset{Path: "/" + ReleaseRepo + "/releases/download/v" + i.Version + "/" + name, SHA256: digest}
		source, err := i.Downloader.Download(ctx, asset, local)
		if err != nil {
			return fmt.Errorf("下载 %s %s：%w", name, i.Version, err)
		}
		i.logf("downloaded %s %s from %s", name, i.Version, source)
	}
	res, err := i.run(ctx, i.Target.Wslpath(local))
	if err != nil {
		return err
	}
	src := strings.TrimSpace(wslclient.DecodeOutput(res.Stdout))
	if res.Code != 0 || !strings.HasPrefix(src, "/") {
		return ErrNoWindowsFiles
	}
	if res, err = i.run(ctx, i.Target.Install(src)); err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("安装到 %s 失败：%s", i.Target.Webmux, output(res))
	}
	if res, err = i.run(ctx, i.Target.Version()); err != nil {
		return err
	}
	if got := ParseBuildInfo(wslclient.DecodeOutput(res.Stdout)).Version; got != i.Version {
		return fmt.Errorf("安装后的 webmux 报告版本 %q，应为 %s：%s", got, i.Version, output(res))
	}
	i.logf("installed webmux %s (%s) at %s", i.Version, arch, i.Target.Webmux)
	return nil
}

func (i Installer) arch(ctx context.Context) (string, error) {
	res, err := i.run(ctx, i.Target.Arch())
	if err != nil {
		return "", err
	}
	machine := strings.TrimSpace(wslclient.DecodeOutput(res.Stdout))
	if res.Code != 0 {
		return "", fmt.Errorf("无法检测 WSL 架构：%s", output(res))
	}
	switch machine {
	case "x86_64", "amd64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	}
	return "", fmt.Errorf("不支持的 WSL 架构 %s", machine)
}

func (i Installer) run(ctx context.Context, inv Invocation) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return i.Runner.Run(ctx, inv.Argv, inv.Env)
}

func (i Installer) logf(format string, args ...any) {
	if i.Logf != nil {
		i.Logf(format, args...)
	}
}

func output(res Result) string {
	return strings.TrimSpace(wslclient.DecodeOutput(res.Stderr) + wslclient.DecodeOutput(res.Stdout))
}

// ParseVersion extracts the version from `webmux version` output
// ("webmux 0.2.0"); it returns "" when the output has another shape.
func ParseVersion(out string) string {
	fields := strings.Fields(out)
	if len(fields) != 2 || fields[0] != "webmux" {
		return ""
	}
	return fields[1]
}

func releaseVersion(v string) bool {
	_, ok := semver(v)
	return ok
}

func semver(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for n, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 || p != strconv.Itoa(x) {
			return out, false
		}
		out[n] = x
	}
	return out, true
}
