## Why

手机是 WebMux 的主要使用场景，但目前手机上的输入与恢复都不可信：

- `ws.ts` 的 `send()` 在浏览器 `socket.send()` 返回后即返回 `true`，`Terminal.svelte` 的 `sendDraft()` 在正文与 Enter 两次调用都“成功”后立即清空草稿。这只能说明浏览器接受了发送调用，不能证明服务端收到或完整写入 PTY；锁屏、切网时草稿可能已丢失而终端并未收到。
- 草稿只在页面内存中，刷新、关闭页面、系统回收标签页或认证失效都会丢失。
- 重连一律清屏后全量回放最多 2 MiB 缓冲。手机每次回到前台都要重新传输和渲染；在 1 Mb/s 条件下仅传输 2 MiB 的理论下限就约 16.8 秒。
- 所有连接都能输入和 resize，最后一次 resize 决定 PTY 尺寸；手机键盘开合、桌面窗口变化、观察端重连会互相改变布局。每个 xterm 都会回答终端设备查询，同一查询会被重复回答。

## What Changes

- **整段提交送达确认**：草稿发送与带回车的快捷指令改用新的“提交”帧。每次提交有唯一 ID 并绑定会话运行代际；服务端回报“已收到”“已完整写入终端”“部分写入”“未写入”，客户端另有“已保存在本机”“结果未知”。正文与提交键在同一个输入临界区内写入。ACK 丢失后按 ID 查询；以同一 ID 重发不会重复写入。未确认的正文锁定保留，重连后自动查询并交由用户处理。普通按键仍走轻量字节通道。
- **草稿跨刷新恢复**：草稿和未确认提交按会话（及运行代际）保存在当前浏览器，7 天过期、会话不存在时清理；设置页可关闭本机保存或清除全部草稿。恢复后先展示正文和目标会话，仍需用户点击才发送。上传路径和代码引用本就以正文形式插入草稿，随正文一起恢复。
- **增量续接**：服务端为输出维护绝对字节 offset，并为每个会话生成运行代际。每次回放前先发送位置帧（含截断标记）。客户端记录已交给 xterm 的 offset，重连时携带代际与 offset；服务端仍保留后续数据时只发送缺失的尾部，否则回退为清屏全量回放。页面隐藏期间丢弃的输出也由前台重连补齐，不再清屏。
- **控制端**：每个浏览器标签页有稳定的客户端 ID 和设备名。会话的首个客户端成为控制端；后来的设备先观察。用户输入（按键、提交、软键盘按钮或“接管控制”）取得控制权。只有控制端的 resize 生效，观察端按 PTY 现有网格显示并可平移浏览；控制端断开后控制权空缺而不自动转移。终端设备查询的自动回复只写入控制端（无控制端时为最早连接）的回答。界面显示当前控制设备。

## Capabilities

### New Capabilities

- `terminal-submission-delivery`: 提交帧、状态帧与查询帧，提交记录与幂等重发，客户端状态展示与恢复操作。
- `terminal-draft-persistence`: 草稿与未确认提交的浏览器本地保存、恢复、过期与清理，以及设置项。
- `terminal-stream-resume`: 输出 offset、运行代际、位置帧与截断标记，增量续接与回退规则。
- `terminal-control`: 控制端的取得、保持与空缺，resize 与自动回复的归属，观察端显示。

### Modified Capabilities

当前 `openspec/specs/` 无已登记主规格。本变更修改尚未归档的 `add-terminal-input-composer` 中“草稿仅在页面内存、刷新后丢失”与“先粘贴再单独发送回车”的约定，以及 `add-terminal-quick-commands` 中带回车指令的发送通路；其余要求不变。

## Impact

- 后端：`internal/session`（环形缓冲 offset、`Info.generation`、`AttachClient`、控制端、提交记录、`writeParts` 输入临界区）、`internal/hub`（查询参数解析、帧 0x03–0x05 处理、控制校验）、`internal/api/router.go`（传入附着参数）。
- 协议：`/ws/s/{id}` 新增查询参数 `client`、`label`、`generation`、`offset`；服务端新增帧 0x04 位置、0x05 提交状态、0x06 控制状态，并在快照前先发位置帧；客户端新增帧 0x03 提交、0x04 查询、0x05 接管。旧帧语义不变。`SessionInfo` 新增 `generation`。
- 前端：`web/src/schemas.ts`、`ws.ts`、`hooks/terminal.svelte.ts`、`hooks/touch.ts`、`terminal-draft.svelte.ts`、`pages/Terminal.svelte`、`pages/Settings.svelte`、`components/TerminalInputComposer.svelte`、`styles.css`；新增 `terminalProtocol.ts`、`draftStorage.ts`。
- 测试：Go 单元与 WebSocket 集成测试、Vitest 单元与浏览器测试，调整依赖旧帧顺序的既有用例。
- 不新增依赖，不持久化服务端会话。
