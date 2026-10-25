# API 与协议

所有 API 以 `/api` 为前缀，WebSocket 以 `/ws` 为前缀，其余路径返回嵌入的前端（SPA 回退）。前端的完整数据契约定义在 [`web/src/schemas.ts`](../web/src/schemas.ts)；后端对应的线上类型都在 [`internal/contract`](../internal/contract)（REST 按资源分文件，终端帧与 `/ws/sessions` 事件在 `terminal.go`），由 `internal/wire` 与 Service 类型互相转换。两边的字段保持一致，`internal/api/testdata/wire` 的快照测试固定了每个端点和帧的 JSON 形状。

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

创建请求有三种互斥形式：

```jsonc
// 预设
{ "cwd": "/home/u/proj", "preset": "claude", "args": ["--resume"], "name": "可选" }
// 自定义命令
{ "cwd": "/home/u/proj", "preset": "custom", "cmd": ["bash", "-lc", "make dev"], "name": "可选" }
// 已保存的启动配置
{ "cwd": "/home/u/proj", "configId": "k3m9q2x7", "name": "可选" }
```

自定义命令必须包含非空可执行程序，且不能同时传非空 `args`；预设形式不能传 `cmd`。可附加 `workspace: {repositoryId, worktreeId}`：省略 `cwd` 时使用工作树根目录，提供时必须位于该工作树内。

预设与自定义命令形式都可附加可选的 `env: {"NAME": "value"}`：只为该会话在服务环境之上追加或覆盖显式列出的变量（名称须匹配 `[A-Za-z_][A-Za-z0-9_]*`，值不含 NUL，`WEBMUX_SESSION_ID` 保留），不复制调用方的环境，也不出现在 `SessionInfo` 中。不认识该字段的旧版服务端以 400 拒绝请求。

`configId` 形式按已保存配置的完整 argv 与环境变量启动，会话记录为 `preset: "custom"`。此时 `preset` 须为空、`cmd`/`args` 须为空数组或省略、`env` 须省略或为 `{}`，否则 400；空字符串 `configId` 400，未知 ID 404，启动配置存储不可用 503。这些检查都在目录与 Git 操作之前完成。之后修改或删除该配置不影响已启动的会话。

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
  // 程序通过 OSC 7 报告的目录；null 表示未报告或进程已退出。
  // 只作导航线索，不改变 cwd、workspace、读取边界或命令执行环境。
  reportedCwd: {
    path: string; // 已解码的绝对路径
    host: string; // 报告中的主机名，可能为空
    remote: boolean; // 非本机（例如 SSH 远端）；不得用于本机文件定位
    since: string; // RFC 3339，路径或主机变化时更新
  } | null;
}
```

### 其他

| 方法 | 路径                   | 说明                                                                                                                |
| ---- | ---------------------- | ------------------------------------------------------------------------------------------------------------------- |
| GET  | `/api/health`          | `{ok: true, sessions: n}`                                                                                           |
| GET  | `/api/presets`         | `Preset[]`：`{name, label, cmd, allowArgs}`                                                                         |
| GET  | `/api/config`          | `{defaultCwd, hostname, workspace: {gitAvailable, registryAvailable, gitError, registryError}, tunnel}`，不含 token |
| GET  | `/api/fs/dirs?prefix=` | 匹配前缀的全部子目录（含符号链接目录），普通目录优先，按名称排序                                                    |
| POST | `/api/fs/mkdir`        | `{path}`，204                                                                                                       |

`tunnel` 是只读的运行时隧道状态：`{enabled, state, named, hostname, publicUrl?}`。`state` 为 `off`、`starting`、`ready` 或 `failed`；`enabled` 仅在 `starting`/`ready` 时为 `true`，启动失败不会被当作可用。`named` 与 `hostname` 来自 `[tunnel]` 配置，未启用时也可用于说明配置模式；Named Tunnel 缺少 `hostname` 时为空字符串。`publicUrl` 仅在 `ready` 时出现，且不含 token。响应不包含 `[tunnel].token` 或其他 Cloudflare 凭据。

### 启动配置

启动配置保存在服务端 `<state_dir>/launch-configs.json`，在所有已认证设备间共享。TOML `[[presets]]` 仍由 `/api/presets` 提供，不复制进启动配置。

| 方法   | 路径                       | 说明                                                                      |
| ------ | -------------------------- | ------------------------------------------------------------------------- |
| GET    | `/api/launch-configs`      | 摘要数组（按创建顺序）：`{id, name, cmd, envNames, createdAt, updatedAt}` |
| GET    | `/api/launch-configs/{id}` | 完整配置：`{id, name, cmd, env, createdAt, updatedAt}`                    |
| POST   | `/api/launch-configs`      | `{name, cmd, env?}`，201 返回完整配置                                     |
| PUT    | `/api/launch-configs/{id}` | `{name, cmd, env?}` 整体替换，省略的 `env` 被清空，200 返回完整配置       |
| DELETE | `/api/launch-configs/{id}` | 204                                                                       |

列表只含环境变量名（字典序），值只由单条读取与创建/更新响应返回。限制：名称去首尾空白后 1–64 个字符且唯一（重名 409）；`cmd` 1–128 项、首项非空、单项不超过 8 KiB；`env` 最多 64 项，名称规则与会话 `env` 相同，值不超过 8 KiB；最多 100 条。校验失败 400，未知 ID 404，落盘失败 500（内存回滚）。文件损坏、目录不可读写或状态目录已被另一实例占用时返回 503，错误信息包含原因与文件路径。

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

错误码：400 输入错误，404 不存在，409 冲突或确认过期，503 Git / 清单不可用，504 超时，500 存储或非预期错误。JSON 结构见 `internal/contract/workspace.go`。

### 只读代码工作区

位于 `/api/sessions/{id}/code`，响应带 `Cache-Control: no-store`。

| 路由                                                       | 说明                                                                         |
| ---------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `GET /context`                                             | 根目录与能力：`contextId`、`rootPath`、`browseAvailable`、`diffAvailable` 等 |
| `GET /tree?contextId&nodeId&includeIgnored&cursor`         | 分页目录列表                                                                 |
| `GET /files?contextId&q&includeIgnored`                    | 有界路径搜索                                                                 |
| `GET /locate?contextId&path`                               | 解析用户输入的路径，返回根内 `nodeId`、`path`、`kind`（file / directory）    |
| `GET /file?contextId&nodeId&source`                        | 文件快照，`source` 为 worktree / index / head                                |
| `GET /changes?contextId&cursor&q&nodeId&group`             | 分页 Git 修改列表                                                            |
| `GET /diff?contextId&changeId`                             | 冻结的新旧双侧原文                                                           |
| `GET /snapshots/{snapshotId}/file?side=old\|new`           | 读取已捕获的比较侧全文                                                       |
| `GET /snapshots/{snapshotId}/raw?side=old\|new&download=1` | 按快照返回原始字节（≤100 MiB）；Diff 快照必须指定 `side`；也接受 HEAD        |
| `POST /check`                                              | 批量变化检测，返回 unchanged / updated / missing / expired / unknown         |

`/locate` 的相对路径以本机 OSC 7 报告目录为基点，没有报告时用启动目录；按实际文件系统解析符号链接后，必须位于代码根内（403），远端报告返回 409。结果只是节点标识，读取仍走根目录的逐段 `O_NOFOLLOW` 检查。

文档（`document` / `old` / `new`）带 `mediaType`（未识别为空串）、`size`（字节数）与 `downloadable`；`kind` 为 text / binary / unsupported / too-large / symlink / image。扩展名属于 png、jpg、jpeg、gif、webp、avif、bmp、ico 且文件头签名匹配时识别为图片，`mediaType` 取自签名；不超过 20 MiB 为 `image`，更大为 `too-large` 但仍可下载。`.svg` 为合法 UTF-8 且首个 XML 元素是 `svg` 时保持 `text` 并带 `image/svg+xml`。`/tree` 与 `/files` 返回的节点带仅按扩展名推导的 `mediaType`。Diff 的 `kind` 增加 `image`（两侧均为图片，或一侧不存在；两侧都是存在的文本时仍为 `text`）。

`/raw` 不受 JSON 接口的 10 秒时限约束，返回与快照完全一致的字节：工作树来源在发送前后校验文件身份、大小与修改时间，索引 / HEAD 按对象 ID 读取。响应带正确的 `Content-Type`（非图片为 `application/octet-stream`）、`Content-Length`、`nosniff` 与 `no-store`；仅图片且未带 `download=1` 时为 `inline`，其余为 `attachment` 并给出原文件名；SVG 另加 `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; sandbox`。同时最多 4 个传输，发送中文件变化会中断连接。

错误码：400 非法路径、比较侧或跨会话快照，404 不存在，409 版本冲突或内容已变化，410 快照失效，413 超出限制，429 传输繁忙，504 取消或超时。

### 注意力推送（Web Push）

| 方法   | 路径                           | 说明                                                                                                                                                                                                              |
| ------ | ------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| GET    | `/api/push`                    | `{enabled, available, reason?, publicKey?, subscriptions}`；`publicKey` 为 base64url 未压缩 P-256 VAPID 公钥，仅可用时出现                                                                                        |
| POST   | `/api/push/subscriptions`      | `{endpoint, expirationTime?, keys: {p256dh, auth}}`（即 `PushSubscription.toJSON()`，`expirationTime` 接受但不保存）→ 200 `{id}`；同一 endpoint 幂等返回原 ID；400 非法订阅，409 `{error, reason}` 或已满 32 台   |
| DELETE | `/api/push/subscriptions/{id}` | 204，ID 不存在同样 204                                                                                                                                                                                            |
| POST   | `/api/push/test`               | 无请求体；同时向全部已登记设备发送测试通知并等待推送服务应答 → 200 `{devices, accepted, removed: [订阅 ID], errors: [信息]}`；`removed` 为推送服务报告失效并已删除的订阅，`errors` 只含推送服务主机名；409 不可用 |

`reason` 取值：`disabled`（`[push] enabled = false`）、`storage`（`<state_dir>/push.json` 不可读写）、`quick_tunnel`、`unsupported_origin`。来源按请求 `Origin` 判定（没有时由 `Host` 推断，经隧道到达的视为 `https`）：只有已配置 Named Tunnel 的 `https://<tunnel.hostname>`（与隧道是否运行无关）和 `127.0.0.1` / `::1` / `localhost` 回环来源可以订阅；当前 Quick Tunnel 公网来源为 `quick_tunnel`，其余（VPN、局域网、其他反向代理、本机套接字）为 `unsupported_origin`。

服务端订阅会话列表，以 `(会话 ID, kind, since)` 去重：启动或重新订阅时快照中已有的 attention 只作为基线，同一信号的 source 变化、输出、spinner 与重连都不再推送；清除后再次置位或种类升级才推送。只推送 `[push] kinds` 中的种类，启发式 `waiting`、attention 清除与进程退出不推送。内部订阅积压断开后按保留的基线比较新快照，补推断开期间新出现的信号一次；500 ms 内置位又清除的信号不推送，不重放断线期间已清除的信号。

推送消息按 RFC 8291 `aes128gcm` 加密、RFC 8292 VAPID 认证，`TTL` 为 `[push] ttl_seconds`，question / permission / error 的 `Urgency` 为 `high`，其余 `normal`；单次请求 10 秒超时，不跟随重定向，推送服务返回 404 / 410 时删除订阅，其他失败只记日志（订阅 ID 与推送服务主机名）且不重试。发送经 64 条有界队列与 2 个 worker，队满丢弃，不阻塞 PTY、会话列表或 HTTP。送达是尽力而为。负载（≤ 4096 字节）：

```json
{
  "web_push": 8030,
  "notification": {
    "title": "deploy",
    "body": "等待授权",
    "navigate": "https://term.example.com/s/abcdefgh",
    "tag": "webmux-abcdefgh",
    "silent": false
  },
  "webmux": {
    "v": 1,
    "sessionId": "abcdefgh",
    "kind": "permission",
    "source": "agent",
    "since": "2026-01-01T00:00:00Z"
  }
}
```

`title` 为去除控制字符、截断到 80 字符的会话标题，为空或 `include_title = false` 时为 `WebMux`；`body` 按种类固定（需要回答、等待授权、出错停止、回合结束、有新通知、响铃提醒）；`navigate` 为登记时的来源加 `/s/<ID>`。负载不含 cwd、命令、预览、OSC 正文或终端输出。测试通知的标题为 `WebMux`、正文为 `测试通知`，`webmux` 为 `{"v": 1, "test": true}`。

前端 Service Worker `/sw.js` 只处理 `push` 与 `notificationclick`，不拦截请求、不持有 Token；点击时只用 `webmux.sessionId`（须匹配 `^[a-z2-7]{8}$`）与自身来源拼出 `/s/<ID>`，否则打开 `/`。

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

终端流，**二进制帧**，首字节为类型（`internal/contract/terminal.go` 中的 `Frame*` 常量）。JSON 控制帧由 `internal/hub` 编码，会话服务只产出类型化消息。

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

`colorSchemeReports` 表示程序已开启 DEC 私有模式 2031（`CSI ? 2031 h`）。服务端持续跟踪该模式，不受回放缓冲淘汰影响，变化时在对应输出之后广播 `0x06`。开启期间，浏览器在主题切换、附着或取得控制时以自动回复发送 `CSI ? 997 ; 1 n`（暗色）或 `CSI ? 997 ; 2 n`（亮色），并应答 `CSI ? 996 n` 查询；程序据此重新查询 OSC 10/11 颜色。原生终端附着（`webmux attach`/`run`）在取得控制或程序开启该模式时，向本地终端发送 OSC 11 背景色查询，按应答亮度以自动回复发送同样的报告，查询应答本身不写入 PTY；本地终端不应答 OSC 11 时不发送报告。分离时关闭本地终端的 2031 模式。

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

| 关闭码 | 含义                                                       |
| ------ | ---------------------------------------------------------- |
| `1011` | 服务端内部消息非法或编码失败；仅关闭当前连接，会话继续运行 |
| `4404` | 会话不存在                                                 |
| `4408` | 客户端过慢或写超时；客户端应立即重连并回放                 |
| `4410` | 会话已退出（先发送快照与退出帧）                           |

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

`webmux version|status|stop|config ... --json` 向标准输出写入恰好一个 JSON 对象，诊断只写标准错误。所有对象带 `v`（当前 `1`）；同一版本内只增加可选字段，不删除或改变既有字段含义，不兼容变更提升 `v`。退出码：`0` 运行中且健康或操作成功，`1` 其他错误，`2` 用法错误，`3` 未运行（`status`、`bridge`）。

`version`：`{"v": 1, "version": "0.3.0", "contract": 1}`。`contract` 是该构建支持的控制契约版本，不读取配置、不启动服务；客户端据此判断兼容。不带 `--json` 的 `webmux version` 与 `webmux --version` 仍输出 `webmux <版本>`。旧版本会忽略 `--json` 并输出文本，客户端应回退解析文本。

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
  "push": {
    // 仅运行中实例经本机 API 返回；未运行或旧版本实例省略。不含 endpoint 或密钥
    "enabled": true, // [push] enabled
    "available": true, // 推送存储可用
    "error": "", // 仅不可用时出现
    "subscriptions": 1, // 已登记设备数
    "publicOrigin": "https://term.example.com", // 仅配置了 Named Tunnel 时出现
  },
}
```

链接不带路径、查询参数或 Token。回环监听的远程链接为空数组；通配监听列出启用中的非回环、非链路本地 IPv4 地址，Tailscale（100.64.0.0/10）在前，WSL NAT 模式下的虚拟网卡地址不列出；通配监听且未配置 Token 时 `warnings` 含暴露警告。

`stop`：`{"v": 1, "result": "stopped" | "killed" | "not-running" | "refused" | "error", "message": "..."}`。

`config show`：`{"v", "path", "exists", "listen", "tokenConfigured", "defaultCwd", "stateDir", "allowedOrigins", "presets": [{"name", "label", "argv", "allowArgs"}]}`，不含 Token。`config path`：`{"v", "path", "exists"}`。`config validate`：`{"v", "ok", "errors": [...]}`。
