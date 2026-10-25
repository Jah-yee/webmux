# Frontend Architecture Guard

前端架构守卫把 [设计系统](design-system.md) 中“复用组件、使用令牌、页面只控制布局”的约定变成可执行的检查，使人和 coding agent 在提交前得到具体的修复提示。所有命令在仓库根目录运行。

```sh
pnpm --dir web check:arch    # 全部架构检查；make check 与 CI 也会运行
```

## 写 UI 之前：先找，再写

按顺序查找，找到就复用，不要重新实现：

1. `web/src/lib/components/ui/`：shadcn-svelte 基础组件（Button、Input、Dialog、DropdownMenu 等）。
2. `web/src/components/`：业务复合组件（ResponsiveDialog、InlineNotice、SessionStatus 等）。
3. shadcn-svelte registry：`pnpm --dir web exec shadcn-svelte add <name>`，生成后审查本地差异。

三处都没有，才新建组件。需要新外观时，在 `ui/` 对应组件的 `tv()` 中增加 variant 或 size，而不是在使用处用 class 覆盖。

## 分层

源码目录保持扁平，不划分 `features/`。dependency-cruiser 按下表检查导入方向：

| 层           | 路径                       | 可以依赖                         |
| ------------ | -------------------------- | -------------------------------- |
| 设计系统叶子 | `src/lib/components/ui/`   | `src/lib/`、第三方包             |
| 工具         | `src/lib/`                 | 不依赖业务组件、hooks 与页面     |
| 领域逻辑     | `src/*.ts`（除 `main.ts`） | 不依赖 `.svelte` 组件            |
| Hooks        | `src/hooks/`               | 领域逻辑；不依赖 `.svelte` 组件  |
| 业务组件     | `src/components/`          | 以上各层；不依赖页面             |
| 页面         | `src/pages/`               | 以上各层；只由 `App.svelte` 组合 |

`bits-ui`、`vaul-svelte`、`paneforge` 等 headless 原语与 `tailwind-variants` 只在 `ui/` 中使用。模块间不允许循环依赖（shadcn-svelte 生成的 barrel 自引用除外），未被引用的模块应删除。

## 检查项

| 命令          | 工具                    | 检查内容                                                                                                                                          |
| ------------- | ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `arch:lint`   | ESLint + `@shadcn/lint` | 用 class 覆盖组件外观、原始调色板颜色、任意值、内联样式、未知类名、动态类名；手写原生控件；在 `ui/` 外导入原语、变体工具或 `@tabler/icons-svelte` |
| `arch:deps`   | dependency-cruiser      | 上节的分层、循环依赖与孤立模块                                                                                                                    |
| `arch:dupes`  | jscpd                   | 新增的复制粘贴代码块                                                                                                                              |
| `arch:unused` | knip                    | 从入口不可达的文件、未使用的依赖、导出与类型，重复导出                                                                                            |
| `arch:test`   | Vitest                  | 同名业务组件、与 `ui/` 组件同名的业务组件、6 个以上外观类在 3 个以上文件中重复                                                                    |

Oxlint（`pnpm --dir web lint`）仍是通用 lint。它只读取 `.svelte` 的 `<script>`，所以模板相关规则由只加载架构规则的 ESLint 执行。

`no-restyle` 只允许使用处调整布局（外边距、宽度、高度、栅格与 flex 位置等）。例外由 `web/eslint.config.mjs` 中的组件契约声明，新增契约需要在 PR 中说明设计理由：

| 组件                                                                                                                                                                                | 使用处还可以调整            | 理由                                                                           |
| ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------- | ------------------------------------------------------------------------------ |
| `CardTitle`                                                                                                                                                                         | 排版（不含字重）            | 标题字号随所在区域变化                                                         |
| `CardContent`、`TabsContent`、`CommandList`、`ItemGroup`、`ItemActions`、`Collapsible`、`CollapsibleContent`、`DialogContent`、`DialogHeader`、`PopoverContent`、`ResponsiveDialog` | 内边距与间距                | 内容区域的留白取决于放入的内容，组件本身不规定                                 |
| `CollapsibleTrigger`、`PopoverTrigger`                                                                                                                                              | 全部外观                    | bits-ui 原语，设计系统没有给它们任何样式；需要按钮外观时用 `child` 渲染 Button |
| `Pane`、`ResizableHandle`                                                                                                                                                           | `styles.css` 中的布局钩子类 | 终端、草稿与代码分栏的显示和尺寸由响应式 CSS 控制                              |

组件需要由样式表控制布局时，优先用 `data-*` 属性作为选择器（如 `DirectoryPicker` 的 `data-directory-picker`），只在样式表中写布局；外观仍通过 variant 表达。

常用的设计系统扩展：

| 组件                | variant / size                                              | 用途                                                                                                      |
| ------------------- | ----------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `Button`            | `variant="quiet"`                                           | 平时弱化、悬停时恢复前景色的 ghost 按钮                                                                   |
| `Button`            | `raised`                                                    | 浮在终端或代码上的按钮（边框加阴影）                                                                      |
| `Button`            | `size="icon-touch"` / `size="sm-touch"`                     | 窄屏（小于 `sm`）44px 触控尺寸，`sm` 以上恢复常规尺寸                                                     |
| `Input`、`Textarea` | `mono`                                                      | 命令、路径、环境变量与终端文本                                                                            |
| `Textarea`          | `variant="ghost"`                                           | 在列表项或卡片中原地编辑                                                                                  |
| `Empty`             | `variant="outline"` / `variant="card"`                      | 区块内的虚线空状态 / 独立卡片上的空页面                                                                   |
| `Command`           | `variant="outline"`                                         | 嵌在表单或对话框中的列表                                                                                  |
| `DropdownMenuLabel` | `variant="title"`                                           | 整个菜单的标题                                                                                            |
| `Item`              | `variant="card"`、`size="flush"`                            | 卡片表面、悬停高亮的列表项；内容自带内边距（如整行链接）                                                  |
| `Input`、`Label`    | `size="sm"`                                                 | 代码导航等紧凑面板                                                                                        |
| `NativeSelect`      | `size="sm"`                                                 | 原生下拉选择；手机保留系统选择器                                                                          |
| `Checkbox`          | —                                                           | 复选框，配合 `Label` 使用                                                                                 |
| `ListButton`        | `row` / `text` / `content` / `option` / `tab` / `tab-close` | 列表、标签栏与卡片中的可点击区域。它不带控件尺寸，虚拟列表的固定行高不受影响；独立的操作按钮仍用 `Button` |

粗指针设备的最小触控高度用 Tailwind 的 `any-pointer-coarse:` 变体（如 `any-pointer-coarse:min-h-11`）。

### 任意值与运行时尺寸

`no-arbitrary-values` 允许布局类的任意值：依赖视口的尺寸计算（如 `max-h-[50dvh]`、`w-[min(90vw,520px)]`）是布局，不是设计令牌。间距、排版、颜色、圆角与阴影仍必须使用主题刻度或令牌，例如：

- 字号用 `text-xs` 等刻度，元数据最小 12px。
- 带安全区的底部留白用 `styles.css` 中的 `pb-safe-4`。

运行时才能确定的几何值（虚拟列表行高、图片缩放、视口高度、预览行数）用 `style:--name` 设置 CSS 自定义属性，再由类读取，如 `h-(--row-height)`、`transform-(--image-transform)`，或由 `styles.css` 中的工具类读取（如 `h-preview`）。不要用 `style:height` 等内联样式。

## 基线与新规则

`frontend-architecture-guard` 的存量治理已经完成，下面三个基线文件都为空，所有检查都按严格模式执行。保留这些文件，是为了以后引入有存量违规的新规则时，可以先记录基线，再逐步收紧：

| 文件                                            | 记录内容                     |
| ----------------------------------------------- | ---------------------------- |
| `web/eslint-suppressions.json`                  | 每个文件、每条规则的违规数量 |
| `web/.dependency-cruiser-known-violations.json` | 已知的分层违规               |
| `web/.jscpd-baseline.json`                      | 已知的重复代码块指纹         |

- 修复存量问题后运行 `pnpm --dir web arch:ratchet`，它只删除已修复的记录并格式化基线文件，然后提交基线变化。重复代码先通过 `arch:dupes` 检查：出现未登记的克隆时立即失败，不改写 jscpd 基线；只有没有新增克隆时才更新基线，删除已解决的指纹。
- ESLint 按“文件 + 规则”计数：同一文件同一规则的违规数超过记录时，该文件该规则的所有违规都会报告。
- 不要为了通过检查而扩大基线、添加 `eslint-disable` 或 `jscpd:ignore`。确有必要的例外，在 PR 中说明原因，并优先改为配置中的契约或白名单。

治理过程与每批的决策记录见 OpenSpec 变更 `frontend-architecture-guard`。
