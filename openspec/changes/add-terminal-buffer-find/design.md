## Context

- 服务端按原始字节保存每会话输出（默认 2 MiB，可配置 `scrollback_bytes`），写满后覆盖最旧数据，不做终端仿真；`GET /api/sessions/{id}/buffer` 返回原始字节，含 ANSI 与光标控制序列。因此服务端缓冲不能直接作为可读文本搜索，也不新增服务端文本接口。
- 浏览器端 xterm 持有解析后的 `buffer.active`：当前屏加最多 10000 行回看（`web/src/hooks/terminal-renderer.ts:61`），提供折行、宽字符、选区与滚动映射。`/api/sessions/{id}/buffer` 的“复制全部缓冲”继续按原始字节工作，语义不变。
- `web/src/hooks/terminal.svelte.ts` 的 `useTerminal` 组合 renderer、socket、键盘/IME 输入与触摸手势，暴露 `selection`、`copySelection`、`clearSelection`、`historyTruncated` 和 `restoring`；`web/src/hooks/terminal-input.ts` 的 `attachCustomKeyEventHandler` 在草稿模式下会拦截全部按键，草稿焦点还可能完全不在 xterm 上。
- 全量回放在 `web/src/hooks/terminal-socket.svelte.ts` 的 `restore()` 中调用 `term.reset()`；增量续接不清空缓冲。`term.buffer.active.type` 为 `alternate` 时（全屏 TUI）没有回看，只有当前屏。
- 终端布局由 `Terminal.svelte` 组合 `TerminalHeader`、`TerminalWorkspace` 与 `TerminalViewport`；选区浮动工具栏位于 `TerminalViewport`，复制走现有 `copySelection`；`TerminalHeader` 菜单已有“复制全部缓冲”“粘贴”“清屏”等动作。代码面板在同页右侧（窄屏为嵌套视图），其查找栏与代码内查找保持独立。
- `historyTruncated` 来自 position 帧的 `truncated`（`internal/session/session.go`），只在回放/续传时更新，表示服务端环形缓冲已覆盖最早字节；它不代表客户端 10000 行回看是否已满，也不保证实时更新。

## Goals / Non-Goals

**Goals:**

- 在当前终端缓冲中查找文本，显示匹配总数与当前位置，并可在匹配间前后移动。
- 桌面键盘与移动端菜单都能打开查找；查找过程不向 PTY 发送任何输入。
- 明确说明可搜索范围、服务端截断与全屏程序限制，不把有限缓冲描述成完整执行记录。
- 通过现有选区复制可读片段，交给编辑器、问题记录或其他命令。
- 回放、清屏、切换全屏程序与关闭查找时，界面状态可预期且不残留误导性高亮。

**Non-Goals:**

- 不新增服务端搜索、文本接口或终端仿真；不改变 `/api/sessions/{id}/buffer` 的原始字节语义。
- 不做完整可读文本导出、文件下载、持久历史索引、持续录制、命令分块或自动上传日志。
- 不修改代码阅读器搜索、会话列表搜索或现有“复制全部缓冲”。
- 不捕获 `Ctrl+F`（readline 的 `forward-char`），也不改变任何终端的按键编码。
- 不保证恢复已覆盖内容；不把 TUI 屏幕更新解释为逐行日志。

## Decisions

### 1. 官方 search addon，纯浏览器端查找

- 采用 `@xterm/addon-search@^0.16.0`。其 `gitHead` 与当前 `@xterm/xterm@6.0.0` 同为 `f447274`，与 xterm 同步发布；`findNext` / `findPrevious` 会滚动并 `term.select` 选中匹配，正好复用现有选区复制。
- 不自行扫描 `buffer.active`：折行、宽字符、跨行匹配与选区映射由 addon 维护，自实现容易与 xterm 行为漂移。也不搜 `/buffer` 原始字节：其中包含控制序列，会重复或漏掉输出。
- `web/src/hooks/terminal.svelte.ts` 在 `mountTerminal` 中加载并持有 addon，dispose 时释放；新增 `findNext` / `findPrevious` / `clearFind`、`findResults`、`bufferType`、`scrollback` 与 `canFind`（`ready` 之外仍允许已退出会话查找）。
- 查询选项传给 addon：`caseSensitive`、`wholeWord`、`regex`、`incremental`。输入时增量查找；Enter 下一个、Shift+Enter 上一个；查询为空时清高亮与计数。

### 2. 匹配计数与高亮上限

- addon 默认 `highlightLimit = 1000`。只有传入 `decorations` 选项时，`onDidChangeResults` 才会给出 `resultIndex` 与 `resultCount`，且 `resultCount` 最多为 1000；界面显示 `i/n`，选中项超出前 1000 个时 `resultIndex === -1`，显示“1000+ 匹配”并说明只统计前 1000 个。
- 上一个/下一个不依赖计数列表，超过上限仍可继续移动；不为突破上限引入第二套扫描。
- 正则先本地校验语法；无效正则只显示行内错误，不调用 addon、不改变当前高亮。10k 行内的普通查找视为可接受；极端回溯可能短暂卡顿，v1 接受并记录在风险中。

### 3. 查找栏与入口

- 新增 `web/src/components/TerminalFindBar.svelte`，放在 `TerminalViewport` 的 `.terminal-area` 顶部 overlay，不占用终端网格空间、不触发 `ResizeObserver` 改 PTY 行列。样式参照 `CodeSearchPanel.svelte`，但状态与快捷键完全独立。
- 内容：查询输入、区分大小写（`Aa`）、全词（`W`）、正则（`.*`）、上一个/下一个、`i/n`、关闭、复制选中，以及固定的范围/状态行。所有图标按钮带可访问名称与 tooltip；Esc 关闭；计数用 `aria-live="polite"`。
- `TerminalHeader` 菜单新增“查找”（所有宽度可见）；桌面同时支持快捷键。窄屏在菜单中提供“复制选中”入口的需求由查找栏内部按钮满足，不新增第二个浮动工具栏。窄屏若当前显示代码视图，打开查找时先切回终端（与“滚动到最新”一致），确保查找栏真正可见。
- 查找栏打开时隐藏原选区浮动工具栏，复制选中改由查找栏提供。关闭查找栏时清除高亮、计数与选区，保留查询词，避免旧选区被误复制；重新选择文本后浮动工具栏按现有行为出现。

### 4. 快捷键与输入隔离

- 桌面入口为 `Ctrl+Shift+F`；macOS 额外接受 `⌘F`，并显式 `preventDefault`，避免触发浏览器原生查找。`Ctrl+F`（无 Shift、非 macOS）不拦截，继续由 shell 作为 `forward-char` 接收。这与现有 `Ctrl+Shift+C/V` 的约定一致。
- 快捷键在终端工作区“终端列”的捕获阶段处理，范围包含 xterm 与草稿输入框、不包含代码面板。这样草稿模式下也能打开查找，且不会抢代码阅读器的 `Ctrl/Cmd+F`；不在 `window` 上监听。
- 查找栏是终端之外的普通 `input`，焦点归属明确。查询输入、上一个/下一个、复制选中、关闭都不会调用 `send` / `term.input` / `paste`，不产生 frame 0 输入帧，也不改变实时输出。输入法组合期间不查找，`compositionend` 后再查；输入框设置 `autocapitalize="off"`、`autocorrect="off"`、`spellcheck="false"`。
- 关闭查找后，桌面把焦点还给打开查找前获得焦点的终端或草稿框；触摸端只收起键盘，不主动聚焦 xterm，避免误开软键盘。

### 5. 范围、截断与全屏程序

- 范围行始终写明“当前终端缓冲（最多回看 10000 行）”，行数取 xterm `scrollback` 实际配置，不声称是完整服务端缓冲或完整执行记录。
- `historyTruncated` 为真时追加“服务端已覆盖更早输出”。这是服务端环形缓冲的截断提示，与客户端 10000 行上限彼此独立，不能互相推断。
- `buffer.active.type === "alternate"` 时范围行改为“全屏程序：仅当前屏幕”，此时没有回看。订阅 `term.buffer.onBufferChange`：切换缓冲时清除高亮与计数、保留查询词；切回 normal 后由下一次查找重新定位，不自动跳转。
- 已退出会话仍可查找其最终缓冲；查找入口不以 `ready` 或连接状态禁用，只在终端尚未挂载时不可用。
- 无匹配时显示“无匹配”；查询为空时只显示范围行。状态行不承诺恢复已覆盖内容。

### 6. 回放、清屏与结果重置

- 在 `terminal-socket.svelte.ts` 的 `restore()` 调用 `term.reset()` 后通知搜索状态重置（通过 attach 回调或等价钩子）：清除高亮、计数与选区，保留查询词并显示“回放完成，按 Enter 重新查找”。全量回放期间 (`restoring`) 禁用上一个/下一个，避免在正在重建的缓冲上跳转。
- 增量续接不清空缓冲，保留查询与计数；addon 会在写入解析后自动重算，且不自动滚动（`noScroll`），避免实时输出把视图拽走。
- 用户执行“清屏”时同时清除高亮与计数，保留查询词；因为 `terminal.clear()` 只影响视图，不承诺搜索结果仍指向原内容。
- 关闭查找栏即调用 addon 的 `clearDecorations()` 清理高亮、计数与选区；再次打开恢复查询词但不自动跳转，用户按 Enter 或点下一个才开始。

### 7. 复制可读片段

- 命中后 xterm 的现有选区即为可读文本；查找栏“复制选中”调用现有 `copySelection`，与浮动工具栏走同一条 `copyToClipboard` 路径（含权限失败时的只读文本框回退）。
- “复制全部缓冲”保持不变：仍走服务端原始字节，不因查找而改为可读文本。完整可读文本导出留待独立变更。
- 复制动作不发送 PTY 输入、不改变控制权；观察端同样可以查找与复制。

### 8. 纯函数与测试边界

- 新增 `web/src/terminal-search.ts`，存放与 DOM 无关的逻辑：查询校验（空值、正则语法）、结果文案（`i/n`、`1000+`、无匹配）、范围文案（回看行数、截断、alternate、回放中）。便于 Node 单测，避免在 Svelte 组件里堆条件。
- 浏览器测试扩展现有 `TerminalHarness.svelte`：断言 `Ctrl+Shift+F` 打开查找且不发送 `\x06`、普通 `Ctrl+F` 仍发送 `^F`、匹配计数与前后移动、折行/宽字符匹配、选项切换、无效正则、alternate 切换清空、回放清空后保留查询词、关闭清高亮，以及全程 `socket.sent` 不含查找输入。
- E2E 在真实输出中查找唯一错误词，覆盖桌面快捷键与 Pixel 7 菜单入口，复制选中得到可读文本，关闭后 shell 仍能正常执行命令。
- 不修改 Go 代码，因此不新增 Go 测试；服务端 `/buffer` 行为由现有测试继续覆盖。

## Risks / Trade-offs

- [正则回溯阻塞 UI 线程] → 本地校验只保证语法；v1 接受 10k 行内普通查找足够快，保留 1000 高亮上限，不引入 Worker。
- [计数只到 1000] → 明确显示上限并说明，导航仍可用；不为此自写扫描。
- [客户端与 addon 版本耦合] → 固定与 xterm 同一发布提交，升级 xterm 时必须同步升级 addon 并重跑查找测试。
- [全屏 TUI 只有当前屏] → 范围行单独说明，避免用户误以为能查询完整历史。
- [服务端截断与客户端回看上限不同步] → 两类提示分开表述，始终标注客户端上限，不互相推断。
- [查找栏覆盖选区工具栏] → 查找栏内置复制选中，关闭后恢复浮动工具栏，避免功能丢失。
- [回放期间查询词保留但结果失效] → 明确提示按 Enter 重新查找，并禁用跳转，避免跳到旧位置。

## Migration Plan

- 无服务端或数据迁移；查找状态只存在于当前页面内存，不写入 `localStorage` 或配置。
- 前后端可独立发布：旧服务端提供相同字节流，新前端即可查找；未加载新前端时行为不变。
- 回滚即回退前端与依赖；服务端缓冲、复制全部缓冲与会话生命周期不受影响。

## Open Questions

- 无阻塞项。是否在后续单独变更中提供“导出可读全文到文件”沿用 #69 的下载体验；本变更不含该能力。
