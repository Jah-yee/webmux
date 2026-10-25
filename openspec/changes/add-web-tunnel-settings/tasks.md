## 1. 契约与设置存储

- [x] 1.1 `internal/config` 暴露可复用的隧道校验（`mode`、`hostname`、Named 必填项），保持 `Config.Validate` 与 `webmux config validate` 的现有错误文本和顺序
- [x] 1.2 新增 `internal/tunnelsettings` Service：`Open(stateDir, defaults)`、`Effective()`、`Save()`、`Reset()`、`Unavailable()`；实现 `<state_dir>/tunnel.json`（0600、`v=1`、原子替换）与 `tunnel-config.lock` flock，损坏或锁冲突时不覆盖原文件
- [x] 1.3 在 `internal/archtest/layers_test.go` 把 `internal/tunnelsettings` 归为 Service，并更新 `docs/architecture.md` 的包表与装配说明
- [x] 1.4 `internal/contract` 增加 `TunnelSettings`、`TunnelSettingsUpdate`、`TunnelSettingsError` 等线上类型；写清 `mode`、凭据省略语义、来源、启用前置条件与存储可用性；`TunnelStatus.enabled` 为可选的当前进程期望开关（含命令行覆盖），`settingsAvailable` 显式声明网页能力，旧服务端回退实际状态；未知字段拒绝且不泄露凭据
- [x] 1.5 定义存储不可用、校验失败、开启前置条件不满足与关闭未持久化的哨兵错误，供 `internal/api/errors.go` 映射到 503/400/409；确保错误文本不包含凭据

## 2. 控制器与启动装配

- [x] 2.1 改造 `cmd/webmux/tunnelController`：在互斥锁下保存当前生效设置；新增 `Settings()`、`UpdateSettings()`、`ResetSettings()`，与现有 `SetEnabled()` 共用同一 apply 路径
- [x] 2.2 实现串行 apply：撤销公网来源、递增 generation、取消并等待旧 worker、按新设置启动；请求立即返回 `starting`；旧 generation 的 `ready`/`fail` 事件不得重新授权
- [x] 2.3 开启、更新与恢复默认先持久化再应用，失败不改运行状态；关闭无条件撤权、取消旧 worker 并设为 `off`，持久化失败返回 503 与 `notPersisted=true`，读接口仍可观察已关闭
- [x] 2.4 `cmd/webmux` 启动时打开 `tunnelsettings` 并与 TOML 合并：无文件保持 TOML 语义；网页 `enabled=true` 保留 `webManaged` 标记，在监听后按运行时路径启动，跳过监听前的 cloudflared 定位；网页 `enabled=false` 覆盖 TOML；`--tunnel` 只覆盖本次进程且不写状态
- [x] 2.5 调整 `ensureToken`：网页托管且根 Token 为空时仍生成；显式短 Token 不阻止启动，只阻止隧道开启并给出原因；TOML/`--tunnel` 的启动失败语义保持不变
- [x] 2.6 注入动态设置读取：`GET /api/config` 的 `named`/`hostname`、`pushOrigin` 与 `localStatus` 的 `PublicOrigin` 使用当前生效设置；修改 `hostname` 后推送来源立即更新
- [x] 2.7 关闭路径释放 `tunnelsettings` 锁并从 `serverRun`/`serve` 中正确清理；存储损坏、不可读或锁失败时记录警告并保持隧道关闭，HTTP 继续运行，只有确认缺失才使用 TOML

## 3. 浏览器 API

- [x] 3.1 新增 `internal/api/tunnel.go`：`GET /api/tunnel`、`PUT /api/tunnel/settings`、`PUT /api/tunnel/enabled`、`DELETE /api/tunnel/settings`，全部走现有 Token、Host 与 Origin 中间件，本机套接字仍拒绝会话内进程绕过隧道控制限制
- [x] 3.2 `GET` 只返回 `tokenConfigured`；`PUT /settings` 支持缺省 `token` 保留现有凭据、非空替换；`named` 下空/缺失凭据返回 400；`quick` 清除凭据与 `hostname`
- [x] 3.3 状态码与错误体：校验 400、未认证 401、Origin 403、前置条件 409、存储写入不可用 503；读状态包括故障仍为 200；成功 200/202；`DELETE` 返回恢复后的设置视图；关闭失败明确 `notPersisted`
- [x] 3.4 `api.Deps` 增加隧道设置控制接口，由 `cmd/webmux` 注入；`api` 只通过接口调用，不直接读写状态文件；`GET /api/config` 的字段形状保持不变
- [x] 3.5 重新录制 `internal/api/testdata/wire` 中受影响的 `/api/config`、`/local/v1/status` 与新增端点快照，并补充 400/401/403/409/503 场景

## 4. 前端页面

- [x] 4.1 `web/src/schemas.ts` 增加隧道设置视图、更新请求与错误 schema；`web/src/api.ts` 复用现有 Bearer、`APIError`、401 事件与 `AbortSignal`，不新增请求约定
- [x] 4.2 `web/src/App.svelte` 增加 `/settings/tunnel` 懒加载路由；设置页只读卡片改为状态摘要与入口，更新重启说明
- [x] 4.3 新增 `TunnelSettingsPage.svelte`、`TunnelManager.svelte`、`TunnelConfiguration.svelte`：状态、公网地址复制、Quick/Named、只写凭据、保存应用、开关、恢复默认、错误与加载
- [x] 4.4 Named 凭据与主机名校验；开启风险确认；公网来源关闭或修改配置前断线确认
- [x] 4.5 显示来源与根 Token 前置条件；存储故障禁用保存/开启但保留紧急关闭，未持久化关闭可重试保存
- [x] 4.6 移动单列、键盘操作、凭据显示/隐藏、可访问名称与 tooltip；亮/暗主题截图与溢出检查
- [x] 4.7 浏览器测试覆盖模式、凭据保留、校验、确认取消、公网警告、503、恢复默认；真实浏览器验收主机名→凭据→显示按钮的 Tab 顺序与键盘切换

## 5. 托盘与推送文案

- [x] 5.1 `internal/tray/menuview.go`：勾选状态读取当前进程期望 `enabled`（包括命令行覆盖与失败状态）；以显式 `settingsAvailable` 能力门控“打开公网访问设置…”；新服务端不显示隧道配置/初始化/重启指引
- [x] 5.2 `internal/tray/winui/render.go` 与相关测试同步菜单项、状态文案和旧服务端降级；确认菜单不含凭据
- [x] 5.3 `/local/v1/tunnel` 的 `SetEnabled` 与网页开关共用持久化状态；本地状态与托盘读取 `TunnelStatus.enabled`（旧服务端缺失时回退到 `starting`/`ready`），`internal/tray` 的 `SetTunnel` 调用保持兼容，前置条件仍为 409，存储失败与关闭未持久化为 503
- [x] 5.4 推送前端说明改为“设置 → 公网访问”配置 Named Tunnel，不再要求编辑 `[tunnel]` 或重启

## 6. 测试与验证

- [x] 6.1 `internal/tunnelsettings` 单元测试：Quick/Named 校验、缺省 token 保留、空 token 拒绝、hostname 规则、0600/0700、原子替换、锁冲突、损坏文件、重置与 TOML 优先级
- [x] 6.2 `cmd/webmux` 测试：网页设置跨重启恢复、网页 `enabled=false` 覆盖 TOML、`--tunnel` 不写状态、短根 Token 软失败、cloudflared 不可用不阻止启动、切换配置的 generation 与旧进程事件、故障启动保持关闭、关闭写失败仍撤权
- [x] 6.3 `internal/api/tunnel_test.go`：四个端点的鉴权、Origin、状态码、凭据不泄露、请求字段校验、`DELETE` 恢复默认、并发保存，以及本机会话进程不能绕过控制限制
- [x] 6.4 回归 `GET /api/config`、推送来源判定、托盘菜单与线上快照；确认 `hostname` 运行时变化后推送资格与 `PublicOrigin` 同步
- [x] 6.5 E2E 使用 fake cloudflared 覆盖保存、启用、切换 Quick/Named、关闭与重启恢复；不连接真实 Cloudflare，不影响已有会话
- [x] 6.6 已运行全量 Go race、单元/目标浏览器/E2E、`make check` 与前端架构守卫。构建、格式、前端检查与 go vet 通过；`make check` 在全库 golangci-lint 既存问题处失败，本次新增告警单独修复并验证，不扩展为无关全库清理

## 7. 文档、用户故事与验收

- [x] 7.1 `docs/cloudflare-tunnel.md`：网页配置、持久化优先级、命令行、恢复默认、主机名断线与重新订阅说明，保留 TOML 运维方式
- [x] 7.2 `docs/protocol.md`：新增 `/api/tunnel` 系列端点、请求/响应字段、状态码、凭据只写语义与 `/local/v1/tunnel` 的持久化变化
- [x] 7.3 `docs/usage.md`、`docs/deployment.md`、`deploy/config.example.toml`：设置入口、Windows 托盘、配置优先级
- [x] 7.4 `SECURITY.md`：公网 shell 风险、凭据权限与根 Token 要求
- [x] 7.5 `docs/user-stories.md`：更新网页配置、运行时应用、启动恢复、托盘入口与推送引导；133 个故事 ID 唯一、索引引用与 143 条来源路径检查通过
- [x] 7.6 严格 OpenSpec 校验通过；隔离样例桌面/手机、亮/暗前后截图位于 `docs/images/terminal-find-tunnel/`。本次仅本地实现，未创建实现 PR，发布时须在 PR 描述嵌入可访问图片链接
