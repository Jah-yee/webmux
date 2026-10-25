## 1. 依赖与基线确认

- [x] 1.1 在 `web/package.json` 增加 `@xterm/addon-search@^0.16.0`，使用 pnpm 12.11.2 更新锁文件；registry 元数据确认与 xterm 6.0.0 的 gitHead 同为 `f447274f430fd22513f6adbf9862d19524471c04`，保留 frozen lockfile 流程
- [x] 1.2 复核 `terminal-renderer.ts` 的 10000 行 `scrollback`、socket 全量/增量 restore 路径与草稿按键分支
- [x] 1.3 本变更不改 `/api/sessions/{id}/buffer`、Go 服务端、会话生命周期与控制权代码

## 2. 搜索 hook 与纯函数

- [x] 2.1 新增 `terminal-search.ts`：查询校验、空值、结果文案与范围/回放文案；计数达到 1000 时无论索引都显示“至少 1000 个匹配（仅统计前 1000 个）”
- [x] 2.2 `mountTerminal` 加载并释放 SearchAddon；暴露导航、clearFind、findResults、bufferType、scrollback 与不依赖 ready 的 canFind
- [x] 2.3 订阅结果与缓冲变化；clearFind 显式执行 clearDecorations、clearSelection 和响应式结果置 null，不依赖不存在的 addon 重置事件
- [x] 2.4 传递大小写/全词/正则/incremental/decorations；新查询以同步选区起点从视口附近搜索，无效正则不调用 addon；空查询清空，无匹配按 addon 行为清除旧选区
- [x] 2.5 全量回放显式重置搜索；增量续接保留搜索，缺失尾部写入前记录视口并在解析后恢复，空 append 也不跳到底部
- [x] 2.6 clear 清除高亮、计数及选区；退出会话仍能查找最终保留的活动缓冲

## 3. 查找栏与页面接线

- [x] 3.1 新增 TerminalFindBar：独立查询、Aa/W/正则、上下导航、计数、关闭、复制选中与范围状态行
- [x] 3.2 查找栏在 terminal-area 顶部 overlay，不占终端网格；打开时隐藏原选区工具栏
- [x] 3.3 所有宽度的终端菜单提供查找；Terminal 页面持有打开状态，窄屏代码视图打开时先返回终端
- [x] 3.4 打开聚焦查询，菜单关闭不抢焦点；关闭/Esc 清理状态，桌面返回原终端/草稿焦点，触摸端仅 blur；复制回退对话框关闭不把查找焦点送入 PTY
- [x] 3.5 搜索语义与名称、Iconify 本地图标、按钮 tooltip、aria-live 计数和可键盘操作的控件
- [x] 3.6 组合输入期间不查找，compositionend 后查找，关闭自动大写/纠错/拼写检查

## 4. 快捷键与输入隔离

- [x] 4.1 终端列捕获 Ctrl+Shift+F 和 macOS Cmd+F，覆盖 xterm 与草稿；preventDefault/stopPropagation，不监听 window
- [x] 4.2 Ctrl+F 保持 shell forward-char；代码面板在捕获范围之外
- [x] 4.3 查询、导航、复制和关闭不调用 send/term.input/paste/草稿发送，不更改控制权

## 5. 范围、截断与回放状态

- [x] 5.1 始终显示当前终端缓冲和实际 scrollback（当前 10000 行），不声称完整日志
- [x] 5.2 historyTruncated 时追加服务端已覆盖更早输出，不承诺可恢复
- [x] 5.3 alternate 追加“全屏程序：仅当前屏幕”；切换缓冲清空旧结果，保留查询，不自动跳转
- [x] 5.4 全量回放禁用导航并提示完成后按 Enter；缺失尾部和空 append 都保留搜索视口
- [x] 5.5 关闭显式清理装饰、选区和计数，重开保留查询但不自动查找

## 6. 测试

- [x] 6.1 新增纯函数测试：空值/正则、结果与 1000 饱和计数、范围/截断/alternate/回放、快捷键判定
- [x] 6.2 扩展 TerminalHarness 并新增 TerminalRendering 浏览器行为测试：真实 addon 与 xterm 的快捷键、Ctrl+F、草稿/IME隔离、视口起点、折行/宽字符、选项、无效正则、计数上限后导航、alternate/clear/回放/退出、关闭重开、两种续接、输入帧及尺寸隔离
- [x] 6.3 扩展真实 PTY E2E：桌面快捷键、手机代码视图菜单入口、可读复制与拒绝回退、草稿焦点恢复、无输入/尺寸帧、关闭后 Shell 继续执行
- [x] 6.4 目标 Vitest 与 Playwright（桌面 + Pixel 7）通过，确认无输入帧和终端尺寸变化；增量刷新后同坐标选区同步另有回归覆盖

## 7. 文档、用户故事与验收

- [x] 7.1 `docs/usage.md` 补充入口、快捷键、有限缓冲范围、截断/全屏与饱和计数语义
- [x] 7.2 `docs/architecture.md` 补充纯浏览器端 xterm 活动缓冲搜索，无新增服务端接口
- [x] 7.3 `docs/user-stories.md` 新增 US-D11，更新组内依据与功能索引；133 个故事 ID 唯一、索引引用与 143 条来源路径检查通过
- [x] 7.4 Svelte/TypeScript、Oxlint、前端架构守卫与目标测试通过；全量 Go lint 的既存问题见隧道验收记录
- [x] 7.5 `openspec validate add-terminal-buffer-find --strict --no-interactive` 通过
- [x] 7.6 已捕获隔离样例桌面/手机、亮/暗主题前后截图，位于 `docs/images/terminal-find-tunnel/`；本次按请求仅本地实现，未创建实现 PR，发布时须在 PR 描述嵌入可访问图片链接
