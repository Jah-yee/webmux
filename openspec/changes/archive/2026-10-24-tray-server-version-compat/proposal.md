# Proposal

## Why

Windows 托盘与 WSL 中的 `webmux` 是分别安装、分别升级的两个程序，用户很容易让两边停在不同版本：只更新了托盘、在 WSL 里手动换了二进制，或升级后没有重启正在运行的实例。目前托盘只在“缺少本机 API”时判为不兼容、在“已安装文件版本低于托盘”时于状态行提示可升级；WSL 中版本更高（包括控制契约更高）、正在运行的实例与已安装文件版本不同，这些情况都不会被发现，出问题时用户只能看到含糊的失败。托盘需要在启动时明确判断两边是否兼容、版本是否对应，并告诉用户该更新哪一边。

## What Changes

- `webmux version` 新增 `--json`，输出版本号与所支持的控制契约版本；不带参数时的输出保持不变。
- 托盘在启动、切换发行版或 `webmux` 路径、安装/升级之后检查 WSL 中的 `webmux`，归为：对应、WSL 较旧、WSL 较新、不兼容（WSL 过旧 / WSL 过新）、缺失、无法判断。版本对应只比较版本号。
- 兼容以控制契约版本为准：托盘声明自己支持的契约版本，WSL 中的 `webmux` 报告自己的契约版本。不支持 `--json` 的旧版本沿用现有的本机 API 探测。
- 结果只显示在菜单状态行，不发系统通知、不弹窗。
- 契约不兼容时仍允许启动与管理 WebMux，状态行注明“不兼容，功能受限”，并按方向提供更新入口：WSL 较旧 → “升级 WSL 中的 webmux”；WSL 较新 → “下载托盘 <版本>”（打开对应 Release 页面）。缺少本机 API 的旧版本仍无法由托盘启动。
- 正在运行的实例与已安装文件版本不同时，提示“重启 WebMux 后生效”；仅托盘管理的实例提供重启，不自动重启。

## Capabilities

### New Capabilities

- `windows-tray-server-compat`: 托盘对 WSL 中 `webmux`（已安装文件与正在运行的实例）的兼容与版本对应判定、检查时机、状态行展示、不兼容时的降级运行与更新入口。

### Modified Capabilities

- `cli-server-control`: 新增 `webmux version --json` 的机器可读版本信息（含契约版本）。

## Impact

- `cmd/webmux`（`version --json`）、`internal/contract`（版本信息结构）。
- `internal/tray`：`Check` 改为优先解析 `version --json`；新增分类、运行实例比较、不兼容实例以可达性维持运行；`Snapshot`、`supervisor.go`、`menuview.go` 与 `winui` 的状态行和菜单入口。
- 与 `add-tray-wsl-install-and-runtime-tunnel` 中“WSL 中 webmux 的分类”一致并细化：“版本高于托盘视为可用”保留，新增状态行提示、契约判定与下载托盘入口。
- 不涉及 Windows 控制台客户端 `webmux.exe`、托盘自更新、系统通知与前端。
