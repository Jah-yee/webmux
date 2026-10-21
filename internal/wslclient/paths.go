package wslclient

import (
	"errors"
	"strings"
)

// ErrUnmappable means a Windows directory has no path inside the target
// distribution (network shares, other distributions, device paths).
var ErrUnmappable = errors.New("directory cannot be mapped into WSL; use --cwd with a Linux path")

// MapPath classifies a working directory for distro (its real name, never
// empty). Linux paths ("/..." or "~...") are returned as linux unchanged;
// paths in the distribution's own \\wsl.localhost or \\wsl$ share convert
// directly; drive paths are returned as drive for `wslpath -a -u`, which
// honours the distribution's automount root. Relative paths are resolved
// against base, an absolute Windows directory. Windows path syntax is
// applied on every platform so the rules are testable anywhere.
func MapPath(path, base, distro string) (linux, drive string, err error) {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "/") {
		return path, "", nil
	}
	p := strings.ReplaceAll(path, "/", `\`)
	switch {
	case strings.HasPrefix(p, `\\?\`) || strings.HasPrefix(p, `\\.\`):
		return "", "", ErrUnmappable
	case strings.HasPrefix(p, `\\`):
		host, rest, _ := strings.Cut(p[2:], `\`)
		share, rest, _ := strings.Cut(rest, `\`)
		host = strings.ToLower(host)
		if (host != "wsl.localhost" && host != "wsl$") || share == "" || !strings.EqualFold(share, distro) {
			return "", "", ErrUnmappable
		}
		parts := cleanParts(strings.Split(rest, `\`))
		return "/" + strings.Join(parts, "/"), "", nil
	case isDriveAbs(p):
		parts := cleanParts(strings.Split(p[3:], `\`))
		return "", strings.ToUpper(p[:1]) + `:\` + strings.Join(parts, `\`), nil
	case len(p) >= 2 && p[1] == ':':
		// Drive-relative ("C:dir") depends on per-drive state Windows keeps.
		return "", "", ErrUnmappable
	case strings.HasPrefix(p, `\`):
		// Root of the current drive.
		if !isDriveAbs(base) {
			return "", "", ErrUnmappable
		}
		return MapPath(base[:2]+p, base, distro)
	}
	if base == "" || (!isDriveAbs(base) && !strings.HasPrefix(base, `\\`)) {
		return "", "", ErrUnmappable
	}
	return MapPath(strings.TrimRight(strings.ReplaceAll(base, "/", `\`), `\`)+`\`+p, "", distro)
}

func isDriveAbs(p string) bool {
	return len(p) >= 3 && isLetter(p[0]) && p[1] == ':' && (p[2] == '\\' || p[2] == '/')
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// cleanParts resolves "." and ".." without climbing above the root.
func cleanParts(parts []string) []string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, part)
		}
	}
	return out
}
