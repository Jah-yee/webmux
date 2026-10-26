## Purpose

让 webmux 识别 CLI agent 通过 OSC 7501 Program Status Protocol 主动上报的运行状态，并在没有浏览器附着时也能应答协议探测。这样 Claude Code、Codex、OpenCode、omp 等 agent 不需要 hook 或修改配置，就能给出准确的工作、阻塞、完成和出错状态。

## ADDED Requirements

### Requirement: 识别 OSC 7501 根记录

系统 SHALL 在实时 PTY 输出上增量识别 `ESC ] 7501 ; <body> (BEL | ESC \)`。识别 SHALL 支持在任意字节处被拆分的读取块，SHALL NOT 修改、删除或延迟发送给订阅者和写入回放缓冲区的原始字节。

`<body>` 按以下规则解析：

- 以 `:` 分隔为若干 `key=value` 对，键与值两侧的空白 SHALL 被去除。
- 键须匹配 `[a-z]+` 且不超过 16 字节。不合法的对 SHALL 被跳过，其余对照常处理。
- 未知键 SHALL 被忽略；重复键以最后一次出现为准。
- `state` 为必需键，取值 ∈ `idle`、`working`、`done`、`blocked`、`error`、`clear`。缺少 `state` 或取值不在此集合内时，SHALL 忽略整条上报。
- `kind` 取值 ∈ `permission`、`question`、`auth`；其他取值视为缺省。`kind` 只在 `state=blocked` 时有意义；`state` 不是 `blocked` 时，`kind` SHALL 视为缺省，因此只差 `kind` 的非阻塞上报被视为相同记录。
- 含合法 `id` 的上报属于子记录，SHALL NOT 改变根记录；`id` 不合法（任一段不匹配 `[A-Za-z0-9_.+-]{1,32}`、总长超过 128 字节或超过 8 级）时 SHALL 忽略整条上报。
- 整条序列超过 4096 字节时 SHALL 整体忽略，且不影响后续序列的识别。

除 `state` 与 `kind` 外，系统 SHALL NOT 解析、存储、写入日志或通过任何 API 下发上报中的其他值。`msg`、`title`、`app`、`progress` 都属于此类。

不含 `id` 且 `state` 合法的上报 SHALL 整体替换会话的根记录；`state=clear` SHALL 删除会话的全部记录。

#### Scenario: 根记录阻塞

- **WHEN** agent 输出 `ESC ] 7501 ; state=blocked:kind=permission:app=codex ESC \`
- **THEN** 会话的根记录为 `blocked` + `permission`，订阅者收到的字节与 PTY 输出逐字节一致

#### Scenario: 拆分读取

- **WHEN** 一条 `state=done` 的根记录上报被逐字节拆分在多次 PTY 读取中到达
- **THEN** 系统识别到与一次性到达完全相同的上报

#### Scenario: 未知状态整体忽略

- **WHEN** agent 输出 `ESC ] 7501 ; state=thinking:kind=question ESC \`
- **THEN** 根记录保持不变

#### Scenario: 子记录不改变根记录

- **WHEN** 根记录为 `working`，agent 输出 `ESC ] 7501 ; id=task/1:state=blocked:kind=question ESC \`
- **THEN** 根记录仍为 `working`

#### Scenario: 不泄露正文

- **WHEN** agent 上报 `state=error:msg=<base64 的错误原文>:title=<base64 的标题>`
- **THEN** 任何 REST、WebSocket 负载、推送负载和服务端日志中都不出现解码后的原文或标题，也不出现其 base64 形式

### Requirement: 根记录的生命周期

在以下情况下，系统 SHALL 丢弃根记录：

1. 收到 `state=clear` 上报。
2. 根记录为 `working` 或 `blocked` 时，收到语义提示符开始序列 `ESC ] 133 ; A …(BEL | ESC \)`。根记录为 `done`、`error` 或 `idle` 时 SHALL 保留。
3. 收到完全重置 RIS（`ESC c`）。
4. 会话退出。

用户输入、浏览器附着或断开、调整尺寸以及其他终端输出 SHALL NOT 改变根记录。

#### Scenario: 回到 shell 提示符

- **WHEN** 根记录为 `blocked` 时 agent 崩溃，shell 随后输出 `ESC ] 133 ; A BEL`
- **THEN** 根记录被丢弃

#### Scenario: 完成状态跨越提示符

- **WHEN** 根记录为 `done`，随后收到 `ESC ] 133 ; A BEL`
- **THEN** 根记录仍为 `done`

#### Scenario: 用户输入不改变记录

- **WHEN** 根记录为 `blocked`，用户在该会话键入一个字符
- **THEN** 根记录仍为 `blocked`

### Requirement: 服务端应答协议探测

会话在实时读取到探测序列 `ESC ] 7501 ; ? (BEL | ESC \)` 时，系统 SHALL 向该会话的 PTY 写入且只写入一次应答：

```
ESC ] 7501 ; ? : states=idle,working,done,blocked,error : kinds=permission,question,auth ESC \
```

应答 SHALL 满足：

- **无需客户端**：没有任何客户端附着时也会写入，不等待浏览器或原生 attach。
- **顺序**：应答 SHALL 先于该探测被发布给订阅者之后进入 PTY 的任何客户端输入，包括浏览器对紧随其后的 DA1 的自动应答；也 SHALL NOT 与另一份输入交错写入。
- **不当作用户输入**：应答 SHALL NOT 取得控制权、改变尺寸、清除关注或计为用户输入。
- **只响应实时输出**：只有实时 PTY 读取会触发应答；浏览器或原生 attach 回放历史、重新同步 SHALL NOT 触发应答。
- **有界、不阻塞**：待写应答 SHALL 有固定上限，超出上限的探测 SHALL 被丢弃而不重试。应答 SHALL NOT 阻塞 PTY 读取，也 SHALL NOT 在持有会话状态锁时等待 PTY 可写。
- **取消**：会话退出 SHALL 取消尚未写入的应答。
- **不改输出**：探测序列本身 SHALL 照常写入回放并发送给订阅者，不被修改或删除。

#### Scenario: 无浏览器时启动 Claude Code

- **WHEN** 通过启动配置创建 Claude Code 会话，没有任何客户端附着，Claude Code 启动时输出 `ESC ] 7501 ; ? ESC \` 与 `ESC [ c`
- **THEN** PTY 输入收到一次 7501 探测应答，DA1 无人应答，Claude Code 能据此判定支持 7501

#### Scenario: 应答先于浏览器的 DA1

- **WHEN** 浏览器控制端已附着，程序在同一次写入中输出 7501 探测和 DA1，浏览器 xterm 随即产生 DA1 应答
- **THEN** PTY 输入中 7501 应答完整出现在浏览器 DA1 应答之前

#### Scenario: 回放不重答

- **WHEN** 回放缓冲区中含有程序启动时的 7501 探测，用户打开或重新打开该会话页
- **THEN** PTY 不再收到新的 7501 应答

#### Scenario: 应答不清除关注

- **WHEN** 会话已有 `question` 关注，随后程序发出 7501 探测并得到服务端应答
- **THEN** 关注仍为 `question`，控制端不变

### Requirement: Program Status 配置

配置项 `program_status`（布尔，默认 `true`）为 `false` 时，系统 SHALL NOT 应答 7501 探测，并 SHALL 把所有 OSC 7501 序列当作未知 OSC 忽略：不维护根记录，也不影响关注或状态。`OSC 133;A` 与 RIS 在此时 SHALL NOT 产生任何可观察效果。旧配置文件没有该项时 SHALL 按默认值 `true` 处理。

#### Scenario: 默认开启

- **WHEN** 配置文件没有 `program_status`
- **THEN** 系统应答 7501 探测并识别根记录

#### Scenario: 关闭后回退

- **WHEN** 配置 `program_status = false` 后，一个同时发出 7501 和 Warp 事件的 omp 会话发出 `question_asked`
- **THEN** 程序得不到 7501 探测应答，关注由 Warp 事件决定，为 `question`
