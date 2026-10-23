## Why

当前 WebMux 只展示平铺的 PTY 会话和启动目录，无法表达同一仓库的多个 worktree 与各自 agent 的关系，也无法在浏览器中完成工作树的创建和清理。需要将首页升级为仓库分组总览，让用户在不同 worktree 中启动、观察和管理多个 agent，同时保留无需 Git、无需注册仓库的直接启动路径。

## What Changes

- 提供显式添加和移除的持久仓库清单；通过 Git 识别主工作区、已有 linked worktree 及其分支、修改、锁定和可用状态。
- 创建 worktree 支持从明确基线创建新分支，或使用未被占用的已有本地分支；默认目录为仓库旁的 `<仓库名>.worktrees/<目录名>`，允许用户修改目标路径，不自动 fetch。
- 在主工作区、任意已有 worktree 或刚创建的 worktree 中启动现有预设或自定义命令；同一 worktree 允许多个 agent/Shell 会话，并明确提示共享文件。
- 保留任意目录直接启动；无 Git 可执行程序、目录不是 Git 仓库或没有 linked worktree 都不成为启动前提。
- 提供安全删除，以及独立二次确认的“关闭关联会话并强制删除”；不自动删除分支，不允许删除主工作区，不绕过 Git worktree 锁。关闭会话本身不会删除 worktree。
- 首页采用“仓库 → 主工作区/worktree → 会话”分组总览，未注册仓库和普通目录会话保留在独立的直接会话分组；支持桌面与手机，不引入常驻侧栏或多终端分屏。
- 仅仓库清单持久化；PTY、终端缓冲和会话列表仍不跨服务重启恢复。已创建的 worktree 是磁盘上的 Git 资源，不随会话退出或服务重启删除。
- Agent 管理沿用当前启动、状态、连接、关闭和移除语义，不增加任务派发、内部对话管理、自动 resume，也不添加提交、合并、推送或代码 PR 操作。

## Capabilities

### New Capabilities

- `repository-registry`：持久仓库注册、去重、移除与不可用状态，以及 Git 能力降级。
- `worktree-lifecycle`：发现、状态读取、创建、安全删除与明确授权的强制删除。
- `workspace-sessions`：启动目录与仓库/worktree 的关联、多会话共享以及直接启动兼容性。
- `workspace-overview`：响应式仓库分组首页、上下文启动入口、状态和危险操作交互。

### Modified Capabilities

无。当前 `openspec/specs/` 没有已有能力规范；本变更为上述行为建立新规范，不伪造既有 delta。

## Impact

- 后端：新增仓库清单/Git 工作树服务，通过 `cmd/webmux/main.go` 显式注入 API；扩展 `internal/api`、`internal/session` 的关联信息与生命周期协调，保持 PTY 回放、连接取消及进程清理不变量。
- 前端：调整 `web/src/pages/SessionList.svelte`、`NewSessionSheet.svelte`、`SessionCard.svelte` 及 `web/src/schemas.ts`；复用 `api.ts`、`ws.ts`、目录选择器与 shadcn-svelte 对话框/抽屉。
- 协议：增加仓库/worktree REST 资源和可空会话关联字段；既有按 `cwd` 创建会话的请求继续有效，终端二进制帧协议不变。
- 部署：Git 是工作树管理的可选系统依赖，不是终端服务启动依赖；增加可配置的状态目录用于仓库清单，不引入数据库或后台守护进程。
- 文档：实施时更新 `README.md`、`WebMux.md` 与部署配置示例，明确“仓库清单落盘，但会话不落盘”，替换旧的平铺首页说明。
- 验证：临时真实 Git 仓库、Go 生命周期/API 测试、真实浏览器交互及隔离后端 E2E；覆盖强制删除、并发启动/删除和无 Git 降级。功能实现、验收进度与交付记录见 tasks.md。
