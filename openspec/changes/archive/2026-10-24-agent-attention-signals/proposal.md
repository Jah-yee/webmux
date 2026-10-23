## Why

现有“可能在等你”判定以“最近 2 秒是否有 PTY 输出”为第一条件。omp 在等待用户回答、等待工具审批时仍持续重绘 spinner、计时器和状态栏，会话因此一直显示“运行中”，真正需要用户介入的时刻反而被埋没。进程树全部睡眠和末行正则也无法区分“等网络响应”与“等键盘输入”。

调研发现 omp 已经主动报告这些时刻，只是 webmux 没有接收：

- 识别不出宿主终端时，omp 在 ask 等待和完成时发 BEL。
- 环境中存在 `WARP_CLI_AGENT_PROTOCOL_VERSION>=1` 时，omp 会通过 `OSC 777;notify;warp://cli-agent;{json}` 发出 `question_asked`、`permission_request`、`stop` 等结构化事件。

本变更以 omp 为主要目标，提出一个折中方案：

- 接收 agent 主动发出的终端侧信道信号，并锁存为“需要关注”。
- 不为每个 agent 编写核心分支，不冒充 Warp 终端，也不在本次重写屏幕启发式。

## What Changes

- **扩展侧信道解析**：在现有 OSC 标题扫描器上，增量识别以下信号；原始字节流和 xterm 渲染保持不变。
  - 不属于 OSC 结束符的裸 BEL。
  - `OSC 9;<text>` 通知；忽略 `OSC 9;4` 进度等数字子命令。
  - `OSC 777;notify;<title>;<body>` 通知。
  - `OSC 777;notify;warp://cli-agent;<json>` 结构化事件。
- **新增锁存式关注状态**：会话信息新增可空字段 `attention: {kind, source, since}`。
  - 一旦被信号置位，后续输出（包括 spinner 动画）不会将其清除。
  - 只有以下情况会清除：用户的真实输入、agent 报告重新开始工作或审批已答复、会话退出。
  - 存在关注时 `status` 为 `waiting`。
  - 现有 `status` 枚举不变；新增字段可空，旧前端忽略它也能正常工作，因此不构成破坏性变更。
- **规范 PTY 环境**：新会话的环境中移除继承自启动 webmux 的宿主终端或多路复用器的身份变量（如 `TERM_PROGRAM`、`WARP_*`、`TMUX`、`KITTY_WINDOW_ID`），避免子程序误判宿主或改用 DCS 透传。
  - 默认注入 `WARP_CLI_AGENT_PROTOCOL_VERSION=1`，使 omp 发送结构化事件。
  - 新增配置 `agent_protocol`（默认 `true`）用于关闭注入。
  - 不设置 `TERM_PROGRAM=WarpTerminal` 或 `WARP_CLIENT_VERSION`。
- **新增 BEL 配置**：新增配置 `bell_attention`（默认 `true`）。BEL 是最弱信号；用户输入后 1 秒内出现的 BEL（如 shell 补全响铃）不置位关注。
- **前端展示**：
  - 会话列表和终端页使用关注种类显示更准确的文案：需要回答、等待授权、已完成、出错、有通知。
  - 没有关注而由旧启发式得出 `waiting` 时，继续显示“可能在等你”。
- **其他 agent 的接入**：通过使用文档中的配置片段接入，不在核心中编写 agent 分支。
  - Claude Code：`Notification`/`Stop` hook 返回 `terminalSequence`。
  - Codex：开启 `tui.notifications`。

## Capabilities

### New Capabilities

- `agent-attention`：终端侧信道信号的识别（BEL、OSC 9、OSC 777 及 Warp cli-agent 事件）、锁存式关注状态的置位与清除规则、与既有活动状态的合成、PTY 环境规范化与协议变量注入、相关配置项，以及前端对关注种类的展示。

### Modified Capabilities

当前 `openspec/specs/` 无已登记主规格，因此没有可修改的主规格路径。

既有状态探测（2 秒输出窗口、末行提示符正则、进程树睡眠、`idle_after_seconds`）在没有关注时保持原样，作为兜底。本变更不改动其判定条件。

## Impact

- **后端**：
  - `internal/session/osc.go`：扫描器扩展，并按类型限制缓冲长度。
  - `internal/session/session.go`：`Info.attention`、读取路径不再覆盖关注、PTY 环境构造。
  - `internal/session/io.go`：在成功的真实输入后清除关注。
  - `internal/session/status.go`：合成状态。
  - `internal/session/manager.go`：`Options`、`sameDisplay`。
  - `internal/config/config.go`：新增配置项及到 `session.Options` 的转换。
- **协议与前端**：
  - `SessionInfo` 新增 `attention` 字段：`web/src/schemas.ts`、`docs/protocol.md`。
  - `web/src/api.ts` 状态文案，以及 `SessionRow.svelte`、`Terminal.svelte` 的展示。
- **配置与文档**：
  - `deploy/config.example.toml` 新增两个配置项。
  - `docs/usage.md` 增加 omp、Claude Code、Codex 的接入说明；`docs/architecture.md` 更新状态探测说明。
- **不涉及**：不增加依赖、REST 路由或 WebSocket 帧类型。帧 2 的会话状态 JSON 仍是唯一载体。
- **隐私**：Warp 事件中的 `query`、`response`、`summary` 以及通知正文都属于终端内容，后端 MUST NOT 存储、记录或下发；只保留事件种类。
