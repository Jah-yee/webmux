## 1. 真实 agent 基线

- [x] 1.1 在隔离临时 HOME 与独立 webmux 实例中，用 `script` 或直接读取 PTY 的方式，分别抓取 omp 18.12.6、Codex CLI ≥0.164.0、OpenCode、Claude Code ≥2.1.295 启动、提问或审批、完成、出错、退出的原始输出。记录内容包括：7501 探测与上报的确切字节、终止符、是否发送 Warp 事件、OSC 9 或 BEL。结果写入本变更目录下的 `baseline.md`，确认规格中的应答格式与 Claude 的探测门槛成立；不成立时先修订规格。OpenCode 没有可运行的 2.x 发行版，未能实测，已在 `baseline.md` 中如实记录。
- [x] 1.2 针对 Claude Code，在仅回答 DA1、仅回答 7501、两者都不回答三种条件下各验证一次其是否输出根记录，结果记入 `baseline.md`。

## 2. 扫描器与根记录

- [x] 2.1 在 `internal/session/osc.go` 实现 7501 body 纯函数解析（键名、去空白、重复键、`state`/`kind`/`id` 校验，不保留其他值），以及 `7501;?`、`133;A`、RIS 分派。验证：`osc_test.go` 表驱动用例覆盖合法与非法 state、未知 kind、合法与非法 id、子记录、超过 4096 字节、逐字节拆分，以及以 BEL 和 ST 两种方式结束，全部通过。
- [x] 2.2 在 `internal/session/attention.go` 加入根记录、`structured` 标记、`Attention.layer` 与 `programStatusLocked`，按规格实现映射、仅在记录变化时置位、L1 独占、低层不能覆盖高层、根记录存在时忽略 Warp 清除事件、`133;A` 与 RIS 丢弃规则。验证：`attention_test.go` 覆盖规格中“锁存式关注状态”与“关注的清除条件”的全部场景。
- [x] 2.3 确认 `msg`、`title` 不会进入 `Info`、日志或负载。验证：用例对含 base64 正文的上报断言 `Info` 的 JSON 序列化不含正文的明文与 base64 形式。

## 3. 状态合成

- [x] 3.1 重构 `internal/session/status.go` 的 `classify` 输入为结构体，实现五级顺序（退出 → 根记录 → 关注 → 结构化降级 → 既有启发式）与 `working` 超过空闲阈值转为 `idle` 的保护；`evaluate` 只在第 5 级计算进程树等待。验证：`status_test.go` 表驱动覆盖规格中“关注与活动状态合成”的全部场景，并保留现有用例不变且通过。
- [x] 3.2 让 `Session.read()` 的即时状态写入复用同一合成函数，避免把由根记录决定的 `idle` 闪回 `running`。验证：新增用例在根记录为 `idle` 时喂入输出，断言状态保持 `idle`。

## 4. 探测应答队列

- [x] 4.1 在 `internal/session` 实现 `pendingReplies`（上限 4 条）、`replyWake`、`replyLoop`；`writeParts` 获得闸门后先排空应答，来源为 `inputReply`；会话退出时取消并 join `replyLoop`。验证：`go test -race ./internal/session` 通过；用例覆盖无客户端应答、超过上限时丢弃、退出时取消、应答不清除关注且不更新 `lastInputAt`。
- [x] 4.2 证明应答先于后续客户端输入。验证：用例在同一次 PTY 输出中写入 `7501;?` 与 `CSI c`，订阅者收到输出后立即以 `WriteInput(reply=true)` 写入模拟的 DA1 应答，断言 PTY 输入端读到的字节中 7501 应答完整位于 DA1 应答之前；循环 200 次，在 `-race` 下稳定通过。
- [x] 4.3 回放不触发应答。验证：`replay_test.go` 或 `api` 层用例在回放缓冲区含有探测时附着并重新附着，断言 PTY 只收到一次应答。

## 5. 原生 attach

- [x] 5.1 在 `internal/attach` 的输出写入路径删除完整的 `OSC 7501;?` 探测（支持跨块，暂存不超过 16 字节），其他 7501 上报与输出原样转发，offset 按原始字节推进。验证：`probe_test.go` 覆盖跨块拆分、BEL 与 ST 两种结束方式、非探测上报透传、以及不相关的 OSC 不受影响。

## 6. 配置与装配

- [x] 6.1 在 `internal/config` 增加 `program_status`（默认 `true`），经 `cmd/internal/assemble` 转为 `session.Options.DisableProgramStatus` 并传入 `Session`；关闭时不应答，且 7501、133;A、RIS 不产生效果。验证：`config` 与 `assemble_test.go` 的默认值和关闭用例通过；`session` 用例断言关闭后同一输出不改变关注或状态，PTY 也收不到应答。
- [x] 6.2 更新 `deploy/config.example.toml`，加入带注释的 `program_status = true`。验证：`webmux config validate` 能接受该示例文件。

## 7. 端到端

- [x] 7.1 在 `web/test/e2e/attention.spec.ts` 增加用例：用 bash 脚本模拟 agent，先输出 7501 探测并读取应答（`dd` 读取定长应答后匹配 `7501;?:states=`），再上报 `blocked`+`permission` 并持续重绘 spinner。断言：列表显示“等待授权”、`status=waiting`、`attention.source=agent`；上报 `working` 后关注清除且为 `running`；上报 `done` 后显示“已完成”；用户输入后变为 `idle`。验证：`pnpm --dir web exec playwright test test/e2e/attention.spec.ts` 通过。
- [x] 7.2 增加用例：只发 Warp 事件的模拟 agent 在 `prompt_submit` 之后保持读 tty、静默超过 2 秒且末行匹配提示符，断言不出现“可能在等你”；普通 shell 会话的既有用例保持通过。
- [x] 7.3 运行 `go test ./internal/api -run TestWireSnapshots`，确认 wire 快照无变化（负载零变化），并运行 `make test` 全部通过。本地已通过 Go race 全套、Vitest 单元与浏览器测试及 attention E2E；完整 E2E 由 CI（Ubuntu 6 分片与 macOS 3 分片）全部通过。

## 8. 文档与用户故事

- [x] 8.1 更新 `docs/usage.md` 的“需要你介入的提醒”：加入 OSC 7501 来源表、来源优先级与状态合成说明；说明 omp、Codex、OpenCode 无需配置，Claude Code ≥2.1.295 无需 hook，旧版本仍可用 hook 方案；说明 `program_status` 开关。验证：Markdown 格式检查（`make format-check`）通过。
- [x] 8.2 更新 `docs/deployment.md` 配置表与 `docs/architecture.md` 的关注段落。验证：`make format-check` 通过，链接路径存在。
- [x] 8.3 更新 `docs/user-stories.md`：US-C05（结构化信号对启发式的降级）、US-C06（7501 来源、探测应答、来源分层）、US-K04（`program_status`），并在 C 组依据中补充 `osc.go`、`attention.go`、`status.go` 等实际源文件。验证：故事 ID 唯一、索引引用可解析、所列源码路径均存在。
