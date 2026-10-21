# Design

## Context

- 托盘对 WSL 只用 argv 调用（`wsl.exe --exec`，`core_test.go` 禁止 `sh`/`bash`/`-c`），`Runner`/`Invocation` 没有标准输入；现有一次性 WSL 动作的范例是“初始化配置”链路：菜单 → `App.initConfig` → `TargetService.ConfigInit` → `WSLService.ConfigInit` → `webmux config init`。
- `WSLService.Check` 以 `webmux -h` 是否含 `-log-file` 判断兼容，不读取版本号；`Snapshot.fatal()` 通过匹配 `Reason` 字符串禁用“启动”，且失败状态只在用户点击“启动”时才会离开（`supervisor.go` 767 行），而“启动”已被禁用，形成死锁。
- `internal/tunnel/locate.go` 已有带镜像回退、头部/空闲超时、大小上限、SHA-256 校验和原子重命名的下载实现，但路径与缓存目录写死为 cloudflared。
- 服务端隧道只在 `serve()` 启动时建立：`api.PublicOrigin` 仅在启用时创建（否则为 nil），`watchTunnel` 只运行一次；Token 在 `api.New`、`NewLocal` 中按值复制，运行中无法更换。
- 仓库没有发布流水线；README 已以 `mzdluo123/webmux` 的 `releases/latest/download/...` 与 `SHA256SUMS` 作为安装说明。

## Goals / Non-Goals

**Goals:**

- 托盘内一次点击完成 WSL 端安装或升级，下载内容由托盘内置摘要把关。
- 运行时启停隧道不触碰会话、TCP 监听与 Token。
- 解除失败状态死锁，且不引入定时重试。

**Non-Goals:**

- 不新增 `webmux tunnel` CLI 子命令（本机 API 已可用 `curl --unix-socket` 调用；需要时另行提出）。
- 不支持运行中更换 Token，也不在运行中修改 `[tunnel]` 配置（Named Tunnel 凭据与主机名仍只在启动时读取）。
- 不做托盘自身的自动更新；不安装 systemd 单元。
- 不支持 Windows 原生运行服务端。

## Decisions

### 1. 在 Windows 侧下载，经挂载路径用 `install` 安装

托盘把文件下载到 `%LOCALAPPDATA%\WebMux\downloads\webmux-<版本>-linux-<架构>`，校验后用现有 `Wslpath` 调用换算为 `/mnt/<盘符>/...`，再执行 `wsl.exe [-d D] --cd ~ --exec install -D -m 0755 <源> <目标>`。`install` 先删除再创建目标，正在运行的旧进程不受影响（不会 `ETXTBSY`）；`-D` 创建父目录。目标沿用 `WebmuxPath`（`~/x` → `./x` 配合 `--cd ~`）。安装后执行 `<目标> version` 确认输出 `webmux <托盘版本>`。

备选：给 `Runner` 增加标准输入，`wsl.exe --exec tee <目标>` 写入——需要另做权限与原子性处理，且 `tee` 写半截文件；在 WSL 内用 `curl` 下载——依赖发行版工具且摘要校验需要再引入 shell。挂载路径方案只依赖 coreutils/busybox 的 `install`。代价：关闭了 automount 的发行版无法安装，报告“WSL 无法访问 Windows 文件（automount 已关闭？）”。

### 2. 摘要在发布构建时注入托盘

`Makefile` 的 `release` 先构建两个 Linux 二进制，计算 SHA-256，再以 `-X github.com/rainchan/webmux/internal/tray.linuxAMD64SHA256=...`（arm64 同理）构建托盘，最后写出 `SHA256SUMS`。托盘只信任内置摘要；摘要为空即开发构建，不提供安装。备选“下载 `SHA256SUMS` 再校验”只能防传输损坏，防不了来源（镜像）篡改，因此不采用。

Release 下载地址为 `https://github.com/mzdluo123/webmux/releases/download/v<托盘版本>/webmux-linux-<架构>`，仓库名作为常量放在托盘包内，可用 ldflags 覆盖。

### 3. 通用校验下载器

把 `locate.go` 中的 `download`/`fetch`/`idleReader`/`sourceHost`/`fileSHA256` 抽到新包 `internal/fetch`：输入 GitHub 路径、期望摘要、可选解包函数、镜像列表、目标路径，输出成功来源或聚合错误。cloudflared 定位改为调用它，行为与现有测试保持不变；托盘复用同一默认镜像（`ghfast.top`、`gh-proxy.com`）与超时。该包不依赖平台特定代码，可在 Windows 编译。

### 4. 版本判定与状态模型

`Check` 在兼容探测之后执行 `webmux version` 解析版本。版本比较只接受 `MAJOR.MINOR.PATCH`；任一方无法解析（`dev` 等）即视为可用。结果作为新字段 `Snapshot.WebmuxVersion` / `UpgradeAvailable` 暴露，较旧不是失败状态。缺失/不兼容改用显式的 `Snapshot.Blocker`（枚举：`missing`、`incompatible`、`noWSL`）取代 `fatal()` 的字符串匹配。

### 5. 失败状态恢复

新增命令 `cmdRecheck`：当 `Blocker` 为 `missing`/`incompatible` 时重新执行 `Check`，通过则清空 `Blocker` 与 `Reason`，状态改为 `Stopped` 并立即刷新状态（可能发现运行中的外部实例）。菜单打开时（`onMenuOpen`）与安装成功后发送该命令；不加入定时器，保留“不反复检测不兼容 webmux”的现有测试。

### 6. 服务端隧道控制器

在 `cmd/webmux` 新增 `tunnelController`（单一互斥锁保护的状态机，`off → starting → ready`，任一时刻可到 `failed`/`off`），持有启动时确定的 `TunnelConfig`、本地地址、Token 与始终非 nil 的 `*api.PublicOrigin`（`hostAllowed` 在未 `Set` 时本就返回 false，因此常驻对象不改变未启用时的行为）。

- `Start`：Token 校验（`len >= minTunnelTokenLength`，否则返回 409 错误）；已在 `starting/ready` 则直接返回；否则递增代号，启动 goroutine：`Locator.Find` → `tunnel.Start` → 等待 `Ready`/`Done`。事件带代号，过期事件丢弃，避免停止后旧 cloudflared 的 Ready 重新授权。
- `Stop`：先 `public.Clear()`，再 `Close()` 并等待，状态 `off`。
- 意外退出：`Clear()`，状态 `failed`，不重试。
- 服务退出：先 `Stop`，再执行既有的会话清理顺序。
- 启动时 `--tunnel`：保持“cloudflared 定位失败即拒绝启动”与打印含 Token 链接、二维码的现有行为，随后由控制器接管同一进程；之后可被停止或重开。运行时开启只打印不含 Token 的公网地址。

控制器在 `startRuntime` 注入 `Local`，新增 `Local.Tunnel(enabled bool) (contract.TunnelStatus, error)` 与状态字段填充；`/local/v1/tunnel` 复用 `fromSession` 拒绝会话内进程。

### 7. 状态契约

`contract.StatusV1` 新增 `Tunnel *TunnelStatus \`json:"tunnel,omitempty"\``，`TunnelStatus{State, PublicURL, Error, Named}`。旧托盘忽略未知字段；新托盘以字段是否存在判断服务端是否支持，不需要提升契约版本。`localclient` 新增 `SetTunnel(ctx, enabled)`。

### 8. 托盘隧道交互

- `Service` 新增 `SetTunnel`；`TargetService`、`fakeService` 同步实现。
- `Supervisor` 在隧道 `starting` 期间把状态轮询间隔降到 `StartingInterval`（2 秒），就绪或失败后恢复 60 秒；比较前后两次 `Tunnel.State`，从 `starting/ready` 变为 `failed` 时通知一次。
- 自动重启（`unexpectedExit`）前若隧道为 `starting/ready`，通知文案附加“公网隧道已关闭”。
- 菜单：勾选项 + 状态/地址行 + “复制公网链接”；勾选时经 `winui/dialog.go` 的 `messageBox` 确认（与 `askQuit` 同一模式）。

### 9. 发布流水线

`.github/workflows/release.yml`：`on: push: tags: ['v*']`，单个 Ubuntu 作业：安装 Go/Node/pnpm → `go test ./...` → `make release VERSION=${GITHUB_REF_NAME#v}`（含 `SHA256SUMS`）→ `gh release create` 上传全部文件。任何步骤失败即终止，Release 只在最后一步创建，满足“失败不产生部分 Release”。

## Risks / Trade-offs

- [镜像是第三方服务] → 只影响可用性，内容由内置摘要把关；与 cloudflared 下载一致，日志记录来源。
- [automount 关闭或盘符映射被修改] → 明确报错并提示手动安装命令；不退回 shell 方案。
- [运行中一键开放公网] → 每次开启都确认；强制 ≥43 字符 Token；会话内进程无法开启；状态不持久化；托盘复制的链接带 Token 片段，但与“复制 Token”一样排除剪贴板历史并 60 秒后清除，菜单与通知不显示 Token。
- [Token 只能重启后变更] → 未满足条件时给出 `webmux config init` 或修改配置后重启的引导，不做运行时换 Token，避免已连接客户端被突然拒绝。
- [隧道状态依赖轮询] → `starting` 期间 2 秒轮询、菜单打开时立即刷新；失败通知最长延迟 60 秒，可接受。
- [Release 摘要注入出错导致托盘始终校验失败] → `Makefile` 用同一变量写入托盘 ldflags 与 `SHA256SUMS`；发布作业上传前检查两个托盘 `.exe` 都包含这两个摘要字符串，否则失败。

## Migration Plan

1. 先合入服务端隧道控制与契约字段（旧托盘忽略新字段）。
2. 合入下载器重构（cloudflared 行为不变，既有测试覆盖）。
3. 合入托盘改动与发布流水线；推送首个标签生成 Release。旧版本服务端在托盘中显示“可升级”。
回滚：删除标签对应的 Release 即可阻止新托盘安装；服务端新增字段与路由可独立回退。
