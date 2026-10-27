package main

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/rainchan/webmux/internal/tailscale"
)

// tailscaleReason maps only genuine CLI errors to user-facing guidance.
func tailscaleReason(err error, provider string) string {
	var cliErr *tailscale.CLIError
	if errors.As(err, &cliErr) {
		return classifyTailscaleError(cliErr, provider)
	}
	return err.Error()
}

func choosePort(serve tailscale.ServeStatus) int {
	for _, port := range []int{443, 8443, 10000} {
		if !hasPort(serve, port) {
			return port
		}
	}
	return 0
}

func autoCandidate(port int) bool {
	return port == 443 || port == 8443 || port == 10000
}

func hasPort(serve tailscale.ServeStatus, port int) bool {
	for _, entry := range serve.Entries {
		if entry.Port == port {
			return true
		}
	}
	return false
}

func targetFromListen(listen string) (string, bool) {
	host, portText, err := net.SplitHostPort(listen)
	if err != nil {
		return "", false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", false
	}
	if host == "" {
		host = "0.0.0.0"
	}
	if strings.EqualFold(host, "localhost") {
		return fmt.Sprintf("http://127.0.0.1:%d", port), true
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsUnspecified() && !ip.Equal(net.IPv4(127, 0, 0, 1)) {
		return "", false
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port), true
}

func portFromTarget(target string) int {
	_, port, err := net.SplitHostPort(strings.TrimPrefix(target, "http://"))
	if err != nil {
		return 0
	}
	value, _ := strconv.Atoi(port)
	return value
}

func localPort(local string) (int, bool) {
	host, portText, err := net.SplitHostPort(strings.TrimPrefix(local, "http://"))
	if err != nil || host != "127.0.0.1" {
		return 0, false
	}
	port, err := strconv.Atoi(portText)
	return port, err == nil && port > 0
}

func classifyTailscaleError(err error, provider string) string {
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "access denied"), strings.Contains(lower, "permission"), strings.Contains(lower, "operator"):
		if provider == "windows" {
			return "Tailscale 权限不足；请在 Windows 管理员终端执行一次 `tailscale set --operator=<Windows 用户>`"
		}
		return "Tailscale 权限不足；请执行一次 `tailscale set --operator=$USER`"
	case strings.Contains(lower, "funnel") && (strings.Contains(lower, "not available") || strings.Contains(lower, "attribute") || strings.Contains(lower, "does not")):
		return "Tailscale 节点缺少 funnel 属性；请在 Tailscale 管理台为节点启用 Funnel"
	case strings.Contains(lower, "certificate"), strings.Contains(lower, "https"):
		return "Tailscale HTTPS 证书未就绪；请先在 Tailscale 管理台启用 HTTPS"
	case strings.Contains(lower, "in use"), strings.Contains(lower, "conflict"), strings.Contains(lower, "already"):
		return "Tailscale 端口冲突；请换一个空闲端口或先移除冲突入口"
	case strings.Contains(lower, "timed out"), strings.Contains(lower, "deadline"):
		return "Tailscale 命令超时；请检查 Tailscale 状态后重试"
	default:
		return "Tailscale 命令失败；请查看服务日志并确认 Tailscale 状态"
	}
}
