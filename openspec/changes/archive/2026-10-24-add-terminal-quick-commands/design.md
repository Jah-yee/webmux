## Context

动机见 proposal.md。与实现相关的现状：

- 软键栏 `web/src/components/SoftKeys.svelte` 为 `<aside aria-label="终端软键">` 内单个 `.soft-row`（`overflow-x: auto`、不换行），仅在 `any-pointer: coarse` 或 `.terminal-page.force-soft-keys` 时显示（`web/src/styles.css`）。软键按钮在 `pointerdown` 上 `preventDefault` 以保留终端焦点。E2E `softkeys.spec.ts` 断言所有按钮同一行、320 px 下“粘贴”“唤起或收起键盘”完整可见。
- 终端顶栏（`Terminal.svelte`）依次为返回、标题、状态、全屏、上传、滚动到底、“草稿”、“代码”、“⋯”菜单；“强制显示底部工具栏”是“⋯”菜单中的 CheckboxItem，仅存于页面内存。
- 浏览器本地设置的既有模式是 `web/src/previewSettings.svelte.ts`：模块级 `$state`，通过 `api.ts` 的 `stored`/`store`（`localStorage`，键前缀 `webmux.`，异常吞掉）读写，Zod 契约位于 `web/src/schemas.ts`。设置页 `Settings.svelte` 用 `Card` 分组，页面声明“设置仅保存在当前浏览器”。
- `Terminal.svelte` 已有统一文本入口 `insertText(text, operation)` → `drafts.insert(draft, text, operation, t.paste)`：草稿模式写入草稿光标处，否则走 `t.paste`（xterm `paste`，按程序状态包装 bracketed paste；未连接/恢复中/已退出返回 `false`，并清除锁定修饰键）。`sendDraft` 已确立“先粘贴、成功后单独 `t.send("\r")`、部分成功单独提示”的语义。
- 弹出层焦点处理已有先例：粘贴对话框 `onCloseAutoFocus={focusTerminal}` 在触屏上不回焦，否则回焦到 `focusInput()`。
- 路由：`App.svelte` 以 `location.pathname` 匹配 `/s/[a-z2-7]{8}` 与 `/settings`；`api.ts` 的 `navigate` 用 `pushState`，查询串与 hash 会保留。
- UI 组件目录已有 DropdownMenu、Dialog、AlertDialog、Item、Empty、Badge、Input、Textarea、Label；没有 Switch/Checkbox。图标库为 `@tabler/icons-svelte`，`components.json` 已配置 shadcn-svelte CLI。
- WebMux 常经 VPN IP 以纯 HTTP 访问，非安全上下文下 `crypto.randomUUID` 不可用（代码中剪贴板已显式检查 `isSecureContext`）。

## Goals / Non-Goals

**Goals:**

- 发送路径完全复用 `insertText` 与 `sendDraft` 的语义，不新增第二套 PTY 写入或错误处理约定。
- 软键栏新增入口不破坏单行布局与既有按钮可见性；任一时刻只有一个入口可见。
- 所有界面由现有 shadcn-svelte 组件组合，唯一新增的 Switch 通过官方 CLI 生成。

**Non-Goals:**

- 服务端存储、跨设备同步、导入导出。
- 变量/占位符模板、按会话或 agent 过滤指令、键盘快捷键触发、最近使用排序。
- 删除后的“撤销”提示、空状态中的“恢复预置”。
- 拖拽排序。

## Decisions

### 0. 已与用户确认的产品决策

| 问题 | 结论 |
|---|---|
| 存储位置 | 仅当前浏览器 `localStorage` |
| 自动回车 | 每条可设；用户新建默认关闭 |
| 预置指令 | `/resume`、`/new`，无名称，自动回车开启；仅在从未保存过时出现 |
| 软键栏入口 | 最右侧固定，仅图标 |
| 图标 | `IconBolt`（⚡） |
| 桌面入口 | 底部工具栏隐藏时，在顶栏“草稿”按钮左侧显示 |
| 草稿模式 | 插入草稿光标处，不发送 |
| 删除 | 确认对话框 |

### 1. 数据契约与存储模块

`schemas.ts` 新增：

```ts
QuickCommandSchema = z.object({
  id: z.string().min(1).max(64),
  name: z.string().max(40),
  command: z.string().min(1).max(4096),   // 已规范化，非空白
  enter: z.boolean(),
});
QuickCommandsSchema = z.array(QuickCommandSchema).max(50);
```

列表契约同时校验所有 `id` 唯一；重复 ID 属于损坏数据，读取时整体降级为空列表，避免传给 Svelte keyed-each 后渲染失败。

新增 `web/src/quickCommands.svelte.ts`，结构对齐 `previewSettings.svelte.ts`：模块级 `quickCommands = $state({ list })`（导出的 `$state` 不能被重新赋值，因此包一层对象）；`saveQuickCommands(list)` 先 `parse` 再整体替换 `list` 并 `store`。不依赖 runes 的纯函数放在 `web/src/quickCommands.ts`：`parseQuickCommands(raw)`（下述三种读取情况）、`normalizeCommand(text)`（CRLF/CR→LF，去末尾换行）、`commandLabel(cmd)`（名称或正文首行）、`commandPreview(cmd)` 和 `newQuickCommandId()`，供设置页、菜单和 Node 环境的单元测试共用（Vitest Node 配置不编译 `.svelte.ts`）。

`parseQuickCommands(stored("quickCommands"))` 区分三种情况：

- 键不存在（`stored("quickCommands", "")` 返回空串）→ 返回预置 `[{id:"preset-resume", name:"", command:"/resume", enter:true}, {id:"preset-new", name:"", command:"/new", enter:true}]`，**不写回**。只要用户没有保存过，键就一直不存在，预置持续生效；任何一次保存（含保存空数组）都会写入键，之后不再出现预置。
- 键存在且 `safeParse` 成功 → 返回解析结果（可能为空数组）。
- 键存在但 JSON 或契约无效 → 返回 `[]`，不回写，直到用户下次保存覆盖。

模块在加载时注册一次 `window` 的 `storage` 事件，键为 `webmux.quickCommands`（或 `null`，即存储被清空）时重新解析，实现多标签页同步。`newQuickCommandId()` 用 `crypto.getRandomValues` 生成十六进制串（非安全上下文可用），仅作 Svelte 列表 key 与编辑定位。

- 备选：服务端 TOML 配置。需要新增写配置 API、改变“配置只在启动时加载”的模型，并扩大认证/Origin 面；用户已选择仅浏览器存储。
- 备选：首次加载时把预置写入存储。会让“从未保存过”与“保存过预置”无法区分，且在只读存储（隐私模式）下行为分叉；不采用。

### 2. 两个互补入口

入口按钮统一为 `Button variant="outline" size="icon-sm"` + `IconBolt`，`aria-label="快捷指令"`、`title="快捷指令"`。

- **软键栏**：`SoftKeys.svelte` 新增可选 snippet prop `trailing`，在 `.soft-row` 之后、`aside` 之内渲染，前面加竖向 `Separator`。`.soft-row` 已是 `flex: 1; min-width: 0`，trailing 区域 `shrink-0` 即固定在右侧、不参与滚动。SoftKeys 不感知快捷指令业务，菜单由 `Terminal.svelte` 注入。按钮尺寸与软键一致（`min-height: 36px; min-width: 40px`），仅图标约占 40 px，320 px 视口下仍留出足够软键区。
- **顶栏**：`Terminal.svelte` 在“草稿”按钮前渲染第二个菜单实例，外层加 `.quick-commands-header` 类。`styles.css` 用与软键栏完全对称的条件隐藏它：

  ```css
  .terminal-page.force-soft-keys .quick-commands-header { display: none; }
  @media (any-pointer: coarse) { .quick-commands-header { display: none; } }
  ```

  窄屏代码视图会隐藏包含软键栏的终端面板，因此在既有 `max-width: 1023px` 布局规则中，通过 `.terminal-page:has(.terminal-workspace.mobile-code) .quick-commands-header` 恢复顶栏入口。返回终端视图后继续按指针类型与 `force-soft-keys` 控制显隐，两处入口保持互斥，不新增 JS 指针判断。

- 备选：按屏宽（≥1024 px）显示顶栏入口。平板横屏是触屏且宽屏，会与软键栏入口同时出现；用户已选择以工具栏显隐为准。
- 备选：图标 + 文字。用户选择仅图标；可发现性由可访问名称、悬停提示和菜单顶部的去向标签弥补（见风险）。

### 3. 菜单组件 `QuickCommandMenu.svelte`

基于 `DropdownMenu`。Props：`commands`、`target: "terminal" | "draft"`、`unavailable: string`、`disabled: boolean`、`side: "top" | "bottom"`、`run(cmd)`、`manage()`、`onCloseAutoFocus`。软键栏实例传 `side="top"`，顶栏实例传 `side="bottom"`；均 `align="end"`。`Content` 限制 `max-h-[min(60dvh,24rem)] w-72 max-w-[calc(100vw-1rem)] overflow-y-auto`。

- 顶部 `DropdownMenu.Label`：`target === "draft"` 时“插入草稿”，否则“发送到终端”；`unavailable` 非空时在其下以 muted 文本显示原因。
- 条目：`DropdownMenu.Item`，两行布局（名称/首行 + 等宽截断预览；无名称时只显示一行等宽正文首行，多行正文附“…”），直接模式且 `enter` 时右侧 `DropdownMenu.Shortcut` 显示 `IconCornerDownLeft`（`aria-hidden`，可访问名称追加“并回车”）。触屏下条目 `min-h-11` 保证点击目标。`disabled={target === "terminal" && !!unavailable}`。
- 空状态：`DropdownMenu.Label`“还没有快捷指令”+ 一行说明，随后“添加快捷指令…”。
- 底部：`Separator` + “管理快捷指令…”（`IconSettings`）；“添加快捷指令…”与其同样调用 `manage()`，由 `Terminal.svelte` 导航到 `/settings?session=<会话 ID>#quick-commands`。
- 触发按钮不加 `pointerdown preventDefault`（会干扰 bits-ui 的打开逻辑）；菜单打开时焦点进入菜单是预期行为。关闭时 `onCloseAutoFocus` 复用 `focusTerminal`：阻止默认回焦，触屏不回焦，否则 `focusInput()`。

`unavailable` 在 `Terminal.svelte` 中按与 `sendUnavailable` 相同的条件派生，但使用面向快捷指令的文案：已退出为“会话已退出，无法发送”，未就绪/重连/恢复中为“终端未就绪，暂不能发送”。`disabled` 仅在 `t.connection === "missing"` 时为真。

- 备选：Popover + Command（可搜索）。50 条上限下搜索价值低，且搜索框会在手机上弹出软键盘遮挡列表；DropdownMenu 与顶栏“⋯”一致，键盘导航和 typeahead 由 bits-ui 提供。
- 备选：Drawer（底部抽屉）。会遮住刚发送后的终端输出，且与桌面顶栏入口体验不一致。

### 4. 发送逻辑放在 `Terminal.svelte`

```ts
function runQuickCommand(cmd: QuickCommand) {
  t.resetModifiers();
  const result = insertText(cmd.command, drafts.begin(draft));
  if (result === "failed") error = "终端未就绪，快捷指令未发送；请恢复连接后重试。";
  else if (result === "terminal" && cmd.enter && !t.send("\r"))
    error = "快捷指令已粘贴，但回车未发送；请检查终端后手动提交。";
  else error = "";
}
```

直接复用 `insertText`，草稿模式的光标插入、选区替换、会话隔离和操作有效性检查全部由 `terminal-draft.svelte.ts` 既有逻辑保证；先 `resetModifiers()` 确保草稿模式下锁定的修饰键也不残留。`"draft"` 结果从不追加回车。

- 备选：用 `t.send(cmd.command)` 模拟逐字输入。多行正文会被逐行提交、无法被 bracketed paste 保护，且与草稿发送语义分叉；不采用。

### 5. 设置页卡片与编辑对话框

`Settings.svelte` 新增 `<Card.Root id="quick-commands">`，放在“会话输出预览”卡片之前（新功能更常用）。`onMount` 中若 `location.hash === "#quick-commands"` 则 `scrollIntoView`。若查询参数 `session` 匹配会话 ID 格式 `^[a-z2-7]{8}$`（与 `App.svelte` 路由一致），顶栏按钮改为“返回终端”并导航到 `/s/<会话 ID>`，否则保持“返回工作区”；只接受会话 ID 而非任意路径，避免开放跳转。独立 PWA 没有浏览器后退按钮，这一入口保证管理完能直接回到原会话。卡片内容：

- 卡片描述：“点击终端底部工具栏或顶栏的 ⚡ 按钮即可发送；仅保存在当前浏览器。”（⚡ 以内联 `IconBolt` 呈现，与入口图标一致，帮助用户把图标与功能对应起来。）
- 空列表：`Empty`（`IconBolt` 媒体、标题“还没有快捷指令”、说明“把常用命令或提示词存起来，在终端一键发送。”、`Button`“添加快捷指令”）。
- 非空：`Item.Group` 内每条 `Item.Root variant="outline"`；`Item.Content` 渲染为 `button`（点击编辑，含名称和等宽预览，`enter` 时 `Badge variant="secondary"`“回车”）；`Item.Actions` 放 `icon-sm` 的上移（`IconArrowUp`）、下移（`IconArrowDown`）、删除（`IconTrash`）按钮，均带 `aria-label`。删除按钮打开 `AlertDialog`，标题“删除快捷指令？”，描述显示 `commandLabel`，操作按钮为 destructive“删除”。
- 列表下方 `Button variant="outline"`“添加快捷指令”（`IconPlus`），满 50 条时禁用并显示“最多 50 条”。

编辑对话框抽为 `web/src/components/QuickCommandDialog.svelte`：`Dialog` + `Label`/`Input`（`maxlength=40`，placeholder“例如：查看状态”）+ `Label`/`Textarea`（`font-mono`，`rows=4`，`maxlength=4096`，placeholder“例如：git status”）+ `Switch`“发送后自动回车”与说明“开启后点击即执行；关闭时只填入，由你确认后再回车”。新建时开关为关闭，编辑时为该指令当前值。对话框持有本地副本，“保存”时 `normalizeCommand` 并校验后回调；正文 `trim()` 为空时“保存”禁用。新建时 `onOpenAutoFocus` 聚焦正文输入。

保存回调返回错误说明或空字符串；对话框仅在成功时关闭。设置页提交时按当前列表重新判断新增是否超过 50 条（编辑已有条目不受此限制），达到上限则显示提示并保留输入，不调用列表保存。其他标签页删除条目后，用户可直接重试保存。

列表的每次结构变化（排序、删除）即时保存，与页面“修改后自动保存”一致；对话框内是一次编辑事务，显式“保存”符合对话框的通用习惯，也避免半成品条目出现在终端菜单中。

- 备选：在卡片内联编辑全部字段并逐字自动保存。手机上会形成很长的表单，且必须处理“空白正文条目”这种半成品状态；不采用。
- Switch 通过 `pnpm --dir web dlx shadcn-svelte@1.7.0 add switch` 生成到 `web/src/lib/components/ui/switch/`，依赖已安装的 bits-ui，不改 lockfile 依赖集。

## Risks / Trade-offs

- [仅图标入口，首次使用者可能不知道 ⚡ 的含义；触屏没有悬停提示] → 入口固定在软键栏末端、点开即见“发送到终端”标签和指令列表，探索成本仅一次点击；预置 `/resume`、`/new` 让首次点开就看到具体内容；设置卡片描述用同一图标对应功能。
- [预置的 `/resume`、`/new` 开启自动回车，在普通 shell 中误点会执行一条不存在的命令] → 仅产生一行“command not found”，无副作用；菜单条目带回车标记；用户可编辑或删除。
- [用户把含密码/令牌的命令存为快捷指令，明文留在 localStorage] → 卡片说明“仅保存在当前浏览器”；不发送到服务端、不记录日志。
- [两个菜单实例同时挂载，只有一个可见] → 仅渲染按钮，菜单内容在打开时才进入 DOM；状态来自同一模块级 `$state`，不存在数据分叉。
- [不同浏览器/设备的指令不互通] → 与现有设置一致，明确写在卡片描述中。
- [部分 TUI 对粘贴与随后的 CR 间隔敏感] → 与草稿发送使用同一序列，已在既有草稿功能中验证；不加延时或重试。

## Migration Plan

纯前端新增功能，无数据迁移。浏览器中没有 `webmux.quickCommands` 键时显示预置指令。回滚只需移除前端代码，残留的本地存储键无副作用。
