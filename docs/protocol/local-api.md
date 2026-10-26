# 本机 API（Unix 套接字）

服务端在 `<state_dir>/run/webmux.sock`（路径超过平台 `sun_path` 上限时改用 `$XDG_RUNTIME_DIR/webmux/<摘要>.sock`，实际路径写入运行时记录）上提供与 TCP 完全相同的 `/api/*`、`/ws/*`，认证方式改为对端进程的有效 UID，不检查 Token、Host 与 Origin。示例：

```sh
curl --unix-socket ~/.local/state/webmux/run/webmux.sock http://webmux/api/sessions
```

套接字额外提供以下接口；它们不在 TCP 上提供（返回 404）。响应为 JSON，错误为 `{"error": "..."}`。`v1` 内只增加可选字段，不兼容变更使用新的路径版本。

| 路由                      | 说明                                                                                                                                                                                                                   |
| ------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /local/v1/status`    | 状态 JSON（与 `webmux status --json` 契约 v1 相同，`record`、`localApi` 为 `true`，`drift` 由服务端重读配置文件计算）                                                                                                  |
| `POST /local/v1/shutdown` | 可选请求体 `{"force": bool}`；systemd / launchd 启动且未设 `force` 时 409 并给出管理命令；否则 202，按 SIGTERM 流程退出。会话内部的连接 403                                                                            |
| `GET /local/v1/token`     | `{"token": "..."}`；未配置 Token 时 404；会话内部的连接 403                                                                                                                                                            |
| `POST /local/v1/tunnel`   | 请求体 `{"enabled": bool}`；开启或关闭 Cloudflare Tunnel，立即返回 202 与 `TunnelStatus`；无效请求 400，启用前提不足或控制拒绝 409，设置存储不可用 503，会话内部的连接 403；关闭落盘失败的 503 带 `notPersisted: true` |

本机开关与 Web 设置共用当前有效配置及持久化控制器，定位 cloudflared 的规则相同，要求当前登录 Token 不少于 43 字符（运行中不生成或更换 Token）。成功操作保存开关，重复开启不启动第二个 cloudflared；运行时就绪后只打印不含 Token 的公网地址。关闭先撤销公网授权再结束 cloudflared，同样适用于 `--tunnel` 启动的隧道；即使写盘失败也停止当前进程，但必须报告未持久化。定位失败或意外退出时状态为 `failed` 并撤销授权，不自动重试。本机套接字上的 `/api/tunnel` 及其所有子路径也拒绝会话内进程（403），并非只有 `/local/v1/tunnel` 受限。

`TunnelStatus` 为 `{state, publicUrl?, error?, named, enabled?, settingsAvailable?}`。新增可选 `enabled` 表示当前进程启用期望，包括命令行覆盖，不是持久磁盘值或运行就绪状态；旧服务缺失时客户端才按 starting/ready 推断开关。`settingsAvailable: true` 显式声明 Web 设置页/API 可用，托盘仅凭该能力提供 `/settings/tunnel` 入口，不能从版本号、`enabled` 或存在 `tunnel` 推断；缺失或 false 时仍可使用旧本机开关。

`webmux bridge` 把一条套接字连接原样桥接到标准输入输出（HTTP/1.1 keep-alive 与 WebSocket 升级照常工作）；连接失败时不写标准输出，服务未运行退出码 3，其他错误 1。
