## Why

WebMux 只支持 Linux/macOS，Windows 用户目前要在 WSL 里手动启动 `webmux`，再自己记住地址和 Token；WSL 发行版没有前台进程时会被回收，`systemd --user` 服务也不一定启用，导致“开机即用、浏览器断开不丢会话”的体验在 Windows 上不成立。提供一个 Windows 托盘程序，在用户登录后把 WebMux 拉起在 WSL 中、保持其存活，并让链接信息和常用管理操作在任务栏一键可达。

## What Changes

**前置依赖：`add-cli-server-control`（PR #83）。** 该变更确立了客户端/服务端通讯方案：所有客户端只用现有 HTTP/WebSocket 协议；本机客户端经 Unix 套接字访问（按系统用户认证、免 Token，另有 `/local/v1/status|shutdown|token`），Windows 客户端经 `wsl.exe --exec webmux bridge` 把该套接字桥接到标准输入输出。托盘因此：状态、停止、取 Token 与实时会话变化都经桥接访问本机 API；只有启动（`serve`）、配置初始化（`config init`）与服务未运行时的状态（`status --json`）仍执行子命令；不解析 WebMux 配置、不自行管理 PID、不在 Windows 侧持有 Token；只在 WSL 网络特殊情况下经 TCP 中转浏览器流量。

- 新增 Windows 托盘程序 `webmux-tray.exe`（Windows amd64/arm64，同仓库同 Go module，独立于服务端二进制）：
  - 单实例；通知区域图标区分已停止 / 启动中 / 运行中 / 异常；悬停提示显示状态摘要。
  - 菜单：状态行、打开 WebMux、复制本机链接、复制远程链接、复制 Token、启动 / 停止 / 重启、打开日志、打开 WebMux 配置文件、发行版、启动方式、编辑托盘设置、登录时自动启动、退出。
- 两种托管方式，可在菜单中切换（默认“自动”）：
  - **托盘托管**：托盘以 `wsl.exe` 在发行版中运行 `webmux serve --log-file ...`，该进程同时让发行版保持存活；托盘退出后仍继续运行。
  - **systemd 用户服务**：托盘通过 `systemctl --user` 启动 / 停止 / 重启已安装的 `webmux` 单元，并另起一个后台保活进程，日志来自 journal。
  - “自动”：检测到 WSL 启用了 systemd 且已安装 `webmux` 用户单元时用 systemd，否则用托盘托管。
- 生命周期：启动后等待就绪（60 秒）；识别不归当前方式管理的外部实例只做监控；停止 / 重启在有会话时确认（会结束所有会话）；托盘托管方式下意外退出（含 `wsl --shutdown`）按退避自动重启并通知，systemd 方式下重启交给 systemd。
- 登录自动启动：在当前用户登录启动项（HKCU `Run`）中注册 / 注销托盘，菜单可切换；托盘启动时按设置自动启动 WebMux。
- 链接信息：本机链接与远程链接来自本机 API 的状态，托盘另从 Windows 侧经 TCP 检查本机链接是否可达并给出 WSL 网络设置诊断。链接不含 Token；“复制 Token”仅在点击时经桥接获取，写入剪贴板时排除剪贴板历史与云同步，60 秒后自动清除。
- WSL 网络特殊情况下的转发：WSL 处于 NAT 模式、局域网或 Tailscale 设备无法直达 WSL 时，托盘可在用户勾选的 Windows 主机地址（Tailscale 置顶）上提供转发入口；要求已配置 Token，校验 Host 与同源 Origin 后透明中转 HTTP/WebSocket，认证仍由 WebMux 完成。镜像网络模式或 WSL 内已有可用远程地址时提示无需转发。默认关闭。
- 配置：菜单可打开 WebMux 配置文件；配置文件不存在时提供“初始化配置（生成 Token）”（调用 `webmux config init`）；配置修改后提示重启生效。
- 退出托盘时询问：仅退出托盘 / 停止 WebMux 并退出 / 取消（说明转发会随托盘退出而停止）。
- 交付：`make release` 额外产出 `webmux-tray-windows-amd64.exe` 与 `webmux-tray-windows-arm64.exe`；不做安装器、签名与自动更新。
- 服务端与前端：除前置变更外无修改；不支持带 Token 的链接。
- 不在本变更内：在终端中启动 agent 并纳入服务端管理（#67，走数据面，另立变更）、Windows 控制台客户端。

## Capabilities

### New Capabilities

- `windows-tray-host`: 托盘程序本身——运行形态、单实例、图标与悬停状态、菜单结构与可用性、托盘设置、发行版与启动方式选择、WSL 中 `webmux` 版本要求、登录自动启动、托盘日志、退出行为。
- `wsl-service-lifecycle`: 托盘如何在 WSL 中启动、保活、监控、停止、重启 WebMux——两种托管方式与自动选择、就绪判定、外部实例、会话确认、意外退出处理。
- `windows-tray-access`: 链接信息——本机/远程链接的来源与 Windows 侧可达性诊断、打开浏览器、复制链接与 Token 及其安全约束。
- `windows-tray-forwarding`: WSL 网络特殊情况下的转发——适用判定与推荐、启用条件、监听范围、Host/Origin 安全边界、资源与生命周期。

### Modified Capabilities

当前 `openspec/specs/` 无已登记主规格。本变更依赖 `add-cli-server-control` 新增的 `cli-server-control`、`cli-config` 与 `server-runtime-record`，只消费其 v1 契约，不改变其要求。

## Impact

- 新增代码：`cmd/webmux-tray/`（Windows 入口与非 Windows 占位入口）、`internal/tray/`（平台无关：设置、`wsl.exe` argv 构造、发行版列表解析、启动方式选择、状态机、退避）、`internal/tray/forward/`（平台无关：转发的 Host/Origin 校验与 HTTP/WebSocket 中转）、`internal/tray/winui/`（仅 Windows：图标、菜单、通知、对话框、剪贴板、登录启动项、进程创建、单实例、网卡枚举）。
- 复用：`internal/contract` 的契约类型与健康分类、`internal/localclient` 的 HTTP/WebSocket 客户端；与 Windows 控制台客户端（PR #84）共用 `internal/wslclient`（目标发行版解析、桥接拨号器）。
- 新增 Go 依赖（仅 Windows 构建）：`fyne.io/systray`；`golang.org/x/sys/windows` 属于已有的 `golang.org/x/sys` 模块。保持 `CGO_ENABLED=0`，服务端二进制依赖集不变。
- 构建与 CI：`Makefile` 增加 `tray` 并纳入 `release`；`.github/workflows/test.yml` 增加 Windows 交叉编译与 vet；`go build/vet/test ./...` 在 Linux/macOS 上继续通过。
- 文档：`docs/deployment.md` 增加“Windows（WSL）”一节。
- 不涉及：`internal/session`、`internal/api`、`internal/hub`、`web/` 与 WebSocket 协议。
