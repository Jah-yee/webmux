# terminal-submission-delivery Specification

## Purpose
让草稿发送与带回车的快捷指令具有可确认、可查询、不重复写入的送达语义，使用户能区分“已保存在本机”“服务端已收到”“已完整写入终端”和“结果未知”。

## Requirements

### Requirement: 提交帧与运行代际绑定

客户端 SHALL 以 0x03 提交帧发送草稿与带回车的快捷指令，载荷为一行 JSON 头 `{id, generation, bodyBytes}`、换行符，以及正文字节与提交键字节；终端字节 MUST NOT 编码为 JSON 字符串。提交 ID SHALL 为 8–64 个 `[A-Za-z0-9_-]` 字符且由客户端随机生成。`generation` 与会话当前运行代际不一致时，服务端 SHALL 回报 `failed`（`error: "generation"`）且不写入任何字节；JSON 头无效或 `bodyBytes` 越界时 SHALL 回报 `failed`（`error: "invalid"`）。

正文 SHALL 按 xterm 粘贴规则准备：换行转为 CR，应用开启 bracketed paste 时包裹 `ESC[200~`/`ESC[201~`，使线上字节与原先“粘贴后回车”一致。普通按键 SHALL 继续使用 0x00 输入帧。

#### Scenario: 代际不符

- **WHEN** 客户端提交时携带的代际与会话不一致
- **THEN** 服务端回报 `failed` 与 `generation`，PTY 未收到任何字节

### Requirement: 正文与提交键在同一输入临界区内写入

服务端 SHALL 在登记提交后，于一次输入串行化持有期间依次写入正文和提交键；其他连接的输入 MUST NOT 插入二者之间。登记后连接断开 SHALL NOT 取消写入，写入仍受 10 秒写期限与会话退出约束。

#### Scenario: 多端同时输入

- **WHEN** 手机提交一段正文加回车，同时桌面端在输入按键
- **THEN** 桌面端的按键出现在整段提交之前或之后，不会落在正文与回车之间

### Requirement: 提交状态回报

服务端 SHALL 以 0x05 帧回报提交状态：登记时 `received`；全部字节写入后 `written`；部分字节写入后出错 `partial`；未写入任何字节 `failed`。状态 SHALL 发送给提交客户端（同一客户端 ID）的全部连接，以及在 `received` 期间查询过该提交的其他客户端；最终状态送出后，或查询客户端的全部连接断开后，服务端 SHALL 不再保留该客户端的观察登记。0x02 重新同步清空队列时 SHALL 保留尚未送出的提交状态帧，并在 0x03 退出帧之前送出。界面 SHALL 只把 `written` 表述为“已写入终端”，MUST NOT 表述为 agent 已接受或执行成功。

#### Scenario: 正常提交

- **WHEN** 用户在草稿面板点击发送
- **THEN** 状态依次显示“已保存在本机/已发出”“服务端已收到”，`written` 后清空草稿并关闭草稿面板

#### Scenario: 部分写入

- **WHEN** 正文已写入但提交键写入失败
- **THEN** 草稿保持锁定并提示“部分写入终端，提交键未写入；请检查终端”，提供“转为草稿”

### Requirement: 幂等重发与查询

服务端 SHALL 为每个会话保留最近至少 256 条、至少 30 分钟的提交记录。重复 ID 的提交 MUST NOT 再次写入，服务端 SHALL 回报已记录状态。客户端 SHALL 可用 0x04 帧 `{ids}` 查询，服务端逐个回报状态；无记录时回报 `missing`。

客户端 SHALL 在连接中断时把未确认提交标为“结果未知”，并在重新连接后自动查询。对“结果未知”的草稿，界面 SHALL 提供“按原提交重发”（复用原 ID）和“转为草稿”。`failed` 与 `missing` SHALL 解除锁定、保留正文并提示原因；再次发送使用新 ID。

#### Scenario: ACK 丢失后重发

- **WHEN** 提交已写入终端但 `written` 状态因断线未到达，用户在重连后点击“按原提交重发”
- **THEN** 服务端回报 `written`，终端只收到一次该提交

#### Scenario: 新标签页恢复仍在写入的提交

- **WHEN** 草稿在新标签页（新的客户端 ID）恢复，查询时提交仍为 `received`
- **THEN** 写入完成后该标签页收到最终状态，草稿随之解锁或清空

#### Scenario: 从未送达

- **WHEN** 提交在服务端登记前连接就已断开，重连后自动查询
- **THEN** 查询结果为 `missing`，正文恢复可编辑并提示“可能未送达，或记录已过期”，不自动重发

### Requirement: 快捷指令提交

直接模式下开启“发送后自动回车”的快捷指令 SHALL 作为一次提交发送（正文与回车同一提交）；未开启自动回车的快捷指令与草稿模式下的插入行为不变。`partial`、`failed`、`missing` 与“结果未知”SHALL 给出对应提示且不自动重试。

#### Scenario: 自动回车的快捷指令

- **WHEN** 用户在直接模式下点击开启“发送后自动回车”的快捷指令
- **THEN** 正文与回车作为同一次提交发送；结果为 `partial`、`failed`、`missing` 或“结果未知”时给出对应提示，不自动重试
