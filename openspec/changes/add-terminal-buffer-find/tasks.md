## 1. 依赖与基线确认

- [ ] 1.1 在 `web/package.json` 增加 `@xterm/addon-search@^0.16.0`，用 `pnpm --dir web install` 更新 `web/pnpm-lock.yaml`，确认与 `@xterm/xterm@6.0.0` 同一发布提交并保留 frozen lockfile 流程
- [ ] 1.2 复核 `terminal-renderer.ts` 的 `scrollback`、`terminal.svelte.ts` 的 `restore` 路径与 `terminal-input.ts` 的草稿按键分支，确认没有并行改动改变本设计的落点
- [ ] 1.3 确认本变更不改 `/api/sessions/{id}/buffer`、Go 服务端、会话生命周期与控制权代码

## 2. 搜索 hook 与纯函数

- [ ] 2.1 新增 `web/src/terminal-search.ts`：查询校验（空值、正则语法）、结果文案（`i/n`、`1000+`、无匹配）、范围文案（回看行数、`historyTruncated`、alternate、回放提示），保持 DOM 无关
- [ ] 2.2 `web/src/hooks/terminal.svelte.ts`：在 `mountTerminal` 中加载 `SearchAddon`，dispose 时释放；暴露 `findNext` / `findPrevious` / `clearFind`、`findResults`、`bufferType`、`scrollback`
- [ ] 2.3 订阅 `onDidChangeResults` 更新结果状态；订阅 `term.buffer.onBufferChange`，切换 normal/alternate 时清除高亮、计数与选区并保留查询词
- [ ] 2.4 查询选项传给 addon（`caseSensitive`、`wholeWord`、`regex`、`incremental`、`decorations`），以启用高亮并取得 `resultIndex` / `resultCount`；空查询清除高亮；无效正则不调用 addon
- [ ] 2.5 为 `terminal-socket.svelte.ts` 的全量回放增加搜索重置钩子（或在 `term.reset()` 后由 hook 处理），清除高亮、计数与选区但保留查询词；增量续接不清空
- [ ] 2.6 `clear()` 同时清除高亮与计数；已退出会话仍允许查找，不以 `ready` 或连接状态禁用入口

## 3. 查找栏与页面接线

- [ ] 3.1 新增 `web/src/components/TerminalFindBar.svelte`：查询输入、`Aa` / `W` / `.*`、上一个/下一个、`i/n`、关闭、复制选中与范围/状态行；参照 `CodeSearchPanel.svelte` 的交互，但状态完全独立
- [ ] 3.2 查找栏固定在 `TerminalViewport` 的 `.terminal-area` 顶部 overlay，不改变终端尺寸；打开时隐藏原选区浮动工具栏，复制选中由查找栏提供
- [ ] 3.3 `TerminalHeader.svelte` 菜单新增“查找”，所有宽度可用；`Terminal.svelte` 保存 `findOpen` 并接线到 header 与 viewport；窄屏从代码视图打开时先切回终端
- [ ] 3.4 焦点管理：打开时聚焦查询输入；Esc 与关闭按钮清理状态；桌面关闭后焦点回终端或草稿框，触摸端只收起键盘
- [ ] 3.5 无障碍：查找栏使用搜索语义与可访问名称，图标按钮带 tooltip，计数使用 `aria-live="polite"`，Tab 顺序合理
- [ ] 3.6 输入法：组合期间不查找，`compositionend` 后再查；`autocapitalize` / `autocorrect` / `spellcheck` 关闭

## 4. 快捷键与输入隔离

- [ ] 4.1 在终端工作区的终端列（包含 xterm 与草稿输入框、不包含代码面板）以捕获阶段监听 keydown，识别 `Ctrl+Shift+F` 与 macOS 的 `⌘F`，`preventDefault` / `stopPropagation` 后打开查找
- [ ] 4.2 保持 `Ctrl+F` 不被拦截，继续由 xterm 作为 `forward-char` 发送；不在 `window` 上监听，避免影响代码阅读器
- [ ] 4.3 检查所有查找、移动、复制与关闭路径都不调用 `send` / `term.input` / `paste` / 草稿发送，不产生 frame 0 输入帧，不改变控制权

## 5. 范围、截断与回放状态

- [ ] 5.1 范围行始终显示“当前终端缓冲（最多回看 10000 行）”，行数取实际 `scrollback`，不表述为完整服务端缓冲或完整执行记录
- [ ] 5.2 `historyTruncated` 为真时追加“服务端已覆盖更早输出”；不提示可恢复已覆盖内容
- [ ] 5.3 `alternate` 缓冲显示“全屏程序：仅当前屏幕”，切回 normal 后重新查找，不自动跳转
- [ ] 5.4 全量回放期间禁用上一个/下一个并显示“回放完成，按 Enter 重新查找”；增量续接保留结果且不自动滚动视图
- [ ] 5.5 关闭查找栏清除高亮、计数与选区并保留查询词；再次打开不自动跳转

## 6. 测试

- [ ] 6.1 新增 `web/test/terminal-search.test.ts`：查询校验、结果文案、范围文案（截断/alternate/回放）与空查询
- [ ] 6.2 扩展 `web/test/browser/TerminalRendering.test.ts` 与 `TerminalHarness.svelte`：`Ctrl+Shift+F` 打开且不发送 `\x06`、普通 `Ctrl+F` 仍发送 `^F`、草稿模式可打开、匹配计数与前后移动、折行/宽字符、选项切换、无效正则、alternate 切换清空、回放清空后查询词保留、关闭清高亮、全程 `socket.sent` 不含查找输入
- [ ] 6.3 扩展 `web/test/e2e/terminal.spec.ts`：真实输出中查找唯一错误词，桌面快捷键与 Pixel 7 菜单入口，复制选中得到可读文本，关闭后 shell 仍能执行命令
- [ ] 6.4 运行目标 Vitest 与 Playwright（桌面 + Pixel 7），确认没有新增输入帧或终端尺寸变化

## 7. 文档、用户故事与验收

- [ ] 7.1 `docs/usage.md` 终端一节补充查找入口、快捷键、范围与截断/全屏语义，说明只处理仍保留的缓冲
- [ ] 7.2 `docs/architecture.md` 前端一节补充“查找完全在浏览器端基于 xterm 活动缓冲，无服务端接口”
- [ ] 7.3 `docs/user-stories.md` 新增 US-D11“查找终端输出”并更新组内依据与功能索引；不复用或改号现有故事
- [ ] 7.4 运行 `pnpm --dir web check`、`pnpm --dir web lint`、`pnpm --dir web check:arch` 与目标测试
- [ ] 7.5 `openspec validate add-terminal-buffer-find --strict --no-interactive` 通过
- [ ] 7.6 UI 变更的 PR 描述附桌面/手机与亮/暗主题截图，使用隔离样例数据
