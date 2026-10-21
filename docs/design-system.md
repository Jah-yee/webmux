# WebMux Design System

WebMux 是持续运行的终端工作台。视觉方向为克制的开发工具：中性色、绿色强调、明确的信息层级。规范与生产组件共用实现，Storybook 是可交互的检查入口。

## Design principles

1. **工作内容优先。** 终端、代码和会话名称占据主要空间。避免渐变、大面积品牌色、装饰动画和重复嵌套卡片；阴影用于菜单和弹窗等浮层。
2. **层级稳定。** 先呈现任务名称和主要操作，再呈现状态，最后呈现路径、设备与时间。12px 用于元数据，14px 用于正文和常规操作，16px 用于分区标题，24–30px 用于页面标题。
3. **状态可解释。** 运行/成功为绿色，等待/注意为琥珀色，失败/危险为红色，说明为蓝色，空闲和正常退出为中性色。颜色必须伴随文字，不能仅靠色点传递状态。会话推断状态仍由现有业务逻辑决定。
4. **操作可预期。** 每个任务区域保留一个绿色主要操作。次要操作使用 outline，低频工具使用 ghost。危险操作使用 destructive，并在确认弹窗中说明后果；取消入口始终可见。
5. **紧凑但可操作。** 控件使用统一尺寸，而不是页面级 CSS 覆盖。触屏或小于 640px 的窗口中，常规按钮最小 44px；终端紧凑顶栏保留 32px，软键保持横向滚动，优先保留终端内容空间。
6. **恢复优先。** 等待、重连、未确认提交和失败状态说明当前情况与下一步。保留已有草稿保护、自动重连和提交确认机制。

## Tokens

唯一令牌入口是 `web/src/design-tokens.css`，由应用的 `styles.css` 引入，Storybook 直接使用同一份样式。

| 层        | 内容                                                                                       | 修改规则                               |
| --------- | ------------------------------------------------------------------------------------------ | -------------------------------------- |
| Primitive | neutral、green、amber、blue 色阶，control 尺寸                                             | 定义原始值，避免散落到业务组件         |
| Semantic  | background、card、popover、foreground、primary、muted、success、warning、destructive、info | 根据用途引用；明暗主题覆盖相同角色     |
| Component | control-xs/sm/default/lg/touch，Tailwind radius-sm/md/lg/xl                                | 控件直接消费尺寸和圆角，页面只控制布局 |

- 间距使用 Tailwind 的 4px 基准：4 / 8 / 12 / 16 / 24 / 32。
- 控件圆角 6px，内容容器 8px，浮层 12px；标签圆角 4px。
- 默认控件高 36px，紧凑 32px，工具 28px，大控件 44px。
- 拉丁界面字体使用内置 Inter，路径和代码使用内置 Maple Mono 的拉丁字形、制表符和 Nerd Font 图标；中日韩文字使用系统无衬线字体。
- ANSI 终端和语法高亮令牌保持独立；业务状态不再借用 ANSI 色板。
- 焦点使用 primary 对应的 ring；不依赖 hover 提供唯一反馈。

## Component system

继续使用 `web/src/lib/components/ui/` 中的 shadcn-svelte 与 bits-ui 组件，保留键盘导航、焦点管理和 ARIA 行为。修改生成组件后，重新运行生成器时需要审查本地差异。

| 层级       | 组件                                                               | 约定                                                 |
| ---------- | ------------------------------------------------------------------ | ---------------------------------------------------- |
| 基础操作   | Button、Badge、Input、Textarea、Label、Switch                      | 使用 variant 和 size；不以页面选择器覆盖全体按钮外观 |
| 容器和导航 | Card、Item、Tabs、DropdownMenu、Popover                            | 内容层使用边界和留白；浮层使用独立表面和阴影         |
| 弹层       | Dialog、AlertDialog、Drawer                                        | 共享文字与形状规范；危险菜单项保留红色               |
| 业务反馈   | InlineNotice、SessionStatus                                        | 复用语义颜色和状态文字，长错误信息允许换行           |
| 业务模式   | WorkspaceGroup、SessionRow、WorkspaceDialog、TerminalInputComposer | 组合基础组件；响应式布局与资源生命周期由业务组件管理 |

`SessionStatus` 区分正常退出与非零退出码。`InlineNotice` 的 danger 使用 `role="alert"`，其他提示使用 `role="status"`；常规静态说明可继续使用普通段落，避免过多实时播报。

表单标签应使用 `for` 关联控件；帮助和错误使用 `aria-describedby`，无效字段设置 `aria-invalid`。字段在手机上使用 16px 文字，避免阅读困难和输入缩放。图标按钮需要可访问名称。

长路径可在列表中截断，但应保留完整可访问名称或 `title`。错误描述、警告、表单标签必须换行。终端预览、代码视图和单行输入的内部滚动/截断属于明确的产品行为。

## Storybook

运行 `pnpm --dir web storybook`，打开 `Design system`：

| Story          | 检查内容                                         |
| -------------- | ------------------------------------------------ |
| Foundations    | 设计核心、语义色、文字层级、间距与形状           |
| Actions        | 按钮层级、密度、禁用和处理状态                   |
| Forms          | 常规、无效、只读、禁用字段与开关                 |
| Feedback       | 会话状态、成功、警告、错误和说明                 |
| Surfaces       | 卡片、空状态、加载状态、标签页和菜单             |
| Overlays       | 响应式编辑弹层与危险操作确认                     |
| Session States | 运行、等待、空闲、正常退出和失败退出的真实会话行 |

现有 `Workspace` 展示真实业务布局，`UI/Overflow` 保留极端长文本场景。示例交互只修改内存中的演示数据。

## Preview evidence

以下截图来自运行中的真实 Storybook 组件，使用仓库内的固定示例数据。

| 视图              | 浅色                                                                                                                         | 深色                                                                                                                       |
| ----------------- | ---------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| 设计基础          | [查看](images/design-system/foundations-light.jpg)                                                                           | [查看](images/design-system/foundations-dark.jpg)                                                                          |
| 工作区 · 1280×720 | [修改前](images/design-system/workspace-before-light.jpg) / [修改后](images/design-system/workspace-light.jpg)               | [修改前](images/design-system/workspace-before-dark.jpg) / [修改后](images/design-system/workspace-dark.jpg)               |
| 工作区 · 390×844  | [修改前](images/design-system/workspace-before-mobile-light.jpg) / [修改后](images/design-system/workspace-mobile-light.jpg) | [修改前](images/design-system/workspace-before-mobile-dark.jpg) / [修改后](images/design-system/workspace-mobile-dark.jpg) |

## Verification and scope

新增设计系统场景沿用六种视口/字号与明暗主题矩阵。检查布局、正文颜色对比度、错误文字、表单编辑、菜单语义、弹层关闭与焦点恢复。既有防溢出和组件/E2E 套件继续承担业务行为验证。

本轮统一覆盖核心控件、内容容器、弹层和反馈模式，并应用到工作区、设置、草稿输入和代码变更状态。终端字符网格、语法高亮、虚拟列表行高、会话生命周期和后端协议保持原有实现。

Storybook 的几何与交互检查不是像素快照比较。手机视口和根字号测试不能替代真实 iOS/Android 键盘、触摸及系统缩放验证。
