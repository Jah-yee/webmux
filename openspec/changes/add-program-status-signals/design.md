## Context

动机见 proposal.md，行为合同见 `specs/program-status`、`specs/agent-attention` 和 `specs/terminal-control`。

### 现状

- **扫描器**：`internal/session/osc.go` 的 `OSCScanner` 在 `Session.read()` 持有 `s.mu` 时被调用，逐字节维护 OSC/DCS 状态，并经 `SignalHandler` 回调 Title、Bell、Notify、Event、Cwd。
  - 未知 OSC，包括 7501、133，在 `finish()` 中被直接丢弃。
  - RIS（`ESC c`）落在 `scanEscape` 的 default 分支。
  - 负载上限 4096 字节，与 7501 的整条上限相同。
- **关注**：`attention.go` 用 `raiseAttentionLocked` 按种类优先级合并，`agentEventLocked` 处理 Warp 事件。`Attention` 只有 `Kind`/`Source`/`Since`，没有来源层级。
- **状态**：`status.go` 的 `classify` 顺序为：退出 → 关注 → 2 秒输出窗口 → 提示符正则 → 进程树 `awaitingInput` → 空闲阈值。
  - `read()` 每读到一块输出，就把状态写成 `running`；存在关注时写成 `waiting`。
  - `Manager.updates()` 每 500 ms 调用 `evaluate` 重新分类。
- **输入**：`writeParts` 以 `inputGate`（容量为 1 的令牌通道）串行化所有 PTY 写入。`inputReply` 来源不计为用户输入。令牌通道不保证先到先得。
- **原生 attach**：`internal/attach` 把服务端输出原样写到本地终端。本地终端产生的应答作为普通输入回传。

### 实测与外部事实

见 proposal 中的协议链接。

- 7501 rev 0.4 的应答格式为 `OSC 7501 ; ? : states=… : kinds=… ST`，列表以逗号分隔。规范要求：DA1 应答先到，程序即判定为不支持。
- omp 18.12.6 与 Codex 0.164.0 的源码显示，它们只要 stdout 是 TTY 就发送根记录，并在退出时发送 `state=clear`。
- Claude Code 2.1.295 的探测门槛和超时时间来自二手资料，需在实施第一步用真实 PTY 抓取确认。

## Goals / Non-Goals

**Goals:**

- 一个有界、可取消、顺序可证明的会话级应答通道。它先服务于 7501 探测，结构上可被图形协商变更复用。
- 来源层级与状态合成规则全部集中在 `session` 包内，并有表驱动测试覆盖。
- 线上协议零变化：contract、wire、schemas、推送负载和 wire 快照都不变。

**Non-Goals:**

- 不处理 7501 子记录，也不聚合 `id` 子树。不展示 `progress`、`app`、`msg`、`title`。
- 不新增关注种类。`auth` 映射为 `question`。
- 不接管 DA1 或其他设备查询，不修改 xterm 的应答行为。
- 不为任何 agent 编写核心分支，不注入 hook，不改用户配置文件，不设置 `TERM_PROGRAM`。
- 不移除 Warp 协议注入、`agent_protocol` 或既有启发式。

## Decisions

### 1. 7501 解析放在现有扫描器内

`finish()` 增加三个分派：

| 前缀 | 处理 |
| --- | --- |
| `7501;?`（负载恰为 `7501;?`） | 探测回调 |
| `7501;` | 解析 body 后回调 `Status(state, kind string)` |
| `133;A`（负载为 `133;A` 或以 `133;A;` 开头） | 提示符回调 |

`scanEscape` 遇到 `c` 时触发 RIS 回调。

解析函数是纯函数：按 `:` 切分、去空白、校验键名、后出现的重复键覆盖前面的。只返回 `(state, kind, root bool, ok bool)`，不保留任何其他值。`id` 只做合法性校验，然后丢弃。

**备选方案：新开一个 7501 专用扫描器。** 不采用，因为跨块拆分、ST/BEL 终止和超长处理都已经在现有状态机里，重复实现反而会造成边界不一致。

### 2. 会话级应答队列与输入闸门的顺序

关键是证明“应答先于浏览器对随后 DA1 的应答”。仅仅在读取时把应答交给某个 goroutine 写入并不够：它和 WebSocket 输入同时争抢 `inputGate`，而令牌通道不保证先到先得。

**采用“先入队、闸门持有者先排空”：**

- `Session` 增加一个受 `s.mu` 保护的待写应答切片 `pendingReplies`，上限为 4 条，超出的探测丢弃。另有一个容量为 1 的唤醒通道 `replyWake`。
- `read()` 在持有 `s.mu`、`osc.Feed` 期间收到探测回调时，追加应答并非阻塞地发出唤醒。由于这一步发生在同一次锁内的 `broadcastLocked(outputMessage)` 之前，任何客户端此后产生的输入都必然晚于入队。
- `writeParts` 在拿到 `inputGate` 之后、写入自身内容之前，先取出并写完 `pendingReplies`（来源 `inputReply`）。这样即使客户端输入先拿到闸门，也会先替会话写完应答。
- 一个随会话启动的 `replyLoop` goroutine 在被唤醒且没有其他写入者时，自行获取闸门并排空，覆盖无客户端的情况。它使用以 `s.done` 取消的上下文，以及与 `writeParts` 相同的 10 秒写入预算；会话退出时等待它结束（join）。
- 排空时在 `s.mu` 下取走切片，释放锁后再写 PTY，不持锁等待可写。写入者的上下文已取消时不取队列。一个字节都没写出的应答连同其后的应答放回队首（合并后仍截断到 4 条），并唤醒 `replyLoop` 补发；已写出部分字节时丢弃剩余应答、不重试，避免同一探测被应答两次。

**备选方案：**

- **(a) 等 `negotiate-terminal-graphics-capabilities` 把 DA1 一并改为会话权威。** 语义最干净，但会把 Claude 接入绑在一个大得多的变更上。
- **(b) 在读路径直接同步写 PTY。** 会在持锁时阻塞读取，违反 AGENTS.md。

两者都不采用。本方案的 `pendingReplies` 与 `replyLoop` 正是图形协商设计所说的“会话内单一 FIFO”，后续可以直接复用。

### 3. 内部层级与根记录状态

- `Attention` 增加未导出字段 `layer`。wire 转换只读取导出字段，因此不影响负载。
- `Session` 增加三个字段，均受 `s.mu` 保护：
  - `program *programRecord`：根记录的 `state` 与 `kind`；
  - `programAt time.Time`：最近一次根记录上报的时间，仅用于调试或测试；
  - `structured bool`：进程生命周期内是否收到过 L1 或 L2 信号。
- `programStatusLocked(state, kind)` 的流程：
  1. 与当前记录比较。相同则直接返回；不同则替换记录。
  2. 按映射表替换关注（`layer = L1`），或清除关注。
  3. `clear` 时把记录置空。
- `raiseAttentionLocked` 增加 `layer` 参数：
  - `program != nil` 时，L2–L4 直接返回。
  - 否则，当前关注的层级高于新信号时返回。
  - 其余情况沿用既有种类比较。
- `agentEventLocked` 的清除类事件在 `program != nil` 时忽略。
- `133;A` 与 RIS 回调按规格丢弃记录；若当前关注的 `layer == L1`，一并清除。

**备选方案：把层级作为 `source` 的新枚举值暴露给前端。** 不采用。前端只需要种类文案，层级属于服务端的合并策略，暴露它会扩大 contract 和 schema。

### 4. 状态合成

- `classify` 的入参改为一个小的结构体，包含 `exited`、`program`、`attention`、`structured`、`blocked`、`idleAfter`、`patterns` 等，按规格中的五级顺序判定。
- `evaluate` 只在第 5 级才计算进程树 `awaitingInput`，以免为 agent 会话做无用的 `/proc` 扫描。
- `read()` 的即时状态写入改为调用同一个合成函数，并传入 `now` 与刚更新的 `LastActiveAt`。这样读路径不会把由根记录决定的 `idle` 闪回 `running`。
- `working` 的保护：超过 `idleAfter` 没有输出时为 `idle`。这复用已有阈值，不新增配置。
- `sameDisplay` 已经比较 `Status` 与 `Attention`，根记录引起的状态变化会自然推送，不需要新增比较字段。

**备选方案：`structured` 只在根记录或关注存在期间生效。** 不采用。Warp 协议没有“结束”事件，一旦回合结束、关注被输入清除，启发式会立刻把正在读 tty 的 TUI 判为“可能在等你”。这正是需要消除的误报。代价见风险一节。

### 5. 原生 attach 不转发探测

`internal/attach` 在把输出写到本地终端之前，删除完整的 `ESC ] 7501 ; ? (BEL|ESC \)` 序列：

- 跨块拆分时，暂存最多 16 字节的未决前缀。
- offset 与回放按原始字节计算。
- 其他 7501 上报照常转发，本地终端（如 cmux、kitty）因此可以显示 agent 状态。

浏览器不需要改动：xterm.js 按规范忽略未知 OSC，不会应答。实施时需用测试固定这一点。

### 6. 配置

- `config.Config` 增加 `ProgramStatus bool`（`toml:"program_status"`，默认 `true`）。
- 经 `cmd/internal/assemble` 转为 `session.Options.DisableProgramStatus`，再传到 `Session`，与 `agentProtocol`、`bellAttention` 的做法一致。
- 关闭时，扫描器的 7501/133/RIS 回调为 nil，等同于未知序列。`structured` 只会由 L2 置位。

## Risks / Trade-offs

- **[Claude Code 的探测细节与二手资料不符，例如对终止符或应答字段有不同要求]** → 实施第一步在隔离实例中用真实 Claude Code 抓取 PTY 字节。规格中的应答格式取自协议 rev 0.4；若真实行为不同，先回到规格修订再实现。
- **[`structured` 粘性导致误判]** shell 会话里运行过一次 omp 后退出，之后普通命令不再出现“可能在等你”。→ 只影响启发式的“等待”提示，不影响关注和推送；只要有新输出，状态仍为 `running`/`idle`。如果实测抱怨明显，后续可以把 `133;A` 作为 L2 粘性的重置点，这只是实现细节调整。
- **[agent 异常退出未发送 `clear`，留下陈旧的 `working` 根记录]** → 超过 `idle_after_seconds` 无输出显示为空闲；shell 发出 `133;A` 时丢弃；会话退出时清空。
- **[agent 在 `blocked` 时被强制结束（如 `kill -9`），且 shell 未启用语义提示符]** → `blocked` 的记录会一直显示等待。用户输入只清除提醒，不清除记录。这种记录不能像 `working` 那样按静默时长降级，因为真实的审批提示本来就长时间没有输出，降级会把真正的等待显示成空闲。恢复方式有三种：shell 发出 `133;A`、程序重新报告状态、会话退出。这是已知限制，已写入使用文档。实测的 agent 在正常退出（包括 Ctrl+C）时都会发送 `state=clear`。
- **[根记录持久化“已完成”]** `done` 根记录在用户查看后仍保留，状态显示为 `idle`。→ 这符合协议的“done 跨越提示符”语义，也与 Codex “用户确认后转 idle” 的行为一致。
- **[与图形协商变更同时修改“终端自动回复的归属”]** → 先归档的一方成为基线，另一方归档前重写自己的增量。本设计让应答队列可复用，以减少后续冲突。
- **[应答队列成为新的写入者]** → 应答走与用户输入相同的闸门和取消路径，上限为 4 条，回放不触发，会话退出时 join 后台 goroutine。测试覆盖无客户端、探测与 DA1 同块到达、取消与慢写。
- **[非预期程序也会被应答]** 任何发出 7501 探测的程序都会得到“支持”的回答。→ 这正是该协议的设计意图；`program_status = false` 可以整体关闭。

## Migration Plan

1. **建立基线**：在隔离实例中分别运行 omp、Codex、OpenCode、Claude Code，抓取探测、根记录上报和退出序列，记录哪些 agent 同时发送 Warp 事件或 OSC 9/BEL。
2. **实现**：先实现扫描器、记录与层级（不含探测应答），验证 omp 与 Codex；再实现应答队列和原生 attach 过滤，验证 Claude Code。
3. **兼容性**：新增配置有默认值，旧配置文件无需改动。前端与后端随同一二进制发布，负载不变，不存在版本错配。
4. **回滚**：设置 `program_status = false`，即回到本变更前的信号行为。唯一的残留差异是 L2 粘性对启发式的降级；如需完全回退，直接回退二进制。
