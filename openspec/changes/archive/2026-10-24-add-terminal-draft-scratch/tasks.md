## 1. 存储与模型

- [x] 1.1 在 `web/src/schemas.ts` 增加暂存条目与 `webmux.scratch.<sessionId>` v1 记录 schema，校验稳定 ID、重复 ID、会话 ID 和代际。
- [x] 1.2 扩展 `web/src/draftStorage.ts`：独立暂存键、读取/保存/条数统计、7 天保留、20 条 / 20k 单条 / 100k 合计限制，以及 `keep`、`remove`、`clear` 同时处理两个前缀；保存失败时保留上一次成功记录。
- [x] 1.3 扩展 `web/src/terminal-draft.svelte.ts`：暂存列表、`addScratch`、`editScratch`、`removeScratch`、`moveScratch`、`stash`、`takeScratch`、`clearScratch`、持久化降级和隐藏更新状态。

## 2. 界面与设置

- [x] 2.1 新增 `web/src/components/ScratchPanel.svelte`，提供新增、编辑、排序、删除、清空、取用追加/替换冲突、容量/保存失败提示和锁定提示。
- [x] 2.2 在 `TerminalInputComposer.svelte` 增加“暂存 (N)”入口；在终端菜单增加“暂存”入口，使草稿关闭时仍可整理暂存。
- [x] 2.3 在 `Terminal.svelte` 顶栏显示暂存条数和“暂存已更新”状态，传入暂存保存状态，不自动打开草稿或发送；取用后保存失败时面板保持打开并提示。
- [x] 2.4 更新 `Settings.svelte` 分别显示草稿会话数与暂存条数，关闭保存和清除全部同时处理两者。

## 3. 文档与故事

- [x] 3.1 更新 `docs/usage.md` 的草稿章节，说明暂存入口、取用冲突、容量、保存失败、恢复和不自动发送边界。
- [x] 3.2 更新 `docs/user-stories.md`：新增 US-F09，更新 US-F01、US-F03、US-F06、US-F07、US-F08、F 组索引和功能覆盖索引。

## 4. 验证

- [x] 4.1 增加 `draftStorage` 单元测试：暂存往返、过期/损坏/重复 ID、两个前缀的 `keep`/`remove`/`clear`、条数与容量限制、保存失败保留旧记录、禁用存储降级。
- [x] 4.2 增加草稿模型测试：新增、编辑、排序、删除、取用追加/替换、追加换行规则、替换失败回滚、未确认提交锁定、隐藏更新、会话隔离、刷新恢复、代际提示和异步输入不写暂存。
- [x] 4.3 增加暂存面板浏览器测试：入口、空状态、新增、取用、追加/替换冲突、取消、锁定禁用、暂存/删除/清空和代际提示；并在 E2E 中断言设置页计数。
- [x] 4.4 运行 `svelte-check`、Vitest 单元/浏览器测试、Oxlint、Playwright E2E 和 Vite 生产构建，记录实际结果与未验证边界。
- [x] 4.5 使用 Storybook 真实组件捕获桌面/手机、明/暗主题截图，并补充改动前后草稿操作行截图，按仓库要求附入 PR。

## Verification

- `svelte-check --tsconfig ./tsconfig.json` 与 `.storybook/tsconfig.json`：0 errors，0 warnings。
- Node Vitest：19 个文件、133 项通过。
- 浏览器 Vitest（`TerminalDraft`、`ScratchPanel`）：18 + 7 项通过。
- Oxlint：0 warnings，0 errors。
- Vite 生产构建成功；保留仓库既有的超过 500 kB chunk 提示。
- Playwright chromium-desktop：新增“scratch notes persist and move into the draft only on take”和更新后的“drafts survive navigation and reload…”共 2 项通过。
- Storybook 截图：桌面 1280×900 与手机 390×844、明/暗主题的暂存面板，以及改动前后的草稿操作行；截图附在 PR 中。
- 未验证边界：未使用真实 iOS/Android 设备检查软键盘、输入法组合和系统本地存储配额；浏览器模拟与单元测试不能替代真机检查。
