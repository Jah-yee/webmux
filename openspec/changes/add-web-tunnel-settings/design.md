## Context

WebMux 的隧道配置目前全部来自启动时的 TOML：`cmd/webmux/main.go` 在 `loadConfig` 中读取 `[tunnel]`，`ensureToken` 根据 `enabled` 生成或校验 WebMux 登录 Token，`serve` 在监听前定位 cloudflared；运行中只有 `tunnelController.SetEnabled` 通过 `/local/v1/tunnel` 启停，且状态不持久化。`GET /api/config` 的 `TunnelInfo` 与推送来源判定都读取静态 `config.TunnelConfig`。设置页的 `TunnelSettings.svelte` 只展示状态与配置文件模板。

因此以下需求无法在网页完成：填写或更换隧道凭据、切换 Quick/Named、修改 `hostname`、在 Linux/远程浏览器中启停、避免改配置后重启。持久化设置还牵动三处派生状态：`GET /api/config`、`pushOrigin` 的 Named 主机名，以及托盘的勾选状态。

## Goals / Non-Goals

### Goals

- 网页提供完整、可校验、可恢复的隧道配置与启停，不要求编辑 `config.toml` 或重启。
- 设置跨重启保留；首次保存前 TOML 行为完全不变。
- 凭据只写不读，服务端持久化、日志与响应均不泄露。
- 保存/启停不中断 TCP 服务、WebSocket 或任何 PTY 会话；旧 cloudflared 不能重新授权。
- 托盘保留快捷开关，但配置只在网页完成。

### Non-Goals

- 不实现 Cloudflare 账号授权、自动创建 Tunnel 或自动写入 Cloudflare DNS。
- 不把 `cloudflared`、`download_mirrors`、`allowed_origins`、根 `token` 等运维配置迁移到网页。
- 不改变 cloudflared 下载来源、校验与缓存实现。
- 不新增公开分享、二维码页面或隧道用量统计。
- 不把开关状态写入 `config.toml`，也不重写用户手写配置。

## Decisions

### 1. 独立页面而不是继续扩展现有卡片

新增 `/settings/tunnel`。设置页保留一张摘要卡（状态、模式、入口按钮），完整表单、状态、风险说明和操作放在独立页面。原因：Quick/Named 两种模式、凭据、`hostname`、启用确认、恢复默认和错误说明会让单张卡片过长，窄屏尤其明显；独立页面也可以被托盘直接打开。

页面沿用现有 `safe-area-page`、`app-header`、`Card`、`Tabs`、`AlertDialog`、`InlineNotice` 与 shadcn-svelte 组件，不引入新的对话框或菜单实现。移动端单列，桌面端最大宽度与设置页一致。

### 2. 设置存储与优先级

新增 Service 包 `internal/tunnelsettings`，负责 `<state_dir>/tunnel.json` 的读取、校验、原子写入、flock 与不可用原因。文件权限 0600，状态目录 0700，写入复用 `internal/statefile`，锁文件为 `tunnel-config.lock`。结构带 `v`，当前为 1：

```json
{
  "v": 1,
  "enabled": true,
  "mode": "named",
  "hostname": "term.example.com",
  "token": "eyJ..."
}
```

`mode` 为 `quick` 或 `named`；`quick` 必须清空 `token` 与 `hostname`，`named` 必须同时有凭据与合法 `hostname`。`cloudflared` 与 `download_mirrors` 仍来自 TOML，不进入该文件。

优先级：

- 文件存在时，`enabled`、`mode`、`token`、`hostname` 覆盖 `[tunnel]` 同名项；`cloudflared` 与 `download_mirrors` 继续取 TOML。
- 文件不存在时完全使用 TOML，不创建文件。
- “恢复配置文件设置”先成功删除文件，再停止当前隧道并回到 TOML 字段；删除失败不改变当前运行状态。当前进程保持关闭，网页明确提示“下次启动按配置文件决定是否开启”。

锁冲突、文件损坏、不可读或目录不可写时，存储报告不可用并保留原文件；当前进程 MUST 保持隧道关闭，即使 TOML 或显式 `--tunnel` 要求开启。只有确认文件不存在才回退到 TOML。HTTP 服务继续监听；`GET /api/tunnel` 返回 200、关闭状态与 `storageAvailable=false`/`storageError`，写接口返回 503。故障不能隐藏紧急关闭入口。

### 3. 浏览器 API 与凭据语义

新增四个端点，全部走现有 `/api/` Token、Host 与 Origin 校验：

| 端点                          | 作用                                        | 成功状态 |
| ----------------------------- | ------------------------------------------- | -------- |
| `GET /api/tunnel`             | 读取生效设置、来源与运行状态                | 200      |
| `PUT /api/tunnel/settings`    | 保存 `mode` / `hostname` / 可选新凭据并应用 | 200/202  |
| `PUT /api/tunnel/enabled`     | 持久化并应用开关                            | 202      |
| `DELETE /api/tunnel/settings` | 删除网页覆盖，恢复 TOML                     | 200      |

`GET` 只返回 `tokenConfigured`，不返回凭据或其片段。`PUT /settings` 的 `token` 是指针：字段缺省表示保留现有凭据，非空字符串表示替换；`named` 下空字符串、缺省且没有已存凭据、或非法 JSON 都返回 400。表单永远不回填明文，保存成功后清空输入并显示“已配置”。

请求体上限沿用 64 KiB，未知字段拒绝且 JSON 错误不回显提交字段值。错误映射：校验失败 400，存储不可用 503，开启前置条件不满足 409，其他未预期错误 500 且不包含凭据。线上类型放在 `internal/contract`，由 `internal/wire`/`api` 转换；前端 Zod schema 必填字段与后端一致。视图包含 `enabled`（当前进程期望）、`mode`、`hostname`、`tokenConfigured`、`source`（`web`/`config`）、`state`、可选 `publicUrl`/`error`、`canEnable`、可选 `enableBlockedReason`、`storageAvailable` 与可选 `storageError`。错误体为 `{error, notPersisted?}`。

`GET /api/config` 的 `TunnelInfo` 保持字段形状，但 `named` 与 `hostname` 改为来自生效设置；`enabled` 仍表示 `starting`/`ready` 的实际状态。

### 4. 运行时应用与 generation

`tunnelController` 从只读 `cfg config.TunnelConfig` 改为在互斥锁下保存当前生效设置。配置更新采用串行 apply：

1. 设置更新、开启与恢复默认先校验并写入存储；写入失败时不改变运行状态。关闭为安全例外：无论写入是否成功，都立即撤销公网来源、取消旧 generation 并进入 `off`。
2. 若当前期望关闭，只保存设置；若期望开启且配置改变，即使之前为 `failed` 也按新配置重新应用。关闭写入失败返回 503 与 `notPersisted=true`，明确说明当前进程已关闭但重启状态未持久化。
3. 若正在运行且模式、凭据或 `hostname` 变化：先撤销 `PublicOrigin`，把状态设为 `starting`，递增 generation，取消旧 generation 并等待其 worker 结束，再用新设置启动 cloudflared。请求不等待整个过程，立即返回。
4. 旧 generation 的 `ready`/`fail` 事件按 generation 丢弃，因此停止或切换后的旧进程不能重新授权公网来源。
5. 只改 `enabled` 时走现有 Stop/Start 路径；停止仍先撤销授权再结束进程。

“先等旧 worker 结束再启动新进程”避免同名 Named Tunnel 短暂出现两个 connector。apply 期间公网页面会短暂 403 或断线，本机与 VPN 访问、会话不受影响。

推送的 `PublicOrigin` 与 `pushOrigin` 直接在每次请求读取生效设置，不缓存旧主机名；修改 `hostname` 后新来源立即可用于登记推送。已有订阅保留其登记时的 origin，不会被静默改写，因此旧通知仍指向旧地址，页面提示用户重新订阅。

### 5. 启动装配：TOML 语义与网页语义分开

启动时先解析 TOML 与命令行参数，再打开设置存储、合并网页覆盖、应用进程级开关并校验生效配置：

- 没有持久化设置：保持现有行为。`c.Tunnel.Enabled` 为真时在监听前定位 cloudflared，失败即以非零状态退出；`ensureToken` 的 43 字符规则不变。
- 有持久化设置且 `enabled=true`：合并配置，但保留 `webManaged` 标记；`serve` 只对 TOML/`--tunnel` 的 `enabled` 做监听前定位，网页托管设置则在监听开始后按“运行时开启”路径启动。cloudflared 不可用、下载失败或根 Token 不足只把状态置为 `failed`，服务继续监听，用户可在网页修正。根 Token 为空时仍按现有隧道模式生成随机 Token；显式配置但不足 43 字符时不阻止启动，只阻止开启并返回原因。
- 有持久化设置且 `enabled=false`：即使 TOML `enabled=true` 也不启动。网页显示“由网页设置覆盖配置文件”。
- `--tunnel` / `--tunnel=false`：覆盖本次进程的开关，不写持久化设置；显式 `--tunnel` 仍使用监听前定位与失败退出的语义。网页或本机 API 的后续开关会覆盖本次进程的临时值。

这样避免“网页曾开启、重启后 cloudflared 下载失败导致 WebMux 无法启动”的锁死，同时不改变仅在 TOML 中启用隧道的现有部署。

### 6. 托盘与推送文案

托盘保留“公网隧道（Cloudflare）”勾选项，因为它是运行中快捷开关，不是配置。勾选状态读取服务端当前进程期望的 `enabled`（包括 `--tunnel`/`--tunnel=false`、`failed` 但仍期望开启、持久化失败后的紧急关闭），而不是只读持久化文件或 `starting`/`ready`。`TunnelStatus.enabled` 使用可选布尔值，旧服务端缺字段时回退实际状态。只有显式 `settingsAvailable=true` 时，才新增“打开公网访问设置…”菜单项进入 `/settings/tunnel`；旧服务端已经可能有 `tunnel` 字段，不能用其存在推断页面能力。推送说明同样按能力决定网页步骤或旧版配置说明。

删除托盘中隧道配置相关的“打开配置文件 / 初始化配置 / 修改后重启”指引；根 Token 不足时只说明原因并指向网页入口。推送说明与 Quick Tunnel 提示改为“设置 → 公网访问 → 选择 Named Tunnel → 填写凭据与 hostname → 保存并启用”，不再要求编辑 `[tunnel]` 或重启。

托盘的 `SetTunnel` 继续走 `/local/v1/tunnel`，服务端把它与网页开关写入同一持久化状态；两个入口并发时由控制器互斥串行，最后写入者生效。

### 7. 安全与确认

- 凭据只在 `PUT` 请求体中出现，绝不出现在 `GET`、日志、错误、URL、二维码、HTML 或前端持久化中；输入框为密码类型，可临时显示。
- `tunnel.json` 为 0600，目录 0700，原子替换并 fsync；锁冲突时另一个实例保持隧道关闭，而不是恢复 TOML 公网暴露。
- 所有新端点继承现有认证；未认证 401，Origin 不合法 403。会话内进程与普通浏览器一样，需要 WebMux Token，不新增旁路。
- 从关闭到开启必须确认公开 shell 的风险；如果当前页面来自公网来源，关闭隧道或修改 `hostname` 的确认还要说明“应用后当前公网页面会断开”，避免用户把自己锁在外面。
- 修改隧道设置等同于扩大 shell 暴露面，权限不高于现有根 Token；文档与页面明确写出这一点。

### 8. 测试与兼容

- `internal/tunnelsettings`：权限、原子写入、锁冲突、损坏文件、优先级、Quick/Named 校验、重置。
- `cmd/webmux`：持久化开关跨重启、启动软失败、`--tunnel` 不写状态、切换配置的 generation、停止/启动竞态、推送主机名更新。
- `internal/api`：四个端点的 200/202/400/401/403/409/503、凭据不泄露、未知字段、恢复默认；重新录制线上 JSON 快照。
- 前端：模式切换、凭据保留/替换、校验、确认框、来自公网来源的警告、503、恢复默认、窄屏可访问性。
- E2E 使用 fake cloudflared（与现有控制器测试相同的方式）验证保存、启用、切换和关闭，不依赖真实 Cloudflare 网络。
- 新增 Service 包需要更新 `internal/archtest/layers_test.go` 与 `docs/architecture.md`；UI 变更的 PR 附桌面/手机、亮/暗截图。

## Risks / Trade-offs

- [网页开启后重启会自动暴露公网] → 开关持久化是明确的产品选择；启用前确认、页面持续显示状态，关闭会持久化。仍无法阻止拥有根 Token 的人开启，这与拥有 shell 等价。
- [持久化自动开启遇到 cloudflared 缺失时会“看起来失败”] → 监听先启动、失败可在网页修正；这是有意区别于 TOML `enabled` 的启动失败语义。
- [`--tunnel` 与持久化开关同时存在时语义容易混淆] → 文档与页面说明 `--tunnel` 只覆盖本次进程；任何网页/本机开关都会改写持久化状态。
- [修改 `hostname` 会让当前公网页面与旧推送订阅失效] → 应用前确认，保留订阅不静默改写；文档提示重新订阅。
- [网页优先级让运维的 TOML 修改暂时不生效] → 页面显示“来源：网页设置”并提供“恢复配置文件设置”；删除文件即可回到 TOML。
- [凭据持久化在磁盘上] → 0600、目录 0700、原子替换；不进入 `config.toml` 与日志。拥有服务用户读权限的人本来就能读配置与进程环境。
- [新旧入口并发开关] → 控制器互斥串行，generation 丢弃旧事件；本地 API 与网页最终一致。
- [本机 API 从“临时开关”改为持久化] → 这是本变更的显式行为变化；旧客户端仍调用同一端点，无需改协议。

## Migration Plan

- 纯增量：首次运行没有 `tunnel.json` 时行为不变；第一次网页保存才创建文件。
- 不迁移或改写 `config.toml`。运维仍可通过 TOML 提供默认值；网页保存后以网页设置为准。
- 回滚到旧版本时，旧版本忽略 `tunnel.json`，继续按 TOML 与 `--tunnel` 启动；如需同步，删除新文件或在旧版本中修改 TOML。
- 文档、协议、用户故事与配置示例随实现同一 PR 更新；线上快照用 `go test ./internal/api -run TestWireSnapshots -update` 重新录制。
