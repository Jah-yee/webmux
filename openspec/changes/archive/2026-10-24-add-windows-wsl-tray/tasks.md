## 0. 前置

- [x] 0.1 确认 `add-cli-server-control`（PR #83）已实现并合入（本机 API `/local/v1/*`、`bridge`、`serve --log-file`、`status --json` v1、`stop --json`、`config init`、`WEBMUX_LAUNCHER`、`internal/contract`、`internal/localclient`）。验证：Linux 上 `webmux status --json` 输出 `"v":1`，`GOOS=windows go build ./internal/contract ./internal/localclient` 成功

## 1. 平台无关核心（`internal/tray`）

- [x] 1.1 新增 `settings.go`：设置结构（发行版、启动方式 `auto/tray/systemd`、`webmux` 路径、配置路径、日志路径、托盘启动时自动启动、自动重启）、默认值、JSON 读写与校验；损坏时返回默认值与错误且不回写。验证：`go test ./internal/tray -run Settings` 覆盖缺失文件、损坏 JSON、未知字段、非法启动方式、不含 Token 字段
- [x] 1.2 新增 `wsl.go`：`Runner` 接口与设计表中每种调用的 argv 构造（`-d`、`--cd ~`、`~/x` → `./x` 改写、`WEBMUX_LAUNCHER`/`WSLENV` 合并）；`--list` 解析同时支持 UTF-16LE（含 BOM）与 UTF-8。验证：单元测试断言含空格、`;`、`$()`、引号的路径只作为独立 argv 元素出现，`WSLENV` 已有值时正确追加，中文发行版名解析正确
- [x] 1.3 新增 `mode.go`：`auto` 判定（`/proc/1/comm`、`systemctl --user cat`）、显式 systemd 条件不满足时报错、外部实例判定（`record`、`launcher` 与方式对应）。验证：注入假 `Runner` 的表驱动测试
- [x] 1.4 新增（或复用 PR #84 已建立的）`internal/wslclient` 桥接拨号器（`bridge.go`，平台无关部分）：把子进程 stdio 包装为 `net.Conn`、退出码 3 且无输出 → `ErrNotRunning`、`Close` 超时结束进程；`internal/localclient` 使用它访问 `/local/v1/*` 与 `/ws/sessions`。验证：在 Linux 上以 `webmux bridge` 子进程（替代 `wsl.exe`）对真实服务完成状态请求、会话订阅与停止；服务未运行时得到 `ErrNotRunning`
- [x] 1.5 新增 `supervisor.go`：状态机（Stopped/Starting/Running/Unreachable/External/Stopping/Restarting/Failed）、60 秒就绪、10 秒 Windows 可达性、`/ws/sessions` 订阅驱动会话数（断开即刷新并退避重连）与 60 秒 `/local/v1/status` 刷新、停止先走 `/local/v1/shutdown` 后退回 `webmux stop`、连续 3 次无响应转异常、托盘托管方式退避 2s→60s 且 10 分钟内 ≤5 次、systemd 方式不自行重启、用户停止不重启、版本过旧/可执行文件缺失不重试、托盘重启后按启动方恢复控制、进程退出与状态事件去重。验证：注入假 `Runner`/状态/可达性/时钟的单元测试逐条覆盖 `wsl-service-lifecycle` 场景，`go test -race` 通过
- [x] 1.6 新增 `internal/tray/forward`：每地址监听、`httputil.ReverseProxy` 指向本机链接、Host 必须等于监听 IP:端口、Origin 为空或同源、改写 Host/Origin、删除 `X-Forwarded-*`、不记录 URL 查询串与内容、并发上限 64、非 WebSocket 10 分钟空闲关闭、按启用条件暂停/恢复。验证：以真实 `api.New`（配置 Token、回环监听）为后端的测试覆盖：经代理的 REST 与 `/ws/s/{id}` 终端读写成功、错误 Token 得到 401、异源 Origin 与非监听 Host 得到 403 且后端未收到请求、`?token=` 不出现在日志中、超出并发时新连接被关闭
- [x] 1.7 转发适用判定：网络模式（`wslinfo --networking-mode`，失败按 NAT）、状态报告的远程链接、Token、Windows 可达性 → 推荐与条目可用性；Windows 网卡过滤（回环、链路本地、`vEthernet (WSL*)`）与 Tailscale 置顶。验证：表驱动单元测试覆盖 `windows-tray-forwarding` 的适用判定与启用条件场景
- [x] 1.8 托盘日志写入器：1 MiB 轮转保留 1 份，对已知 Token 做字面替换兜底。验证：单元测试写入含 Token 的消息后文件中不出现 Token

## 2. Windows 适配层（`internal/tray/winui`，`//go:build windows`）

- [x] 2.1 引入 `fyne.io/systray`，确认 `CGO_ENABLED=0 GOOS=windows` 可构建，且 `go version -m bin/webmux` 的依赖集不变。验证：`go mod tidy` 无额外变更，两条构建命令成功
- [x] 2.2 `process.go`：基于 `os/exec` 的 `Runner`，所有调用 `CREATE_NO_WINDOW` 与 `WSL_UTF8=1`；长生命周期进程 detached、标准流为 NUL、不入 Job Object；systemd 保活进程 PID+创建时间记录与校验。验证：`GOOS=windows go vet` 通过；手动验收中无控制台闪烁、仅退出托盘后进程仍在
- [x] 2.3 `tray.go`/`menu.go`：状态图标（4 个 `.ico`，`go:embed`）、悬停提示、左键打开、右键菜单分组与启用规则、发行版与启动方式子菜单、远程链接子菜单与警告项、打开日志子菜单；订阅快照刷新且菜单弹出不阻塞。验证：手动验收 4.2 中菜单相关条目
- [x] 2.4 `notify.go`/`dialog.go`：气泡通知（复制反馈、意外退出、单实例唤起、systemd 单元失败），`MessageBoxW` 确认停止/重启/切换（含会话数）与退出三选一。验证：手动验收 4.2
- [x] 2.5 `clipboard.go`：`CF_UNICODETEXT` + 排除历史/云剪贴板格式；Token 仅在点击时经桥接 `/local/v1/token` 获取、写入后立即丢弃，60 秒后按剪贴板序号条件清除。验证：手动确认 Win+V 中不出现 Token，期间复制其他文本后不被清除
- [x] 2.6 `autostart.go`：HKCU `Run` 读写、指向路径比较（大小写不敏感、规范化）。验证：手动注销重登后自动出现；移动 exe 后复选项显示未勾选
- [x] 2.7 `singleinstance.go`：`Local\WebMuxTray-<SID>` 互斥量与唤起消息。验证：重复启动只保留一个图标且已有实例弹出状态通知
- [x] 2.8 “远程访问”子菜单（推荐说明、可勾选地址、失败原因、经托盘转发的远程链接）、勾选地址持久化与托盘启动后恢复、退出对话框转发提示；“初始化配置（生成 Token）”调用 `webmux config init` 并提示重启。验证：手动验收 4.2 中转发与初始化条目
- [x] 2.9 Windows 侧可达性检查（`Proxy: nil`、3 秒、不带 Token、200/401 视为可达）、设置文件 `%APPDATA%\WebMux\tray.json` 与 2 秒热加载、“编辑托盘设置”“打开 WebMux 配置文件”“打开日志”（托盘托管为 `\\wsl.localhost` 下的日志文件，systemd 为 journal 导出）。验证：手动修改设置后无需重启托盘即生效；关闭 localhostForwarding 时状态行给出诊断

## 3. 入口、构建与 CI

- [x] 3.1 新增 `cmd/webmux-tray/main_windows.go`（显式依赖组装、`version`）与 `main_other.go`（非 Windows 打印不支持并退出 1）。验证：Linux 上 `go build ./...`、`go vet ./...`、`go test -race ./...` 通过
- [x] 3.2 用 `go-winres` 生成并提交 exe 图标/版本资源 `.syso`。验证：资源管理器中 exe 显示图标与版本
- [x] 3.3 `Makefile` 新增 `tray`（amd64/arm64、`-H=windowsgui`、`CGO_ENABLED=0`、`-X main.version`），`release` 依赖 `tray`。验证：`make tray` 产出两个 exe，`make format-check` 通过
- [x] 3.4 `.github/workflows/test.yml` 的 `go` 作业追加 Windows 交叉编译与 `GOOS=windows go vet ./cmd/webmux-tray ./internal/tray/...`。验证：PR 上 CI 绿色
- [x] 3.5 `docs/deployment.md` 新增“Windows（WSL）”一节：前置条件（WSL 2、Store 版 WSL）、在发行版内安装 `webmux` 与 `config init`、放置托盘与登录启动、两种启动方式及与现有 systemd 单元的关系、localhost 转发诊断、远程访问的三种途径（mirrored 模式 / WSL 内 Tailscale / 托盘转发）与防火墙提示、Token 剪贴板说明。验证：`make format-check` 通过

## 4. 验收

- [x] 4.1 运行 `make format-check`、`go vet ./...`、`go test -race ./...`、`make tray`，全部通过
- [ ] 4.2 在真实 Windows 11 + WSL 2（NAT 与 mirrored 各一次）手动验收：首次运行默认设置启动、无控制台窗口、单实例、菜单启用规则与不阻塞、左键打开、复制本机/远程链接、复制 Token 不入 Win+V 且 60 秒清除、有会话时停止/重启确认、停止超时强制结束、托盘托管下 `wsl --shutdown` 后通知与自动重启、连续崩溃 5 次后停止重试、两种方式下仅退出托盘后会话保持 5 分钟且重开托盘恢复控制、`auto` 在启用/未启用 systemd 的发行版上分别选对、手动启动的实例识别为外部、端口被占用提示、旧版 `webmux` 提示升级、关闭 localhostForwarding 时的诊断、登录自动启动与 WSL 冷启动、切换发行版/启动方式确认、配置不存在时初始化并重启后出现“复制 Token”、NAT 模式下勾选 Tailscale 与局域网地址后手机经转发使用终端（含 WebSocket 输入输出）、mirrored 模式下提示无需转发、未设 Token 时转发禁用、WebMux 停止后转发暂停并在重启后恢复、退出托盘后转发停止、托盘日志不含 Token
  - 2026-10-24 部分验收（Windows 11 26300 + WSL 2 Debian，mirrored 网络，启用 systemd；证据见 `mzdluo123/webmux@63881974` 的 `windows/`）通过：首次运行默认启动、无控制台窗口、单实例、菜单启用规则与不阻塞、左键打开、复制本机链接、复制 Token 不入 Win+V 且约 60 秒清除、有会话时停止/重启确认、停止超时强制结束、连续崩溃 5 次后停止重试、托盘托管下仅退出托盘会话保持 5 分钟并重开恢复控制、`auto` 在启用 systemd 的发行版上选对、外部实例识别、端口占用提示、切换发行版确认、初始化配置并重启后出现“复制 Token”、mirrored 提示无需转发、托盘日志不含 Token。
  - 未验收：NAT 模式与转发相关项、`wsl --shutdown` 后自动重启、关闭 localhostForwarding 的诊断（均需重启 WSL）；未启用 systemd 的发行版（仅有一个发行版）；登录自动启动与 WSL 冷启动；旧版升级提示、切换启动方式确认、复制远程链接；systemd 方式下仅退出托盘的 5 分钟保持（3 分 50 秒时会话仍在，验收被中止）。
  - 观察：一次由托盘发起的启动先显示“运行中（外部实例）”约 19 秒才转为托盘托管，之后 3 次重启未复现。
- [ ] 4.3 按 AGENTS.md 的 UI 证据要求，在 PR 描述中附托盘图标各状态与菜单（浅色/深色任务栏）截图
  - 2026-10-24：深色任务栏下四种图标状态与运行中、启动中、失败、外部实例、端口占用、远程访问菜单截图已提供（同上证据）；浅色任务栏截图未验收。
