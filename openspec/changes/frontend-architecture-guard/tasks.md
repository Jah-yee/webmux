## 1. 守卫工具链与基线（首个 draft PR）

- [x] 1.1 `web/package.json`：新增 `eslint`、`@shadcn/lint`、`eslint-plugin-svelte`、`svelte-eslint-parser`、`@typescript-eslint/parser`、`dependency-cruiser`、`jscpd`、`knip` 开发依赖，用 pnpm 12.11.2 更新锁文件，构建脚本白名单不变
- [x] 1.2 `web/eslint.config.mjs`：只加载架构规则（`@shadcn/lint` 六条规则、`svelte/no-restricted-html-elements`、原语/变体工具/`@tabler/icons-svelte` 导入限制），`ui/` 目录豁免，保留 `CardTitle`、`CardContent` 契约
- [x] 1.3 `web/.dependency-cruiser.cjs`：按扁平目录定义 `ui-is-a-leaf`、`lib-is-framework-free`、`only-app-composes-pages`、`components-do-not-know-pages`、`hooks-do-not-render`、`domain-does-not-render`、`primitives-only-in-ui`、`variants-only-in-ui`、`no-circular`、`no-orphans`
- [x] 1.4 `web/.jscpd.json`、`web/knip.json`：重复代码检查忽略测试、stories、`ui/` 与生成的字体 CSS；knip 的 `exports`/`types` 先为警告（2.1 完成后升为 `error`）
- [x] 1.5 `web/test/architecture.test.ts`：同名业务组件、与 `ui/` 同名的业务组件、跨 3 个以上文件复制的外观类；用临时文件确认能报出违规
- [x] 1.6 生成并格式化基线：`eslint-suppressions.json`（40 个文件、225 处）、`.dependency-cruiser-known-violations.json`（4 处）、`.jscpd-baseline.json`（3 处）
- [x] 1.7 `arch:*`、`check:arch`、`arch:ratchet` 脚本；`make check` 与 CI 格式和构建 job 运行 `check:arch`
- [x] 1.8 文档：新增 `docs/frontend-architecture.md`，更新 `AGENTS.md`、`CONTRIBUTING.md`、`docs/design-system.md`，在 `docs/user-stories.md` 新增 US-L06

## 2. 不改变 UI 的治理

- [x] 2.1 删除 knip 报告的未使用导出（`api.ts` 的 `statusText`、`attentionText`，`code-diff-model.ts` 的 `DiffResultSchema`，`code-diff.ts` 的 `DIFF_BUDGET_MS`，`fonts.ts` 的 `terminalFontFamily`，`homeWorktrees.ts` 的 `pinnedIds`，`launchConfigs.ts` 的 `LAUNCH_NAME_MAX`、`LAUNCH_ARGS_MAX`、`LAUNCH_VALUE_BYTES`、`envNameError`，`terminal-draft.svelte.ts` 的 `DraftSubmission`），只用于模块内部的改为不导出
- [x] 2.2 分层：`SearchSpec` 类型移入 `code-search.ts`；挂载 Svelte 组件的 `code-reader-panels.ts`、`codePopout.ts` 移到 `components/`；dependency-cruiser 基线清空，`code-search.test.ts` 仍在 Node 中运行
- [x] 2.3 Iconify 迁移：21 个业务文件从 `@tabler/icons-svelte` 改为 `~icons/tabler/*`，图标名逐个对照 `@iconify-json/tabler` 校验；未设尺寸类的图标都位于 Button、DropdownMenu.Item、Empty.Media 等以 `[&_svg:not([class*=size-])]` 设置尺寸的容器内，尺寸不变。`ui/` 中 6 个生成组件仍使用 `@tabler/icons-svelte`（`components.json` 的 `iconLibrary`），随生成器一并处理
- [x] 2.4 未知类名：`code-goto-line`、`code-reader`、`code-search`、`session-link` 改为 `data-*` 属性，同步源码与 `test/e2e`、`test/browser`、`test/performance` 中的选择器；未被引用的 `workspace-header`、`workspace-toolbar` 删除
- [x] 2.5 任意值（有完全等价内置工具类的部分，逐个编译确认 CSS 相同）：`[overflow-wrap:anywhere]` → `wrap-anywhere`、`[field-sizing:fixed]` → `field-sizing-fixed`、`[contain:strict]` → `contain-strict`、`[scrollbar-width:thin]` → `scrollbar-thin`、`flex-[2]` → `flex-2`、`focus-visible:ring-[3px]` → `focus-visible:ring-3`、`mt-[0.3125rem]` → `mt-1.25`（47 → 33 处）
- [x] 2.6 重复代码：`Settings.svelte` 的两个删除按钮与 `SoftKeys.svelte` 的三段发送键按钮改为 snippet；`design-tokens.css` 的 `.dark` 中与 `:root` 完全相同的 `--chart-1..5` 删除；jscpd 基线清空
- [x] 2.7 其余 33 处任意值：`no-arbitrary-values` 改为允许布局类（依赖视口的尺寸计算属于布局，不是设计令牌），31 处随之合规；排版与间距仍按刻度：分栏 Diff 列标题 `text-[11px]` 改为 `text-xs`（与设计系统 12px 元数据字号一致），抽屉底部安全区留白改为 `styles.css` 中的 `pb-safe-4`
- [x] 2.8 `cn-rtl-flip` 是 shadcn-svelte 为方向相关图标（子菜单箭头）预留的钩子，本项目未定义；在 `styles.css` 中定义为只在 RTL 下水平翻转的工具类，保留生成组件不改，LTR 下无变化

## 3. 设计系统补充 size / variant（需要截图）

- [x] 3.1 Button：新增 `variant="quiet"`（ScratchPanel 清空）、`raised`（CodeLineAction 与“回到最新”浮动按钮；原 `TerminalViewport` 的 `border` 因基础样式边框色透明而无效，现在显示边框，`CodeLineAction` 阴影由 `shadow-sm` 统一为 `shadow-md`）、`size="icon-touch"` / `size="sm-touch"`（窄屏 44px 触控）；`CodeLineAction` 的 `size-9`、`NewSessionSheet` 的 `text-xs`、`SessionFilters` 的 `gap-1.5` 与基础样式重复，直接删除；`SessionFilters` 改用 `size="xs"`（标签与计数间距由 6px 变为 4px）；`CodeSearchPanel` 的等宽字形与 `ScratchPanel` 取用按钮的强调色移到按钮内容；`RemoveWorktreeDialog` 的“进入强制删除确认”改为 `variant="destructive"`
- [x] 3.2 删除 `lib/touch-targets.ts`：图标与小按钮用 3.1 的触控尺寸，菜单项写静态的 `min-h-11 sm:min-h-9` 与 `any-pointer-coarse:min-h-11`，消除 11 处 `require-static-classes`
- [x] 3.3 Input、Textarea：新增 `mono`（11 处等宽覆盖）与 Textarea `variant="ghost"`（暂存条目原地编辑）；会话搜索框改用 InputGroup（图标与 `/` 提示作为 addon），InputGroup 基础样式与本项目 Input 对齐（`bg-card`、2px 焦点环）
- [x] 3.4 容器：`TabsContent`、`CommandList`、`ItemGroup`、`ItemActions`、`Collapsible`、`CollapsibleContent`、`DialogContent`、`DialogHeader`、`PopoverContent`、`ResponsiveDialog` 写入“可调整内边距与间距”的契约；`Empty` 新增 `outline` / `card`（删除 `.workspace-empty` 样式），`Command` 新增 `outline`，`Item` 新增 `card` 与 `size="flush"`（删除 `.session-row` 样式，测试改用 `data-session-id`）
- [x] 3.5 其余：`DropdownMenuLabel` 新增 `variant="title"`（代码区菜单的路径标题由常规字重变为 `font-medium`）；`CollapsibleTrigger`、`PopoverTrigger` 为无样式原语，写入契约；`Pane`、`ResizableHandle` 的布局钩子类写入契约；`DirectoryPicker` 的布局钩子改为 `data-*` 属性；`TokenHelp` 的行高移到各段落
- [x] 3.6 用同一组隔离数据在改动前后各截取 78 张截图（21 个界面，明暗主题、桌面 1280 px 与 Pixel 7），逐像素对比：改前两次截图之间的噪声（会话状态与随机分支名）之外，只有 3.1、3.3、3.5 列出的有意变化；PR 附前后截图，并运行 `arch:ratchet` 提交收缩后的基线

## 4. 原生控件换成设计系统组件（需要截图）

- [x] 4.1 `CodeNavigator.svelte`：路径搜索、定位与修改筛选改用 `Input size="sm"`；“显示忽略项”改用 Checkbox + `Label size="sm"`；修改来源改用 `NativeSelect size="sm"`；路径导航与文件行改用新增的 `ListButton`（`text` / `row`）。没有改用 DropdownMenu/Command：NativeSelect 保留手机系统选择器与现有 `selectOption` 测试
- [x] 4.2 `CodePanel.svelte` 的文档标签与关闭按钮改用 `ListButton`（`tab` / `tab-close`）；`CodeChangesList.svelte` 的虚拟列表行改用 `ListButton variant="row"`。Button 的 `wm-control` 在触控设备上有不受工具类控制的最小高度，会破坏固定行高，因此列表、标签与卡片中的可点击区域统一用不带控件尺寸的 `ListButton`
- [x] 4.3 `NewSessionSheet.svelte` 的仓库与工作树选择改用 `NativeSelect`，启动配置卡片改用 `ListButton variant="option"`；`CodeDocumentView.svelte` 的图片预览来源改用 `Label` + `NativeSelect size="sm"`（窄屏仍在代码菜单中用单选）。新增的 NativeSelect 与本项目 Input 对齐（`bg-card`、圆角、2px 焦点环、控件高度令牌），并加入手机 16px 字号规则
- [x] 4.4 用 shadcn-svelte 添加 Checkbox 与 NativeSelect（图标改用 Iconify，`bits-ui` 保持锁定版本）；`RemoveWorktreeDialog.svelte` 的确认复选框改用 Checkbox + Label；`TerminalHeader.svelte` 的隐藏文件输入改用 `Input type="file"`（同时去掉 Input 文件分支上的 `bind:value`：文件输入的值只能被清空，原绑定会在选择文件后写回路径并抛出 `InvalidStateError`）；`Settings.svelte` 两个列表的条目主体改用 `ListButton variant="content"`

## 5. 运行时几何值

- [x] 5.1 10 处 `style:` 改为 `style:--name` 加读取它的工具类（`h-(--list-height)`、`top-(--row-top)`、`transform-(--image-transform)`、`top-(--action-top)` 等）；终端页高度由 `.terminal-page` 读取 `--viewport-height`（缺省 `100dvh`），预览高度由 `styles.css` 的 `h-preview` 读取 `--preview-lines`；计算结果与原内联样式相同

## 6. 收尾

- [x] 6.1 三个基线文件均为空，所有检查按严格模式执行；契约、布局任意值策略、运行时尺寸做法与新增组件写入 `docs/frontend-architecture.md`。基线文件与 `arch:ratchet` 保留，仅用于以后引入带存量违规的新规则
- [x] 6.2 knip 的 `exports`、`types` 升为 `error`（2.1 完成后即升级）
- [x] 6.3 更新 US-L06、`AGENTS.md`、`CONTRIBUTING.md` 与 `docs/frontend-architecture.md` 中的基线说明；运行格式、Oxlint、svelte-check、`check:arch`、单元、浏览器、Storybook、E2E 与性能测试
