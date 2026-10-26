# 真实 agent 基线（2026-10-26）

## 方法

用 Python `pty.fork()` 在 120×40 的伪终端中启动 agent，记录全部输出字节，并按需应答 DA1 与 OSC 7501 探测。具体做法：

- 每个 agent 使用独立的临时 HOME、虚假 API key 和临时工作目录，不连接任何已有的开发者会话。
- 子进程环境移除 `TMUX*`、`TERM_PROGRAM*`、`WARP_*`、`KITTY*`、`ITERM*`，设置 `TERM=xterm-256color`。
- 按定时向 PTY 写入 `hello\r`，用于触发一个回合；之后写入 Ctrl+C、Ctrl+D 退出。

应答方式取以下四种之一：

| 模式 | 应答 DA1 | 应答 7501 探测 |
| --- | --- | --- |
| none | 否 | 否 |
| da1 | 是 | 否 |
| 7501 | 否 | 是 |
| both | 是 | 是 |

两种应答的确切字节：

- DA1：`ESC [ ? 62 ; 22 c`
- 7501：`ESC ] 7501 ; ? : states=idle,working,done,blocked,error : kinds=permission,question,auth ESC \`

## 结果

### Claude Code 2.1.310

启动后，在一次写入中依次输出 `ESC [ > 0 q`、`ESC [ ? u`、`ESC ] 7501 ; ? BEL`、`ESC [ c`。

- **探测终止符**为 BEL。规格同时接受 BEL 与 ST，无需修改。
- **只应答 DA1**：`--debug` 日志记录 `programStatus=no (probe: no reply to OSC 7501 ; ?)`，此后不再发送任何 7501。这证实了“DA1 先到即判定不支持”。
- **都不应答**：同样不发送 7501。
- **应答 7501，无论是否应答 DA1**：日志记录 `programStatus=yes (probe: OSC 7501 reply)`。随后依次上报：
  1. 根记录 `state=idle:app=claude-code`；
  2. 提交 `hello` 后上报 `state=working`；
  3. 回合结束时上报 `state=done:app=claude-code:msg=<base64>`，其中 `msg` 为正文。
- 以 ST 结束的应答被正确识别。所有上报都以 BEL 结束，不带 `id`。
- 环境变量 `CLAUDE_CODE_DISABLE_TERMINAL_TITLE` 存在时完全不探测，日志记录 `programStatus=no (env: CLAUDE_CODE_DISABLE_TERMINAL_TITLE)`。这一点需要写进文档。

### Codex CLI 0.164.0

- 不探测。stdout 是 TTY 时直接上报，并以 ST 结束。
- 状态序列：`state=idle:app=codex` → 提交 `hello` 后 `state=working:app=codex` → 请求失败后 `state=idle:app=codex` → 退出时 `state=clear`。
- 请求失败后报告 `idle` 而不是 `error`。由于用户之前的输入已经清除了关注，不会产生多余提醒。
- 本次运行没有出现 OSC 9 或 BEL。按 Codex 默认的 `notification_condition=unfocused`，没有焦点上报时不会发通知。

### omp 18.12.6

- 同时设置 `WARP_CLI_AGENT_PROTOCOL_VERSION=1` 时，启动即上报 `state=idle:app=omp`，不探测，以 BEL 结束。
- 使用虚假凭据时，`hello` 停留在模型或登录界面，没有产生回合状态迁移。根据源码 `src/utils/run-status.ts`，回合中会依次发送 `working`、`blocked`、`done`、`error`，并在退出时发送 `clear`。

### OpenCode

- 测试了两个版本：npm `opencode-ai` 的 `latest`（1.19.4，界面提示升级到 OpenCode 2.0）和 `dev`（`0.0.0-dev-202610260443`）。
- 两个版本在本环境中都没有观察到 7501 上报，也没有发出探测；只有 OSC 0 标题和若干 BEL。
- 调研阶段，其源码 `packages/tui/src/context/program-status.ts` 显示 2.x 版本会直接上报。但本次没有拿到可运行的 2.x 发行版，因此这一点**未实测**。文档只把 OpenCode 列为“按源码会直接报告”，不作已验证的承诺。

## 对规格的影响

- 应答格式与“应答须先于 DA1 应答”的要求成立，规格无需修订。
- 所有被观察的 agent 都只上报根记录，因此只处理根记录足以覆盖它们。
- 用户文档需要说明：webmux 进程环境中存在 `CLAUDE_CODE_DISABLE_TERMINAL_TITLE` 时，该变量会进入会话，使 Claude Code 不再报告状态。
