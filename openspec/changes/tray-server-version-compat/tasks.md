# Tasks

## 1. 服务端版本信息

- [x] 1.1 `internal/contract` 新增 `VersionV1{V, Version, Contract}`；`cmd/webmux` 的 `version` 支持 `--json` 输出它，无参数与 `--version` 输出不变，未知参数退出码 2。验证：`cmd/webmux` 测试断言 JSON 字段、`contract` 等于 `contract.V`、不读取配置、文本输出仍为 `webmux <版本>`；`make build` 后执行 `./bin/webmux version --json` 确认输出
- [x] 1.2 `docs/protocol.md` 控制面契约中补充 `version --json`。验证：`make format-check` 通过，文档字段与 1.1 的 JSON 标签一致

## 2. 托盘兼容与对应判定

- [x] 2.1 `internal/tray` 新增 `BuildInfo{Version, Contract}` 与纯函数 `Classify(tray, server BuildInfo) Compat`（`matched`、`server-older`、`server-newer`、`incompatible-older`、`incompatible-newer`、`unknown`）。验证：表驱动测试覆盖 `windows-tray-server-compat` 中“兼容判定”“版本对应判定”的全部场景，包括 `dev`、本地构建同为 0.1.0、契约过高/过低
- [x] 2.2 `WSLService.Check` 在探测通过后执行 `version --json`，解析失败时回退 `ParseVersion` 文本并视契约为 1，返回 `BuildInfo`；`Target` 的调用保持 argv 无 shell。验证：`core_test.go` 的 argv 检查覆盖新调用；`service_test.go` 用假 Runner 覆盖 JSON、旧文本、无法执行三种输出
- [x] 2.3 `Status` 在本机 API 与 `status --json` 两条路径上都把不受支持的 `v` 返回为 `ErrContractUnsupported`（携带服务端版本）。验证：服务测试中两条路径返回 `v: 2` 时都得到该错误

## 3. 托盘状态与菜单

- [x] 3.1 `Snapshot` 以 `Installed BuildInfo`、`Running *BuildInfo`、`Compat`、`RunningCompat` 取代 `WebmuxVersion` / `UpgradeTo`；`BlockerIncompatible` 只表示缺少本机 API；更新所有调用方与既有测试。验证：`supervisor_test.go` 覆盖契约不兼容时“启动”可用、缺少本机 API 时“启动”禁用、升级后状态行恢复
- [x] 3.2 Supervisor 处理 `ErrContractUnsupported`：Starting 以 Windows 侧可达性判定就绪并标记 `Degraded`，Running 保持并以可达性维持，状态解析失败不计入连续无响应次数，不自动重启。验证：supervisor 测试用返回 `v: 2` 的假服务完成“启动 → 运行中（受限）→ 停止”，且时钟推进多个状态周期后仍为 Running
- [x] 3.3 运行实例比较：每次取得运行中状态时分类，并与已安装文件版本比较，不同即提示“重启后生效”，仅托盘管理实例提供重启。验证：supervisor 测试覆盖 systemd 实例 0.2.0 + 已安装 0.3.0 时有提示且无重启入口
- [x] 3.4 `menuview.go` 统一生成状态行与更新入口：两侧版本与较旧一方、不兼容方向、“不兼容，功能受限”、“下载托盘 <版本>”（仅发布版本），不产生通知；`winui` 菜单接入并用 `winui.OpenURL` 打开 Release 页面。验证：`menuview_test.go` 表驱动覆盖每种分类的文案与入口；`GOOS=windows go vet ./cmd/webmux-tray ./internal/tray/...` 通过
- [ ] 3.5 手动验收（真实 Windows + WSL）：对应、WSL 较旧（升级后恢复）、WSL 较新（打开 Release 页面）、契约不兼容时启动并运行、运行中实例未重启五种情况的状态行与菜单。验证：PR 描述附截图
