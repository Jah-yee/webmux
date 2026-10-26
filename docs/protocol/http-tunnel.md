# HTTP API：公网隧道设置

## 公网隧道设置

以下端点复用网络 API 的认证和 Host/Origin 检查；经本机 Unix 套接字访问时仍禁止会话内进程调用（包括只读 GET，返回 403）。Web 页面入口为 `/settings/tunnel`，不增加终端 WebSocket 消息。

| 方法   | 路径                   | 请求及成功响应                                                                                         |
| ------ | ---------------------- | ------------------------------------------------------------------------------------------------------ |
| GET    | `/api/tunnel`          | 200，返回 `TunnelSettings`；存储不可用仍返回可读状态及存储错误                                         |
| PUT    | `/api/tunnel/settings` | `{mode, hostname, token?}`；返回 `TunnelSettings`，响应时 `state == "starting"` 为 202，否则 200       |
| PUT    | `/api/tunnel/enabled`  | `{"enabled": true}` 或 `{"enabled": false}`；202，返回 `TunnelSettings`，不等待 cloudflared 就绪或退出 |
| DELETE | `/api/tunnel/settings` | 无请求体；200，返回 `TunnelSettings`；删除 Web 覆盖，恢复配置文件模式/凭据并停止当前隧道               |

```ts
interface TunnelSettings {
  enabled: boolean; // 当前进程启用期望，含 CLI 覆盖；不是 ready，也不保证等于磁盘值
  mode: "quick" | "named";
  hostname: string; // Quick 为空；Named 为规范化后的裸主机名
  tokenConfigured: boolean; // 仅报告 Named 凭据是否存在，不返回凭据
  source: "config" | "web"; // 模式/凭据的来源，不是 CLI 开关是否生效的标记
  state: "off" | "starting" | "ready" | "failed";
  publicUrl?: string; // 仅 ready，不含登录 token
  error?: string; // 运行时失败原因；Cloudflare 凭据脱敏
  canEnable: boolean;
  enableBlockedReason?: string;
  storageAvailable: boolean;
  storageError?: string;
}
```

`mode` 只接受 `quick`/`named`。Named 必须有非空 token 与合法裸 ASCII/punycode 主机名（不能带 scheme、端口、路径、通配符或空白，须满足 DNS 标签限制）。省略 `token` 保留现有凭据，显式空字符串在 Named 模式下无效；Quick 模式会清除主机名与凭据。凭据是写入式字段，不能通过 GET、成功响应或错误消息读回；不放入 URL、浏览器持久存储或 cloudflared 命令行。PUT 请求严格解析：未知字段、类型错误、多份 JSON 或缺少必需 `enabled` 返回 400。

设置保存在 `<state_dir>/tunnel.json`，目录 0700、文件 0600，持有独占锁并原子持久化。已保存的 Web 模式、主机名、凭据与开关优先于 TOML；cloudflared 路径、下载镜像等运维配置仍由 TOML 管理。显式 `--tunnel` 仅覆盖当前进程开关，不写盘。`enabled` 因此描述当前进程期望：启动失败时可为 true，关闭写盘失败时为 false。删除覆盖成功后当前进程保持 off，即使 TOML 默认启用也要下次启动才生效。

普通保存与启用先成功持久化再应用；保存不单独改变开关，已启用时更换模式、主机名或凭据会异步重启隧道。应用先撤销旧公网授权、取消并等待旧 cloudflared，再启动新代际；旧代际的迟到事件不能重新授权。重复启用不创建第二个进程。运行中启用要求当前 WebMux 登录 token 至少 43 字符，不生成或轮换登录 token。Web 管理的启动遇到短 token、依赖定位/下载或隧道启动失败时置为 failed，HTTP/本机 API 继续可用；显式 CLI/TOML 启动仍保留其前置失败规则。设置存储损坏、不可读或锁冲突时禁止公网启动，不能回退到 TOML 或 CLI 开启，原文件不被自动覆盖。

错误响应使用 `{"error":"..."}`：请求/设置无效 400，认证失败 401，来源或本机会话权限拒绝 403，启用前提不足 409，设置服务/存储不可用 503，其他设置操作失败 500（固定通用错误，不返回内部错误值）。控制器未提供时包括 GET 在内返回 503；存在控制器但存储出错时 GET 仍为 200。

关闭是安全例外：即使无法落盘，也立即把当前进程置为 off、撤销公网授权并取消 cloudflared，返回 **503** `{"error":"...", "notPersisted":true}`。客户端应重新 GET 状态并明确告知“当前已关闭，但关闭状态未保存，修复存储后再重启”，不得据此认为旧磁盘启用值已被清除。普通保存/启用失败不应用新配置，DELETE 失败不宣称已恢复默认。

契约与实现：[线上字段](../../internal/contract/tunnel.go)、[HTTP 处理器](../../internal/api/tunnel.go)、[持久化服务](../../internal/tunnelsettings/store.go)、[运行时控制](../../cmd/webmux/tunnelctl.go)、[端点与动态来源测试用例](../../internal/api/tunnel_test.go)。终端缓冲查找完全在浏览器 xterm 中完成，不使用新的搜索 API，也不扩大 `/api/sessions/{id}/buffer` 的保留范围。
