## Why

WebMux 的前端大部分由 coding agent 编写。`docs/design-system.md` 与 `AGENTS.md` 已经写明“复用 shadcn-svelte 组件、用 variant/size 而不是页面覆盖、使用主题令牌、用 Iconify 打包图标”，但这些约定只靠文字约束，没有任何检查：Oxlint 只读 `.svelte` 的 `<script>`，`svelte-check` 只检查类型。

按 `frontend-architecture-guard` 方案（ESLint + `@shadcn/lint`、dependency-cruiser、jscpd、knip、架构测试）对 2026-10-24 的 `main`（`8afeffb`）实测，结果如下：

- 用 `class` 覆盖设计系统组件外观 106 处，分布在 25 个文件，集中在 Button、Textarea、Input、CollapsibleTrigger、Empty、PopoverTrigger、TabsContent、CardContent 等。
- 手写原生控件 23 处（`<button>` 9、`<input>` 6、`<select>` 4、`<label>` 4），其中 10 处在 `CodeNavigator.svelte`。
- 任意值 47 处（如 `w-[3ch]`、`[overflow-wrap:anywhere]`、`max-h-[50dvh]`、`focus-visible:ring-[3px]`、`text-[11px]`）；`style:` 内联样式 10 处；Tailwind 不认识的类名 7 处（6 处用作测试或样式选择器，1 处是生成组件中未定义的 `cn-rtl-flip`）；无法静态检查的动态类名 11 处。
- 21 个业务文件仍直接导入 `@tabler/icons-svelte`，违反 Iconify 约定。
- 4 处分层违规：顶层领域模块 `code-search.ts`、`code-reader-panels.ts`、`codePopout.ts` 直接导入并挂载 `.svelte` 组件。
- 3 处重复代码块（`SoftKeys.svelte`、`Settings.svelte` 模板与 `design-tokens.css` 的明暗主题段；生成的字体 CSS 不计入）；10 个未使用的导出和 1 个未使用的类型。
- 没有循环依赖、没有在 `ui/` 外直接使用 `bits-ui`/`tailwind-variants`、没有原始调色板颜色，也没有同名或重造 `ui/` 组件的业务组件。这些需要保持。

没有检查时，agent 每次新增 UI 都可能复制样式、重造控件或绕过设计系统，问题只能靠人工评审发现。

## What Changes

- 引入前端架构守卫，入口为 `pnpm --dir web check:arch`，`make check` 和 CI 的“Format, check and build” job 也运行它：
  - ESLint 只加载架构规则：`@shadcn/lint` 的 `no-restyle`、`no-raw-colors`、`no-arbitrary-values`、`no-inline-styles`、`no-unknown-classes`、`require-static-classes`，`svelte/no-restricted-html-elements`（禁止手写 button/input/textarea/select/dialog/label），以及禁止在 `ui/` 外导入 headless 原语、变体工具和 `@tabler/icons-svelte`。Oxlint 仍是通用 lint。
  - dependency-cruiser 按现有扁平目录定义分层：`ui/` 为叶子、`src/lib` 不依赖业务、领域模块和 hooks 不依赖 `.svelte`、页面只由 `App.svelte` 组合；禁止循环依赖和孤立模块。
  - jscpd 只拦截新增的重复代码；knip 检查未使用的文件、依赖和重复导出。
  - Vitest 架构测试检查同名业务组件、重造 `ui/` 组件，以及跨 3 个以上文件复制的外观类。
- 存量违规写入提交的基线文件（ESLint bulk suppressions、dependency-cruiser known violations、jscpd baseline），检查只拦截新增违规；`arch:ratchet` 只删除已修复的记录。
- 新增 `docs/frontend-architecture.md`，并在 `AGENTS.md` 中要求“先找再写”和完成前运行 `check:arch`。
- 按 tasks.md 分批治理存量违规，每批收紧基线，最终清空基线并把 knip 的导出检查升为错误。
- 不改变产品行为。本变更的第一个 PR 只加入工具与基线；后续治理 PR 中改变可见 UI 的部分按项目要求附截图。

## Capabilities

### New Capabilities

- `frontend-architecture-guard`：前端代码复用设计系统、使用主题令牌、遵守分层的可执行检查，以及存量基线只减不增的约定。

### Modified Capabilities

无。

## Impact

- 前端工具：`web/package.json`（新增 devDependencies `eslint`、`@shadcn/lint`、`eslint-plugin-svelte`、`svelte-eslint-parser`、`@typescript-eslint/parser`、`dependency-cruiser`、`jscpd`、`knip`，以及 `arch:*`、`check:arch` 脚本）、`web/pnpm-lock.yaml`；新增 `web/eslint.config.mjs`、`web/.dependency-cruiser.cjs`、`web/.jscpd.json`、`web/knip.json`、`web/test/architecture.test.ts` 和三个基线文件。新依赖都不需要安装脚本，`pnpm-workspace.yaml` 的构建脚本白名单不变。
- 构建与 CI：`Makefile` 的 `check`，`.github/workflows/test.yml` 的格式与构建 job。
- 文档：`docs/frontend-architecture.md`（新增）、`docs/design-system.md`、`AGENTS.md`、`CONTRIBUTING.md`、`docs/user-stories.md`（新增 US-L06）。
- 治理阶段会修改 `web/src/components/`、`web/src/pages/`、`web/src/lib/components/ui/` 中的组件与 variant，以及 `web/src/styles.css`、`design-tokens.css`。
- 不涉及：后端、REST/WebSocket 契约、运行时依赖与产物体积。
