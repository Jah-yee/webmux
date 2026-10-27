# attention-push Specification

## Purpose
在用户离开页面、关闭浏览器标签或手机锁屏后，通过 Web Push 把会话的明确注意力信号（提问、授权、本轮完成、错误、终端通知）提醒到已订阅的设备，并以 Cloudflare Named Tunnel 作为手机端的安全上下文。

## Requirements

### Requirement: 推送配置

系统 SHALL 读取 TOML `[push]` 配置节，字段及默认值如下：

- `enabled = true`
- `subject = "https://github.com/mzdluo123/webmux"`：须为 `https://` URL 或 `mailto:` 地址
- `ttl_seconds = 86400`：须在 0 到 2419200 之间
- `kinds = ["question", "permission", "error", "done", "notify"]`：元素须属于 `question`、`permission`、`error`、`done`、`notify`、`bell`，不得重复
- `include_title = true`

任一字段非法时，系统 MUST 输出可定位的配置错误并以非零状态退出，不开始监听。

#### Scenario: 默认配置

- **WHEN** 配置文件没有 `[push]` 节
- **THEN** 推送启用，TTL 为 86400 秒，推送类型为 question、permission、error、done、notify，不推送 bell

#### Scenario: 非法类型

- **WHEN** 配置 `kinds = ["question", "waiting"]`
- **THEN** 启动失败，错误指出 `push.kinds` 中的 `waiting` 不受支持

#### Scenario: 非法 subject

- **WHEN** 配置 `subject = "http://example.com"`
- **THEN** 启动失败，错误说明 subject 必须是 https URL 或 mailto 地址

#### Scenario: 配置校验命令

- **WHEN** 用户对含 `ttl_seconds = -1` 的配置执行 `webmux config validate`
- **THEN** 命令以退出码 1 结束并报告 `push.ttl_seconds` 越界

### Requirement: 停用推送

`[push] enabled = false` 时，系统 MUST NOT 创建或读取推送存储文件，MUST NOT 订阅会话事件或向推送服务发起请求。`GET /api/push` SHALL 报告不可用，原因为 `disabled`；登记订阅与测试通知 SHALL 返回 409。

#### Scenario: 停用后不产生文件

- **WHEN** 以 `enabled = false` 启动，且 `state_dir` 下没有推送存储文件
- **THEN** 运行期间不生成该文件，会话中出现 attention 时不发出任何推送请求

#### Scenario: 停用后登记订阅

- **WHEN** 推送停用时浏览器请求 `POST /api/push/subscriptions`
- **THEN** 返回 409，错误说明推送已在配置中停用

### Requirement: 推送存储

推送启用时，系统 SHALL 在 `state_dir` 下的推送存储文件中保存 VAPID P-256 密钥对与设备订阅。文件不存在时 SHALL 生成新密钥。所在目录权限 MUST 为 0700，文件权限 MUST 为 0600，每次写入 MUST 以原子替换完成。重启后 SHALL 复用同一密钥与订阅。

#### Scenario: 首次启动生成密钥

- **WHEN** 推送启用，`state_dir` 下没有推送存储文件
- **THEN** 系统生成 VAPID 密钥并写入权限为 0600 的文件，`GET /api/push` 返回对应公钥

#### Scenario: 重启后订阅保留

- **WHEN** 已登记一个订阅后重启 WebMux
- **THEN** `GET /api/push` 返回相同公钥，订阅数仍为 1，新的 attention 仍推送到该订阅

### Requirement: 推送存储故障隔离

推送存储无法解析时，系统 SHALL 把原文件改名保留，并以新密钥和空订阅重建，同时记录警告。存储目录或文件不可读写时，系统 SHALL 只停用推送，`GET /api/push` 报告原因 `storage`；两种情况都 MUST NOT 阻止启动或影响会话。

#### Scenario: 文件损坏

- **WHEN** 推送存储文件内容不是合法 JSON
- **THEN** WebMux 正常启动，原文件改名保留，新文件含新密钥与空订阅，日志出现警告

#### Scenario: 目录不可写

- **WHEN** 推送存储所在目录不可写
- **THEN** WebMux 正常启动，会话可创建和使用，`GET /api/push` 返回 `available: false` 与原因 `storage`

### Requirement: 推送来源判定

系统 SHALL 按页面来源判定推送资格；请求带 Origin 时以 Origin 为准，否则以 Host 推断。符合资格的来源只有两类：

- 已配置 Named Tunnel 时的 `https://<tunnel.hostname>`，与隧道当前是否运行无关；
- 主机为 `127.0.0.1`、`::1` 或 `localhost` 的回环来源。

当前 Quick Tunnel 公网来源 SHALL 判为 `quick_tunnel`，其他来源 SHALL 判为 `unsupported_origin`。

#### Scenario: Named Tunnel 来源

- **WHEN** 配置 `tunnel.token` 与 `tunnel.hostname = "term.example.com"`，手机从 `https://term.example.com` 请求 `GET /api/push`
- **THEN** 响应 `available` 为 `true`

#### Scenario: Quick Tunnel 来源

- **WHEN** Quick Tunnel 就绪于 `https://abc.trycloudflare.com`，浏览器从该来源请求 `GET /api/push`
- **THEN** 响应 `available` 为 `false`，`reason` 为 `quick_tunnel`

#### Scenario: 局域网或 VPN 地址

- **WHEN** 浏览器从 `http://100.64.0.2:7681`（已在 `allowed_origins` 中）请求 `GET /api/push`
- **THEN** 响应 `available` 为 `false`，`reason` 为 `unsupported_origin`

#### Scenario: 桌面回环地址

- **WHEN** 本机浏览器从 `http://127.0.0.1:7681` 请求 `GET /api/push`
- **THEN** 响应 `available` 为 `true`

### Requirement: 推送状态接口

`GET /api/push` SHALL 返回 `{enabled, available, reason, publicKey, subscriptions}`：`publicKey` 为 base64url 编码的未压缩 P-256 公钥，仅在 `available` 为 `true` 时出现；`reason` 为 `disabled`、`storage`、`quick_tunnel`、`unsupported_origin` 之一，仅在不可用时出现；`subscriptions` 为已登记订阅数。响应 MUST NOT 含任何订阅 endpoint 或密钥。

#### Scenario: 可用时的状态

- **WHEN** 推送可用且已登记 2 个订阅
- **THEN** 响应为 `{"enabled":true,"available":true,"publicKey":"<base64url>","subscriptions":2}`，不含 endpoint

#### Scenario: 未认证请求

- **WHEN** 配置了 Token，请求 `GET /api/push` 未携带 Token
- **THEN** 返回 401

### Requirement: 登记订阅

`POST /api/push/subscriptions` SHALL 接受 `{"endpoint": "...", "keys": {"p256dh": "...", "auth": "..."}}`，返回 200 与 `{"id": "<订阅 ID>"}`。endpoint MUST 是不含凭据、长度不超过 2048 字节的 `https://` URL；`p256dh` MUST 解码为 P-256 曲线上的 65 字节未压缩点，`auth` MUST 解码为 16 字节，否则返回 400。同一 endpoint 再次登记 SHALL 更新密钥并返回原 ID。订阅 SHALL 记录登记时的页面来源。来源不符合资格时 SHALL 返回 409 及原因；已有 32 个订阅时登记新 endpoint SHALL 返回 409。

#### Scenario: 幂等登记

- **WHEN** 同一浏览器用相同 endpoint 连续登记两次
- **THEN** 两次返回相同 `id`，`GET /api/push` 的 `subscriptions` 为 1

#### Scenario: Quick Tunnel 来源被拒

- **WHEN** 浏览器从 Quick Tunnel 公网来源登记订阅
- **THEN** 返回 409，错误说明推送需要 Named Tunnel，订阅数不变

#### Scenario: 非法 endpoint

- **WHEN** 请求体 endpoint 为 `http://push.example/x`
- **THEN** 返回 400，不保存订阅

#### Scenario: 订阅已满

- **WHEN** 已有 32 个订阅，又登记一个新 endpoint
- **THEN** 返回 409，提示先在其他设备关闭推送

#### Scenario: Origin 不符

- **WHEN** 登记请求的 Origin 不在允许来源内
- **THEN** 返回 403，与其他写接口相同

### Requirement: 删除订阅

`DELETE /api/push/subscriptions/{id}` SHALL 删除对应订阅并返回 204；ID 不存在时同样返回 204。

#### Scenario: 删除后不再推送

- **WHEN** 删除某订阅后会话出现新的 question
- **THEN** 不向该订阅的 endpoint 发出请求

#### Scenario: 重复删除

- **WHEN** 对同一 ID 第二次请求删除
- **THEN** 返回 204

### Requirement: 测试通知

`POST /api/push/test` SHALL 同时向全部已登记的订阅发送一条测试通知，等待各推送服务应答后返回 200 与 `{"devices", "accepted", "removed", "errors"}`：`devices` 为发送时的订阅数，`accepted` 为推送服务接受的数量；推送服务返回 404 或 410 的订阅 SHALL 被删除，其 ID 列入 `removed`；其他失败列入 `errors`，信息 MUST NOT 含 endpoint。单个订阅失败不影响其他订阅。推送不可用时返回 409。

#### Scenario: 测试成功

- **WHEN** 推送服务以 201 接受全部订阅的测试通知
- **THEN** 接口返回 200，`accepted` 等于 `devices`，每台设备显示标题为“WebMux 测试通知”、正文为“若看到此消息，说明推送已生效”的通知

#### Scenario: 部分订阅已失效

- **WHEN** 两台设备中的一台的推送服务对测试通知返回 410
- **THEN** 接口返回 200，`accepted` 为 1，`removed` 只含该订阅 ID，该订阅被删除，订阅数减 1

### Requirement: 注意力差分与去重

系统 SHALL 以 `(会话 ID, kind, since)` 作为推送去重键，在会话的 attention 出现新的去重键时推送一次。启动时与会话列表快照中已有的 attention SHALL 只作为基线，不推送。去重键不变的会话更新（包括 source 变化、输出、spinner、预览、客户端数、状态变化）MUST NOT 推送。会话移除后 SHALL 丢弃其基线。

#### Scenario: 新出现的提问

- **WHEN** 会话的 attention 从空变为 `{kind:"question", source:"agent"}`
- **THEN** 系统向所有订阅推送一次该会话的 question 提醒

#### Scenario: spinner 持续输出

- **WHEN** question 已推送，之后 agent 持续输出 spinner 并重复上报 question，attention 的 since 不变
- **THEN** 不再推送

#### Scenario: 清除后再次置位

- **WHEN** 用户输入清除了 question，之后 agent 再次提问，since 更新
- **THEN** 系统再推送一次

#### Scenario: 种类升级

- **WHEN** attention 由 done 变为 permission
- **THEN** 系统推送 permission 提醒

#### Scenario: 内部事件流落后

- **WHEN** 推送子系统的会话事件订阅因积压被断开并重新订阅，断开期间某会话出现了新的 error
- **THEN** 重新订阅后按保留的基线比较新快照，推送该 error 一次；断开前已推送的信号不重复推送

### Requirement: 推送范围

系统 SHALL 只推送 `kinds` 配置中的 attention 种类。启发式 `waiting` 状态、attention 被清除和会话退出 MUST NOT 产生推送。

#### Scenario: 启发式等待

- **WHEN** 会话没有 attention，但因空闲被判定为 `waiting`
- **THEN** 不推送

#### Scenario: 进程退出

- **WHEN** 会话进程退出，状态变为 `exited`
- **THEN** 不推送

#### Scenario: 默认不推送响铃

- **WHEN** 使用默认 `kinds`，会话因 BEL 产生 `bell` attention
- **THEN** 不推送

#### Scenario: 不在配置内的种类

- **WHEN** `kinds = ["question", "permission"]`，会话出现 done
- **THEN** 不推送，且随后升级为 permission 时仍推送

### Requirement: 推送内容

推送负载 SHALL 是不超过 4096 字节的 JSON，含 Declarative Web Push 字段 `web_push: 8030` 与 `notification`（`title`、`body`、`navigate`、`tag`），以及 `webmux` 对象（`v`、`sessionId`、`kind`、`source`、`since`）。`title` 为会话标题（去除控制字符、截断至 80 字符）；标题为空或 `include_title = false` 时为“WebMux”。`body` 为该种类的固定文案（形如“等待授权 · Agent 请求执行工具操作”，前半是会话列表同款短标签，后半是可执行的下一步），`navigate` 为订阅来源加 `/s/<会话 ID>`，`tag` 按会话区分。负载 MUST NOT 含 cwd、命令、预览、OSC 通知正文或任何 PTY 输出。

#### Scenario: 授权请求的负载

- **WHEN** 来源为 `https://term.example.com` 的订阅收到标题为 `deploy` 的会话 `abcdefgh` 的 permission 提醒
- **THEN** 解密后的负载中 `notification.title` 为 `deploy`，`notification.body` 为“等待授权 · Agent 请求执行工具操作”，`notification.navigate` 为 `https://term.example.com/s/abcdefgh`，`webmux.kind` 为 `permission`

#### Scenario: 不包含终端内容

- **WHEN** 会话因 `OSC 9;构建失败: /home/u/secret` 产生 notify 提醒
- **THEN** 负载中不出现 `构建失败`、`/home/u/secret`、会话 cwd 或命令

#### Scenario: 隐藏标题

- **WHEN** `include_title = false`
- **THEN** 负载中 `notification.title` 为 `WebMux`，不含会话标题

### Requirement: 推送发送

系统 SHALL 按 RFC 8291 以 `aes128gcm` 加密负载，按 RFC 8292 携带以 `subject` 签名的 VAPID 认证，并设置 `TTL` 为 `ttl_seconds`。question、permission、error 的 `Urgency` SHALL 为 `high`，其余为 `normal`。单次请求 SHALL 在 10 秒内超时，MUST NOT 跟随重定向。

#### Scenario: 高优先级提醒

- **WHEN** 发送 permission 提醒
- **THEN** 请求头含 `Content-Encoding: aes128gcm`、`TTL: 86400`、`Urgency: high` 与 `Authorization: vapid t=…, k=<公钥>`

#### Scenario: 推送服务重定向

- **WHEN** 推送服务返回 307 重定向
- **THEN** 系统不访问重定向目标，本次发送记为失败，订阅保留

### Requirement: 失效订阅清理

推送服务返回 404 或 410 时，系统 SHALL 删除该订阅。其他失败（超时、网络错误、其他状态码）SHALL 保留订阅，只记录不含 endpoint 的日志，且不重试本条提醒。

#### Scenario: 订阅过期

- **WHEN** 推送服务对某订阅返回 410
- **THEN** 该订阅被删除，之后的提醒不再发往该 endpoint

#### Scenario: 推送服务不可达

- **WHEN** 服务端无法连接推送服务
- **THEN** 订阅保留，日志记录失败但不含 endpoint，会话不受影响

### Requirement: 发送与会话隔离

推送发送 MUST NOT 阻塞 PTY 读取、会话列表更新或 HTTP 请求。待发送提醒 SHALL 进入有界队列并以有限并发发送；队列已满时 SHALL 丢弃新提醒并记录警告。WebMux 关闭时 SHALL 停止发送并在关闭超时内结束推送相关的 goroutine。

#### Scenario: 推送服务缓慢

- **WHEN** 推送服务每个请求都在 10 秒后才超时，期间会话持续产生输出与新的 attention
- **THEN** 终端输出与 `/ws/sessions` 更新不受延迟，超出队列容量的提醒被丢弃并记录

### Requirement: 推送不依赖隧道状态

推送 SHALL 直接从服务端发往推送服务，与 Cloudflare Tunnel 当前是否运行无关：隧道关闭、失败或尚未就绪时，系统 SHALL 照常向已登记的订阅发送提醒。

#### Scenario: 隧道关闭期间

- **WHEN** 用户通过托盘关闭公网隧道后，会话出现新的 question
- **THEN** 系统仍向来源为 Named Tunnel 的订阅发送提醒

### Requirement: 推送凭据保密

订阅 endpoint、`p256dh`、`auth` 与 VAPID 私钥 MUST NOT 出现在日志、`/api/*` 响应、`/local/v1/*` 响应或 `webmux status` 输出中。

#### Scenario: 查询接口不泄露

- **WHEN** 已登记订阅后请求 `GET /api/push`、`GET /api/config` 与本机状态
- **THEN** 响应中不出现任何订阅的 endpoint、`p256dh`、`auth` 或 VAPID 私钥
