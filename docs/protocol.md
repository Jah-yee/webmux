# API 与协议

所有 API 以 `/api` 为前缀，WebSocket 以 `/ws` 为前缀，其余路径返回嵌入的前端（SPA 回退）。前端的完整数据契约定义在 [`web/src/schemas.ts`](../web/src/schemas.ts)，与后端 JSON 字段保持一致。

## 认证与 Origin

- 启用 token 时，HTTP 请求使用 `Authorization: Bearer <token>` 或 Cookie `webmux_token`。浏览器无法为 WebSocket 握手设置请求头，前端改为同时提供子协议 `webmux` 与 `webmux.token.<base64url(UTF-8 token)>`（无填充），服务端只选择 `webmux`、不回显 token。WebSocket 不接受 query `?token=`，token 不进入请求 URL；前端也不把 token 写入 Cookie，因为 Cookie 不按端口隔离。
- token 以定长摘要做常量时间比较。
- 带 `Origin` 的请求必须与 `Host` 同源，或在 `allowed_origins` 中。Origin 必须是完整的 HTTP(S) origin，不能含路径、query 或 fragment。
- 静态页面公开；所有 API 与 WebSocket 都受认证和 Origin 检查保护。认证失败返回 401，Origin 拒绝返回 403。
- 错误响应为 JSON `{"error": "..."}`。

## HTTP API

### 会话

| 方法   | 路径                          | 说明                                             |
| ------ | ----------------------------- | ------------------------------------------------ |
| GET    | `/api/sessions`               | 会话列表 `SessionInfo[]`，按 `lastActiveAt` 倒序 |
| POST   | `/api/sessions`               | 创建会话，201 返回 `SessionInfo`                 |
| GET    | `/api/sessions/{id}`          | 单个会话；不存在 404                             |
| DELETE | `/api/sessions/{id}`          | 关闭会话（SIGHUP → SIGKILL），204                |
| DELETE | `/api/sessions/{id}?remove=1` | 移除已退出会话，204；仍在运行 409                |
| GET    | `/api/sessions/{id}/buffer`   | 下载完整原始缓冲（`application/octet-stream`）   |
| POST   | `/api/sessions/{id}/upload`   | 上传文件，见下文                                 |

创建请求有两种互斥形式：

```jsonc
// 预设
{ "cwd": "/home/u/proj", "preset": "claude", "args": ["--resume"], "name": "可选" }
// 自定义命令
{ "cwd": "/home/u/proj", "preset": "custom", "cmd": ["bash", "-lc", "make dev"], "name": "可选" }
```

自定义命令必须包含非空可执行程序，且不能同时传非空 `args`；预设形式不能传 `cmd`。可附加 `workspace: {repositoryId, worktreeId}`：省略 `cwd` 时使用工作树根目录，提供时必须位于该工作树内。

两种形式都可附加可选的 `env: {"NAME": "value"}`：只为该会话在服务环境之上追加或覆盖显式列出的变量（名称须匹配 `[A-Za-z_][A-Za-z0-9_]*`，值不含 NUL，`WEBMUX_SESSION_ID` 保留），不复制调用方的环境，也不出现在 `SessionInfo` 中。不认识该字段的旧版服务端以 400 拒绝请求。

**上传**：`multipart/form-data`，恰好一个文件字段 `file`。成功返回 201 `{"path": "<服务器绝对路径>"}`。单文件上限 100 MiB（超限 413），读期限 5 分钟（超时 408）；无效请求 400，会话不存在 404，会话已退出 409。

```ts
interface SessionInfo {
  id: string;
  generation: string; // 本次运行的代际，16 位十六进制；输出 offset 与提交都绑定它
  title: string;
  cwd: string;
  preset: string; // 预设名或 "custom"
  cmd: string[];
  status: "running" | "waiting" | "idle" | "exited"; // waiting 表示提示符、终端读取或 attention；计时休眠不是 waiting
  exitCode: number | null;
  preview: string; // 屏幕底部最多 100 条非空行，以 \n 分隔
  createdAt: string; // RFC 3339
  lastActiveAt: string;
  cols: number;
  rows: number;
  clients: number; // 当前附着的连接数
  workspace: {
    repositoryId: string;
    worktreeId: string;
    worktreePath: string;
  } | null;
  // 程序主动报告的“需要介入”，存在时 status 为 "waiting"；不含任何终端正文。
  attention: {
    kind: "question" | "permission" | "done" | "error" | "notify" | "bell";
    source: "agent" | "terminal"; // 结构化事件 / 普通通知或响铃
    since: string; // RFC 3339，种类变化时更新
  } | null;
}
```

### 其他

| 方法 | 路径                   | 说明                                                                                                        |
| ---- | ---------------------- | ----------------------------------------------------------------------------------------------------------- |
| GET  | `/api/health`          | `{ok: true, sessions: n}`                                                                                   |
| GET  | `/api/presets`         | `Preset[]`：`{name, label, cmd, allowArgs}`                                                                 |
| GET  | `/api/config`          | `{defaultCwd, hostname, workspace: {gitAvailable, registryAvailable, gitError, registryError}}`，不含 token |
| GET  | `/api/fs/dirs?prefix=` | 匹配前缀的全部子目录（含符号链接目录），普通目录优先，按名称排序                                            |
| POST | `/api/fs/mkdir`        | `{path}`，204                                                                                               |

### 仓库与 worktree

| 方法       | 路径                                                            | 说明                                                                                       |
| ---------- | --------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| GET / POST | `/api/repositories`                                             | 列出仓库快照；POST `{path}` 登记仓库（新增 201，已存在 200）                               |
| DELETE     | `/api/repositories/{id}`                                        | 仅移除登记，204                                                                            |
| GET        | `/api/repositories/{id}/worktrees`                              | 刷新仓库快照（含 `checkedAt` 与 `worktrees`）                                              |
| GET        | `/api/repositories/{id}/branches`                               | `{branches, bases, defaultBase, defaultParent}`，不 fetch                                  |
| POST       | `/api/repositories/{id}/worktrees`                              | `{path, mode: "new", branch, base}` 或 `{path, mode: "existing", branch}`，201             |
| POST       | `/api/repositories/{id}/worktrees/{worktreeId}/removal-preview` | 删除预览：`{confirmationId, expiresAt, worktree, sessions, blockers, forceAllowed}`        |
| POST       | `/api/repositories/{id}/worktrees/{worktreeId}/remove`          | `{confirmationId, mode: "safe"}`；强制删除另需 `confirmPath` 与 `closeSessions: true`，204 |

错误码：400 输入错误，404 不存在，409 冲突或确认过期，503 Git / 清单不可用，504 超时，500 存储或非预期错误。JSON 结构见 `internal/workspace/types.go`。

### 只读代码工作区

位于 `/api/sessions/{id}/code`，响应带 `Cache-Control: no-store`。

| 路由                                               | 说明                                                                         |
| -------------------------------------------------- | ---------------------------------------------------------------------------- |
| `GET /context`                                     | 根目录与能力：`contextId`、`rootPath`、`browseAvailable`、`diffAvailable` 等 |
| `GET /tree?contextId&nodeId&includeIgnored&cursor` | 分页目录列表                                                                 |
| `GET /files?contextId&q&includeIgnored`            | 有界路径搜索                                                                 |
| `GET /file?contextId&nodeId&source`                | 文件快照，`source` 为 worktree / index / head                                |
| `GET /changes?contextId&cursor&q&nodeId&group`     | 分页 Git 修改列表                                                            |
| `GET /diff?contextId&changeId`                     | 冻结的新旧双侧原文                                                           |
| `GET /snapshots/{snapshotId}/file?side=old\|new`   | 读取已捕获的比较侧全文                                                       |
| `POST /check`                                      | 批量变化检测，返回 unchanged / updated / missing / expired / unknown         |

错误码：400 非法路径，404 不存在，409 版本冲突，410 快照失效，413 超出限制，504 取消或超时。

## WebSocket `/ws/sessions`

会话列表，文本帧 JSON。连接后先收到 `snapshot`，之后为增量事件：

```jsonc
{"type": "snapshot", "sessions": [SessionInfo, ...]}
{"type": "created", "session": SessionInfo}
{"type": "updated", "session": SessionInfo}
{"type": "removed", "id": "abc123"}
```

`updated` 每会话最多 500 ms 一次；仅预览变化最多 2 秒一次。客户端无需发送消息。

## WebSocket `/ws/s/{id}`

终端流，**二进制帧**，首字节为类型。

可选查询参数：

| 参数         | 说明                                                                 |
| ------------ | -------------------------------------------------------------------- |
| `client`     | 客户端 ID，`[A-Za-z0-9_-]{8,64}`；前端每个标签页一个，刷新与重连保持 |
| `label`      | 设备名，去除控制字符后最多 40 个字符，用于显示控制端                 |
| `generation` | 续接点：客户端终端所属的会话代际                                     |
| `offset`     | 续接点：客户端已渲染到的输出 offset（十进制）                        |

格式不符的参数被忽略：缺少 `client` 时服务端为连接生成随机 ID；续接点无效时全量回放。

服务端 → 客户端：

| 首字节 | 含义     | 载荷                                                                                                |
| ------ | -------- | --------------------------------------------------------------------------------------------------- |
| `0x00` | 输出     | PTY 原始字节                                                                                        |
| `0x01` | 快照     | 完整缓冲；客户端应先 `reset()` 再写入                                                               |
| `0x02` | 状态     | JSON `SessionInfo`                                                                                  |
| `0x03` | 退出     | JSON `{"exitCode": n}`，随后关闭连接                                                                |
| `0x04` | 位置     | JSON `{"generation", "mode": "reset"\|"append", "start", "end", "truncated"}`，先于回放             |
| `0x05` | 提交状态 | JSON `{"id", "state", "error"?}`                                                                    |
| `0x06` | 控制     | JSON `{"controller": {"clientId", "label", "since"} \| null, "cols", "rows", "colorSchemeReports"}` |

客户端 → 服务端：

| 首字节 | 含义     | 载荷                                                                |
| ------ | -------- | ------------------------------------------------------------------- |
| `0x00` | 输入     | 写入 PTY 的 UTF-8 字节                                              |
| `0x01` | resize   | JSON `{"cols": n, "rows": n}`；仅控制端生效                         |
| `0x02` | 重新同步 | 空；服务端回复 `reset` 位置帧与 `0x01` 快照                         |
| `0x03` | 提交     | JSON 头 `{"id", "generation", "bodyBytes"}`、`\n`、正文与提交键字节 |
| `0x04` | 查询提交 | JSON `{"ids": [...]}`，最多 64 个                                   |
| `0x05` | 接管控制 | 空                                                                  |
| `0x06` | 自动回复 | xterm 对设备查询的应答字节；仅负责回复的连接写入，不取得控制        |

`colorSchemeReports` 表示程序已开启 DEC 私有模式 2031（`CSI ? 2031 h`）。服务端持续跟踪该模式，不受回放缓冲淘汰影响，变化时在对应输出之后广播 `0x06`。开启期间，浏览器在主题切换、附着或取得控制时以自动回复发送 `CSI ? 997 ; 1 n`（暗色）或 `CSI ? 997 ; 2 n`（亮色），并应答 `CSI ? 996 n` 查询；程序据此重新查询 OSC 10/11 颜色。

使用二进制帧而非 JSON，是为了避免 base64 开销以及在多字节字符中间截断时的 UTF-8 问题；提交帧同样只把元数据放进 JSON 头。

### 回放与续接

每次回放（附着或 `0x02`）依次发送 `0x06` 控制帧和 `0x04` 位置帧：观察端先按控制端网格调整终端，再解析按光标定位的历史。offset 为会话自启动以来累计输出的字节数。`0x02` 清空队列时保留尚未送出的 `0x05` 提交状态，排在状态帧之后、`0x03` 退出帧之前；退出帧始终是最后一帧。

- `mode: "reset"`：随后一帧 `0x01` 快照，覆盖 `[start, end)`。
- `mode: "append"`：续接点的代际一致且 `start ≤ offset ≤ end` 时使用，随后（如有）一帧 `0x00`，内容恰为 `[offset, end)`。
- `truncated`：最早输出已被环形缓冲覆盖（`start > 0`）。

回放与实时订阅在同一把锁内登记，此后的 `0x00` 帧按字节累加即为客户端的 offset。

### 提交

提交 ID 为 `[A-Za-z0-9_-]{8,64}`。服务端在一次输入锁持有期间写入正文与提交键，状态依次为：

| `state`    | 含义                                                  |
| ---------- | ----------------------------------------------------- |
| `received` | 已登记，正在写入                                      |
| `written`  | 全部字节已写入 PTY（不代表 agent 已处理）             |
| `partial`  | 部分字节已写入后出错                                  |
| `failed`   | 未写入；`error` 为 `generation`、`invalid` 或 `write` |
| `missing`  | 仅用于查询：本次运行无此 ID 的记录（未送达或已过期）  |

重复 ID 不会再次写入，服务端直接回报已记录状态。状态发往提交客户端（同一 `client`）的全部连接；在 `received` 期间查询过该 ID 的其他客户端（例如在新标签页恢复的草稿）也会收到最终状态。记录保留最近 256 条、30 分钟。

### 控制端

- 会话无控制端时，首个附着的客户端成为控制端；同一 `client` 重连保持控制。控制端客户端最近附着的连接是唯一有效连接（复制的标签页可能带着相同 ID），只有它的 resize 与自动回复生效；它断开时由该客户端的其他连接接替。同一客户端的另一条连接成为有效连接时（接替或用户输入），服务端向它单独发送 `0x06`，使它重新发送尺寸。
- `0x00` 输入、`0x03` 提交与 `0x05` 把控制权交给发送方；控制端的最后一个连接断开后控制权空缺，不自动转交。
- 只有控制端连接的 `0x01` 生效，所有权检查与 resize 在同一临界区内完成；尺寸或控制变化时广播 `0x06`。
- 输入来源由客户端标明：xterm 的用户输入走 `0x00`，对设备查询的自动应答走 `0x06`（同样的字节可能来自按键，如 Shift+F3 与光标位置报告都是 `ESC [ 1 ; 2 R`）。`0x06` 只接受控制端连接，无控制端时只接受最早附着的连接，且不取得控制、不清除关注提醒。
- 前端复制标签页时，若在 `sessionStorage` 中看到原标签页的存活标记，会换用新的客户端 ID。

### 关闭码

| 关闭码 | 含义                                       |
| ------ | ------------------------------------------ |
| `4404` | 会话不存在                                 |
| `4408` | 客户端过慢或写超时；客户端应立即重连并回放 |
| `4410` | 会话已退出（先发送快照与退出帧）           |

认证失败在握手阶段直接返回 HTTP 401，不升级连接。

## 本机 API（Unix 套接字）

服务端在 `<state_dir>/run/webmux.sock`（路径超过平台 `sun_path` 上限时改用 `$XDG_RUNTIME_DIR/webmux/<摘要>.sock`，实际路径写入运行时记录）上提供与 TCP 完全相同的 `/api/*`、`/ws/*`，认证方式改为对端进程的有效 UID，不检查 Token、Host 与 Origin。示例：

```sh
curl --unix-socket ~/.local/state/webmux/run/webmux.sock http://webmux/api/sessions
```

套接字额外提供以下接口；它们不在 TCP 上提供（返回 404）。响应为 JSON，错误为 `{"error": "..."}`。`v1` 内只增加可选字段，不兼容变更使用新的路径版本。

| 路由                      | 说明                                                                                                                                                  |
| ------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /local/v1/status`    | 状态 JSON（与 `webmux status --json` 契约 v1 相同，`record`、`localApi` 为 `true`，`drift` 由服务端重读配置文件计算）                                 |
| `POST /local/v1/shutdown` | 可选请求体 `{"force": bool}`；systemd / launchd 启动且未设 `force` 时 409 并给出管理命令；否则 202，按 SIGTERM 流程退出。会话内部的连接 403           |
| `GET /local/v1/token`     | `{"token": "..."}`；未配置 Token 时 404；会话内部的连接 403                                                                                           |
| `POST /local/v1/tunnel`   | 请求体 `{"enabled": bool}`；开启或关闭 Cloudflare Tunnel，立即返回 202 与隧道状态对象；请求体无效 400；Token 不满足要求时开启 409；会话内部的连接 403 |

隧道开启按启动时加载的 `[tunnel]` 配置与 `--tunnel` 相同的规则定位 cloudflared，要求当前 Token 不少于 43 字符（运行中不生成或更换 Token），重复开启不启动第二个 cloudflared；就绪后服务输出打印不含 Token 的公网地址。关闭先撤销公网主机名授权再结束 cloudflared，同样适用于 `--tunnel` 启动的隧道，已关闭时无副作用。cloudflared 定位失败或意外退出时状态为 `failed` 并撤销授权，不自动重试；开启状态不持久化。

`webmux bridge` 把一条套接字连接原样桥接到标准输入输出（HTTP/1.1 keep-alive 与 WebSocket 升级照常工作）；连接失败时不写标准输出，服务未运行退出码 3，其他错误 1。

## 控制面 JSON 契约

`webmux status|stop|config ... --json` 向标准输出写入恰好一个 JSON 对象，诊断只写标准错误。所有对象带 `v`（当前 `1`）；同一版本内只增加可选字段，不删除或改变既有字段含义，不兼容变更提升 `v`。退出码：`0` 运行中且健康或操作成功，`1` 其他错误，`2` 用法错误，`3` 未运行（`status`、`bridge`）。

`status`（亦为 `/local/v1/status`）：

```jsonc
{
  "v": 1,
  "state": "running", // running | stopped | unresponsive | error
  "record": true, // 来自有效运行时记录或本机 API
  "localApi": true, // 经本机 API 获取
  "pid": 1234, // 未运行时为 null
  "version": "0.1.0",
  "launcher": "manual", // manual | systemd | launchd | windows-tray
  "listen": "127.0.0.1:7681", // 实际监听地址
  "sessions": 2, // 健康时的会话数，否则为 null
  "health": { "status": "ok", "message": "" }, // ok | unauthorized | refused | timeout | not-webmux | error
  "urls": {
    "local": "http://127.0.0.1:7681",
    "remote": [
      {
        "url": "http://100.64.0.5:7681",
        "interface": "tailscale0",
        "kind": "tailscale",
      },
    ],
  },
  "tokenConfigured": true,
  "drift": [], // 配置已修改但未重启的字段：listen、token
  "warnings": [],
  "config": {
    "path": "/home/u/.config/webmux/config.toml",
    "exists": true,
    "error": "",
  },
  "logFile": "",
  "socket": "/home/u/.local/state/webmux/run/webmux.sock",
  "startedAt": "2026-01-01T00:00:00Z",
  "token": "...", // 仅 --reveal-token 且配置了 Token 时出现
  "tunnel": {
    // 仅运行中实例经本机 API 返回；未运行或旧版本实例省略
    "state": "ready", // off | starting | ready | failed
    "publicUrl": "https://abc.trycloudflare.com", // 仅 ready，不含 Token
    "error": "", // 仅 failed，失败原因
    "named": false, // 是否 Named Tunnel
  },
}
```

链接不带路径、查询参数或 Token。回环监听的远程链接为空数组；通配监听列出启用中的非回环、非链路本地 IPv4 地址，Tailscale（100.64.0.0/10）在前，WSL NAT 模式下的虚拟网卡地址不列出；通配监听且未配置 Token 时 `warnings` 含暴露警告。

`stop`：`{"v": 1, "result": "stopped" | "killed" | "not-running" | "refused" | "error", "message": "..."}`。

`config show`：`{"v", "path", "exists", "listen", "tokenConfigured", "defaultCwd", "stateDir", "allowedOrigins", "presets": [{"name", "label", "argv", "allowArgs"}]}`，不含 Token。`config path`：`{"v", "path", "exists"}`。`config validate`：`{"v", "ok", "errors": [...]}`。
