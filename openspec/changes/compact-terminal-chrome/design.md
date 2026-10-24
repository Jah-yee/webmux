## Context

动机和实测数据见 proposal.md 的 Why 部分；行为契约见各 delta spec。与实现相关的现状如下：

- 终端顶栏在 `web/src/pages/Terminal.svelte`。页面已有两套宽度判断：JS 中的 `desktop`（`min-width:1024px`，决定左右分栏还是“终端 / 代码”切换），以及 `styles.css` 中的 `(any-pointer: coarse), (max-width: 639px)`（控件触控尺寸）。快捷指令入口的显隐完全由 CSS 决定（`.quick-commands-header`）。
- 代码区由 `CodePanel.svelte`（工作区栏、标签栏）、`CodeNavigator.svelte`、`CodeDocumentView.svelte`（文档栏、操作栏）、`CodeDiffReader.svelte`（Diff 工具栏、选择提示）、`CodeReader.svelte`（阅读工具栏）和 `CodeImageView.svelte`（图片工具栏）组成。代码区可以弹到独立窗口（Document Picture-in-Picture 或普通弹窗），这时适用该窗口自己的视口宽度。
- #114（本提案之后合入 main）已经精简了 Diff：布局按阅读宽度自动选择并排或统一，固定显示行号，去掉了统一/并排、行号开关和“跳转行”按钮（保留 Ctrl/⌘+Alt+G）；文字 Diff 按选中行自动确定引用版本，不再提供引用来源选择；两侧完整文件改为一个“完整文件”菜单（“修改前的文件 / 修改后的文件”，在新标签中打开）；文件与 Diff 的切换按钮只显示下一步操作（“查看 Diff / 返回文件”）；Diff 未选行时已不显示“引用整个文件”。本变更以这些行为为基础，不恢复被移除的控件，也不引入分段视图控件。
- `ui/` 下已有 `dropdown-menu`、`tabs`、`drawer`、`popover` 等 shadcn-svelte 组件。
- `terminal.svelte.ts` 只暴露 `scrollToBottom()`，不暴露当前是否位于底部。观察者（非控制方）按控制方网格渲染，网格超出宿主时平移的是宿主 `.terminal-host.observing` 的 `scrollTop`，xterm 缓冲此时仍在底部（`touch.ts`）。
- 已有的 iOS 与主屏幕 Web App 适配：`.terminal-page` 是固定定位容器，高度取 `visualViewport` 高度（`viewport.svelte.ts`），四边用 `env(safe-area-inset-*)` 留出安全区；`display-mode: standalone` 下给状态栏区域铺上顶栏背景色，以避开 iOS 26 起顶部约 40pt 的模糊层。`toggleFullscreen` 在 Fullscreen API 不可用时已经显示“添加到主屏幕”的提示。`popoutAvailable()` 的条件是“有 Document PiP，或有精细指针”。软键栏用 `onmousedown` 调 `preventDefault` 保住焦点，避免收起键盘。
- 兼容基线为 iOS/iPadOS 27 上的 Safari（WebKit 27）及其主屏幕 Web App。按 firt.dev 的 iOS PWA 兼容表（2026-10-13 更新）：iPhone 不支持 Fullscreen API（只有 iPad 支持），manifest 的 `display: fullscreen` 会回退为 `standalone`，没有 Virtual Keyboard API。现有代码已按 WebKit 不支持 Document Picture-in-Picture 处理（`codePopout.ts` 走 `window.open` 回退）。

## Goals / Non-Goals

**Goals:**

- 窄屏的显隐只用 CSS 断点（Tailwind 的 `max-sm:` / `sm:`，即 640 px）实现，不新增 JS 布局状态，避免首帧闪烁，也与现有 `.quick-commands-header` 规则一致。
- 菜单项与原按钮共用同一个处理函数和禁用条件，不复制逻辑。
- 宽屏终端顶栏的按钮集合与顺序不变。

**Non-Goals:**

- 不调整 1024 px 分栏断点，不改变 640–1023 px 平板宽度的“终端 / 代码”切换行为。
- 不改动代码导航面板内部的搜索、过滤控件。
- 不改变 #114 确定的 Diff 控件集合与引用版本推断。
- 不引入新的图标库或 UI 依赖。

## Decisions

### 1. 窄屏断点用 640 px，不按指针类型判断

顶栏收纳与否取决于可用宽度，而不是输入方式：横屏手机、带触屏的笔记本宽度都够，不应该被收纳。640 px 与 `docs/design-system.md` 中“小于 640px 的窗口”控件规则一致。图片的放大/缩小按钮是例外：只有在有双指缩放替代时才能隐藏，所以按“窄屏且粗指针”判断（`max-sm:any-pointer-coarse:hidden`）。

备选：沿用 `(any-pointer: coarse)`。会让宽屏触屏设备也丢失顶栏按钮，不采用。

### 2. 同一份标记，按断点切换按钮与菜单项

顶栏中的全屏、上传、滚到底按钮加 `max-sm:hidden`；终端菜单顶部新增对应菜单项和一条分隔线，均加 `sm:hidden`。两处调用同一函数（`toggleFullscreen`、`fileInput.click()`、`scrollToLatest`），上传的禁用条件抽成 `$derived` 后共享。

全屏入口另有一个能力条件 `fullscreenOffered({ supported, standalone }) = supported || !standalone`：`supported` 沿用 `toggleFullscreen` 现有的检测（`document.fullscreenEnabled` 且 `requestFullscreen` 为函数），`standalone` 为 `matchMedia("(display-mode: standalone)")`。这两项在页面运行期间不变，挂载时计算一次。条件为假时，按钮和菜单项都不渲染。Safari 标签页中不支持全屏时保留入口，复用现有提示文字。判断函数放在 `web/src/capabilities.ts`，接收能力参数，便于单元测试。

备选：用 JS 媒体查询只渲染其中一种。需要新增状态并处理变更事件，与快捷指令入口的现有做法不一致，不采用。

### 3. 连接状态并入状态点

连接文字只在 `t.connection !== "connected"` 时渲染，并加 `whitespace-nowrap`。状态点的 `aria-label` / `title` 改为 `${sessionStatusText(t.info)} · ${连接状态文字}`。会话信息尚未加载、没有状态点时，仍单独显示连接文字，保证状态可见。连接文字放在标题与状态点之间：文字出现或消失时只改变可伸缩的标题宽度，状态点和右侧按钮不移动，不产生布局偏移（性能测试对布局偏移的要求为 0）。

### 4. “回到最新”按钮的状态来源

在 `terminal.svelte.ts` 中新增 `$state` 字段 `atBottom`，由 `updateBottom()` 计算：xterm 视口位于缓冲底部（`viewportY >= baseY`），并且在观察模式下宿主也已滚动到底部（`scrollTop + clientHeight >= scrollHeight - 1`）。`updateBottom()` 在 xterm 的 `onScroll`、`onWriteParsed`、宿主的 `scroll` 事件以及每次 `safeFit()` 后运行。触屏滚动经 `touch.ts` 调用 xterm 的滚动接口，同样会触发 `onScroll`；观察者平移宿主会触发宿主的 `scroll`。`scrollToBottom()` 在观察模式下同时把宿主滚到底部。

`Terminal.svelte` 在 `.terminal-area` 内绝对定位一个带图标和“回到最新”文字的 `Button`（`sm:hidden`），显示条件为 `!t.atBottom && !t.restoring && 终端视图可见`。点击调用 `t.scrollToBottom()`；按钮沿用软键栏的 `onmousedown` → `preventDefault` 做法，不抢 xterm 焦点，也不收起已弹出的键盘。`.terminal-area` 位于已扣除安全区、高度跟随 `visualViewport` 的 `.terminal-page` 内，并在软键栏上方，所以按钮自然在键盘上方，也不会落到主屏幕指示条下面。

备选：监听 `.xterm-viewport` 的 DOM scroll 事件。依赖 xterm 内部 DOM 结构，不采用。

### 5. 视图切换沿用 #114 的“下一步操作”形式

原提案计划用“文件 / Diff”“Diff / 旧侧 / 新侧”分段控件标明当前视图。#114 已改为“查看 Diff / 返回文件”按钮加“完整文件”菜单，并在 US-I08 中写明“只展示下一步操作”。本变更沿用这一形式：宽屏保留这些控件，窄屏把它们作为代码区菜单项。两侧完整文件仍在新标签中打开（标签名带 `index` / `head` / `worktree` 来源），当前位置由标签与操作栏左侧的“修改前/修改后 · 来源”标明，不需要在同一文档内切换视图状态。

### 6. 窄屏代码区菜单

`CodePanel` 标签行末尾新增 ⋯ `DropdownMenu`（触发按钮 `sm:hidden`，名称“代码菜单”）。菜单内容由 `CodeDocumentView` 以顶层 snippet `documentMenu` 提供，经 `bind:menu` 存入对应标签对象；`CodePanel` 只渲染当前标签的 snippet，不了解文档内部状态。JS 无法直接读取 `env()`，所以在 `:root` 上定义 `--safe-top` / `--safe-bottom`，打开菜单时用 `getComputedStyle` 读出数值，加到 `collisionPadding` 的上下两边；菜单内容沿用 shadcn 组件的可用高度上限和内部滚动。菜单项：

- `DropdownMenu.Label`：文档完整路径（`break-all`），重命名时附双方路径。
- “查看 Diff / 返回文件”（普通文件）。
- SVG 显示方式单选（预览 / 源码）。
- 图片 Diff 的预览来源单选（修改后 / 修改前，带来源标签；不存在的一侧禁用）。
- “完整文件”分组：修改前的文件 / 修改后的文件（与宽屏菜单共用 `fullItems` snippet）。
- 刷新文档。刷新全部已在同一行常驻，不重复。

文档栏只隐藏操作行与重命名行；图片变化与大小、更新/删除/过期/检测失败状态及其“刷新文档”、不可完整展示的原因、SVG 预览回退说明在窄屏仍直接显示。没有这些提示时，整块文档栏在窄屏隐藏。

Diff 工具栏在 #114 后只有增删统计、查找和自动换行，单行即可容纳，不再把换行开关移入菜单。

备选：使用底部 `drawer`。选项多时更像设置面板，会额外占用一层交互；下拉菜单与终端菜单一致，不采用。

### 7. 工作区栏与标签栏合并

`CodePanel` 的工作区栏与标签列表合并为一个 flex 行：导航开关、`min-w-0 flex-1 overflow-x-auto` 的标签条、刷新全部、独立窗口（`max-sm:hidden`）、代码区菜单。根路径移到 `CodeNavigator` 顶部（末尾优先显示），同时拼入刷新全部按钮的 `title`。导航状态异常（`navStatus`）时，提示与“刷新导航”在导航面板顶部替代根路径；导航收起时，导航开关显示提醒点，`title` 带上状态文字。

### 8. 操作栏

`CodeDocumentView` 的操作栏改为单行：左侧是一个 `flex-1 basis-0 truncate` 的文字区，依次为 Diff 的引用版本（#114 已有）和选择方式提示（`max-sm:hidden`），右侧是按钮。零 basis 让文字只省略、不把按钮挤到下一行。提示由 `CodeDiffReader` / `CodeReader` 通过可绑定的 `hint` 属性提供，阅读器卸载时清空；两者原来的提示行/提示文字移除。“引用整个文件”的条件统一为“存在行选择或无效跨侧选择”，普通文件与 Diff 一致。

### 9. 独立窗口入口

`popoutOffered({ documentPictureInPicture, finePointer, touch, standalone }) = documentPictureInPicture || (finePointer && !(standalone && touch))`，`touch` 为 `any-pointer: coarse`。这样手机与带触控板 iPad 的主屏幕 Web App 隐藏入口；macOS Safari 的 Web App（同样是 standalone，但没有触摸屏）保留普通弹出窗口。

### 10. iOS 27 与主屏幕 Web App 的限制

| 限制 | 处理 |
|---|---|
| iPhone 没有 Fullscreen API | 见决策 2：Safari 标签页保留入口并提示添加到主屏幕；主屏幕 Web App 隐藏入口 |
| 主屏幕 Web App 没有地址栏和后退按钮 | 返回会话列表按钮在任何宽度都常驻；终端菜单保留“重新同步”，代替浏览器的刷新 |
| 没有 Document PiP；主屏幕 Web App 中 `window.open` 会打开 App 内浏览器页面（推断，需真机确认） | 见决策 9 |
| 触屏没有悬停提示 | 浮动按钮带可见文字；纯图标控件保留 `aria-label`，tooltip 只作为宽屏补充 |
| 字号小于 16px 的表单控件在聚焦时会让页面放大 | 窄屏图片预览来源改为菜单单选项；宽屏 `<select>` 不变 |
| 没有 Virtual Keyboard API，键盘高度只能从 `visualViewport` 得到 | 浮动按钮和菜单都位于跟随 `visualViewport` 的容器内，不自行估算键盘高度 |
| 状态栏使用 black-translucent，iOS 26 起顶部有模糊层 | 顶栏仍是 `.terminal-page` 的第一个子元素，沿用现有安全区留白与背景色，不在顶栏上方插入任何内容 |
| 带触控板的 iPad 同时匹配 `any-pointer: coarse` 和 `pointer: fine` | 图片 ＋/− 只在“窄屏且粗指针”时隐藏，触控板捏合仍按 Ctrl+滚轮缩放 |

兼容基线是 iOS 27，可以直接使用 `:has()`、`dvh`、`field-sizing` 等现有代码已在用的特性，不为更早的 iOS 写降级代码。

## Risks / Trade-offs

- [窄屏菜单让低频操作多一次点击] → 只收纳实测低频的操作；滚到底这类高频操作有浮动按钮补偿。
- [现有 E2E / 浏览器测试依赖按钮名称与位置] → 同一变更中更新选择器，新增 320/393/1440 px 布局断言；菜单项保留原可访问名称，减少改动面。
- [连接正常时没有可见文字] → 状态点的可访问名称与提示包含连接状态；异常状态仍显示文字。
- [窄屏去掉选择方式提示后，触屏用户少一处“点击代码行，点 ＋”的引导] → 点击代码行后浮出的 ＋ 按钮本身带可访问名称；宽屏触屏设备仍显示提示。
- [独立窗口宽度小于 640 px 时也会套用窄屏收纳] → 这与窗口实际可用空间一致，可以接受；独立窗口入口本身在窄屏主页面不显示。
- [`code-image-preview` 尚未归档，本变更修改其要求] → 归档顺序必须是先 `add-code-image-preview`，再本变更；tasks 中写明。
- [移动端 Chromium 模拟不能替代真机，Playwright 的 `emulateMedia` 也不能模拟 `display-mode: standalone`] → 能力条件（`fullscreenOffered`、`popoutOffered`）是接收能力参数的纯函数，用单元测试覆盖各种组合；真机在 iOS 27 iPhone 的 Safari 标签页与主屏幕 Web App、iPadOS 27（含触控板）和 Android Chrome 上，检查浮动按钮与键盘、菜单安全区、全屏入口、独立窗口入口和图片双指缩放。
- [iOS 26 起 Safari 标签页底部的浮动工具栏可能盖住页面底部] → 浮动按钮位于软键栏上方，不比现有软键栏更靠下；真机确认，如有遮挡再按 `visualViewport` 调整（推断，尚未验证）。

## Migration Plan

纯前端变更，随下一次构建发布，无数据或配置迁移。回滚即还原前端提交。
