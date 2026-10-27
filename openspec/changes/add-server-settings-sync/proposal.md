## Why

WebMux 的设置目前分散在三处：`config.toml` 是启动时读取的部署配置；启动配置、仓库清单及已有网页可管理的隧道覆盖配置保存在 `<state_dir>`；主题、字号、输出预览、快捷指令、代码阅读器偏好与主页工作树选择仍只写入浏览器 `localStorage`，强制底部工具栏甚至只存在于组件状态。同一实例换设备后这些浏览器偏好全部消失：手机上新加的快捷指令在桌面看不到，换浏览器后主题和字号恢复默认，主页工作树需要重新整理。`terminal-quick-commands` 规格明确把快捷指令限定为本地存储（#119）。

同一浏览器的多个标签页中，主题、快捷指令与主页工作树依靠 `storage` 事件同步，字号、预览与代码阅读器偏好没有对应的跨标签页监听；跨设备没有通道。`config.toml` 的修改需要编辑文件并重启服务，也不是面向使用者的设置入口。把可跨设备共享的偏好收敛到服务端，可以让所有客户端读取同一份权威值，同时保留凭据、设备能力和草稿这类必须留在本地的数据。

## What Changes

- 新增 `internal/settings` 服务，把与浏览器无关、可安全跨设备共享的偏好保存到 `<state_dir>/settings.json`：`theme`、`fontSize`、`preview`、`codeReader`（行号与 Diff 换行）、`quickCommands`、`homeWorktrees` 与 `forceSoftKeys`。文件使用独立锁 `settings.json.lock`、0600 权限、原子替换与版本化修订号；文件损坏或锁冲突只禁用设置功能，不影响会话、终端与其他服务。
- 新增已认证的 `GET /api/settings`、`PATCH /api/settings` 与 `/ws/settings`：PATCH 携带 `baseRevision` 做乐观并发控制，冲突返回 409；客户端重读后仅重放本次操作，预览与代码阅读器按用户修改的子字段合并；WebSocket 原子地捕获快照并注册订阅，先发完整快照，再推送只含变化键的有序更新事件；断线重连重新取快照，慢客户端断开而不是阻塞服务。
- 用 `null` 表达“未显式设置并采用默认值”，区分“快捷指令从未初始化”（`null`，显示内置 `/resume`、`/new`）与“用户明确删除全部”（`[]`）。主题的 `null` 表示跟随系统，字号的 `null` 表示按设备默认。
- **BREAKING**：旧 localStorage 偏好不自动上传；服务端快照为唯一权威，成功同步结果可缓存用于首屏与离线只读显示。
- 设置页重构为“服务端同步 + 设备本地”两部分说明，并新增外观与阅读设置（主题含跟随系统、字号、预览、行号、Diff 换行、强制底部工具栏）。快捷指令卡片改为服务端存储，显示同步不可用原因并保留编辑输入；主页工作树选择、终端主题与字号、代码阅读器开关共用同一份设置。
- 明确边界：`listen`、`token`、`state_dir`、`allowed_origins`、`default_cwd`、`scrollback_bytes`、`idle_after_seconds`、`prompt_patterns`、`agent_protocol`、`bell_attention` 与 `push.*` 继续由 `config.toml` 管理；`tunnel.*` 仍提供部署默认值，已有的 `tunnel-settings.json` 与公网访问专用配置服务保留开关、模式、凭据、主机名的覆盖与即时应用，`cloudflared` 和下载镜像仍由 TOML 管理。通用设置接口不接受这些部署或隧道配置；浏览器 token、Web Push 订阅、草稿与暂存正文、最近目录、上次启动选择与临时命令继续留在当前设备。
- 不改变 PTY、会话生命周期、终端帧、回放、WebSocket 终端连接、启动配置或 TOML 解析规则；不新增多用户账户、设置端到端加密、跨实例同步或从网页编辑 `config.toml` 的能力。

## Capabilities

### New Capabilities

- `server-settings-sync`：服务端设置存储、修订号并发控制、跨客户端实时同步、旧键隔离、同步不可用降级，以及主题、字号、预览、代码阅读器、主页工作树和强制底部工具栏的共享语义。

### Modified Capabilities

- `terminal-quick-commands`：把“快捷指令数据与本地持久化”需求重命名为“快捷指令数据与服务端持久化”，快捷指令改为保存在服务端并在同一实例的所有设备间同步；首次预置指令改由服务端 `null` 状态决定；设置页增加跨设备与同步不可用行为。

## Impact

- 后端：新增 `internal/settings`（文件锁、原子保存、校验、修订号、订阅）；`internal/contract` 与 `internal/wire` 增加设置快照、PATCH 与 WebSocket 事件类型；`internal/api` 增加 `/api/settings` 路由与错误映射；`internal/hub` 增加设置事件编码；`cmd/webmux` 打开服务并注入 `api.Deps`；`internal/archtest/layers_test.go` 登记新包。
- 前端：新增 `web/src/settings.svelte.ts`（权威快照、缓存、WebSocket 与 PATCH）与对应 Zod schema；改造 `api.ts`、`schemas.ts`、`theme.ts`、`previewSettings.svelte.ts`、`quickCommands.svelte.ts`、`homeWorktrees.svelte.ts`、`CodeReader.svelte`、`CodeDiffReader.svelte`、`hooks/terminal.svelte.ts`、`TerminalHeader.svelte`、`Terminal.svelte`、`Settings.svelte` 与 `NewSessionSheet.svelte`（仅保留上次选择为本地）。
- 状态文件：新增 `<state_dir>/settings.json` 与 `<state_dir>/settings.json.lock`；`README.md` 的 `state_dir` 说明、`docs/architecture.md` 的包表与状态目录说明、`docs/deployment.md` 的状态文件清单需要同步。
- 文档：`docs/protocol.md` 增加设置 API 与 WebSocket 帧，`docs/usage.md` 说明跨设备同步与设备本地边界，`docs/user-stories.md` 更新 US-C07、US-C09、US-D08、US-G01–US-G04、US-H04、US-J03 并补充设置同步故事；`deploy/config.example.toml` 保持 TOML 边界说明。
- 测试：Go 存储/API/WebSocket 与架构测试；Vitest schema、旧键隔离、冲突重试、降级；Playwright 双浏览器上下文跨客户端同步、旧键隔离与设置页故障；现有断言“仅保存在当前浏览器”的测试需要同步修改。
- 依赖与兼容：无新增运行时依赖；允许破坏性升级，不支持混合版本或自动迁移。
