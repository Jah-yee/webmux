# Tasks

## 1. 通用校验下载器

- [x] 1.1 新建 `internal/fetch`：从 `internal/tunnel/locate.go` 抽出镜像回退、头部/空闲超时、大小上限、SHA-256 校验、可选解包与原子重命名，接口接收 GitHub 路径、期望摘要、镜像列表与目标路径
- [x] 1.2 `tunnel.Locator` 改用 `internal/fetch`，现有 `locate_test.go` 全部通过且不改断言；确认 `GOOS=windows` 可编译 `internal/fetch`

## 2. 服务端运行时隧道

- [x] 2.1 `contract` 新增 `TunnelStatus` 与 `StatusV1.Tunnel`（`omitempty`），`PathTunnel` 常量；`localclient` 新增 `SetTunnel`
- [x] 2.2 `cmd/webmux` 实现 `tunnelController`（代号丢弃过期事件、Token ≥43 校验、Stop 先撤销授权再结束 cloudflared、意外退出转 `failed`、服务退出时停止）；`api.PublicOrigin` 改为始终创建
- [x] 2.3 启动 `--tunnel` 路径改由控制器接管，保留定位失败拒绝启动、含 Token 链接与二维码的输出；运行时开启只打印不含 Token 的公网地址
- [x] 2.4 `api.Local` 增加隧道控制回调与状态填充；新增 `POST /local/v1/tunnel`（400 无效请求体、403 会话内进程、409 Token 不足、202 成功），TCP 上 404
- [x] 2.5 测试：用现有 `fakeTunnel` 脚本覆盖运行时开启→就绪→关闭后公网 403、重复开启不起第二个进程、cloudflared 意外退出转 `failed` 且撤销授权、Token 不足 409、会话内进程 403、`--tunnel` 启动后可关闭再开启
- [x] 2.6 `docs/protocol.md` 控制面契约补充 `/local/v1/tunnel` 与 `tunnel` 字段

## 3. 托盘安装与恢复

- [x] 3.1 `Snapshot` 以显式 `Blocker`（`missing`、`incompatible`、`noWSL`）替换 `fatal()` 字符串匹配；`Check` 追加 `webmux version` 解析，产生 `WebmuxVersion` 与 `UpgradeAvailable`（仅比较 `MAJOR.MINOR.PATCH`，`dev` 等视为可用）
- [x] 3.2 Supervisor 新增 `cmdRecheck`：阻塞状态下重新 `Check`，通过即回到 `Stopped` 并刷新状态；`onMenuOpen` 与安装成功后发送；补回归测试“缺失 → 安装 → 打开菜单 → 启动可用”与“仍缺失时状态不变且不启动”
- [x] 3.3 `Target` 新增 `Arch()`（`uname -m`）、`Install(src)`（`install -D -m 0755 <src> <目标>`）调用；argv 测试覆盖含空格与分号的目标路径且不含 shell
- [x] 3.4 `internal/tray` 新增安装器：内置摘要变量（ldflags 注入）、Release 地址、下载到 `%LOCALAPPDATA%\WebMux\downloads`、`Wslpath` 换算、安装后 `version` 核对；摘要为空时报告“此版本托盘不支持安装 webmux”；单元测试覆盖摘要不符不安装、全部来源失败保留原文件、架构不支持
- [x] 3.5 `Service`/`TargetService`/`fakeService` 增加 `Install`；菜单增加“安装 / 升级 WSL 中的 webmux”与较旧时的状态行提示；运行中升级后提示重启生效，仅托盘管理实例提供重启

## 4. 托盘隧道开关

- [x] 4.1 `Service` 增加 `SetTunnel`；Supervisor 在隧道 `starting` 时以 `StartingInterval` 轮询，`starting/ready → failed` 时通知一次；自动重启前隧道开启则通知附加“公网隧道已关闭”；补对应 supervisor 测试
- [x] 4.2 菜单：仅在状态含 `tunnel` 且实例健康时显示“公网隧道（Cloudflare）”勾选项、状态/地址行与“复制公网链接”（带 Token 片段，同“复制 Token”的剪贴板保护）；开启前 `messageBox` 风险确认，取消不发请求；409 时显示原因并在无配置时提供“初始化配置”
- [x] 4.3 `GOOS=windows` 编译 `cmd/webmux-tray`、`go vet`，确认菜单逻辑相关的纯函数有测试

## 5. 发布流水线

- [x] 5.1 `Makefile` `release`：先构建 Linux 二进制并计算摘要，以 ldflags 注入托盘构建，生成 `SHA256SUMS`；校验两个托盘 `.exe` 包含两个摘要字符串
- [x] 5.2 新增 `.github/workflows/release.yml`：`v*` 标签触发，`go test ./...` → `make release VERSION=${GITHUB_REF_NAME#v}` → `gh release create` 上传全部产物；任一步失败不创建 Release
- [x] 5.3 本地执行 `make release VERSION=0.0.0-test`，用 `sha256sum -c SHA256SUMS` 验证并检查托盘摘要注入

## 6. 端到端验证

- [x] 6.1 运行 `go test -race ./...`、`make check`
- [ ] 6.2 在本机 WSL 用托盘实测：删除 `~/.local/bin/webmux` 后安装（下载源指向本地构建的 Release 产物或测试 Release）、运行中升级提示、隧道开启/复制链接/关闭/杀死 cloudflared 后的通知
  - 2026-10-24 部分验收（证据见 `mzdluo123/webmux@63881974` 的 `windows/`）：删除 `~/.local/bin/webmux` 后托盘提示安装；无缓存时下载的 GitHub v0.2.0 因摘要与本地构建不符被三个源拒绝且未安装；把本地 Release 构建放入托盘下载缓存后成功安装 0.2.0 并显示“对应”（设计未提供可配置的本地下载源）。
  - 未验收：运行中升级提示，隧道开启/复制链接/关闭/杀死 cloudflared 后的通知（验收中止）。
