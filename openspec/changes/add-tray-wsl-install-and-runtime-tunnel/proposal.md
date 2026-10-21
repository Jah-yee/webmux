# Proposal

## Why

Windows 用户安装托盘后，还必须手动进入 WSL 下载、安装 Linux 版 `webmux`；缺失或过旧时托盘只显示错误，且安装后不会自动恢复，必须重开托盘。另外，Cloudflare Tunnel 只能在启动时用 `--tunnel` 开启，托盘无法控制；想临时开放公网访问就必须重启实例并丢失全部会话。

## What Changes

- 新增发布流水线：推送 `v*` 标签时构建 Linux / macOS / Windows 控制台 / 托盘全部二进制，发布到 GitHub Release，并附带 `SHA256SUMS`。构建托盘时把同版本 Linux 二进制的 SHA-256 写入托盘。
- 托盘检测到 WSL 中的 `webmux` 缺失、不兼容或版本低于托盘时，在菜单提供“安装 / 升级 WSL 中的 webmux”：在 Windows 侧从 GitHub Release（失败时依次使用镜像）下载与托盘同版本、与发行版架构匹配的 Linux 二进制，校验内置 SHA-256 后，以 argv 方式安装到配置的路径，再重新检测。托盘不在后台自动安装或覆盖。
- 修复托盘在“找不到 webmux / 版本过旧”失败状态下无法恢复的问题：打开菜单或安装完成后重新检测，通过则回到“已停止”。
- 服务端新增运行时隧道控制：本机 API 可在不重启、不影响会话的情况下启动或停止 Cloudflare Tunnel；状态契约新增可选的 `tunnel` 字段（状态、公网地址、失败原因）。启动时的 `--tunnel` 行为保持不变。
- 运行时开启隧道要求实例的 Token 已配置且不少于 43 个字符；不满足时拒绝并给出原因，不在运行中更换 Token。
- 托盘新增“公网隧道”开关：首次开启前确认风险；显示公网地址并提供复制（链接不含 Token）；隧道状态随 WebMux 重启清空，不持久化。

## Capabilities

### New Capabilities

- `release-distribution`: GitHub Release 发布流水线、产物命名、`SHA256SUMS` 与托盘内置的 Linux 二进制摘要。
- `windows-tray-wsl-install`: 托盘检测 WSL 中 `webmux` 的缺失 / 不兼容 / 过旧，下载校验并安装或升级，以及失败状态的恢复。
- `runtime-tunnel-control`: 运行中的 WebMux 启停 Cloudflare Tunnel、Token 前置条件、公网 Origin 授权的建立与撤销、隧道状态上报。
- `windows-tray-tunnel`: 托盘的公网隧道开关、风险确认、公网地址展示与复制。

### Modified Capabilities

- `server-local-api`: 新增隧道控制路由与状态中的 `tunnel` 字段（向后兼容的可选字段，契约版本仍为 v1）。

## Impact

- 新增 `.github/workflows/release.yml`；`Makefile` 的 `release` 目标生成 `SHA256SUMS`，并向托盘注入 Linux 二进制摘要。
- `internal/tunnel`：把校验下载与镜像回退抽成可复用的通用下载器，cloudflared 与托盘共用。
- `cmd/webmux/main.go`、`cmd/webmux/local.go`：隧道从一次性启动改为可重复启停的管理器；`api.PublicOrigin` 始终存在。
- `internal/api/local.go`、`internal/contract`、`internal/localclient`：新路由、状态字段、客户端方法；`docs/protocol.md` 的控制面契约同步。
- `internal/tray`（`service.go`、`supervisor.go`、`wsl.go`、`winui/*`）：安装动作、版本比较、失败恢复、隧道开关与菜单。
- 安全：一键开放公网增大误操作风险，因此要求确认、强 Token、不持久化；安装只执行摘要匹配的文件。
