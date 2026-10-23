## Purpose

让 webmux 依据 CLI agent 主动发出的终端侧信道信号（BEL、OSC 9/777 通知与 Warp cli-agent 结构化事件），准确、稳定地标记“需要用户介入”的会话，不被持续刷新的终端动画掩盖，并在不冒充其他终端的前提下让 omp 等 agent 发出结构化事件。

## ADDED Requirements

### Requirement: 侧信道信号识别不改变终端字节流

系统 SHALL 在 PTY 输出上增量识别以下信号，识别 SHALL 支持在任意字节处被拆分的读取块：

- 裸 BEL：不属于 OSC 序列结束符的 `0x07`。
- 普通通知：`ESC ] 9 ; <text> (BEL | ESC \)`，其中 `<text>` 以“数字后接分号”开头的（如 `9;4;3` 进度、ConEmu 风格子命令）SHALL NOT 视为通知。
- 普通通知：`ESC ] 777 ; notify ; <title> ; <body> (BEL | ESC \)`，`<title>` 不等于 `warp://cli-agent`。
- 结构化事件：`ESC ] 777 ; notify ; warp://cli-agent ; <json> (BEL | ESC \)`。

识别 SHALL NOT 修改、删除或延迟发送给订阅者和写入回放缓冲区的原始字节，浏览器终端渲染行为保持不变。单个 OSC 负载超过 4096 字节时 SHALL 整体忽略且不得产生任何信号；OSC 0/2 标题既有的 512 字节上限及其行为 SHALL 保持不变。

#### Scenario: OSC 结束符不是响铃

- **WHEN** 程序输出 `ESC ] 0 ; title BEL`
- **THEN** 会话标题按既有规则更新，且不产生 BEL 信号

#### Scenario: 拆分读取的结构化事件

- **WHEN** 一个 `warp://cli-agent` 事件被逐字节拆分在多次 PTY 读取中到达
- **THEN** 系统识别到与一次性到达完全相同的事件，订阅者收到的字节与 PTY 输出逐字节一致

#### Scenario: 进度序列不是通知

- **WHEN** 程序输出 `ESC ] 9 ; 4 ; 3 BEL`
- **THEN** 不产生通知信号，也不产生 BEL 信号

#### Scenario: 超长负载

- **WHEN** 程序输出负载为 5000 字节的 `OSC 777` 序列
- **THEN** 该序列不产生任何信号，后续序列的识别不受影响

### Requirement: 锁存式关注状态

会话信息 SHALL 包含可空字段 `attention`，非空时为 `{kind, source, since}`：

- `kind` ∈ `question`、`permission`、`done`、`error`、`notify`、`bell`。
- `source` ∈ `agent`（结构化事件）、`terminal`（普通通知或 BEL）。
- `since` 为 RFC 3339 时间。

信号与关注种类的映射 SHALL 为：

| 信号                         | kind         | source     |
| ---------------------------- | ------------ | ---------- |
| 结构化 `question_asked`      | `question`   | `agent`    |
| 结构化 `permission_request`  | `permission` | `agent`    |
| 结构化 `stop`、`idle_prompt` | `done`       | `agent`    |
| 结构化 `stop_failure`        | `error`      | `agent`    |
| OSC 9 / OSC 777 普通通知     | `notify`     | `terminal` |
| 裸 BEL                       | `bell`       | `terminal` |

种类优先级 SHALL 为 `question` = `permission` > `error` > `done` > `notify` > `bell`。新信号优先级低于当前关注时 SHALL NOT 改变当前关注；优先级不低于当前关注时 SHALL 替换，种类发生变化时 `since` 更新为当前时间。

关注置位后，任何后续 PTY 输出（包括 spinner、计时器和状态栏重绘）SHALL NOT 清除或降级关注。

#### Scenario: 动画不掩盖等待回答

- **WHEN** omp 发出 `question_asked` 事件后每 100 ms 持续重绘 spinner
- **THEN** 会话的 `attention.kind` 保持 `question`，`status` 保持 `waiting`，直到满足清除条件

#### Scenario: 弱信号不降级

- **WHEN** 会话已有 `kind = question` 的关注，随后收到一个裸 BEL
- **THEN** 关注仍为 `question`，`since` 不变

#### Scenario: 强信号升级

- **WHEN** 会话已有 `kind = done` 的关注，随后收到 `permission_request`
- **THEN** 关注变为 `permission`，`since` 更新为收到该事件的时间

### Requirement: 关注的清除条件

系统 SHALL 在且仅在以下任一情况清除关注：

1. 一次成功写入 PTY 的用户输入中至少含有一个非“终端应答”的字节。“终端应答”指仅由 OSC 序列、DCS 序列，以及终止字节为 `c`、`R`、`n`、`I`、`O`、`t` 或以 `$y` 结尾的 CSI 序列（设备属性、光标位置、状态报告、焦点事件、窗口尺寸报告、DECRQM 模式报告）组成的输入。
2. 收到结构化事件 `prompt_submit`、`permission_replied` 或 `session_start`。
3. 会话退出。

调整终端尺寸、浏览器附着或断开、终端输出 SHALL NOT 清除关注。结构化事件 `tool_complete` 和未知事件 SHALL NOT 改变关注。

#### Scenario: 用户回答后清除

- **WHEN** 会话有 `question` 关注，用户在该会话键入字符并成功写入 PTY
- **THEN** 关注被清除，会话状态回到既有探测规则的结果

#### Scenario: 终端自动应答不清除

- **WHEN** 会话有关注，浏览器终端仅回送设备属性应答 `ESC [ ? 6 2 ; 2 2 c` 或焦点事件 `ESC [ I`
- **THEN** 关注保持不变

#### Scenario: 打开会话页回放的终端应答不清除

- **WHEN** 会话有 `question` 关注，且回放缓冲区中含有程序启动时发出的 DECRQM、设备属性和光标位置查询；用户打开或重新打开该会话的终端页
- **THEN** 浏览器终端对回放查询发出的自动应答（如 `ESC [ ? 2026 ; 2 $ y`）写入 PTY 后，关注保持不变

#### Scenario: 断开重连不清除

- **WHEN** 会话有 `permission` 关注，所有浏览器断开后又重新附着
- **THEN** 重新附着收到的会话状态仍包含该关注

#### Scenario: 退出清除

- **WHEN** 有关注的会话进程退出
- **THEN** `attention` 为 null，`status` 为 `exited`

### Requirement: 关注与活动状态合成

会话 `status` SHALL 按以下顺序确定：已退出为 `exited`；否则存在关注为 `waiting`；否则沿用既有探测规则（最近输出窗口、提示符模式、进程树睡眠与空闲阈值），既有规则的判定条件 SHALL 保持不变。`status` 的取值集合 SHALL 保持 `running`、`waiting`、`idle`、`exited` 不变。关注或其种类变化 SHALL 触发会话列表 `updated` 事件和终端连接上的会话状态帧。

#### Scenario: 有输出时仍为等待

- **WHEN** 会话存在关注且最近 2 秒内有 PTY 输出
- **THEN** `status` 为 `waiting`

#### Scenario: 无信号程序沿用旧规则

- **WHEN** 普通 shell 会话从未发出任何信号
- **THEN** 其 `status` 与本变更前的探测结果一致，`attention` 为 null

### Requirement: 结构化事件解析的安全边界

系统 SHALL 只在负载为合法 JSON 对象、`v` 为不小于 1 的整数、`event` 为字符串时处理结构化事件；否则 SHALL 忽略该序列，且 SHALL NOT 将其降级为普通通知。系统 SHALL 只使用 `event` 字段决定关注；`query`、`response`、`summary`、`tool_input`、`cwd` 等内容以及普通通知的标题与正文 SHALL NOT 被存储、写入日志或通过任何 API 下发。

#### Scenario: 非法 JSON

- **WHEN** 程序输出 `OSC 777;notify;warp://cli-agent;{not json` 并以 BEL 结束
- **THEN** 不产生任何关注，也不把结束符 BEL 计为裸 BEL

#### Scenario: 不泄露正文

- **WHEN** omp 发出包含用户提问原文的 `stop` 事件
- **THEN** 会话信息中只出现 `attention.kind = done` 与 `source = agent`，任何 REST、WebSocket 负载和服务端日志中都不出现该原文

### Requirement: BEL 信号可配置且避开输入回声

配置项 `bell_attention`（布尔，默认 `true`）为 `false` 时，裸 BEL SHALL NOT 产生关注。为 `true` 时，在最近一次清除类用户输入后 1 秒内到达的裸 BEL SHALL NOT 产生关注，以避免 shell 补全等对按键的即时响铃。

#### Scenario: Tab 补全响铃

- **WHEN** 用户在 shell 中按 Tab，shell 在 200 ms 内响铃
- **THEN** 不产生关注

#### Scenario: 空闲后的响铃

- **WHEN** 用户最后一次输入超过 1 秒后 omp 发出 BEL
- **THEN** 产生 `kind = bell`、`source = terminal` 的关注

### Requirement: PTY 环境规范化与协议变量注入

新会话的子进程环境 SHALL 移除继承自 webmux 进程的宿主终端与多路复用器身份变量，至少包括：`TERM_PROGRAM`、`TERM_PROGRAM_VERSION`、所有 `WARP_` 前缀变量、`TMUX`、`TMUX_PANE`、`STY`、所有 `ZELLIJ` 前缀变量、所有 `KITTY_` 前缀变量、所有 `ITERM_` 前缀变量、`LC_TERMINAL`、`LC_TERMINAL_VERSION`、所有 `WEZTERM_` 前缀变量、所有 `GHOSTTY_` 前缀变量、`WT_SESSION`、`WT_PROFILE_ID`、`VTE_VERSION`。其余继承变量及既有的 `TERM`、`COLORTERM`、`WEBMUX_SESSION_ID` 设置 SHALL 保持不变。

配置项 `agent_protocol`（布尔，默认 `true`）为 `true` 时，系统 SHALL 在子进程环境中设置 `WARP_CLI_AGENT_PROTOCOL_VERSION=1`；为 `false` 时 SHALL NOT 设置。系统 SHALL NOT 设置 `TERM_PROGRAM=WarpTerminal` 或 `WARP_CLIENT_VERSION`。无论该配置如何，结构化事件一旦出现在 PTY 输出中都 SHALL 被识别。

#### Scenario: 在 tmux 中启动 webmux

- **WHEN** 开发者在 tmux 窗格内启动 webmux 并新建会话
- **THEN** 会话内 `env` 不含 `TMUX` 与 `TMUX_PANE`，因此 omp 不会用 DCS 透传包裹事件

#### Scenario: 默认注入

- **WHEN** 使用默认配置新建 omp 会话
- **THEN** omp 进程环境中 `WARP_CLI_AGENT_PROTOCOL_VERSION=1`，且不存在 `TERM_PROGRAM` 与 `WARP_CLIENT_VERSION`

#### Scenario: 关闭注入

- **WHEN** 配置 `agent_protocol = false` 后新建会话
- **THEN** 子进程环境中不存在任何 `WARP_` 前缀变量；omp 仍以 BEL 报告 ask 等待和完成，系统据此产生 `bell` 关注

### Requirement: 前端展示关注种类

前端 SHALL 用 Zod 校验 `attention` 字段（可空对象，`kind` 与 `source` 为上述枚举，`since` 为带偏移的时间），校验失败按既有会话数据校验失败处理。会话列表行与终端页状态处 SHALL 在存在关注时按种类显示文案：`question` 为“需要回答”，`permission` 为“等待授权”，`done` 为“已完成”，`error` 为“出错停止”，`notify` 为“有通知”，`bell` 为“响铃提醒”；不存在关注时 SHALL 沿用既有状态文案（`waiting` 仍为“可能在等你”）。状态圆点颜色与列表计数 SHALL 继续由 `status` 决定。

#### Scenario: 列表显示等待授权

- **WHEN** 某会话收到 `permission_request`
- **THEN** 会话列表中该行显示“等待授权”，并计入“等待”数量

#### Scenario: 启发式等待保持旧文案

- **WHEN** 会话无关注但既有规则判定为 `waiting`
- **THEN** 显示“可能在等你”
