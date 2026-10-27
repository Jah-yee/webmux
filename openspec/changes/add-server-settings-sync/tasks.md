## 1. 契约与存储

- [x] 1.1 在 `internal/settings` 定义设置值类型：主题、字号、预览、代码阅读器、快捷指令、主页工作树与强制底部工具栏；用可区分“缺失”“`null`”“显式值”的字段表示 PATCH，实现默认值与规范化
- [x] 1.2 实现 `<state_dir>/settings.json` 的加载、校验、持久提交与失败冻结；复用 `internal/statefile` descriptor/typed handle（0600、`fsync`、`rename`、目录同步），文件不存在视为 `revision: 0` 且全部为 `null`
- [x] 1.3 实现 `<state_dir>/settings.json.lock` 独占非阻塞锁；锁冲突、文件损坏、版本不支持或超过 4 MiB 时只让设置服务不可用，保留原文件，错误包含原因与路径
- [x] 1.4 实现修订号、`updatedAt`、全局校验（数量、长度、UTF-8、快捷指令规范化、主页工作树上限）与 PATCH 原子应用；失败不递增修订号
- [x] 1.5 实现订阅：在与 PATCH 提交共用的设置锁内捕获快照、将快照排入新订阅队首并注册订阅，后续变化键事件按修订号入队；提供有界队列，解锁后由单写协程发送网络帧并关闭慢客户端；`Close` 停止订阅并释放锁
- [x] 1.6 新增 `internal/settings/*_test.go`：默认快照、首次不写文件、重开保留、锁冲突、损坏文件不覆盖、保存失败不发布或递增修订号、修订号递增、`null` 与 `[]` 区分、全部字段边界、并发 PATCH、订阅与关闭；用受控并发覆盖快照捕获/订阅注册期间仅发生一次 PATCH，确保无修订号丢失且快照先于后续事件

## 2. API、WebSocket 与接线

- [x] 2.1 在 `internal/contract` 增加 `SettingsSnapshot`、`SettingsValues`、`SettingsPatch` 与 `SettingsEvent`（`settings` / `settings.update`）类型，保持 stdlib-only
- [x] 2.2 在 `internal/wire` 增加设置服务类型与 contract 类型的双向转换；确保 PATCH 缺省键、`null` 与显式值在转换中不丢失
- [x] 2.3 新增 `internal/api/settings.go`：注册 `GET /api/settings` 与 `PATCH /api/settings`，使用 2 MiB 专用限长解码、严格 JSON 与未知字段拒绝；映射 200/400/401/403/409/503，错误不包含设置值
- [x] 2.4 在 `internal/api/router.go` 注入设置服务；`Deps` 增加 `Settings`，nil 或不可用时返回 503；`cmd/webmux` 按现有装配顺序打开服务并在退出时关闭，同步更新 `cmd/baseline/fixture.go`、`internal/tray/forward` 与所有 `api.New` 调用点
- [x] 2.5 在 `internal/hub` 增加 `/ws/settings`：复用现有认证与 `webmux` / `webmux.token.<base64url(token)>` 子协议，消费原子注册的订阅，先发完整快照，再按序编码变化键更新帧；连接加入与退出与订阅生命周期绑定，写入失败关闭连接，网络写入不持设置锁
- [x] 2.6 在 `internal/archtest/layers_test.go` 与 `docs/architecture.md` 中登记 `internal/settings` 为 service；检查 controller 不直接读写设置文件
- [x] 2.7 在 API 集成、既有认证中间件与 settings 服务测试中覆盖：HTTP Bearer、WebSocket token 子协议与 Cookie 兼容回退、Host/Origin、GET/PATCH、409、400、503、未知键、请求体上限、`null` 重置、快照帧、更新帧、慢客户端与断线重连

## 3. 前端设置仓库与旧偏好隔离

- [x] 3.1 在 `web/src/schemas.ts` 增加设置快照、值、PATCH 与 WebSocket 帧的 Zod schema，字段与 Go 契约一致；增加最大长度、唯一 ID 与 `null` 校验
- [x] 3.2 新增 `web/src/settings.svelte.ts`：保存权威快照与修订号、打开 `/ws/settings`、按修订号合并变化键、断线退避重连、可见性恢复校验；所有写入走 PATCH，409 重读后按操作意图重放一次：`preview` / `codeReader` 仅覆盖用户改动的子字段（最新值为 `null` 时展开默认），同字段冲突使用本次目标值、明确改动两字段时两者都重放，分组重置仍写 `null`；快捷指令与主页工作树维持按 ID 重放
- [x] 3.3 实现 `webmux.settingsCache` 只读缓存：首屏同步读取主题与字号、认证后覆盖、写入失败忽略、损坏删除；缓存不接收用户写入
- [x] 3.4 按破坏性升级决策移除旧偏好读写入口；不上传、不删除旧键，不创建迁移标记。
- [x] 3.5 调整 `web/src/api.ts` 与 `theme.ts`：保留 token、`recentDirs`、`lastLaunch`、`customCommand` 等本地读写；主题与字号改为读写设置仓库，登录页使用缓存值
- [x] 3.6 增加仓库与意图重放测试：复合字段、整组 null、空列表、命令/工作树 ID 合并、409 最多一次重试、失败回滚且不污染缓存、重连接受重置后的服务端快照。

## 4. 各设置入口

- [x] 4.1 `quickCommands.svelte.ts`：列表来自设置快照；首次 `null` 显示 `/resume`、`/new`，保存写 PATCH；移除 `storage` 事件依赖，保留上限与排序逻辑
- [x] 4.2 `previewSettings.svelte.ts`、`CodeReader.svelte`、`CodeDiffReader.svelte`：预览、行号与 Diff 换行读写设置仓库，提交用户实际修改的子字段意图；控件在远端更新时无需刷新即反映，409 重试不覆盖未修改的兄弟字段
- [x] 4.3 `hooks/terminal.svelte.ts` 与 `TerminalHeader.svelte`：字体大小与主题切换写入设置仓库；乐观更新失败时回滚；`forceSoftKeys` 改为持久化设置并在刷新后恢复
- [x] 4.4 `homeWorktrees.svelte.ts` 与 `SessionList.svelte`：固定工作树来自服务端设置；读取时只做展示过滤，不因加载自动 PATCH；显式添加或移除时按 ID 写入并处理 409
- [x] 4.5 确认 `NewSessionSheet.svelte` 的 `lastLaunch`、`customCommand`、`recentDirs` 仍为设备本地；不把这些值上传为设置
- [x] 4.6 更新现有浏览器/单元测试：删除“快捷指令仅保存在当前浏览器”“主题本地保存”等断言，新增跨客户端即时更新与失败回滚断言

## 5. 设置页

- [x] 5.1 `Settings.svelte` 顶部说明改为服务端同步与设备本地两部分；保留启动配置、公网访问与通知推送说明
- [x] 5.2 新增 `#appearance`“外观与阅读”卡片：主题（跟随系统/浅色/深色）、字号 8–20、预览行数与跳过行数、代码行号、Diff 换行、强制底部工具栏；无效值不覆盖已保存设置
- [x] 5.3 快捷指令卡片说明改为服务端保存；不可用时显示原因与文件路径，禁用写入但保留编辑输入；实现远端更新与 409 提示
- [x] 5.4 为单个分组提供“恢复默认”，使用 PATCH `null`；快捷指令“删除全部”仍确认并写入 `[]`；保留 `#quick-commands`、`#launch-configs`、`#drafts` 深链接
- [x] 5.5 设置页显示简短同步状态与“重新读取”入口；不可用时不影响草稿、启动配置与其他本地设置；桌面与窄屏均无横向溢出
- [x] 5.6 浏览器测试：主题/字号/预览/代码偏好持久化、远端事件更新、复合设置按子字段重放与整组恢复默认、503 禁用与重试、快捷指令冲突保留输入

## 6. 端到端与回归

- [x] 6.1 Playwright 使用两个隔离浏览器上下文连接同一后端：桌面修改主题、字号与快捷指令，手机上下文无需刷新即看到；反向修改同样成立
- [x] 6.2 覆盖旧 localStorage 隔离：预置旧键和损坏缓存后连接，不上传旧偏好，服务端 null 保留，显示默认指令。
- [x] 6.3 覆盖故障：设置文件损坏或第二个实例锁冲突时终端仍可用、设置页显示 503 与路径、修复并重启后恢复
- [x] 6.4 结合已有工作树 E2E 验证固定、移除与展示过滤；意图单元测试验证并发 ID 合并，仓库浏览器测试验证 409 重读重放协议。
- [x] 6.5 运行 `go test -race ./internal/settings ./internal/api ./internal/hub ./internal/archtest`、`pnpm --dir web check`、`pnpm --dir web lint`、目标 Vitest、`make test-e2e` 与 `make check`

## 7. 文档、用户故事与验收

- [x] 7.1 `docs/protocol.md`：增加 `GET /api/settings`、`PATCH /api/settings`、`/ws/settings` 的请求/响应形状、`null` 语义、修订号、409、大小上限与错误码
- [x] 7.2 `docs/usage.md`：说明哪些设置跨设备同步、哪些仅当前设备、旧偏好隔离与不可用降级；更新设置页截图说明
- [x] 7.3 `docs/deployment.md`、`README.md`、`docs/architecture.md`：登记 `settings.json`、`settings.json.lock`、`internal/settings` 与装配关系
- [x] 7.4 更新已有 US-C07、US-C09、US-D08、US-G01–US-G04、US-H04、US-I05、US-J03 的服务端同步验收；复用 US-G04 描述跨设备同步与旧键隔离，核对索引、来源链接与核对记录。
- [x] 7.5 `deploy/config.example.toml`：补充注释说明 TOML 仍管理启动、安全与全局推送配置，用户偏好由设置服务保存
- [x] 7.6 运行 `openspec validate add-server-settings-sync --strict --no-interactive`；确认快捷指令持久化需求的 RENAMED FROM 精确匹配主规格旧名称，TO 与 MODIFIED 使用同一服务端持久化新名称，其他修改需求名称与主规格一致
- [x] 7.7 UI 变更的 PR 描述附桌面/手机与亮/暗主题截图，使用隔离样例数据，不包含真实 token、快捷指令密钥或终端输出

验证记录：Linux/macOS 原生 Go race、282 项单元、258 项浏览器、211 项 Storybook、设置/工作树定向 E2E 已通过；完整 E2E 与最终提交 CI 结果见 PR #147。`make check` 除基线已有的 52 项 golangci-lint 告警外通过，新改动告警为零。
