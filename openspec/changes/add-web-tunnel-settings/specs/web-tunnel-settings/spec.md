## Purpose

在 WebMux 网页设置中提供完整的 Cloudflare Tunnel 配置、启停和状态管理，并把设置持久化到服务端，使 Linux、macOS、Windows 与远程用户无需编辑配置文件或重启服务即可获得公网入口。

## ADDED Requirements

### Requirement: 网页公网访问设置页

系统 SHALL 提供 `/settings/tunnel` 页面，并从设置页的“公网访问（Cloudflare Tunnel）”摘要卡提供入口。页面 SHALL 展示当前生效模式（Quick 或 Named）、当前进程期望开关（含命令行覆盖与紧急关闭）、运行状态（`off`、`starting`、`ready`、`failed`）、公共主机名、凭据是否已配置、当前设置的来源（网页设置或配置文件）、公网地址与失败原因，并 SHALL 提供保存并应用、启用、关闭、恢复配置文件设置、复制访问链接和打开配置指南的操作。

页面 MUST NOT 展示、回填或请求隧道凭据的明文；凭据输入框在保存成功后 SHALL 清空并显示“已配置”。页面 SHALL 在加载、保存、应用与失败时给出可区分状态，并在窄屏、桌面、亮色与暗色主题下保持可用和可访问。

#### Scenario: 从设置页进入

- **WHEN** 用户打开设置页并点击“管理公网访问”
- **THEN** 浏览器进入 `/settings/tunnel`，显示当前模式、开关、运行状态与公共主机名

#### Scenario: 已配置 Named Tunnel

- **WHEN** 网页设置中已保存 Named 凭据与 `hostname = "term.example.com"`，隧道处于 `ready`
- **THEN** 页面显示 Named Tunnel、`term.example.com`、已就绪与公网地址，凭据输入框为空并提示“已配置”

#### Scenario: 不显示凭据

- **WHEN** 页面加载、保存或刷新
- **THEN** 页面 DOM、网络响应与错误提示中都不包含隧道凭据的值或可还原片段

#### Scenario: 设置页摘要

- **WHEN** 用户打开 `/settings` 且隧道未启用
- **THEN** 摘要卡显示“未启用”与“管理公网访问”，不展示 Quick/Named 的完整表单或配置文件模板

### Requirement: 隧道配置字段与校验

系统 SHALL 支持 `mode` 为 `quick` 或 `named`。`quick` MUST 清空隧道凭据与 `hostname`；`named` MUST 同时具有非空隧道凭据与合法 `hostname`。`hostname` MUST 是纯 ASCII/punycode 主机名，不能包含协议、端口、路径、查询参数、片段、凭据、通配符或空白。

更新请求中的隧道凭据 SHALL 为可选字段：缺省表示保留现有凭据，非空字符串表示替换；`named` 下空字符串、缺省且没有已存凭据 MUST 被拒绝。系统 SHALL 在校验全部通过后才写入设置，不得部分保存。字段错误 SHALL 指明具体字段且不包含凭据值。

#### Scenario: Named 缺少凭据

- **WHEN** 用户选择 Named 且没有已保存凭据，也未填写新凭据
- **THEN** 页面就地提示需要隧道凭据，保存请求被拒绝，服务端设置保持不变

#### Scenario: Named 缺少主机名

- **WHEN** 用户选择 Named 并填写凭据，但 `hostname` 为空
- **THEN** 页面提示需要公共主机名，保存请求被拒绝

#### Scenario: 非法主机名

- **WHEN** 用户填写 `https://term.example.com/path` 或 `*.example.com`
- **THEN** 页面与服务端都拒绝该值，提示只填写 Cloudflare 转发的纯主机名

#### Scenario: 切换到 Quick

- **WHEN** 当前为 Named 且已保存凭据，用户切换到 Quick 并保存
- **THEN** 生效设置变为 Quick，持久化文件中的凭据与 `hostname` 被清空，响应只报告凭据未配置

#### Scenario: 保留已配置凭据

- **WHEN** 当前 Named 已配置凭据，用户只修改 `hostname` 并保存
- **THEN** 现有凭据保持不变，隧道按新 `hostname` 重启

### Requirement: 设置持久化与配置文件优先级

系统 SHALL 把网页保存的隧道设置持久化到 `<state_dir>/tunnel.json`，目录权限 MUST 为 0700，文件权限 MUST 为 0600，写入 MUST 原子替换并 fsync。文件 SHALL 包含版本、`enabled`、`mode`、`hostname` 与可选凭据，并使用独立 `tunnel-config.lock` flock 阻止同一状态目录的第二个实例写入。

文件存在时，其 `enabled`、`mode`、`hostname` 与凭据 SHALL 覆盖 TOML `[tunnel]` 的同名项；`cloudflared` 与 `download_mirrors` SHALL 继续来自 TOML。文件不存在时，系统 MUST 完全沿用 TOML 行为且不创建文件。系统 MUST NOT 改写 `config.toml`。

“恢复配置文件设置” SHALL 删除持久化文件、停止当前隧道并回到 TOML 设置；页面 SHALL 说明当前进程保持关闭、下次启动按配置文件决定是否自动开启。文件损坏、不可读或被锁定时，系统 SHALL 报告存储不可用、保留原文件、保持隧道关闭（即使 TOML 或 `--tunnel` 要求开启），同时保持 HTTP 服务可用。只有确认文件不存在才能使用 TOML 开关。读接口 SHALL 返回 200 与关闭状态、`storageAvailable=false` 及 `storageError`；写接口 SHALL 返回 503。

#### Scenario: 首次保存创建文件

- **WHEN** 状态目录中没有 `tunnel.json`，用户在网页保存 Named 设置
- **THEN** 系统创建 0600 的 `tunnel.json`，之后重启仍使用该设置

#### Scenario: 网页覆盖配置文件

- **WHEN** `config.toml` 中 `[tunnel].enabled = true` 且 `hostname = "old.example.com"`，网页已保存 `enabled = false` 与 `hostname = "new.example.com"`
- **THEN** 启动后不开启隧道，页面显示 `new.example.com` 与“来源：网页设置”

#### Scenario: 恢复配置文件设置

- **WHEN** 用户确认“恢复配置文件设置”
- **THEN** `tunnel.json` 被删除，当前隧道停止，页面显示 TOML 中的模式、主机名与开关来源

#### Scenario: 损坏的设置文件

- **WHEN** `tunnel.json` 不是合法 JSON
- **THEN** WebMux 正常启动并保持隧道关闭，读接口报告存储不可用，写接口返回 503，原文件不被覆盖

#### Scenario: 第二个实例

- **WHEN** 另一个 WebMux 实例持有同一 `state_dir` 的 `tunnel-config.lock`
- **THEN** 本实例正常启动但保持隧道关闭，读接口报告存储被占用，写接口返回 503

### Requirement: 隧道设置接口

系统 SHALL 在 `/api/` 下提供以下端点，并复用现有 Token、Host 与 Origin 校验：

- `GET /api/tunnel`：返回生效设置、来源、`tokenConfigured`、运行状态、公网地址、错误、`canEnable`、不可启用原因与 `storageAvailable`/`storageError`，存储故障也可读取；
- `PUT /api/tunnel/settings`：保存 `mode`、`hostname` 与可选新凭据，并在已启用时应用；
- `PUT /api/tunnel/enabled`：保存并应用 `enabled`；
- `DELETE /api/tunnel/settings`：删除网页覆盖并恢复 TOML。

`GET` MUST NOT 返回凭据。请求体上限为 64 KiB，未知字段 MUST 被拒绝。校验失败返回 400，未认证返回 401，Origin 不合法返回 403，开启前置条件不满足返回 409，存储不可用返回 503。保存设置导致运行中隧道重启时返回 202 与新的设置视图；无需重启时返回 200。删除成功返回 200 与恢复后的视图。

#### Scenario: 读取设置

- **WHEN** 已认证用户请求 `GET /api/tunnel`
- **THEN** 响应包含模式、来源、`tokenConfigured`、状态与 `canEnable`，不包含隧道凭据或 WebMux 登录 Token

#### Scenario: 保存并应用

- **WHEN** 隧道 `ready`，用户通过 `PUT /api/tunnel/settings` 修改 `hostname`
- **THEN** 返回 202 与 `starting` 视图，旧公网来源被撤销，新主机名就绪后可用

#### Scenario: 开启前置条件不满足

- **WHEN** WebMux 登录 Token 少于 43 个字符，用户请求 `PUT /api/tunnel/enabled` 开启隧道
- **THEN** 返回 409 与可执行的原因，`enabled` 与运行状态不变

#### Scenario: 存储不可用

- **WHEN** 设置文件被损坏或被另一个实例锁定，用户请求任一写接口
- **THEN** 返回 503 且原文件不变；设置更新、开启与恢复默认不改变运行状态，但关闭请求仍立即撤权、停止旧 worker 并报告 `off` 和 `notPersisted=true`

#### Scenario: 未认证与非法 Origin

- **WHEN** 请求未携带有效 Token，或写请求的 Origin 不属于本服务
- **THEN** 分别返回 401 与 403，不读取或写入设置

#### Scenario: 未知字段

- **WHEN** `PUT /api/tunnel/settings` 的请求体包含未定义字段
- **THEN** 返回 400，设置不变

### Requirement: 运行时应用与状态一致性

保存设置或切换开关 SHALL 立即应用，不重启 WebMux、不中断 TCP 服务、本机 API、WebSocket 或任何 PTY 会话。运行中修改模式、凭据或 `hostname` 时，系统 SHALL 先撤销旧公网来源，再结束旧 generation 的 cloudflared，并按新设置启动；请求立即返回，状态经 `starting` 到 `ready`，失败时变为 `failed` 且不自动重试。旧 generation 的迟到事件 MUST NOT 重新授权公网来源。

关闭 SHALL 尝试持久化 `enabled=false`，并且不论持久化结果都立即撤销公网授权后结束 cloudflared，状态变为 `off`。持久化失败 SHALL 返回 503、`notPersisted=true` 与明确错误，之后读接口仍能看到当前进程已关闭，并说明重启状态未保存。开启失败时持久化开关 SHALL 保持用户选择的开启状态，页面显示原因并允许重试或关闭。`GET /api/config` 的 `named`/`hostname`、推送来源判定与托盘状态 SHALL 读取当前生效设置；修改 `hostname` 后旧推送订阅 SHALL 保留原 origin，页面 SHALL 提示重新订阅。

如果当前页面来自即将失效的公网来源，关闭隧道或修改 `hostname` 前 SHALL 弹出确认，说明应用后当前公网页面会断开。

#### Scenario: 修改主机名

- **WHEN** 隧道在 `https://old.example.com` 就绪，用户把 `hostname` 改为 `new.example.com` 并确认
- **THEN** 旧公网来源返回 403，`new.example.com` 最终变为 `ready`，现有终端会话不中断

#### Scenario: 关闭隧道

- **WHEN** 隧道 `ready`，用户关闭公网访问
- **THEN** 持久化开关变为关闭，公网来源立即失效，cloudflared 结束，本机与 VPN 访问正常

#### Scenario: 关闭时持久化失败

- **WHEN** 隧道已就绪但设置文件不能写入，用户请求关闭
- **THEN** 公网来源立即失效，旧 generation 被取消，当前状态为 `off`，响应为 503 且 `notPersisted=true`；之后 `GET /api/tunnel` 仍为 200，并可读到关闭状态与存储错误

#### Scenario: 隧道启动失败

- **WHEN** 新设置下 cloudflared 无法定位或启动失败
- **THEN** 状态变为 `failed` 并显示原因，持久化开关保持开启，用户可以在页面重试或关闭

#### Scenario: 旧进程迟到事件

- **WHEN** 切换配置后旧 cloudflared 才报告就绪或退出
- **THEN** 旧事件被 generation 丢弃，不会重新授权旧公网来源

#### Scenario: 公网页面上的危险操作

- **WHEN** 用户从 `https://term.example.com` 打开设置页并准备关闭隧道
- **THEN** 确认框说明当前公网页面会断开、本机或 VPN 访问仍可用，取消时不发出请求

### Requirement: 启动恢复与登录 Token 前置条件

持久化设置存在且 `enabled=true` 时，WebMux SHALL 在开始监听后按运行时开启路径恢复隧道。cloudflared 不可用、下载失败、启动失败或 WebMux 登录 Token 不足时，系统 SHALL 只把隧道状态置为 `failed` 并记录原因，MUST NOT 阻止 HTTP 服务启动或影响会话。根 Token 为空时 SHALL 沿用隧道模式的随机 Token 生成规则；显式配置但少于 43 个字符时 MUST NOT 开启隧道，并向网页返回可执行的前置条件说明。

持久化设置存在且 `enabled=false` 时，即使 TOML `[tunnel].enabled = true` MUST NOT 自动开启。没有持久化设置时，TOML `enabled` 的启动前定位、失败退出与 Token 校验行为 SHALL 保持不变。`--tunnel` / `--tunnel=false` 只覆盖本次进程，MUST NOT 写入持久化设置。

#### Scenario: 重启恢复

- **WHEN** 网页开启 Named Tunnel 后 WebMux 重启，设置文件仍为 `enabled=true`
- **THEN** 服务开始监听后隧道进入 `starting`，最终 `ready` 或带原因 `failed`，会话创建不受影响

#### Scenario: cloudflared 不可用

- **WHEN** 网页曾开启隧道，重启后 cloudflared 无法下载且 PATH 中不存在
- **THEN** WebMux 继续监听，隧道显示 `failed` 与原因，用户可在网页修正或关闭

#### Scenario: 登录 Token 过短

- **WHEN** 持久化设置为开启，但显式配置的 WebMux 登录 Token 只有 5 个字符
- **THEN** WebMux 继续监听，隧道不开启并报告登录 Token 不足，网页提供修复说明

#### Scenario: TOML 启动失败保持

- **WHEN** 没有持久化设置，TOML `[tunnel].enabled = true` 且 `tunnel.cloudflared` 指向不存在的文件
- **THEN** 系统仍以非零状态退出，不开始监听

#### Scenario: 一次性覆盖

- **WHEN** 持久化设置为关闭，用户以 `--tunnel` 启动
- **THEN** 本次进程开启隧道但不改写 `tunnel.json`，下次启动仍按持久化设置

### Requirement: 凭据与公开访问安全

隧道凭据 MUST 只出现在经过认证的写请求体与服务端 0600 状态文件中，MUST NOT 出现在 `GET` 响应、日志、错误文本、URL、二维码、页面 HTML、浏览器持久化或托盘菜单中。凭据输入框 SHALL 为密码类型并允许用户临时显示。

从关闭到开启公网访问前，网页 SHALL 要求用户确认公开 shell 的风险，说明任何获得链接和 Token 的人都能以服务用户身份执行命令。开启、关闭与修改设置 SHALL 与现有 API 一样需要有效 WebMux Token，MUST NOT 新增绕过认证或 Origin 检查的入口。设置页 SHALL 明确说明修改隧道设置等同于扩大 shell 暴露面。

#### Scenario: 响应与日志

- **WHEN** 用户保存 Named 凭据并随后触发成功、校验失败与 cloudflared 失败
- **THEN** `GET /api/tunnel`、错误响应与服务日志都不包含凭据值

#### Scenario: 开启确认

- **WHEN** 用户从关闭状态点击启用
- **THEN** 页面先显示公开 shell 风险确认，取消时不发出启用请求

#### Scenario: 状态文件权限

- **WHEN** 首次保存设置后检查状态目录
- **THEN** `tunnel.json` 权限为 0600，目录为 0700，锁文件为 0600
