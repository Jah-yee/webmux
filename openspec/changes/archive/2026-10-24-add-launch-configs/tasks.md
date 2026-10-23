## 1. 服务端存储

- [x] 1.1 新增 `internal/launch/types.go`：配置结构（`id`、`name`、`cmd`、`env`、`createdAt`、`updatedAt`）与摘要结构（`envNames`）、最大限制、名称去空白与唯一性校验、argv/env 防御性复制、`[a-z2-7]{8}` ID 生成与冲突重试
- [x] 1.2 新增 `internal/launch/store.go`：取得 `<state_dir>/launch-configs.lock` 独占非阻塞 `flock` 并持有到 `Close`；从 `<state_dir>/launch-configs.json` 加载，文件不存在视为空列表且不写文件；CRUD、互斥锁、原子保存（临时文件 + `chmod 0600` + `fsync` + `rename` + 目录同步）与保存失败回滚；不读取 TOML 预设
- [x] 1.3 实现不可用处理：锁冲突、文件无法解析或校验失败、目录或文件不可读写时只禁用该功能并记录原因与路径；保留原文件，不自动重建，不覆盖用户数据
- [x] 1.4 新增 `internal/launch/store_test.go`：重开保留、首次无文件为空列表且不创建文件、第二个实例锁冲突时不可用且不修改文件、空列表、重名、名称/argv/env 上限、损坏文件不被覆盖、保存失败回滚、ID 唯一性、摘要不含值

## 2. API 与接线

- [x] 2.1 新增 `internal/api/launch.go`：注册 `GET`/`POST /api/launch-configs` 与 `GET`/`PUT`/`DELETE /api/launch-configs/{id}`；列表返回摘要（`envNames`），单条与创建/更新返回完整配置；复用 `decode` 严格 JSON 与 64 KiB 上限；返回 200/201/204、400、404、409、500、503；错误文本包含原因与状态文件路径但不包含 env 值
- [x] 2.2 在 `internal/api/router.go` 的 `POST /api/sessions` 请求中增加可区分缺省与空值的 `configId`；实现与 `preset`/`cmd`/`args`/`env` 的互斥（`env: {}` 视为未提供）、存储不可用 503、未知 ID 404、`session.ValidateEnv` 二次校验，均在目录与 Git 操作之前；调用 `Workspace.CreateSession(..., "custom", cfg.Cmd, nil, name, workspace, cfg.Env)`
- [x] 2.3 在 `cmd/webmux/main.go` 中于 `workspace.New` 之后、`api.New` 之前创建启动配置服务并在退出时 `Close`；同步更新 `cmd/baseline/fixture.go`、`internal/tray/forward/forward_test.go` 与所有 `api.New(...)` 调用点，以编译错误为准逐个处理
- [x] 2.4 新增 `internal/api/launch_test.go`：认证与 Origin 继承、CRUD、列表响应不含任何 env 值、单条读取返回值、严格 JSON、重名 409、未知 ID 404、存储损坏与锁冲突 503、保存失败 500 回滚
- [x] 2.5 新增会话创建测试：`configId` 启动的 argv/env 正确、互斥字段 400、`env: {}` 不算冲突、空 `configId` 400、未知 ID 404 且不创建会话、伪造 `WEBMUX_SESSION_ID` 被拒绝、删除配置不影响运行中会话、现有 `preset`/`cmd`/`env` 路径不回归

## 3. 前端

- [x] 3.1 在 `web/src/schemas.ts` 增加 `LaunchConfigSummarySchema`（`envNames`）、`LaunchConfigSchema`（`env`）与列表 schema（最多 100 条、ID 唯一），导出 `z.infer` 类型
- [x] 3.2 在 `web/src/api.ts` 或独立工具模块新增 `quoteArg`/`joinArgs`，覆盖空白、单双引号、反斜杠、`$`、空参数与 `parseArgs` 的往返一致性；新增 Node 单元测试
- [x] 3.3 新增 `web/src/components/LaunchConfigDialog.svelte`：编辑前读取单条完整配置且加载完成前禁用保存；名称、命令（等宽 Textarea）、环境变量行（添加/删除、默认遮挡与显示切换、名称校验与 `WEBMUX_SESSION_ID` 拒绝）；从内置预设填入（合并追加参数，名称为空时预填 `label`）；保存错误（400/409/503）保留输入
- [x] 3.4 在 `Settings.svelte` 新增 `id="launch-configs"` 卡片、列表（名称/命令预览/env 数量）、空状态、编辑、删除确认与 100 条上限；说明内置预设与启动配置的区别；把 `#launch-configs` 加入深链接滚动目标；加载或写入失败时显示原因与状态文件路径且本页其他设置不受影响；改写顶部“快捷指令、草稿和预览只保存在当前浏览器……”说明以区分服务端保存的启动配置
- [x] 3.5 调整 `SessionList.svelte` 与 `NewSessionSheet.svelte`：加载启动配置摘要；分组展示“内置预设”（保留追加参数，提交 `{preset, args}`）与“我的启动配置”（提交 `{configId, cwd | workspace, name}`）；折叠的“临时命令（不保存）”只承载自定义命令；`lastLaunch` 记住选择并从 `lastPreset` 迁移后删除旧键；启动配置为空或不可用时显示空状态、原因与设置页链接，不阻塞内置预设
- [x] 3.6 增加前端 Node/浏览器测试：schema 校验、`quoteArg`/`joinArgs` 往返、设置对话框 env 遮挡与校验、编辑只改名称时保留 env、新建会话两组选择/`lastPreset` 迁移/空状态/存储不可用时内置预设仍可启动

## 4. 测试、文档与评审

- [x] 4.1 新增 Playwright E2E（隔离后端）：设置页从内置预设填入并创建带环境变量的启动配置 → 新建会话选择并启动 → 终端输出包含该变量 → 刷新或重新打开设置后配置仍在；删除配置后已运行会话继续工作；内置预设启动不回归
- [x] 4.2 更新 `docs/protocol.md`（接口、摘要与完整形状、`configId` 互斥与状态码）、`docs/usage.md`（内置预设、启动配置管理与临时命令）、`docs/deployment.md`（`presets` 仍为实时内置预设；`launch-configs.json` 与 `launch-configs.lock`、0600、锁冲突与损坏处理）与 `deploy/config.example.toml` 的预设注释
- [x] 4.3 更新 `docs/user-stories.md`：新增 `US-B08 管理启动配置`，并更新 `US-B01`–`US-B03` 中“上次预设/自定义命令”的表述；核对索引、来源链接与计数
- [x] 4.4 运行 `pnpm --dir web check`、`pnpm --dir web lint`、`make format-check`、`make test-unit`、`make test-browser`、`make test-e2e` 与相关 `go test -race ./internal/launch ./internal/api ./internal/workspace ./internal/session`
- [x] 4.5 UI PR 附最终实现的桌面/移动与明/暗主题截图；截图使用不含真实密钥的示例配置
- [x] 4.6 在 `openspec/changes/add-launch-configs/` 撰写 proposal、design、spec delta 与任务清单（中文正文、英文结构标题与 SHALL/MUST）
- [x] 4.7 按 PR #103 评审结论修订工件：内置预设实时并列不种子、从预设填入时名称取 `label`、列表不返回环境变量值、独立文件锁、`configId` 与 `env: {}` 的互斥口径、启动接线顺序
