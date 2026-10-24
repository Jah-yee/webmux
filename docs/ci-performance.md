# CI 耗时优化与验收

按 [autoresearch](https://github.com/uditgoenka/autoresearch) 的测量、修改、验证、保留/撤回循环优化。指标是实际墙钟耗时，目标为至少降低 50%。测试数量、跳过规则、断言和 race 检查是约束条件。

## 本机测量

2026-10-21，macOS arm64、Apple M5（10 核、32 GiB）、Node 24.22.0、pnpm 12.11.2、Go 1.26.1。固定源码与依赖快照，串行测量；基线两轮、最终版本三轮，取中位数。Go 使用 `-count=1`，未复用测试结果缓存。E2E 和 Storybook 每轮都包含实际构建。

| 环节                 |     基线 |  优化后 | 耗时降低 | 约束                                         |
| -------------------- | -------: | ------: | -------: | -------------------------------------------- |
| Playwright E2E       | 165.72 s | 72.50 s |    56.3% | 同一 126 个用例，113 通过、13 按原规则跳过   |
| Go race              |  31.35 s | 14.87 s |    52.6% | 同一 386 条含子测试的结果，385 通过、1 跳过  |
| Storybook 构建与测试 |  68.50 s | 33.55 s |    51.0% | 同一 199 个用例，包含全部视口/字号与明暗主题 |

E2E 的 worker 数从 2 调到 4，Storybook 从 2 调到 6，使用同一台机器；这些是配置优化的总体收益，不能称为相同 worker 数下的算法收益。没有增加 CI job 或 runner 数量。本机数字不能代替 GitHub runner 的整体耗时验收。

这些数字来自固定快照的本机实验。候选分支仍须完整回归和 Actions 计时，不能把不同源码或负载条件混入前后比较。

## 改动

- 本地 E2E 每次调用构建一次真实的生产前端和嵌入式 Go 程序，各 worker 只共享不可变可执行文件。Go 源码与前端产物进入独立临时目录，避免其他构建清理 `web/dist` 时破坏嵌入。每个 worker 仍有独立的后端、HOME、状态目录、端口、仓库和 PTY。
- E2E 按用例调度，均衡长文件和分片。仍使用真实键盘、触摸、终端输出和原来的时间窗口。
- Go 的独立资源用例并行；workspace 的公共 Git 环境在测试进程启动时设置一次。会修改 PATH 或钩子环境的用例保持串行。`-parallel=4` 限制每个包的测试并发。
- Storybook 将视口/主题矩阵移到 test-scoped 配置，复用浏览器进程，每个用例仍创建全新上下文。试点击自带滚动与稳定性检查；禁用按钮仍显式滚动，两种情况都检查完整入屏。生产构建、tree shaking、语法高亮 worker 和反例检测保持验证。
- Storybook 在 CI 只为首次重试记录 trace，与 E2E 一致；失败截图、报告和本地失败 trace 保留。Storybook 重试只用于诊断，flaky 仍使 CI 失败；速度比较也拒绝 flaky 结果。
- CI 安装 Chromium 及其 headless shell，覆盖通知与常规浏览器用例。macOS 在一个 job 内并行运行 Go race 和 E2E，并等待两个退出码；任一失败都会令 job 失败。E2E 通过显式的 `WEBMUX_E2E_DIST` 路径复制该 job 刚生成的生产产物，避免重复构建，默认运行仍从源码构建。`Test result` 仍要求所有 job 成功。
- Ubuntu E2E 的格式检查与构建 job 统一构建当前提交的内嵌程序，使用 tar 保留可执行权限并通过同轮 artifact 分发给六个分片。分片通过 `WEBMUX_E2E_BINARY` 使用该程序，不安装 Go、不恢复 Go 缓存，也不重复构建前端或后端；显式路径无效时直接失败。本地未指定时仍从源码构建，分片的后端进程、HOME、目录、端口和 PTY 仍各自独立。Node/pnpm 安装与浏览器系统依赖仍由每个 runner 准备。
- Ubuntu E2E 保持两个 worker，避免较少 CPU 上的争用；macOS 使用四个。E2E 和 Storybook 的 flaky 结果均使 CI 失败，重试仅保留诊断。异步生成默认分支名的用例等待初始值后再填入目标名称。

## 复测

先在基线源码运行，再在候选源码运行；需要复制测量脚本到旧版本时，仅复制脚本。不要同时运行其他重任务。

```sh
node web/scripts/benchmark-ci.mjs --suite e2e --workers 2 --runs 3 \
  --label before --output /tmp/webmux-before-e2e.json
node web/scripts/benchmark-ci.mjs --suite e2e --workers 4 --runs 3 \
  --label after --output /tmp/webmux-after-e2e.json \
  --compare /tmp/webmux-before-e2e.json --allow-worker-change
```

Storybook 使用 `--suite storybook`，分别设置 2 和 6 个 worker。脚本记录每次构建/测试耗时，校验用例身份、预期跳过状态、Node、平台和 CPU 数；worker 数变化必须显式声明。视口矩阵从 project 移到 describe 后，按原矩阵名称规范化身份。原始 JSON、日志默认写入被忽略的 `web/test-results/ci-benchmark/`。

Go 可使用以下命令分别重复测量，比较实际耗时及 JSON 中的测试身份与结果：

```sh
go test -race -count=1 -parallel=4 -json ./...
```

## GitHub Actions 验收

基准提交 `424b208` 的[成功运行 39479795307](https://github.com/mzdluo123/webmux/actions/runs/39479795307)，从首个 job 开始到最后一个 job 完成为 615 秒。队列等待单独报告；并行 job 的耗时不相加。整体 50% 目标为不超过 307.5 秒，同时每个原有 job 都成功。

推送并运行候选版本后，将 `NEW_RUN_ID` 换成实际运行 ID：

```sh
node web/scripts/benchmark-actions.mjs \
  --baseline 39479795307 --candidate NEW_RUN_ID
```

脚本要求两次运行全部成功、job 名称集合一致，保存墙钟与队列耗时，并仅在整体耗时降低至少 50% 时返回成功。在真实 runner 完成此验收前，整体 CI 目标仍是未验证状态。
