# Design

## Context

- `WSLService.Check`（`internal/tray/service.go`）先执行 `webmux -h`，以输出中是否有 `-log-file` 判断是否支持本机 API，然后执行 `webmux version` 并用 `ParseVersion` 解析 `webmux <版本>`。`version` 只能在探测通过后调用：更早的版本会把未知参数当作启动服务。
- `Supervisor.checked` 只在“已安装文件版本低于托盘”时设置 `UpgradeTo`；版本更高、`dev`、无法解析都视为可用且不提示。`Blocker` 有 `missing`、`incompatible`、`no-wsl` 三种，`incompatible`（缺少本机 API）会禁用“启动”。
- 运行中的实例经桥接返回 `contract.StatusV1`，其中有 `Version`，但托盘只把它拼进状态行，不与已安装文件比较。`Status` 只在 `status --json` 回退路径上检查 `v > contract.V`，本机 API 路径没有同样的检查。
- 启动后的就绪由 `/local/v1/status` 驱动（`ReadyTimeout` 60 秒、`StartingInterval` 2 秒）；Windows 侧可达性检查（`onReach`）只在已处于 Running / Unreachable 时生效。
- `contract.V = 1`，同一契约版本内只增加可选字段；隧道等可选功能已按字段是否存在判断支持与否。

## Goals / Non-Goals

**Goals:**

- 兼容由机器可读的契约版本决定；对旧版本保持现有探测作为回退。
- 已安装文件与运行中实例分别做版本对应判定，升级后未重启能被发现。
- 不增加 `wsl.exe` 调用次数：`version --json` 取代现有的 `version` 调用。

**Non-Goals:**

- 系统通知或对话框提示（只用状态行）。
- 以构建提交区分版本号相同的构建；本地构建都报告 0.1.0 时视为对应。
- 托盘自更新、后台自动安装或自动重启。
- Windows 控制台客户端 `webmux.exe` 的版本检查。

## Decisions

### 1. 版本信息来自 `webmux version --json`

输出 `{v, version, contract}`，`contract` 直接取 `contract.V`。现有 `version` 分支忽略多余参数，所以旧版本收到 `version --json` 会打印文本：托盘先尝试解析 JSON，失败时用 `ParseVersion` 解析文本并把契约视为 1（能走到这一步说明已通过本机 API 探测）。

备选：在 `-h` 输出里加标记，或新增 `capabilities` 子命令。前者仍是字符串匹配；后者在旧版本上会被当作启动服务，必须先探测，并不比扩展 `version` 省事。

### 2. 兼容以契约版本集合判定

托盘内置 `supportedContracts`（当前只有 `contract.V`）。服务端契约低于集合为不兼容（WSL 过旧），高于集合为不兼容（WSL 过新）。可选功能继续按字段是否存在判断，不进入兼容判定。

备选：同 `MAJOR.MINOR` 兼容，或版本必须完全相同。0.x 阶段的次版本号不承载兼容含义，按版本号判定会把能正常工作的组合标为不兼容。

### 3. 分类

新增纯函数 `Classify(tray, server BuildInfo) Compat`，`BuildInfo{Version, Contract}`，返回 `matched`、`server-older`、`server-newer`、`incompatible-older`、`incompatible-newer`、`unknown`。规则见 `windows-tray-server-compat` 的“兼容判定”与“版本对应判定”。不做 I/O，用表驱动测试覆盖全部分支。

`Snapshot` 用 `Installed BuildInfo`、`Running *BuildInfo`、`Compat`、`RunningCompat` 取代 `WebmuxVersion` / `UpgradeTo`。`Blocker` 保留 `missing`、`no-wsl` 与 `incompatible`，但 `incompatible` 只表示“缺少本机 API”（无法由托盘启动）；契约不兼容不再是 `Blocker`，由 `Compat` 表达，不禁用任何操作。状态行与菜单文案由 `menuview.go` 统一生成。

### 4. 不兼容实例的启动与运行

契约不兼容时允许启动。`Status` 在两条路径上都把 `v` 不在支持集合内的状态返回为一个可识别的错误（`ErrContractUnsupported`，携带服务端版本）。Supervisor 收到该错误时：

- Starting：改用 Windows 侧可达性判断就绪，就绪后进入 Running 并标记 `Degraded`；60 秒内不可达仍按现有超时失败。
- Running：保持 Running，以可达性维持状态，会话数与隧道等依赖状态的信息显示为未知；停止优先走 `/local/v1/shutdown`，失败时退回 `webmux stop`（现有路径）。
- 不进入自动重启循环：`Degraded` 状态下状态解析失败不计入连续无响应次数。

备选：不兼容时直接禁用启动。已按决定改为允许启动并提供更新入口。

### 5. 运行中实例

托盘在每次取得运行中状态时，以 `Status.Version` 与 `Status.V` 组成运行实例的 `BuildInfo` 再做一次 `Classify`，并与已安装文件的版本比较，版本不同即提示“重启后生效”。

### 6. 下载托盘入口

Release 页面地址为 `https://github.com/<ReleaseRepo>/releases/tag/v<版本>`，复用安装器已有的 `ReleaseRepo` 变量与 `winui.OpenURL`。只在服务端版本是 `MAJOR.MINOR.PATCH` 时提供。

## Risks / Trade-offs

- [本地构建都报告 0.1.0，来自不同提交时也判为对应] → 已按决定接受；需要区分时由用户自行设置 `VERSION`。
- [契约版本长期不变，增量功能的缺失不会被判为不兼容] → 有意为之：功能按字段降级；不兼容的变更必须提升契约版本，`docs/protocol.md` 已有该约定。
- [允许启动不兼容实例后托盘功能受限] → 状态行持续注明“不兼容，功能受限”并显示更新入口；打开 WebMux 走 Windows 侧链接，不依赖契约。
- [只用状态行，用户可能不打开菜单就不知道] → 已按决定接受；不兼容实例的“功能受限”同样只在状态行说明。

## Migration Plan

1. 服务端：`version --json`，向后兼容的新增，单独可发布。
2. 托盘：`Check` 改用 `version --json` 并回退文本；新增分类、运行实例比较、不兼容实例的降级运行与菜单入口。旧服务端得到“兼容 + 版本比较”，行为不比现在差。
3. 回滚：托盘回退到旧版本即恢复现有行为；服务端新增参数可保留。
