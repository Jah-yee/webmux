## MODIFIED Requirements

### Requirement: 终端自动回复的归属

输入来源 SHALL 由客户端标明，而不是按字节模式推断：xterm 报告为用户输入的数据以 0x00 发送，其余数据（对设备查询的自动应答）以 0x06 发送。服务端 SHALL 只把来自负责连接的 0x06 写入 PTY：存在控制端时为控制连接，否则为最早附着的连接。其他连接的 0x06 SHALL 被丢弃；0x06 SHALL NOT 取得控制权或清除关注提醒。

例外：OSC 7501 协议探测 `ESC ] 7501 ; ? ST` 由会话自身应答（见 `program-status`），不依赖任何客户端。该应答 SHALL NOT 取得控制权、改变尺寸或清除关注提醒。DA1 及其他设备查询仍由负责连接应答。浏览器 xterm SHALL NOT 为 OSC 7501 产生应答。原生 attach SHALL NOT 把 7501 探测转发给本地终端，以免本地终端再答一份；非探测的 7501 上报及其他输出照常显示，流 offset 仍按未过滤的原始字节推进。

#### Scenario: 多端同时回答设备查询

- **WHEN** 程序发出设备属性查询，两个已附着的 xterm 都产生回答
- **THEN** PTY 只收到控制端的回答

#### Scenario: 形似报告的按键

- **WHEN** 观察端用户按下 Shift+F3（字节为 `ESC [ 1 ; 2 R`，与光标位置报告相同）
- **THEN** 按键写入 PTY 并取得控制权

#### Scenario: 7501 探测只有会话应答

- **WHEN** 浏览器控制端和观察端都已附着，程序发出 `ESC ] 7501 ; ? ESC \`
- **THEN** PTY 只收到一份由会话写入的 7501 应答，控制端不变

#### Scenario: 原生 attach 不转发探测

- **WHEN** 用户在支持 OSC 7501 的本地终端中执行 `webmux attach <ID>` 并取得控制权，会话程序随后发出 `ESC ] 7501 ; ? ESC \`
- **THEN** 本地终端收不到该探测，PTY 只收到一份由会话写入的 7501 应答，后续 `state=working` 等上报照常显示在本地终端
