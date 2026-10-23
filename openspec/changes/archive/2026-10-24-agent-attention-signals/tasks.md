## 1. 侧信道扫描器

- [x] 1.1 将 `internal/session/osc.go` 扩展为侧信道扫描器。
  - 回调接口拆分为标题、BEL、普通通知和结构化事件。
  - 状态 0 的裸 BEL 触发回调；OSC 的 BEL/ST 结束符不触发。
  - 整体负载上限 4096 字节；标题仍保持超过 512 字节即忽略。
  - 验证：在 `osc_test.go` 中逐字节和整块喂入相同输入，得到相同的回调序列；既有标题测试全部通过。
- [x] 1.2 实现 OSC 分派。
  - `9;<text>` 中以“数字;”开头的子命令（含 `9;4`）忽略。
  - `777;notify;warp://cli-agent;<json>` 只在 `v>=1` 且 `event` 为字符串时上报事件名；非法 JSON 静默忽略。
  - 其余 `777;notify;…` 作为普通通知。
  - 验证：表驱动测试覆盖进度、ConEmu 子命令、普通 9/777、合法与非法 JSON、`v` 缺失或为 0、5000 字节超长负载。

## 2. 关注状态与合成

- [x] 2.1 在 `session.go` 中新增 `Attention` 类型和 `Info.Attention`（JSON 字段 `attention`，可空）。
  - 实现事件到种类的映射和优先级替换（种类变化时更新 `since`）。
  - 实现清除事件 `prompt_submit`、`permission_replied`、`session_start`；`tool_complete` 和未知事件不产生作用。
  - `infoLocked` 返回副本。
  - 验证：单元测试覆盖弱信号不降级、强信号升级并更新 `since`、清除事件、未知事件无作用。
- [x] 2.2 修改 `read()`：存在关注时状态写为 `waiting` 而不是 `running`。
  - `classify` 增加关注参数，判定顺序为 exited > 关注 > 旧规则。
  - `sameDisplay` 比较关注。
  - 验证：`status_test.go` 新增用例——有关注且有新输出时为 `waiting`、退出时清空关注并为 `exited`；原有 `TestStatusBoundaries` 等测试保持通过。
- [x] 2.3 在 `WriteContext` 成功写入后，按“终端应答”纯函数判定是否清除关注，并记录 `lastInputAt`；写入失败或取消时不改变状态。
  - 验证：纯函数测试覆盖 DA、CPR、DSR、焦点、OSC、DCS、鼠标报告、普通字符、混合输入、不完整序列；`io_test.go` 中取消的写入不清除关注。
- [x] 2.4 BEL 处理。
  - 接入 `bell_attention` 选项；最近一次清除类输入后 1 秒内的 BEL 忽略。
  - 关注、状态和输入时间都在 `s.mu` 下读写，不跨锁执行阻塞操作。
  - 验证：使用可注入时钟或时间参数测试窗口内外两种情况，以及开关关闭；`go test -race ./internal/session` 通过。

## 3. 环境与配置

- [x] 3.1 新增环境构造函数。
  - 按精确名和前缀两张表移除宿主终端与多路复用器变量，再追加 `TERM`、`COLORTERM`、`WEBMUX_SESSION_ID`。
  - `agent_protocol` 开启时追加 `WARP_CLI_AGENT_PROTOCOL_VERSION=1`。
  - 验证：纯函数测试覆盖移除表、保留其他变量、开关两种状态、不含 `TERM_PROGRAM` 与 `WARP_CLIENT_VERSION`；lifecycle 测试在设置了 `TMUX` 的父环境中启动 `env`，输出中不含 `TMUX`。
- [x] 3.2 打通配置。
  - `config.Config` 新增 `agent_protocol` 与 `bell_attention`，均为 `*bool` 或带默认值的布尔，未设置时为 `true`。
  - `SessionOptions()` 传入 `session.Options`，Manager 再传给 `start()`。
  - 更新 `deploy/config.example.toml`。
  - 验证：`config_test.go` 覆盖缺省为 true、显式 false、旧配置文件可加载。

## 4. 前端

- [x] 4.1 在 `web/src/schemas.ts` 中新增 `AttentionSchema`（`kind`/`source` 枚举，`since` 为带偏移的时间），并在 `SessionInfoSchema` 中加入 `attention: AttentionSchema.nullable()`。
  - 验证：Vitest 单元测试覆盖合法对象、null，以及未知 `kind` 时校验失败。
- [x] 4.2 在 `web/src/api.ts` 中新增 `attentionText`。`SessionRow.svelte` 和 `Terminal.svelte` 的状态文字在存在关注时显示种类文案；圆点颜色和计数仍由 `status` 决定。
  - 验证：浏览器组件测试渲染带 `permission` 关注的会话行时显示“等待授权”，无关注的 `waiting` 显示“可能在等你”；`pnpm --dir web check` 通过。

## 5. 端到端与文档

- [x] 5.1 新增 E2E 用例。
  - 自定义命令会话先输出 `warp://cli-agent` 的 `question_asked` 事件，再以 100 ms 间隔持续输出 spinner。
  - 验证：列表在 2 秒后仍显示“需要回答”；在终端键入字符后该文案消失；同一会话中，用户输入后立即响铃不产生关注，空闲 1 秒以上后响铃显示“响铃提醒”。
- [x] 5.2 文档更新。
  - `docs/protocol.md`：`SessionInfo.attention`。
  - `docs/architecture.md`：状态探测顺序与侧信道。
  - `docs/usage.md`：omp 默认可用及 `agent_protocol`/`bell_attention` 的说明；Claude Code 的 `Notification`/`Stop` hook 通过 `terminalSequence` 输出 `warp://cli-agent` 事件的片段（以 `WEBMUX_SESSION_ID` 作为启用条件）；Codex 的 `tui.notifications` 配置。
  - 验证：`make format-check` 通过，片段中的事件名与 spec 映射表一致。
- [x] 5.3 手动验收：以真实 omp 分别触发 ask、工具审批、正常完成和出错（需要可用的模型凭据），确认列表文案依次为“需要回答”“等待授权”“已完成”“出错停止”；设置 `agent_protocol = false` 后，ask 和完成显示“响铃提醒”。最后运行 `make test` 全部通过。
  - 2026-10-24 验收（`main` d85ab21，omp v18.12.6，隔离配置与状态目录）：ask→`question` “需要回答”，`--approval-mode always-ask` 的 bash 审批→`permission` “等待授权”（spinner 重绘期间保持），正常回复→`done` “已完成”，伪造 API key 的 401→`error` “出错停止”；`agent_protocol = false` 后 ask 与完成均为 `bell` “响铃提醒”。回答与授权后提醒立即清除。`make test`：Go race 与 Vitest unit 通过；browser 套件首轮 `CodeDocument.test.ts` 一例在并发负载下失败，单独重跑 3 次与整套重跑均通过（143/143）；E2E 132 passed。
