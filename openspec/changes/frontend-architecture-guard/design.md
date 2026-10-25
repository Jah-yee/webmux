## Context

- 方案来源：用户提供的 `frontend-architecture-guard` 模板，分四层：0 预防（AGENTS.md“先找再写”）、1 即时反馈（ESLint + `@shadcn/lint`）、2 架构（dependency-cruiser、jscpd、knip）、3 补缝（Vitest 架构测试），由 CI 门禁兜底。模板面向 `src/components/{ui,shared}` 与 `src/features/<name>` 的目录约定，并按 npm 编写。
- 本项目现状：`web/src/lib/components/ui/` 是 shadcn-svelte 生成的组件（`components.json` 的 `aliases.ui`），变体用 `tailwind-variants`；业务组件平铺在 `web/src/components/`，页面在 `web/src/pages/`，资源型 rune 在 `web/src/hooks/`，领域逻辑、schema、传输与存储是 `web/src/*.ts`。没有 `features/` 目录。
- 现有检查：Oxlint（`correctness` 级别，只读 `<script>`）、Oxfmt、`svelte-check`、Vitest、Playwright、Storybook。包管理器为 pnpm 12.11.2，使用 frozen lockfile，构建脚本白名单只有 `esbuild`。
- 2026-10-24 对 `main`（`8afeffb`）的实测违规数量见 proposal.md。

## Goals / Non-Goals

**Goals:**

- 让 agent 在提交前得到可操作的报错：违反了什么、应复用哪个组件、哪个 size/variant 或主题令牌。
- 新代码立即受约束，存量问题不阻塞开发，并且基线只减不增。
- 沿用现有工具链与目录结构，不增加运行时依赖。

**Non-Goals:**

- 不重组为 `features/` 目录，不启用 feature 之间的隔离规则。
- 不用 ESLint 替换 Oxlint，ESLint 只运行架构规则。
- 首个 PR 不修复存量违规；治理在后续 PR 中分批进行。
- 不检查 `<style>` 块中的 CSS（`@shadcn/lint` 不读取它们），也不检查 `test/`、`stories/` 中的代码。

## Decisions

### 1. 保持扁平目录，把模板的层映射到现有路径

| 模板层                    | 本项目                                       |
| ------------------------- | -------------------------------------------- |
| `src/components/ui/`      | `src/lib/components/ui/`                     |
| `src/lib/`                | `src/lib/`（`utils.ts`、`touch-targets.ts`） |
| `src/components/shared/`  | `src/components/`                            |
| `src/pages\|routes\|app/` | `src/pages/` 与 `src/App.svelte`             |
| `src/features/<name>/`    | 不使用                                       |

在此基础上补充两条本项目特有的方向规则：领域模块（`src/*.ts`，`main.ts` 除外）与 hooks 不依赖 `.svelte` 组件，让 schema、传输和存储逻辑保持可在 Node 中单元测试。

备选：按终端、代码、工作区、设置拆分 `features/`，启用 `no-cross-feature`。需要移动大部分组件，并且这些区域共享 ResponsiveDialog、InlineNotice、SessionStatus 等组件，收益不足以抵消改动量，不采用。

### 2. 新增 ESLint，只运行架构规则

`@shadcn/lint` 同时支持 ESLint 和 Oxlint，但 Oxlint 只读 `.svelte` 的 `<script>`，`no-restyle`、原生元素和类名规则都需要读模板。因此新增 `web/eslint.config.mjs`，用 `svelte-eslint-parser` 解析模板，只启用 `@shadcn/lint`、`svelte/no-restricted-html-elements` 和 `no-restricted-imports`。Oxlint 和 `pnpm lint` 保持不变。

- `@shadcn/lint` 通过 `components.json` 自动发现 `ui/` 组件、variant/size 和 `styles.css` 中的主题令牌，不需要额外配置。
- `no-restyle` 默认只允许 `layout` 类。按模板保留两条契约：`CardTitle` 可调整排版但不能改字重，`CardContent` 可调整内边距。
- `ui/` 目录关闭 `no-restyle`、任意值、内联样式、动态类名和导入限制，因为生成组件本身负责封装这些内容。
- 原生元素映射到本项目的组件：`<select>` 指向 NativeSelect，复选框指向 Checkbox（两者在治理第 4 批用 shadcn-svelte 添加），列表、标签栏与卡片中的可点击区域指向 ListButton；`<dialog>` 指向 Dialog 或 ResponsiveDialog；本项目没有 Table 组件，不禁止 `<table>`。
- `no-arbitrary-values` 允许布局类的任意值：依赖视口的尺寸计算是布局，不是设计令牌；间距、排版、颜色、形状与效果仍按刻度。
- 额外禁止在 `ui/` 外导入 `@tabler/icons-svelte`，把 `AGENTS.md` 中的 Iconify 约定变成检查。
- 忽略 `src/lib/utils.ts`：它是 shadcn-svelte 生成的辅助文件，其中的禁用注释引用了未安装的 typescript-eslint 规则。

备选：只用正则写架构测试覆盖原生元素和 restyle。无法识别多行标签、`class={[...]}` 与 `cn()` 的组合，也不能给出 size/令牌建议，不采用。

### 3. 存量违规用工具自带的基线，只减不增

| 工具               | 基线机制                                                                       |
| ------------------ | ------------------------------------------------------------------------------ |
| ESLint             | bulk suppressions（`eslint-suppressions.json`，按文件 + 规则计数）             |
| dependency-cruiser | `--baseline` / `--ignore-known`（`.dependency-cruiser-known-violations.json`） |
| jscpd              | `--baseline` + `--fail-on-new-clones=0`（`.jscpd-baseline.json`）              |
| knip               | 无基线；未使用的导出和类型先为 `warn`，清理后（tasks 2.1）升为 `error`         |

所有规则直接设为 `error`。新代码立即受约束，同时不必在每个文件中写禁用注释。`arch:ratchet` 依次运行 `eslint --prune-suppressions`、`depcruise --baseline-mode shrink-only` 和 `jscpd --update-baseline`，最后用 Oxfmt 格式化基线文件。

ESLint 的计数粒度是“文件 + 规则”：在已有违规的文件中新增同类违规会让该文件该规则的全部违规重新报告，这正好促使修改者顺手治理。

jscpd 的 `--baseline-from-ref origin/main`（模板 CI 的做法）需要完整 git 历史，与现有 CI 的浅克隆和本地离线运行不一致，因此改用提交的基线文件。

备选：先把规则设为 `warn`。warn 不阻止新增违规，agent 也容易忽略，不采用。

### 4. 架构测试并入现有 Vitest 单元套件

模板的三项检查（同名组件、重造 `ui/` 组件、跨文件复制的外观类）写在 `web/test/architecture.test.ts`。它匹配现有 `test/*.test.ts` 的 include，会随 `test:unit` 在 CI 的 Vitest job 中运行，`arch:test` 也可以单独运行它。测试用 Node 的 `fs.globSync`，不新增 `tinyglobby`。类名规则只比较外观类（去掉 margin、宽高、栅格等布局类），6 个以上外观类出现在 3 个以上文件中即失败，目前为 0。

### 5. CI 与本地入口

`check:arch` 放在 CI 的“Format, check and build” job 中、`pnpm lint` 之后运行。该 job 已经安装依赖，五项检查合计只需要几秒，不新增 job，也不改动 `Test result` 汇总。`make check` 同样运行它。

### 6. 治理顺序

先做不改变外观的部分，再做需要截图的部分：

1. 不改变 UI：删除未使用的导出；把 4 处挂载组件的领域模块移到组件层；Iconify 迁移（同一图标，外观不变）；用 `data-*` 属性替换测试选择器类名；把任意值换成 Tailwind 已有的工具类或 `@utility`。
2. 小幅 UI：为 Button/Input/Textarea 等补充 size 或 variant（如触控尺寸、等宽小字），替换使用处的覆盖，以及 `lib/touch-targets.ts` 的动态类名；原生控件换成 `ui/` 组件。
3. 收尾：清空基线，删除空的基线文件或保留空基线，并把 knip 的 `exports`/`types` 升为 `error`。

动态几何值（虚拟列表行高、图片缩放、视口高度、预览行数）属于合理的运行时样式。处理方式是改为 CSS 自定义属性加工具类；无法改写时，在 `docs/frontend-architecture.md` 中登记为例外，而不是一直留在基线中。

## Risks / Trade-offs

- [新增约 190 个开发依赖包，安装时间和锁文件体积增加] → 只影响开发和 CI；新依赖都不需要安装脚本，`pnpm-workspace.yaml` 的白名单不变。
- [`@shadcn/lint` 版本为 0.2.x，规则启发式可能在小版本中变化] → 依赖使用 `^0.2.3`（0.x 下只接受补丁更新）；升级时重建基线，并在 PR 中说明差异。
- [基线文件变成“合法化”存量的地方] → `arch:ratchet` 只收缩基线；文档和 `AGENTS.md` 禁止扩大基线。tasks.md 列出全部存量，治理完成时基线应为空。
- [ESLint 按文件计数，修改大文件时可能一次报出整批存量] → 这是预期行为；修改者可以顺手治理，或拆小改动。
- [jscpd 会把生成文件中的重复算作克隆] → 由 `scripts/build-fonts.py` 生成的 `fonts.css`、`fonts-terminal.css` 已加入 jscpd 的忽略列表；以后新增生成文件时同样处理。

## Migration Plan

1. 第一个 PR（本 draft PR）：加入工具、配置、基线、文档与 CI 步骤，不修改 `src/`。
2. 后续 PR 按 tasks.md 第 2–7 节分批治理，每个 PR 运行 `arch:ratchet` 并提交收缩后的基线；改变可见 UI 的 PR 附前后截图。
3. 回滚：从 CI 和 `make check` 中移除 `check:arch` 步骤即可，工具配置不影响构建产物。

## Open Questions

治理过程中已解决：

- 动态几何值不需要例外：全部改为 `style:--name` 加读取它的工具类，计算结果不变。
- `cn-rtl-flip` 在 `styles.css` 中定义为只在 RTL 下水平翻转的工具类，生成组件保持不变。
- Button 的 `wm-control` 在触控设备上带有不受工具类控制的最小高度，不适合虚拟列表行和标签，因此新增 `ListButton` 承担列表、标签栏与卡片中的可点击区域。
