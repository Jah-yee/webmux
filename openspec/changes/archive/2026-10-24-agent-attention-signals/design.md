## Context

动机见 proposal.md，行为合同见 `specs/agent-attention/spec.md`。与实现相关的现状：

- **状态写入路径有两处**：
  - `Session.read()` 每收到一块输出，就设置 `LastActiveAt = now` 和 `Status = running`。
  - `Manager.updates()` 每 500 ms 调用 `evaluate → classify` 重新分类。
  - 两处都需要感知关注，否则读取路径会在两次分类之间把 `waiting` 闪回 `running`。
- **`OSCScanner` 只服务标题**：它在 `Session.mu` 下由 `read()` 调用，状态机只追踪 OSC。缓冲上限 512 字节，超限后丢弃整个序列。Warp 事件常带 `cwd` 和最长 200 个码点的 `response`，中文时可达数百到上千字节，必须为它提高上限。
- **输入路径**：`WriteContext` 经 `inputGate` 串行化后写入 PTY。终端粘贴、上传路径、草稿发送、xterm 的设备查询应答都走这一路径，所以清除判定必须能区分“终端应答”。
- **环境构造**：`start()` 目前是 `os.Environ()` 后追加三个变量。开发者在 tmux、Warp、kitty 中启动 webmux 时，宿主身份变量会漏进会话。
- **omp 的实际行为**（源码 `packages/tui/src/terminal-capabilities.ts`、`coding-agent/src/tools/ask.ts`、`modes/warp-events.ts`、`modes/controllers/event-controller.ts`）：
  - 识别不出终端时，通知协议是 BEL。ask 等待和完成默认会发，错误默认不发。
  - 检测到 `WARP_CLI_AGENT_PROTOCOL_VERSION>=1` 时，发送 `session_start`、`prompt_submit`、`permission_request`、`permission_replied`、`question_asked`、`tool_complete`、`stop`、`stop_failure`，同时**跳过**完成和错误的普通通知；ask 的 BEL 仍会发送。
  - 检测到 `TMUX` 时，这些事件会被 DCS 透传包裹。

## Goals / Non-Goals

**Goals:**

- omp 在默认配置下，ask、审批、完成和出错都能被可靠地标为关注，不受动画影响。
- 不带任何信号的程序，行为与现在完全一致。
- 实现局限在 session 包内的一个扫描器、一个字段和两个配置项，不新增依赖或传输通道。

**Non-Goals:**

- 不做屏幕内容差分。本次不把 spinner 或计时器从“活动”中剔除，没有信号的 agent 仍按旧规则判定。
- 不解析 OSC 99 和 `OSC 9;4` 进度。默认环境下 omp 都不会发送它们，留给后续变更。
- 不做浏览器桌面通知、声音或推送。
- 不提供按 agent 匹配的配置档，不为任何 agent 编写核心分支。
- 不保存或展示通知正文。

## Decisions

### 1. 在现有扫描器上扩展，而不是新开一个解析器

将 `OSCScanner` 改名或扩展为“侧信道扫描器”：

- 状态 0 遇到 `0x07`，就回调一次 BEL。
- OSC 结束时按前缀分派：`0;`/`2;` 交给标题，`9;` 交给普通通知，`777;notify;` 交给通知或结构化事件。
- 回调接口改为传入一个小型 handler 结构，不再只接受一个 title 函数。
- 缓冲上限按类型区分：整体上限提高到 4096 字节；标题沿用“超过 512 字节就忽略”的规则，在 `finish` 时检查长度，因此标题的外部行为不变。

**备选方案**：用 `vt` 终端的 OSC 钩子。不采用，原因有二：

- Preview 只在需要时 Resize，钩子行为依赖第三方库内部。
- 现有扫描器已经处理了跨块拆分，并有测试。

### 2. 关注状态放在 Session 内，由读取路径和分类路径共同遵守

- `Session` 持有 `attention *Attention` 和 `lastInputAt time.Time`，都受 `s.mu` 保护。
- 扫描器回调运行在 `read()` 已持有的锁内，可以直接修改。
- `read()` 中的 `Status = running` 改为：存在关注时设为 `waiting`。
- `classify` 新增一个参数 `attention bool`，在 `exited` 之后、2 秒窗口之前返回 `waiting`。
- `infoLocked()` 复制关注值，避免泄露内部指针。
- `sameDisplay` 比较关注的种类、来源和时间，使变化能在 500 ms 内推送。

**备选方案**：在 Manager 层维护关注。不采用，因为信号来自读取路径，状态也归 Session 所有，放在 Manager 会多一把锁的协调。

### 3. 优先级替换，而不是事件状态机

结构化事件之间存在乱序和缺失：

- tmux 可能丢包。
- 用户可以在 omp 内直接回答，不经过 webmux 输入。
- 部分事件，如 `tool_complete`，与审批无因果关系。

因此不采用完整状态机（idle → running → blocked → done），而是采用：

- 单一锁存值。
- 按优先级替换。
- 少量明确的清除事件。

`tool_complete` 不参与清除：omp 可能在等待某个审批时，另一个并行工具恰好完成，若用它清除会误清审批。用户在 webmux 中回答会经输入路径清除；在别处回答则由下一次 `prompt_submit` 清除。

### 4. 输入清除时跳过终端应答

- 在 `WriteContext` 写入成功后，在 `s.mu` 下判定是否清除，并记录 `lastInputAt`。
- 判定函数是纯函数：从头扫描输入，跳过完整的 OSC 和 DCS 序列，以及终止字节为 `c`/`R`/`n`/`I`/`O`/`t` 或以 `$y` 结尾的 CSI 序列。只要还剩下任何其他字节，或遇到不完整的序列，就视为用户输入。
- 浏览器附着时会把回放缓冲区重新写入 xterm.js，而 `onData` 在回放期间仍连到 PTY，xterm.js 会再次应答历史中的所有查询（omp 启动时会发 DECRQM 模式查询，应答形如 `CSI ? 2026 ; 2 $ y`）。因此应答白名单必须覆盖 xterm.js 会自动发出的全部报告，否则仅打开会话页就会清除关注。
- 鼠标报告（`CSI M`、`CSI <…M/m`）算用户输入。点击就是介入，所以会清除。
- 不能在写入前清除：写入失败或被取消时，不应改变状态。

**备选方案**：前端给“应答”打上单独的帧标记。不采用，因为它会改动 WebSocket 协议，还要求 `terminal.svelte.ts` 区分 xterm `onData` 的来源，代价远高于纯函数判定。

### 5. BEL 抑制窗口固定为 1 秒

- 使用 `lastInputAt`（只记录清除类输入）判断是否抑制 BEL。不做成配置项，以减少配置面。
- `bell_attention = false` 是总开关，在 Manager 构造时传入 Session 的选项。

### 6. 环境规范化使用固定的移除表

- `start()` 改为：过滤 `os.Environ()`，按精确名和前缀两张表移除宿主终端身份变量，再追加 `TERM`、`COLORTERM`、`WEBMUX_SESSION_ID`，并在 `agent_protocol` 开启时追加协议变量。
- 移除表写在代码中，不开放配置。名单之外的变量继续透传。
- `start()` 的签名需要接收选项。目前它的参数来自 Manager，可以把这两个布尔值打包成一个小结构体传入。

**备选方案**：只追加协议变量，不做清理。不采用：在 tmux 中启动 webmux 时，`TMUX` 会让 omp 用 DCS 包裹事件，扫描器就看不到；在 Warp 中启动时，`WARP_CLIENT_VERSION` 会让各个 Warp 插件误判为真正的 Warp。

### 7. 协议变量默认开启

- omp 只检查这一个变量。开启后可以得到 `question`、`permission`、`done`、`error` 四种可区分的关注，而 BEL 只能给出模糊的 `bell`。
- 代价：omp 会改走结构化完成通知，跳过普通完成通知。所以扫描器必须同时支持 777，这两项不能拆开发布。
- 其他检查 `WARP_CLIENT_VERSION` 或 `TERM_PROGRAM` 的插件不会被激活，这符合预期。

### 8. 前端只增加字段和文案

- `SessionInfoSchema` 加入 `attention: AttentionSchema.nullable()`。
- `api.ts` 新增 `attentionText`。
- `SessionRow.svelte` 和 `Terminal.svelte` 在显示状态文字的位置优先显示 `attentionText[kind]`。
- 圆点颜色和计数逻辑不变。

## Risks / Trade-offs

- [借用 Warp 的变量名，可能与 Warp 未来的行为冲突] → 只设置协议版本，不冒充客户端；提供 `agent_protocol` 开关；文档注明来源。
- [Warp 协议升级 `v` 或改动事件名] → 只按 `event` 名处理，忽略未知事件；`v` 只做下限检查。即使升级失败也只会退回 BEL 或旧启发式，不会出错。
- [BEL 噪声，如 vim 报错、less 到底时响铃] → 优先级最低，任何真实输入都会清除；可用 `bell_attention = false` 关闭。
- [用户在 ask 界面敲下第一个字符就清除关注，而这时还没回答完] → 可以接受：用户已经在介入，列表中不需要再提醒。
- [无信号 agent 的动画问题依旧] → 明确列为非目标，由后续的屏幕差分变更处理。
- [扫描器上限提高到 4096 字节，每个会话多占内存] → 每个会话只有一份缓冲，按需增长，影响可以忽略。

## Migration Plan

- 新字段可空，新配置项有默认值，旧配置文件无需改动。前端和后端随同一个二进制发布，没有版本错配问题。
- 回滚：设置 `agent_protocol = false` 和 `bell_attention = false`，关注就不会再被置位，行为退回旧规则；或者直接回退二进制。
