## Why

当前的“需要介入”提醒只在 omp 上开箱即用。原因是 webmux 唯一可靠的结构化来源是 Warp 的 `OSC 777;notify;warp://cli-agent` 事件，而原生发出它的 agent 只有 omp。其他 agent 的情况如下：

- Claude Code 需要用户手工安装 hook。
- Codex 只能给出模糊的 `notify`/`bell`。
- OpenCode、Gemini CLI 没有任何信号。

没有信号时只能依赖启发式。启发式的误报很明显：

- TUI agent 总在读 tty，进程树判定会让它在静默 2 秒后显示“可能在等你”。
- agent 输出的正文也可能命中提示符正则。

2026-10 出现的 OSC 7501 Program Status Protocol（rev 0.4，https://www.superlogical.com/rex/docs/build/program-status）已经被 omp 18.12.6、Codex CLI 0.164.0、OpenCode、pi、Crush 和 Claude Code 2.1.295 采用：

- 前几家只要 stdout 是 TTY 就会直接发送。
- Claude Code 先发 `OSC 7501;?` 探测，只有终端在 DA1 应答之前回应，它才会发送。

webmux 在 PTY 侧信道中识别 7501，并由服务端应答探测，就能在不改用户配置、不在核心中按 agent 分支的前提下，覆盖主流 agent。

与此同时，同一会话会同时出现多种来源：7501、Warp 事件、OSC 9/777、BEL 和启发式。例如 omp 同时发出 7501 和 Warp 事件；Codex 同时发出 7501 和 OSC 9/BEL。现有规则只按关注种类合并，可能出现两类问题：

- 来源之间互相清除或覆盖，例如 7501 的状态迁移被“弱信号不降级”规则挡住。
- 启发式误报。

这次需要明确各来源的优先级。

## What Changes

- **识别 OSC 7501 根记录**：在现有扫描器中增量识别 `OSC 7501 ; key=value(:key=value)* ST`。
  - 只采用无 `id` 的根记录，只读取 `state` 与 `kind`。
  - `msg`、`title`、`app`、`progress` 不解析为状态，也不存储、不下发。
  - 识别 `OSC 133;A` 与 RIS，按协议丢弃过期记录。
  - 原始字节、回放与渲染保持不变。
- **服务端应答探测**：会话在实时读取到 `OSC 7501;?` 后，经会话级有界应答队列写入应答 `OSC 7501;?:states=…:kinds=… ST`。
  - 该应答保证先于任何在此之后进入 PTY 的客户端输入（包括浏览器对随后 DA1 的应答），因此 Claude Code 能判定支持。
  - 服务端应答不取得控制权，也不清除关注。
  - DA1 及其他设备查询仍归负责客户端。
  - 回放永远不触发应答。
- **分层的信号来源优先级**：

  | 层级 | 来源                     |
  | ---- | ------------------------ |
  | L1   | 7501 根记录              |
  | L2   | Warp cli-agent 事件      |
  | L3   | OSC 9 / OSC 777 普通通知 |
  | L4   | BEL                      |
  | L5   | 启发式                   |

  - 7501 根记录存在期间，它是关注与状态的唯一权威：其迁移可以替换或清除关注，不受种类优先级限制；L2–L4 信号不改变关注。
  - 低层信号不能覆盖或清除高层置位的关注。
  - 同层内沿用既有种类优先级。
  - 启发式只影响 `status`，永不产生关注或推送。
- **状态合成的优先级**：

  | 顺序 | 条件                                           | 结果                                                                         |
  | ---- | ---------------------------------------------- | ---------------------------------------------------------------------------- |
  | 1    | 已退出                                         | `exited`                                                                     |
  | 2    | 存在 7501 根记录                               | 由记录决定 `running`/`waiting`/`idle`，完全绕过启发式                        |
  | 3    | 存在关注                                       | `waiting`                                                                    |
  | 4    | 当前进程生命周期内收到过结构化 agent 信号（L1 或 L2） | 只按输出新旧判定 `running`/`idle`，不再使用提示符正则和进程树等待判定        |
  | 5    | 以上都不满足                                   | 既有启发式，判定条件不变                                                     |

  - `status` 取值集合不变。
- **7501 到关注的映射**：

  | 7501 状态                                      | 关注                                                  |
  | ---------------------------------------------- | ----------------------------------------------------- |
  | `blocked`（`kind` 为 `question`、`auth` 或缺省） | `question`                                            |
  | `blocked` + `kind=permission`                  | `permission`                                          |
  | `done`                                         | `done`                                                |
  | `error`                                        | `error`                                               |
  | `working`、`idle`、`clear`                     | 清除                                                  |

  - 只在根记录的 `(state, kind)` 变化时置位，重复上报不会在用户清除后重新提醒。
  - `source` 为 `agent`。
  - 关注种类与 `source` 枚举、REST/WebSocket 负载和前端 schema 都不变。
- **新增配置项** `program_status`（布尔，默认 `true`）：为 `false` 时不应答探测，并把 7501 当作未知 OSC 忽略，作为回滚开关。

## Capabilities

### New Capabilities

- `program-status`：OSC 7501 根记录的识别、有效性与生命周期（`clear`、`OSC 133;A`、RIS、退出），服务端对 `OSC 7501;?` 的探测应答及其顺序保证，以及 `program_status` 配置。

### Modified Capabilities

- `agent-attention`：
  - 关注置位改为分层来源优先级，并加入 7501 映射。
  - 清除条件加入 7501 的 `working`/`idle`/`clear`，并限制 Warp 清除事件只在无 7501 根记录时生效。
  - 状态合成加入 7501 权威与结构化信号对启发式等待的降级。
- `terminal-control`：“终端自动回复的归属”增加一项例外，`OSC 7501;?` 由会话应答，不由客户端应答。

## Impact

- **后端**：
  - `internal/session/osc.go`：扫描器分派 7501、`133;A` 和 RIS。
  - `internal/session/attention.go`：来源层级、7501 记录与映射。
  - `internal/session/status.go`：状态合成顺序。
  - `internal/session/session.go`、`internal/session/io.go`：会话应答队列与输入闸门的顺序。
  - `internal/session/manager.go`：`Options`。
  - `internal/config/config.go`、`cmd/internal/assemble`：`program_status`。
  - `deploy/config.example.toml`。
- **协议与前端**：REST/WebSocket 负载、`internal/contract`、`web/src/schemas.ts` 和推送负载都不变；前端文案沿用既有关注种类，wire 快照不应变化。
- **测试**：
  - Go：扫描器、映射、层级、状态合成、应答顺序与取消。
  - E2E：用 shell 脚本模拟发送 7501 和探测的 agent。
  - 隔离实例中用真实 omp、Codex、Claude Code 抓取字节作为基线。
- **文档**：
  - `docs/user-stories.md` 的 US-C05、US-C06、US-K04。
  - `docs/usage.md` 的“需要你介入的提醒”。
  - `docs/deployment.md` 的配置表。
- **协作**：在途变更 `negotiate-terminal-graphics-capabilities` 也修改“终端自动回复的归属”。两者以先归档者为基线，后者需要按此重写自己的增量。本变更的应答队列应能被图形协商复用，避免出现两套会话应答机制。
