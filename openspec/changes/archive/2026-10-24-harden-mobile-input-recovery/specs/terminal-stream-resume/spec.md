## Purpose

让短暂断线和回到前台时增量续接终端输出，减少全量回放的传输与渲染成本，并标明历史是否已被截断。

## ADDED Requirements

### Requirement: 输出 offset 与运行代际

服务端 SHALL 为每个会话生成运行代际（16 位十六进制）并在 `SessionInfo.generation` 中返回，SHALL 以累计写入字节数作为输出的绝对 offset。环形缓冲覆盖旧数据后，最早保留字节的 offset SHALL 大于 0。

#### Scenario: 缓冲覆盖后的 offset

- **WHEN** 会话输出超过环形缓冲容量，旧数据被覆盖
- **THEN** `SessionInfo.generation` 保持不变，最早保留字节的 offset 大于 0

### Requirement: 位置帧

服务端每次回放（附着与 0x02 重新同步）SHALL 先发送 0x04 位置帧 `{generation, mode, start, end, truncated}`。`mode: "reset"` 时随后 SHALL 发送覆盖 `[start, end)` 的 0x01 快照；`mode: "append"` 时随后 SHALL 以 0x00 输出帧发送 `[start, end)`（为空则不发送）。`truncated` SHALL 表示会话最早的输出已被覆盖。位置帧、回放数据与订阅登记 SHALL 在同一把锁内完成，保持无缺口。

#### Scenario: 首次附着全量回放

- **WHEN** 客户端不带 `generation` 与 `offset` 附着会话
- **THEN** 服务端先发送 `mode: "reset"` 的 0x04 位置帧，再发送覆盖 `[start, end)` 的 0x01 快照

### Requirement: 增量续接

客户端 SHALL 记录自最近快照以来已交给 xterm 的字节 offset，并在重连 URL 中携带 `generation` 与 `offset`。代际一致且 offset 位于保留范围 `[start, end]` 内时，服务端 SHALL 以 `append` 续接；否则 SHALL 以 `reset` 全量回放。客户端收到与本地 offset 不符的 `append` 时 SHALL 请求 0x02 全量同步。

页面隐藏时客户端 SHALL 停止写入 xterm 并保留最后的 offset；回到前台重连后 SHALL 续接缺失的输出而不清屏。可续接的终端在重连期间 SHALL 保持显示原内容。

#### Scenario: 短暂掉线

- **WHEN** 已连接的页面断线期间会话又输出了若干字节，随后重连
- **THEN** 服务端只发送断线后的字节，终端不清屏

#### Scenario: 缓冲已覆盖

- **WHEN** 客户端的 offset 早于服务端保留的最早字节
- **THEN** 服务端发送 `reset` 位置帧与完整快照，客户端清屏重放

#### Scenario: 历史截断提示

- **WHEN** 位置帧的 `truncated` 为真
- **THEN** 终端菜单的“复制全部缓冲”标注“更早输出已被覆盖”
