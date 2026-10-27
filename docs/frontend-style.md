# 前端代码质量基线

本文把 [Svelte 官方 Best practices](https://svelte.dev/docs/svelte/best-practices) 确立为 WebMux 前端的主规范，并以 TypeScript 严格检查、Svelte 模板规则与分层测试作为可执行的落地手段。它描述"写前端代码时以什么为准"，不重复 [前端架构守卫](frontend-architecture.md) 的组件复用与分层规则，也不重复 [设计系统](design-system.md) 的视觉令牌规则。

适用范围：`web/src`、`web/test`、`web/stories` 与前端配置文件。与 `AGENTS.md`、[架构设计](architecture.md)、[前端架构守卫](frontend-architecture.md)、[设计系统](design-system.md) 冲突时，以仓库既有架构约定为准，并在本文"本地化条款"登记例外。

## 规范来源

| 文档                                                                                         | 在项目中的角色            | 主要覆盖                                        |
| -------------------------------------------------------------------------------------------- | ------------------------- | ----------------------------------------------- |
| [Svelte Best practices](https://svelte.dev/docs/svelte/best-practices)                       | **主规范**                | 响应式状态、派生值、副作用、列表、参数、Context |
| [Svelte Testing](https://svelte.dev/docs/svelte/testing)                                     | 测试分层指导              | 单元、组件与端到端测试的职责边界                |
| [shadcn-svelte 文档](https://www.shadcn-svelte.com/docs)                                     | 组件源码、主题与 CLI 维护 | `ui/` 组件的定制、更新与覆盖审查                |
| [Bits UI 文档](https://www.bits-ui.com/docs/components/dialog)                               | 无头原语的键盘与焦点行为  | Dialog、Select、Menu 的可访问性契约             |
| [eslint-plugin-svelte 用户指南](https://sveltejs.github.io/eslint-plugin-svelte/user-guide/) | 规则到自动检查的映射      | 模板级规则、parser 与 TypeScript 集成           |
| [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)            | **补充参考**              | 类型设计、命名、模块组织                        |
| [TypeScript tsconfig 参考](https://www.typescriptlang.org/tsconfig/)                         | 编译选项语义              | `strict`、`noUncheckedIndexedAccess` 等         |
| [Playwright Best practices](https://playwright.dev/docs/best-practices)                      | E2E 编写指导              | 用户可见行为、用例独立性与定位方式              |

Google 的指南自述部分条款受其内部环境约束，因此只采纳类型设计与模块组织部分；Svelte 语法、响应式规则与工具链配置以 Svelte、shadcn-svelte、Bits UI 与 Playwright 的官方文档为准。

## 本地化条款

上游建议与本仓库现状不同时，按此表执行，不再逐条讨论：

| 上游建议                         | 本仓库决定                       | 理由                                                         |
| -------------------------------- | -------------------------------- | ------------------------------------------------------------ |
| Prettier 负责格式化              | 继续使用 Oxfmt（80 列）          | `format:check` 已进 CI，避免第二套格式化工具                 |
| SvelteKit 的 `load` 与服务端状态 | 不适用                           | 纯 Vite SPA，无 SSR 与服务端 `load`                          |
| `sv add eslint prettier vitest`  | 不执行                           | 会覆盖现有 oxfmt、Oxlint、ESLint 双轨与自定义配置            |
| `openapi-typescript` 生成类型    | 用 Go wire 快照做契约测试        | 仓库没有 OpenAPI spec；先产出 spec 的成本高于消费已有快照    |
| `features/<业务模块>` 目录       | 不采纳，沿用扁平分层             | 见 [前端架构守卫](frontend-architecture.md) 的分层表         |
| Testing Library                  | 继续使用 `vitest-browser-svelte` | 已在用，且固定视口并用 CDP 真触摸                            |
| `exactOptionalPropertyTypes`     | 暂缓，先记录基线                 | 存量错误集中在 shadcn 上游与 `RequestInit`，收益低于改造成本 |

## 日常审查规则

这七条覆盖绝大多数 Svelte 5 代码评审，按顺序检查：

1. **只有驱动界面更新的状态才用 `$state`。** 仅参与流程控制、不参与渲染的一次性标志位用普通 `let`；整体替换的列表优先 `$state.raw`。
2. **能从已有状态算出的值用 `$derived` / `$derived.by`。** 不要把派生结果再存进 `$state` 手动同步，也不要用 `$effect` 回写。
3. **`$effect` 只做副作用。** 典型用途是订阅、计时器、观察器与 DOM 集成；必须返回清理函数，且不能写入自身依赖。
4. **可增删、排序的列表用稳定业务 ID 作 key。** 只有内容固定的静态列表才允许 index。
5. **`$props()` 必须有类型。** 可选参数给默认值；需要双向绑定时显式 `$bindable`，不要让可绑定 prop 充当派生输出。
6. **事件统一使用 Svelte 5 属性写法。** 使用 `onclick={...}` 等，不新增 `on:` 指令；异步处理函数进入 `onclick` 前要显式处理 rejection。
7. **跨层共享用 Context，模块单例只用于进程级配置。** `setContext` / `getContext` 优先于隐式全局；单实例应用的模块级状态需集中在 `src/*.svelte.ts` 并说明生命周期。

组件参数、列表与 Context 的完整说明见 [Svelte Best practices](https://svelte.dev/docs/svelte/best-practices)。可访问性要求（图标、弹窗键盘、焦点返回、触控尺寸）见 [设计系统](design-system.md)。

## 检查映射

| 规则                               | 执行方式                                                | 现状   |
| ---------------------------------- | ------------------------------------------------------- | ------ |
| 类型正确性与未检查索引             | `pnpm --dir web check`（含 `noUncheckedIndexedAccess`） | 已启用 |
| 格式化                             | `make format-check`（Oxfmt）                            | 已启用 |
| 通用 JS/TS 问题                    | `pnpm --dir web lint`（Oxlint）                         | 已启用 |
| 组件复用、令牌、分层、重复与死代码 | `pnpm --dir web check:arch`                             | 已启用 |
| 异步 Promise 未处理                | `pnpm --dir web lint`（Oxlint 类型感知）                | 已启用 |
| Svelte 模板规则                    | `svelte.configs.recommended`                            | 已启用 |
| 测试代码类型                       | `tsconfig.test.json` + `check:test`                     | 已启用 |
| 前后端契约                         | `web/test/wire-contract.test.ts` 消费 Go wire 快照      | 已启用 |

`pnpm --dir web lint` 通过 `oxlint-tsgolint` 启用 Oxlint 类型感知，`typescript/no-floating-promises` 与 `typescript/no-misused-promises` 仅作用于 `src` 下的 `.ts` 与 `.svelte.ts`。Oxlint 只解析 Svelte 的 `<script>`，不提供模板规则，也不对 `.svelte` 脚本做类型感知检查；模板事件处理器中的异步 rejection 仍需按第 6 条人工审查，Svelte 模板规则由 `pnpm --dir web check:arch` 的 ESLint 执行。类型感知模式同时启用其余 correctness 规则（`await-thenable`、`no-array-delete`、`no-base-to-string`、`no-implied-eval`、`no-misused-spread` 与 `unbound-method`）；`codePopout.ts` 的属性描述符解构和 `terminal.svelte.ts` 的对象方法转交不依赖 `this`，在这两个文件关闭 `unbound-method`。

`check`、`build`、`check:storybook` 与 `check:test` 都以 `--fail-on-warnings` 运行，警告不得静默累积。引入带存量违规的新规则时，先记录基线，再按 [前端架构守卫](frontend-architecture.md#基线与新规则) 的方式收紧。Markdown 预览唯一保留的 `{@html}` 必须先经过专用 DOMPurify 实例；嵌套控制器接口通过模板别名读取，`svelte/no-unused-props` 只放宽这类嵌套属性，顶层 props 仍受检查。

## 测试分层

| 层         | 工具                                          | 优先覆盖                                   |
| ---------- | --------------------------------------------- | ------------------------------------------ |
| 纯逻辑     | Vitest（Node）                                | 解析、转换、校验、状态映射、重试与错误映射 |
| 组件行为   | Vitest Browser Mode + `vitest-browser-svelte` | 输入、提交、加载与错误状态、弹窗交互       |
| 端到端     | Playwright（桌面与 Pixel 7）                  | 关键用户流程、失败路径、键盘与触控         |
| 视觉与溢出 | Storybook + Playwright                        | 极端内容、明暗主题、窄屏布局               |

测试定位优先使用角色与标签（`getByRole`、`getByLabel`），断言用户可见行为；避免内部类名、实现细节与固定 `sleep`。用例之间保持独立，不连接开发中的服务。

## 维护约定

- 新增或调整本地化条款时，同步更新本文与 issue #149 的任务状态。
- 新增必须遵守的规则时，优先落成可执行检查；无法自动化的，写入"日常审查规则"并在 PR 模板或评审清单中引用。
- 不通过扩大基线、添加 `eslint-disable` 或跳过测试来满足门禁；确有例外时在 PR 中说明原因。
- 规范文件本身遵守 [文档治理](doc-governance.md) 的规模限制。
