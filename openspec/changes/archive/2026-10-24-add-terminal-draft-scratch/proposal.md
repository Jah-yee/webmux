## Why

在 [#95](https://github.com/mzdluo123/webmux/issues/95) 的调研收敛中，用户确认采用“多条草稿暂存”：agent 工作时需要先记下后续任务，而当前每个会话只有一份草稿，继续记录只能覆盖正在编辑的正文。系统分享导入和语音转写因收益与平台覆盖不足不进入实施；审批/问答卡片和结构化对话阅读因需要逐 agent 协议适配或第二套消息契约也不进入实施。

本变更只实现多条暂存，保留现有真实 PTY、显式发送、发送确认和本机草稿语义。

## What Changes

- 保持 `webmux.draft.<sessionId>` 的 v1 当前草稿结构不变，新增独立的 `webmux.scratch.<sessionId>` v1 记录，避免旧草稿迁移和旧版本回退时被误删。
- 每个会话增加多条暂存文本，支持新增、编辑、上移、下移、删除、清空和取用；暂存与当前草稿是两个独立编辑目标。
- 取用时，当前草稿为空则直接写入；非空时提供“追加到当前草稿”和“替换当前草稿”，替换前把原正文保留为一条新的暂存。
- 当前草稿存在未确认提交时禁止转存和取用，暂存仍可查看、编辑、排序和删除；任何取用都不自动发送、不自动聚焦终端。
- 上传、粘贴、代码引用和快捷指令仍只写入当前草稿，不直接写入暂存；取用是暂存进入当前草稿的唯一出口。
- 暂存按会话保存在当前浏览器，沿用 7 天保留期；每会话最多 20 条、单条最多 20,000 字符、合计最多 100,000 字符。超出容量时拒绝新增或编辑并说明限制；存储关闭或写入失败时保留页面内内容并提示“仅当前页面”。
- 草稿面板增加“暂存 (N)”入口，终端菜单也可在不开启草稿时打开暂存；关闭草稿后顶栏按钮显示暂存条数，暂存更新时提示“暂存已更新”但不自动打开草稿。
- 设置页分别显示草稿会话数和暂存条数；关闭本机保存或清除全部草稿时同时处理暂存。
- 更新使用文档、User Stories（新增 US-F09，更新 US-F01、US-F03、US-F06、US-F07、US-F08 与索引）以及单元/浏览器测试。
- 不新增 Go API、WebSocket 消息、服务端存储或 agent 专用协议。

## Capabilities

### New Capabilities

- `terminal-draft-scratch`: 每会话多条暂存的添加、编辑、排序、删除、取用冲突处理、未确认提交锁定、本机持久化与恢复、容量限制和界面提示。

### Modified Capabilities

无。当前 `openspec/specs/` 未登记终端草稿主规格；`add-terminal-input-composer` 仍在变更阶段，本变更只在其当前草稿契约旁新增独立暂存，不修改其发送、隐藏和输入路由合同。

## Impact

- 前端契约：`web/src/schemas.ts` 新增暂存记录 schema。
- 本机存储：`web/src/draftStorage.ts` 增加暂存前缀、读取、保存、计数和清理。
- 状态模型：`web/src/terminal-draft.svelte.ts` 增加暂存列表和操作。
- 界面：`web/src/components/TerminalInputComposer.svelte`、新增 `ScratchPanel.svelte`、`web/src/pages/Terminal.svelte`、`web/src/pages/Settings.svelte`。
- 文档：`docs/usage.md`、`docs/user-stories.md`。
- 测试：`web/test/draftStorage.test.ts`、`web/test/browser/TerminalDraft.test.ts`，以及暂存面板浏览器测试。
- 不改变 Go 服务端、PTY 回放、发送确认、队列和认证边界。
