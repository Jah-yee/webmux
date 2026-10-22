# Spec Delta

## ADDED Requirements

### Requirement: 状态中的推送字段

`/local/v1/status` 与 `webmux status --json` SHALL 在运行中的实例支持注意力推送时包含 `push` 对象，字段为：

- `enabled`：配置是否启用推送；
- `available`：推送存储是否可用；
- `error`：仅不可用时出现，说明原因；
- `subscriptions`：已登记订阅数；
- `publicOrigin`：仅配置了 Named Tunnel 时出现，为 `https://<tunnel.hostname>`。

该字段 SHALL 为可选新增字段，契约版本保持 v1；实例未运行或不支持推送时 SHALL 省略。字段内容 MUST NOT 含订阅 endpoint、订阅密钥、VAPID 私钥或任何 Token。

#### Scenario: Named Tunnel 实例

- **WHEN** 实例配置了 `tunnel.hostname = "term.example.com"` 与隧道凭据，已登记 1 个订阅，客户端请求本机状态
- **THEN** `push` 为 `{"enabled":true,"available":true,"subscriptions":1,"publicOrigin":"https://term.example.com"}`

#### Scenario: 未配置 Named Tunnel

- **WHEN** 实例只使用 Quick Tunnel 或未启用隧道
- **THEN** `push` 中没有 `publicOrigin` 字段

#### Scenario: 实例未运行

- **WHEN** WebMux 未运行时执行 `webmux status --json`
- **THEN** 输出不含 `push` 字段
