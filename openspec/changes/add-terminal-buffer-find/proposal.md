## Why

#70 要求在仍保留的终端输出中查找文本、在匹配之间移动，并把选中的可读片段带到编辑器、问题记录或其他命令。当前终端只能滚动浏览、复制选中文字或复制全部缓冲；大量输出中定位一条错误信息仍要反复翻屏，手机上尤其费时。

服务端按原始字节保存缓冲（默认 2 MiB，可配置），不做终端仿真，`GET /api/sessions/{id}/buffer` 返回的是含 ANSI 与光标控制序列的字节，不能直接当可读文本搜索。xterm 已把回放字节解析成 `buffer.active`，其中包含当前屏与最多 10000 行回看，并提供折行、宽字符与选区映射。因此查找与选段复制应完全在浏览器端基于该缓冲完成，服务端零改动。

本变更只做查找与“复制选中片段”。已有的“复制全部缓冲”继续返回服务端原始字节，含义不变；完整可读文本导出后置为独立变更，避免把查找拖成导出与文件下载设计。

## What Changes

- 引入官方 `@xterm/addon-search@^0.16.0`（与当前 `@xterm/xterm@6.0.0` 同一发布提交 `f447274`），由 addon 处理折行、宽字符、跨行匹配、滚动与选区映射。
- 在终端 hook 中加载搜索 addon，暴露 `findNext` / `findPrevious` / `clearFind`、匹配计数、缓冲类型与回看行数；回放、清屏、关闭及缓冲切换时显式清理装饰、xterm 选区与响应式计数；增量续接的缺失尾部和空 append 分支都保留搜索视口。
- 新增终端专用查找栏：查询输入、区分大小写、全词、正则、上一个/下一个、`i/n` 计数、关闭与“复制选中”；输入框位于 xterm 之外，不改变终端尺寸。
- 新查询从当前视图附近定位；无匹配清除选区，无效正则保留选区；关闭后保留查询，重新打开不自动搜索。计数达到 1000 时统一显示“至少 1000 个匹配（仅统计前 1000 个）”，不把饱和计数误报为精确总数。
- 桌面用 `Ctrl+Shift+F` 打开，macOS 额外支持 `⌘F`；移动端从终端菜单进入。`Ctrl+F` 保持 readline 的 `forward-char`，继续作为终端输入发送，不被吞掉。
- 查找栏固定说明范围是“当前终端缓冲（最多回看 10000 行）”；服务端截断时追加“服务端已覆盖更早输出”；全屏 TUI 的 alternate buffer 追加“全屏程序：仅当前屏幕”。
- 命中后复用现有选区与“复制选中”，得到可读文本；不新增导出 API、文件下载或服务端文本接口。
- 查找、关闭、复制选中都不向 PTY 发送输入，不写入终端输入帧，也不改变实时输出。
- 不做持久索引、持续录制、命令分块、自动上传日志，不修改 `/api/sessions/{id}/buffer`、代码阅读器搜索或会话列表搜索。

## Capabilities

### New Capabilities

- `terminal-buffer-find`：在客户端 xterm 活动缓冲中查找文本、在匹配间移动、说明可搜索范围与截断语义，并通过现有选区复制可读片段。

### Modified Capabilities

无。

## Impact

- 前端：`web/package.json`、`web/pnpm-lock.yaml`、`web/src/hooks/terminal.svelte.ts`、`web/src/hooks/terminal-socket.svelte.ts`、`web/src/hooks/terminal-find-shortcut.ts`（新增）、`web/src/components/TerminalFindBar.svelte`（新增）、`TerminalHeader.svelte`、`TerminalViewport.svelte`、`TerminalWorkspace.svelte`、`web/src/pages/Terminal.svelte`、`web/src/terminal-search.ts`（新增纯函数）。既有输入路由与 renderer 的 10000 行回看配置不变。
- 测试：`web/test/terminal-search.test.ts`（新增）、`web/test/browser/TerminalRendering.test.ts` 与 `TerminalHarness.svelte`、`web/test/e2e/terminal.spec.ts`。
- 文档与故事：`docs/usage.md`、`docs/architecture.md`、`docs/user-stories.md`。
- 不涉及：Go 服务端、`/api/sessions/{id}/buffer`、会话生命周期、控制权、认证或 Origin 策略。
