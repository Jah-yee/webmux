# Spec Delta

## ADDED Requirements

### Requirement: 隧道控制接口

本机 API SHALL 提供 `POST /local/v1/tunnel`，请求体 `{"enabled": bool}`：`true` 开启、`false` 关闭隧道，成功时返回 202 与当前隧道状态对象。请求体无效时 SHALL 返回 400。该接口 SHALL 只在本机套接字上可用，TCP 监听上 SHALL 返回 404。

#### Scenario: 开启请求

- **WHEN** 同一用户向本机套接字发送 `POST /local/v1/tunnel` 与 `{"enabled": true}`，Token 满足要求
- **THEN** 返回 202，响应体中 `state` 为 `starting` 或 `ready`

#### Scenario: TCP 上不可用

- **WHEN** 持有 Token 的浏览器请求 TCP 监听上的 `/local/v1/tunnel`
- **THEN** 返回 404

### Requirement: 状态中的隧道字段

`/local/v1/status` 与 `webmux status --json` SHALL 在实例支持运行时隧道控制时包含 `tunnel` 对象：`state`（`off`、`starting`、`ready`、`failed` 之一）、`ready` 时的 `publicUrl`（不含 Token）、`failed` 时的 `error`、以及 `named`（是否 Named Tunnel）。该字段 SHALL 为可选新增字段，契约版本保持 v1；不支持的旧实例 SHALL 省略该字段。实例未运行时 SHALL 省略该字段。

#### Scenario: 隧道就绪

- **WHEN** 隧道 `ready`，客户端请求状态
- **THEN** `tunnel` 为 `{"state":"ready","publicUrl":"https://abc.trycloudflare.com","named":false}`，任何字段都不含 Token

#### Scenario: 实例未运行

- **WHEN** WebMux 未运行时执行 `webmux status --json`
- **THEN** 输出不含 `tunnel` 字段
