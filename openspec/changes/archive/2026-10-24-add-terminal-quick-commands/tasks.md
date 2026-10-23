## 1. 数据契约与存储

- [x] 1.1 在 `web/src/schemas.ts` 新增 `QuickCommandSchema`（`id`、`name` ≤40、`command` 1–4096、`enter`）与 `QuickCommandsSchema`（≤50 条），导出 `z.infer` 类型
- [x] 1.2 新增 `web/src/quickCommands.svelte.ts`（`quickCommands` 状态、`saveQuickCommands`）与纯函数模块 `web/src/quickCommands.ts`（`parseQuickCommands`、`normalizeCommand`、`commandLabel`、`commandPreview`、`newQuickCommandId`）：基于 `api.ts` 的 `stored`/`store` 读写 `webmux.quickCommands`；键不存在时返回预置 `/resume`、`/new`（无名称、自动回车开启）且不写回；损坏数据视为空列表且不回写；正文规范化为 CRLF/CR→LF、去末尾换行；ID 用 `crypto.getRandomValues`
- [x] 1.3 在同一模块注册 `storage` 事件，键匹配时重新加载，实现多标签页同步
- [x] 1.4 新增 `web/test/quickCommands.test.ts`：覆盖正文规范化（末尾多换行、CRLF、保留开头/中间空白）、超长与超数量拒绝、键不存在返回预置、保存空数组后不再返回预置、损坏 JSON 回退为空、`commandLabel` 无名称时取首行

## 2. 设置页管理

- [x] 2.1 用 `pnpm --dir web dlx shadcn-svelte@1.7.0 add switch` 生成 `web/src/lib/components/ui/switch/`，确认未新增 npm 依赖、lockfile 依赖集不变
- [x] 2.2 新增 `web/src/components/QuickCommandDialog.svelte`：Dialog 内名称 Input（maxlength 40）、指令等宽 Textarea（maxlength 4096）、“发送后自动回车”Switch（新建默认关闭，编辑显示当前值）、取消/保存；正文空白时禁用保存，新建时聚焦正文，取消/Esc 丢弃修改
- [x] 2.3 在 `Settings.svelte` 新增 `id="quick-commands"` 卡片（位于预览卡片之前）：含 ⚡ 图标的描述、Empty 空状态、Item 列表（点击主体编辑、回车 Badge、上移/下移/删除按钮及首尾禁用）、AlertDialog 删除确认、底部“添加快捷指令”及 50 条上限提示
- [x] 2.4 `Settings.svelte` 支持 `#quick-commands` 滚动定位；`session` 查询参数匹配 `^[a-z2-7]{8}$` 时顶栏按钮显示“返回终端”并导航到 `/s/<id>`，否则保持“返回工作区”

## 3. 终端入口、菜单与发送

- [x] 3.1 新增 `web/src/components/QuickCommandMenu.svelte`：`IconBolt` 仅图标触发按钮（`aria-label`/`title` 为“快捷指令”）；DropdownMenu 按 `side` 向上/向下展开、`align="end"`、限制最大高度与视口宽度；去向标签（发送到终端/插入草稿）、不可用原因、两行条目与直接模式回车标记、触屏 44 px 条目高度、空状态与“添加快捷指令…”、底部“管理快捷指令…”
- [x] 3.2 `SoftKeys.svelte` 增加可选 `trailing` snippet，在 `.soft-row` 之后以 Separator 分隔、`shrink-0` 固定于右侧，不参与横向滚动；触发按钮尺寸与软键一致
- [x] 3.3 在 `Terminal.svelte` 实现 `runQuickCommand`：`resetModifiers` 后经 `insertText` 发送/插入；直接模式成功且 `enter` 时 `t.send("\r")`；正文失败与回车失败分别提示，成功清除提示，不重试
- [x] 3.4 在 `Terminal.svelte` 派生菜单 `unavailable` 文案（已退出/未就绪），会话不存在时禁用按钮；`onCloseAutoFocus` 复用 `focusTerminal`；`manage` 导航到 `/settings?session=<id>#quick-commands`；通过 `trailing` 向 SoftKeys 注入 `side="top"` 实例
- [x] 3.5 在 `Terminal.svelte` 顶栏“草稿”按钮左侧放置 `side="bottom"` 实例（外层 `.quick-commands-header`），并在 `web/src/styles.css` 中以 `any-pointer: coarse` 与 `.force-soft-keys` 隐藏它，使两处入口与软键栏显隐严格互斥

## 4. 验证

- [x] 4.1 更新 `web/test/e2e/softkeys.spec.ts` 单行布局用例：320/390/844 px 下快捷指令按钮完整可见、位于最右且与其他软键同一行，顶栏无快捷指令按钮；软键行滚到最右端后按钮位置不变
- [x] 4.2 新增 `web/test/e2e/quick-commands.spec.ts`（移动端项目）：首次打开菜单显示 `/resume`、`/new` 及回车标记；设置页添加 `echo quick-ok` 并开启回车后在终端执行并看到输出；新建默认不回车的指令只出现在输入行不执行；Ctrl 锁定后发送不受影响；草稿模式下插入光标处且 PTY 无输入；断线/已退出时条目禁用；删光指令后刷新显示空状态，“添加快捷指令…”进入设置并可“返回终端”；排序与删除确认反映到菜单
- [x] 4.3 同一 spec 的桌面项目用例：顶栏“草稿”左侧显示快捷指令按钮、菜单向下展开并可发送；开启“强制显示底部工具栏”后顶栏按钮隐藏、软键栏出现按钮
- [ ] 4.4 运行 `pnpm --dir web check`、`make format-check`、`make test-unit`、`make test-e2e`，并在真实手机浏览器手动确认菜单弹出方向、点击目标和关闭后不意外弹出软键盘
  - 2026-10-24 归档时（`main` d85ab21）：`pnpm --dir web check`（0 错误）、`make format-check`、`make test-unit`（167 passed）、`make test-e2e`（132 passed）均通过。
  - 未验收：真实手机浏览器的菜单方向、点击目标与软键盘检查未执行（无可用真机）；Chromium 移动端模拟不能替代该项。

## 5. Review 修复

- [x] 5.1 保存新增指令时重新检查当前列表容量；满额时提示并保留输入，允许释放名额后重试及满额编辑
- [x] 5.2 列表契约拒绝重复 ID，损坏数据读取时降级为空列表且不回写
- [x] 5.3 窄屏代码视图隐藏终端面板时恢复顶栏快捷入口，返回终端后与底部入口保持互斥
- [x] 5.4 验证多标签页容量边界、重复 ID 降级、窄屏代码视图入口与管理跳转；运行针对性单元/E2E 回归、类型和格式检查
