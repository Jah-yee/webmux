## 1. 终端顶栏

- [x] 1.1 `Terminal.svelte`：连接文字只在非 `connected` 时渲染并加 `whitespace-nowrap`；状态点的 `aria-label` / `title` 合并会话状态与连接状态；会话信息未加载时保留连接文字
- [x] 1.2 `Terminal.svelte`：全屏、上传、滚到底按钮加 `max-sm:hidden`；抽出共享的上传禁用条件 `$derived` 与 `scrollToLatest`
- [x] 1.3 `Terminal.svelte`：终端菜单顶部新增仅窄屏显示的“全屏/退出全屏”“上传文件”“滚动到最新位置”菜单项及分隔线，复用原处理函数（滚到底在窄屏代码视图下仍切回终端）
- [x] 1.4 确认快捷指令入口、草稿按钮与代码切换的相对位置和显示规则不变（`terminal-quick-commands`、`terminal-input-composer`）
- [x] 1.5 `capabilities.ts` / `Terminal.svelte`：新增 `fullscreenOffered`（支持 Fullscreen API，或不在 `display-mode: standalone` 中），为假时不渲染顶栏全屏按钮和菜单项；Safari 标签页不支持时保留入口并复用现有“添加到主屏幕”提示

## 2. 回到最新浮动按钮

- [x] 2.1 `terminal.svelte.ts`：新增 `atBottom` 状态，在 xterm `onScroll` / `onWriteParsed`、宿主 `scroll` 和 `safeFit()` 后更新：视口位于缓冲底部，且观察模式下宿主也在底部；`scrollToBottom()` 在观察模式下同时把宿主滚到底部
- [x] 2.2 `Terminal.svelte`：`.terminal-area` 内右下角新增 `sm:hidden` 浮动按钮（tabler 图标加可见文字“回到最新”），条件为未在底部、未在恢复且终端视图可见；点击调用 `t.scrollToBottom()`，沿用软键栏的 `onmousedown` → `preventDefault` 做法

## 3. 代码区工作区栏与标签栏

- [x] 3.1 `CodePanel.svelte`：将导航开关、标签条（行内横向滚动）、刷新全部、独立窗口合并为一行，独立窗口加 `max-sm:hidden`
- [x] 3.2 `CodeNavigator.svelte`：顶部显示代码根路径；`navStatus` 提示与“刷新导航”移入导航面板顶部；导航收起时导航开关显示提醒点；刷新全部按钮的提示包含根路径
- [x] 3.3 `CodePanel.svelte`：标签行末尾新增 `sm:hidden` 的代码区菜单，菜单内容由激活的 `CodeDocumentView` 以 snippet 提供；`collisionPadding` 计入安全区（`styles.css` 定义 `--safe-top` / `--safe-bottom`），菜单高度受可用高度限制并可在内部滚动
- [x] 3.4 `capabilities.ts` / `codePopout.ts`：`popoutAvailable()` 改为“有 Document PiP，或有精细指针且不是带触摸屏设备上的 `display-mode: standalone`”

## 4. 文档栏

- [x] 4.1 沿用 #114 的“查看 Diff / 返回文件”和“完整文件”菜单，不引入分段视图控件
- [x] 4.2 `CodeDocumentView.svelte`：宽屏文档栏保持单行（路径末尾优先、查看 Diff / 返回文件、SVG 预览 / 源码、图片预览来源、完整文件菜单），操作行与重命名行在窄屏隐藏
- [x] 4.3 `CodeDocumentView.svelte`：为代码区菜单提供完整路径（含重命名双方）、查看 Diff / 返回文件、SVG 显示方式单选、图片预览来源单选、修改前 / 修改后的文件、刷新文档
- [x] 4.4 `CodeDocumentView.svelte`：图片变化与大小、内容已更新等状态及“刷新文档”、不可展示原因、SVG 回退说明在窄屏仍直接显示；无提示时整块文档栏在窄屏隐藏
- [x] 4.5 `CodeDiffReader.svelte`：Diff 工具栏（增删统计、查找、换行）在 #114 后已为单行，保持不变

## 5. 操作栏与图片工具栏

- [x] 5.1 `CodeDocumentView.svelte`：操作栏单行；“引用整个文件”仅在存在行选择或无效跨侧选择时渲染（普通文件与 Diff 一致）；左侧显示引用版本与选择提示（提示 `max-sm:hidden`），文字省略而不挤压按钮
- [x] 5.2 `CodeReader.svelte`、`CodeDiffReader.svelte`：移除阅读器内的提示文字与提示行，经可绑定的 `hint` 交给操作栏
- [x] 5.3 `CodeImageView.svelte`：工具栏单行（信息省略、缩放比例、适应窗口、1:1）；放大/缩小在窄屏且粗指针时隐藏；下载仍在操作栏

## 6. 测试

- [x] 6.1 更新 `web/test/e2e/` 与 `web/test/browser/` 中依赖“已连接”文字、已移入菜单的按钮（上传文件、滚动到最新位置、完整文件、查看 Diff、SVG 源码、预览来源）或提示位置的用例
- [x] 6.2 E2E：320 px 与 393 px 视口下，终端视图和代码视图的顶栏单行、标题可见、无横向溢出；终端菜单的上传、全屏、滚到底菜单项可用，上传菜单项打开文件选择
- [x] 6.3 E2E：393×851 打开文本 Diff，代码内容区高度不少于视口 70%；代码区菜单显示完整路径、位于视口内，并能打开修改前的文件
- [x] 6.4 浏览器测试：控制方滚动离开底部时报告查看历史，新输出不拉回，回到最新后恢复；观察者平移宿主离开底部同样报告，回到最新后宿主滚到底部。E2E：窄屏滑动查看历史后出现“回到最新”，点击后回到最新输出且不取得终端焦点
- [x] 6.5 浏览器测试：“引用整个文件”在无选择时不渲染，选择后出现，点击后清除选择（沿用现有 Diff 用例）
- [x] 6.6 E2E：1440 px 视口下终端顶栏按钮可见；标签行与刷新全部同一行，文档路径与完整文件同一行，根路径在导航面板，不显示代码区菜单
- [x] 6.7 单元测试：`fullscreenOffered` 与 `popoutOffered` 是接收能力参数的纯函数，覆盖“支持或不支持全屏 × 标签页或主屏幕 Web App”“有无 PiP × 精细或粗指针 × 有无触摸屏 × 标签页或主屏幕 Web App”的组合
- [x] 6.8 运行 `make check` 与 `make test`

## 7. 文档与交付

- [x] 7.1 更新 `docs/user-stories.md`：US-D03（连接正常时顶栏不显示文字，状态点名称含连接状态）、US-D07（窄屏浮动回到最新）、US-D08（窄屏全屏入口在菜单中，主屏幕 Web App 不支持时隐藏）、新增 US-H08（窄屏紧凑顶栏，含主屏幕 Web App）、US-I01、US-I08、US-I09（引用操作栏）、US-I13（带触摸屏的主屏幕 Web App 中隐藏独立窗口入口）、US-I15（窄屏图片工具栏），同步功能覆盖索引与依据链接
- [x] 7.2 更新 `docs/design-system.md` 的紧凑工具栏规则（窄屏常驻操作、菜单收纳、单行工具栏）与 `docs/usage.md`
- [x] 7.3 PR 附 1440 px 与 393 px、明暗主题的改前/改后截图（终端视图、代码文件、Diff、图片）
- [ ] 7.4 真机检查，设备为 iOS 27 iPhone（Safari 标签页、主屏幕 Web App，竖屏和横屏）、iPadOS 27（含触控板，标签页与主屏幕 Web App）和 Android Chrome。检查项：顶栏位于状态栏下方且不被模糊层覆盖；全屏入口的显示与提示；浮动按钮在软键盘弹出时可见、点击不收起键盘、不被主屏幕指示条或 Safari 底部工具栏遮挡；代码区菜单在安全区内并可滚动，预览来源选择不触发页面放大；主屏幕 Web App 中不显示独立窗口入口；图片双指缩放不带动页面
- [ ] 7.5 归档顺序：先归档 `add-code-image-preview`，再归档本变更
