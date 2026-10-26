# API 与协议

所有 API 以 `/api` 为前缀，WebSocket 以 `/ws` 为前缀，其余路径返回嵌入的前端（SPA 回退）。前端的完整数据契约定义在 [`web/src/schemas.ts`](../web/src/schemas.ts)；后端对应的线上类型都在 [`internal/contract`](../internal/contract)（REST 按资源分文件，终端帧与 `/ws/sessions` 事件在 `terminal.go`），由 `internal/wire` 与 Service 类型互相转换。两边的字段保持一致，`internal/api/testdata/wire` 的快照测试固定了每个端点和帧的 JSON 形状。

## 认证与 Origin

- 启用 token 时，HTTP 请求使用 `Authorization: Bearer <token>` 或 Cookie `webmux_token`。浏览器无法为 WebSocket 握手设置请求头，前端改为同时提供子协议 `webmux` 与 `webmux.token.<base64url(UTF-8 token)>`（无填充），服务端只选择 `webmux`、不回显 token。WebSocket 不接受 query `?token=`，token 不进入请求 URL；前端也不把 token 写入 Cookie，因为 Cookie 不按端口隔离。
- token 以定长摘要做常量时间比较。
- 带 `Origin` 的请求必须与 `Host` 同源，或在 `allowed_origins` 中。Origin 必须是完整的 HTTP(S) origin，不能含路径、query 或 fragment。
- 静态页面公开；所有 API 与 WebSocket 都受认证和 Origin 检查保护。认证失败返回 401，Origin 拒绝返回 403。
- 错误响应为 JSON `{"error": "..."}`。

## 文档地图

| 主题                                                        | 内容                                                 |
| ----------------------------------------------------------- | ---------------------------------------------------- |
| [HTTP API：会话、启动配置与其他](protocol/http-sessions.md) | 会话 CRUD、上传、目录、健康检查与启动配置            |
| [HTTP API：仓库与代码工作区](protocol/http-workspace.md)    | 仓库与 worktree、只读代码、Diff、图片与下载          |
| [HTTP API：公网隧道设置](protocol/http-tunnel.md)           | `/api/tunnel` 系列、持久化与错误语义                 |
| [HTTP API：Tailscale 状态](protocol/http-tailscale.md)      | `/api/tailscale` 只读检测、入口与运行状态            |
| [HTTP API：注意力推送](protocol/http-push.md)               | Web Push 订阅、来源资格与推送负载                    |
| [WebSocket 接口](protocol/websocket.md)                     | 会话列表事件、终端帧、回放、提交、控制端与关闭码     |
| [本机 API](protocol/local-api.md)                           | Unix 套接字认证与 `/local/v1/*` 控制面               |
| [控制面 JSON 契约](protocol/control-json.md)                | `version`、`status`、`stop` 与 `config` 的 JSON 输出 |

修改线上字段时，同时更新 [`web/src/schemas.ts`](../web/src/schemas.ts) 与 [`internal/contract`](../internal/contract)；每个子页只描述自己的主题，索引保持短小。
