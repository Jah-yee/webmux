## Why

新建会话时的“启动命令 + 参数”目前一部分来自启动时加载的 TOML `[[presets]]`，一部分来自浏览器 `localStorage` 中的上次预设与自定义命令。追加参数、自定义命令和环境变量无法保存为可复用的服务端配置，换浏览器或换设备就会丢失，也无法作为后续一键启动和 CLI 复用的稳定来源。

本变更建立服务端持久化的启动配置规格：以“名称 + 完整 argv + 环境变量”为单位保存用户自己的启动方式，与始终跟随 TOML 的内置预设并列，由新建会话页、后续首页一键启动和 CLI 共同复用。规格工件与实现代码均由 PR #103 交付。

## What Changes

- 新增服务端持久化的启动配置：名称、完整 argv 与环境变量，保存在 `<state_dir>/launch-configs.json`；跨服务重启保留，跨浏览器与设备共享。
- TOML `[[presets]]` 继续作为只读的“内置预设”实时提供：不复制进启动配置文件，新增或修改预设（包括默认预设的修正）按现有方式在重启后生效。
- 启动配置保存的是创建时的命令快照：从预设填入后即为独立副本，预设之后的修改或删除不改变已保存配置。
- 新增 `/api/launch-configs` 的 `GET`/`POST` 与 `/api/launch-configs/{id}` 的 `GET`/`PUT`/`DELETE`。列表只返回环境变量名称，值只在读取单条配置及创建/更新响应中返回。沿用现有认证、Origin 校验和严格 JSON 规则。
- 启动配置文件由独立的文件锁保护；文件损坏、存储不可用或状态目录已被另一实例占用时只禁用启动配置功能并返回 503，会话创建、内置预设与终端连接不受影响。
- `POST /api/sessions` 新增 `configId` 启动方式，与现有 `preset`、`cmd`、`args`、`env` 互斥；`cwd`、`workspace`、`name` 继续由本次启动决定。通过配置启动的会话记录为 `preset: "custom"`，`cmd` 字段保存完整 argv；删除配置不影响已运行会话。
- 设置页新增“启动配置”卡片与编辑对话框（名称、命令、环境变量，值默认遮挡，可从内置预设填入）；新建会话页并列展示“内置预设”与“我的启动配置”，保留折叠的“临时命令（不保存）”。
- 保留现有 `preset`/`cmd`/`args`/`env` 创建方式，兼容脚本、E2E 与 CLI。
- 首页一键启动、在 `SessionInfo` 中暴露配置 ID、配置导入导出、变量模板与配置级默认工作目录不在本期范围。

## Capabilities

### New Capabilities

- `launch-configs`：启动配置的数据模型与命令快照语义、服务端持久化、文件锁与损坏处理、CRUD API 与环境变量值的暴露边界、设置页管理界面、新建会话页中内置预设与启动配置的并列选择，以及设备间共享约定。

### Modified Capabilities

- `session-launch-environment`：新增通过 `configId` 创建会话的请求契约、与现有命令字段的互斥规则，以及启动时使用已保存 argv/env 快照的语义。

## Impact

- 后端：新增 `internal/launch` 包（存储、文件锁与服务）、`internal/api` 的启动配置路由与会话创建扩展；`cmd/webmux/main.go`、`cmd/baseline/fixture.go`、`internal/tray/forward/forward_test.go` 及所有 `api.New(...)` 调用点需要注入新服务。PTY 启动、回放、退出顺序与会话清理路径不变；`GET /api/presets` 不变。
- 协议：增加 `/api/launch-configs` 资源与 `POST /api/sessions` 的 `configId` 字段；`SessionInfo` 契约本期不变，终端二进制帧协议不变。
- 前端：`web/src/schemas.ts` 增加启动配置摘要与完整配置的 Zod 契约；`Settings.svelte` 新增卡片，新增编辑对话框组件；`NewSessionSheet.svelte` 与 `SessionList.svelte` 加载并分组展示内置预设与启动配置；浏览器“上次选择”改为单一键 `lastLaunch`，从旧 `lastPreset` 迁移。
- 状态目录：新增 `launch-configs.json`（0600）与 `launch-configs.lock`（0600）。
- 文档：实施时更新 `docs/protocol.md`、`docs/usage.md`、`docs/deployment.md`（含 `presets` 与启动配置的关系）、`deploy/config.example.toml` 的预设注释与 `docs/user-stories.md`。
- 验证：存储持久化/重开/损坏/限制/重名/锁冲突、API CRUD 与认证继承、列表不含环境变量值、通过配置启动的 argv/env、删除配置不影响运行中会话、设置页到终端的隔离 E2E；UI 变更按仓库要求附桌面/移动与明/暗主题截图。
- 交付顺序：先落地服务端存储与 API，再接入前端；规格、任务与实现均在 PR #103 中完成。
