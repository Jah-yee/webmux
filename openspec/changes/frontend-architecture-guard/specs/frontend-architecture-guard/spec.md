## ADDED Requirements

### Requirement: 统一的架构检查入口

前端 SHALL 提供 `pnpm --dir web check:arch` 作为架构检查入口，依次运行设计系统 lint（`arch:lint`）、分层检查（`arch:deps`）、重复代码检查（`arch:dupes`）、未使用代码检查（`arch:unused`）和架构测试（`arch:test`），任一项失败时命令 MUST 以非零状态退出。`make check` 与 CI 的格式和构建 job MUST 运行该入口。检查 MUST 只读取源码，不连接后端或现有开发服务，也不改变构建产物。Oxlint SHALL 继续作为通用 lint。

#### Scenario: 本地与 CI 运行

- **WHEN** 贡献者在没有新增违规的分支上运行 `pnpm --dir web check:arch`，或 CI 在该分支上运行格式和构建 job
- **THEN** 五项检查全部通过，命令以 0 退出，`pnpm --dir web lint` 的结果不受影响

#### Scenario: 任一项失败

- **WHEN** 分支新增了一处分层违规，其余检查都通过
- **THEN** `check:arch` 以非零状态退出，输出指出违规的规则、源文件与目标文件

### Requirement: 设计系统组件只允许使用处调整布局

在 `web/src/lib/components/ui/` 以外，向设计系统组件传入的 `class` SHALL 只包含布局类（外边距、宽高、栅格与 flex 位置等）。改变内边距、颜色、圆角、字号、边框或阴影 MUST 被报告，报错 MUST 指出该组件的定义文件以及可用的 size 或 variant。组件契约 MAY 放宽个别组件（`CardTitle` 可调整排版但不能改字重，`CardContent` 可调整内边距）。新增契约 MUST 记录在 ESLint 配置中，并在 PR 中说明理由。

#### Scenario: 覆盖按钮内边距

- **WHEN** 业务组件写 `<Button class="px-4">`
- **THEN** `arch:lint` 报告 `px-4` 不允许用于 Button，并列出 Button 的 size（default、xs、sm、lg、icon 等）

#### Scenario: 只调整布局

- **WHEN** 业务组件写 `<Button size="sm" class="mt-2 w-full">`
- **THEN** 不报告违规

### Requirement: 使用主题令牌而不是原始值

业务代码的类名 MUST NOT 使用 Tailwind 原始调色板颜色（如 `bg-blue-500`）、间距/排版/颜色/形状/效果类的任意值（如 `p-[13px]`、`text-[11px]`）、Tailwind 无法生成的类名，或无法静态读取的动态类名。布局类（宽高、位置、flex 与栅格）MAY 使用任意值，因为依赖视口的尺寸计算（如 `max-h-[50dvh]`）不是设计令牌。业务代码 MUST NOT 用 `style:height` 等内联样式设置属性。报错 SHALL 给出最接近的主题令牌或刻度值，例如“`p-[13px]` 可改为 `p-3.25`”。运行时才能确定的几何值 SHALL 通过 `style:--name` 设置的 CSS 自定义属性与读取它的工具类表达。

#### Scenario: 原始颜色

- **WHEN** 新代码写 `class="bg-blue-500"`
- **THEN** `arch:lint` 报告该颜色不在主题中，并列出可用的语义令牌（background、primary、muted、destructive 等）

#### Scenario: 任意值

- **WHEN** 新代码写 `class="p-[13px]"`
- **THEN** `arch:lint` 报告该值不在刻度上，并建议 `p-3.25`

#### Scenario: 布局尺寸

- **WHEN** 新代码写 `class="max-h-[50dvh] w-[min(90vw,520px)]"`
- **THEN** 不报告违规

#### Scenario: 运行时尺寸

- **WHEN** 虚拟列表行写 `style:top={`${offset}px`}`
- **THEN** `arch:lint` 报告内联样式；改为 `style:--row-top={`${offset}px`}` 与 `class="top-(--row-top)"` 后不再报告

### Requirement: 复用设计系统组件与图标约定

在 `web/src/lib/components/ui/` 以外，`.svelte` 模板 MUST NOT 手写 `<button>`、`<input>`、`<textarea>`、`<select>`、`<dialog>`、`<label>`。报错 SHALL 指出应复用的组件：Button（列表、标签栏与卡片中的可点击区域用 ListButton）、Input（复选框用 Checkbox）、Textarea、NativeSelect、Dialog 或 ResponsiveDialog、Label。`bits-ui`、`vaul-svelte`、`paneforge`、`tailwind-variants` 与 `class-variance-authority` MUST 只在 `ui/` 中导入。业务代码 MUST 通过 Iconify（`~icons/tabler/*`）导入图标，MUST NOT 直接导入 `@tabler/icons-svelte`。

#### Scenario: 手写按钮

- **WHEN** 业务组件新增 `<button onclick={save}>保存</button>`
- **THEN** `arch:lint` 报告应改用 `$lib/components/ui/button` 的 Button

#### Scenario: 直接导入原语

- **WHEN** 业务组件写 `import { Dialog } from "bits-ui"`
- **THEN** `arch:lint` 与 `arch:deps` 都报告原语只能在 `ui/` 中封装

#### Scenario: 直接导入图标库

- **WHEN** 业务组件写 `import { IconPlus } from "@tabler/icons-svelte"`
- **THEN** `arch:lint` 报告应改为 `import IconPlus from "~icons/tabler/plus"`

### Requirement: 分层依赖方向

`arch:deps` SHALL 按以下方向检查 `web/src` 的导入：`lib/components/ui/` 只依赖 `src/lib/` 与第三方包；`src/lib/` 的其他模块不依赖 `components/`、`hooks/`、`pages/`；顶层领域模块（`src/*.ts`，`main.ts` 除外）与 `hooks/` 不依赖 `.svelte` 组件；`components/` 不依赖 `pages/`；只有 `App.svelte` 与页面本身可以导入 `pages/`。模块之间 MUST NOT 形成循环依赖（`ui/` 内生成的 barrel 自引用除外）。

#### Scenario: 领域模块渲染组件

- **WHEN** `src/code-search.ts` 导入 `src/components/CodeSearchPanel.svelte`
- **THEN** `arch:deps` 报告 `domain-does-not-render`（已列入基线的存量除外）

#### Scenario: 引入循环依赖

- **WHEN** `src/api.ts` 与 `src/ws.ts` 互相导入
- **THEN** `arch:deps` 报告 `no-circular`，并列出循环路径

### Requirement: 不重造组件、不复制样式与代码

架构测试 SHALL 保证：`web/src` 中每个业务组件名（忽略大小写与 `-`、`_`、`.`）只有一个实现；业务组件不与 `ui/` 组件同名；同一组 6 个以上外观类（排序并去掉布局类后比较）不出现在 3 个以上业务文件中。`arch:dupes` SHALL 报告新增的、至少 5 行且 50 个 token 的重复代码块（测试、stories 与生成的字体 CSS 除外）。`arch:unused` SHALL 报告从入口不可达的源文件、未使用的依赖和重复导出。在存量清零之前，未使用的导出和类型 MAY 只作为警告。

#### Scenario: 业务组件与 ui 组件同名

- **WHEN** 新增 `src/components/Button.svelte`
- **THEN** `arch:test` 失败，指出它与 `src/lib/components/ui/button/button.svelte` 同名

#### Scenario: 复制长类名

- **WHEN** 三个业务文件都写了同一组 6 个以上外观类
- **THEN** `arch:test` 失败，列出这组类名和文件，提示提取组件或增加 variant

### Requirement: 存量基线只减不增

存量违规 SHALL 记录在提交的基线文件中：`web/eslint-suppressions.json`、`web/.dependency-cruiser-known-violations.json`、`web/.jscpd-baseline.json`。检查 MUST 只因基线以外的违规失败。`pnpm --dir web arch:ratchet` MUST 只删除已修复的记录并格式化基线文件，MUST NOT 加入新的违规。贡献者和 agent MUST NOT 为通过检查而扩大基线、添加 `eslint-disable` 或 `jscpd:ignore`。ESLint 基线按“文件 + 规则”计数；某文件某规则的违规数超过记录时，该文件该规则的全部违规 SHALL 被报告。

#### Scenario: 在存量文件中新增同类违规

- **WHEN** 基线记录 `NewSessionSheet.svelte` 有 12 处 `no-restyle`，修改后变为 13 处
- **THEN** `arch:lint` 失败，并报告该文件全部 13 处 `no-restyle`

#### Scenario: 修复后收紧

- **WHEN** 贡献者修复 `CodeNavigator.svelte` 的全部原生控件后运行 `arch:ratchet`
- **THEN** 基线中该文件的 `svelte/no-restricted-html-elements` 记录被删除，其余记录不变，基线文件保持 Oxfmt 格式

### Requirement: Agent 指引

`AGENTS.md` SHALL 要求写 UI 前依次查找 `web/src/lib/components/ui/`、`web/src/components/` 与 shadcn-svelte registry，需要新外观时为现有组件增加 variant 或 size，并在完成前端改动前运行 `check:arch`。`docs/frontend-architecture.md` SHALL 说明分层、检查项、组件契约、基线与收紧流程。规则或基线流程变化时，两处文档 MUST 同步更新。

#### Scenario: Agent 新增 UI

- **WHEN** coding agent 按 `AGENTS.md` 完成一个新的设置项
- **THEN** 它复用现有 Switch、Label 与 Item 组件，`check:arch` 通过，PR 不扩大任何基线
