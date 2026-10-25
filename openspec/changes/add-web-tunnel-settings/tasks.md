## 1. 契约与设置存储

- [ ] 1.1 `internal/config` 暴露可复用的隧道校验（`mode`、`hostname`、Named 必填项），保持 `Config.Validate` 与 `webmux config validate` 的现有错误文本和顺序
- [ ] 1.2 新增 `internal/tunnelsettings` Service：`Open(stateDir, defaults)`、`Effective()`、`Save()`、`Reset()`、`Unavailable()`；实现 `<state_dir>/tunnel.json`（0600、`v=1`、原子替换）与 `tunnel-config.lock` flock，损坏或锁冲突时不覆盖原文件
- [ ] 1.3 在 `internal/archtest/layers_test.go` 把 `internal/tunnelsettings` 归为 Service，并更新 `docs/architecture.md` 的包表与装配说明
- [ ] 1.4 `internal/contract` 增加 `TunnelSettings`、`TunnelSettingsUpdate`、`TunnelSettingsError` 等线上类型；`mode` 取值、`token` 指针语义、`source`、`canEnable` 与 `enableBlockedReason` 写清楚；为托盘与本地状态补充持久化开关字段（如 `TunnelStatus.enabled`），旧服务端缺字段时客户端回退到实际运行状态；未知字段仍由现有 `decode` 拒绝
- [ ] 1.5 定义存储不可用、校验失败、开启前置条件不满足的哨兵错误，供 `internal/api/errors.go` 映射到 503/400/409；确保错误文本不包含凭据

## 2. 控制器与启动装配

- [ ] 2.1 改造 `cmd/webmux/tunnelController`：在互斥锁下保存当前生效设置；新增 `Settings()`、`UpdateSettings()`、`ResetSettings()`，与现有 `SetEnabled()` 共用同一 apply 路径
- [ ] 2.2 实现串行 apply：撤销公网来源、递增 generation、取消并等待旧 worker、按新设置启动；请求立即返回 `starting`；旧 generation 的 `ready`/`fail` 事件不得重新授权
- [ ] 2.3 `SetEnabled` 与 `UpdateSettings` 先持久化再应用；写入失败时运行状态不变；关闭先持久化 `enabled=false`，再撤权并结束 cloudflared
- [ ] 2.4 `cmd/webmux` 启动时打开 `tunnelsettings` 并与 TOML 合并：无文件保持 TOML 语义；网页 `enabled=true` 保留 `webManaged` 标记，在监听后按运行时路径启动，跳过监听前的 cloudflared 定位；网页 `enabled=false` 覆盖 TOML；`--tunnel` 只覆盖本次进程且不写状态
- [ ] 2.5 调整 `ensureToken`：网页托管且根 Token 为空时仍生成；显式短 Token 不阻止启动，只阻止隧道开启并给出原因；TOML/`--tunnel` 的启动失败语义保持不变
- [ ] 2.6 注入动态设置读取：`GET /api/config` 的 `named`/`hostname`、`pushOrigin` 与 `localStatus` 的 `PublicOrigin` 使用当前生效设置；修改 `hostname` 后推送来源立即更新
- [ ] 2.7 关闭路径释放 `tunnelsettings` 锁并从 `serverRun`/`serve` 中正确清理；启动存储不可用时记录警告并回退 TOML，不阻止服务

## 3. 浏览器 API

- [ ] 3.1 新增 `internal/api/tunnel.go`：`GET /api/tunnel`、`PUT /api/tunnel/settings`、`PUT /api/tunnel/enabled`、`DELETE /api/tunnel/settings`，全部走现有 Token、Host 与 Origin 中间件
- [ ] 3.2 `GET` 只返回 `tokenConfigured`；`PUT /settings` 支持缺省 `token` 保留现有凭据、非空替换；`named` 下空/缺失凭据返回 400；`quick` 清除凭据与 `hostname`
- [ ] 3.3 状态码与错误体：校验 400、未认证 401、Origin 403、前置条件 409、存储不可用 503、成功 200/202；`DELETE` 返回恢复后的设置视图
- [ ] 3.4 `api.Deps` 增加隧道设置控制接口，由 `cmd/webmux` 注入；`api` 只通过接口调用，不直接读写状态文件；`GET /api/config` 的字段形状保持不变
- [ ] 3.5 重新录制 `internal/api/testdata/wire` 中受影响的 `/api/config`、`/local/v1/status` 与新增端点快照，并补充 400/401/403/409/503 场景

## 4. 前端页面

- [ ] 4.1 `web/src/schemas.ts` 增加隧道设置视图、更新请求与错误 schema；`web/src/api.ts` 复用现有 `request()`、Bearer、`APIError`、401 事件与 `AbortSignal`，不新增请求约定
- [ ] 4.2 `web/src/App.svelte` 增加 `/settings/tunnel` 路由与懒加载；`web/src/pages/Settings.svelte` 把现有只读隧道卡片改为摘要（状态、模式、入口按钮），并更新顶部说明中“修改服务端配置需要重启”的描述
- [ ] 4.3 新增 `web/src/pages/TunnelSettings.svelte` 与必要子组件：状态徽章、公网地址复制、Quick/Named 表单、只写凭据输入、保存并应用、启用/关闭、恢复配置文件设置、错误与加载状态
- [ ] 4.4 校验与确认：Named 必填凭据与 `hostname`；非法主机名就地提示；从关闭到开启弹出公开 shell 风险确认；当前页面来自公网来源时，关闭或修改 `hostname` 的确认说明当前页面会断开
- [ ] 4.5 来源与前置条件：显示“网页设置 / 配置文件”；根 Token 不足时禁用启用并说明需要至少 43 字符的 WebMux 登录 Token；存储 503 时禁用表单并显示原因
- [ ] 4.6 可访问性与响应式：移动端单列、键盘可操作、密码框可显示/隐藏、图标有可访问名称与 tooltip；亮/暗主题下状态与警告可读
- [ ] 4.7 浏览器测试覆盖模式切换、凭据保留/替换、校验、确认取消、公网来源警告、503、恢复默认与焦点顺序

## 5. 托盘与推送文案

- [ ] 5.1 `internal/tray/menuview.go`：勾选状态读取持久化 `enabled`（`failed` 但希望开启时仍勾选）；新增“打开公网访问设置…”；删除隧道配置/初始化/重启指引
- [ ] 5.2 `internal/tray/winui/render.go` 与相关测试同步菜单项、状态文案和旧服务端降级；确认菜单不含凭据
- [ ] 5.3 `/local/v1/tunnel` 的 `SetEnabled` 与网页开关共用持久化状态；本地状态与托盘读取 `TunnelStatus.enabled`（旧服务端缺失时回退到 `starting`/`ready`），`internal/tray` 的 `SetTunnel` 调用保持兼容，失败原因与 409 映射不变
- [ ] 5.4 更新 `windows-tray-push-guidance` 与 `push-notification-client` 的前端文案：改为“设置 → 公网访问”配置 Named Tunnel，不再要求编辑 `[tunnel]` 或重启

## 6. 测试与验证

- [ ] 6.1 `internal/tunnelsettings` 单元测试：Quick/Named 校验、缺省 token 保留、空 token 拒绝、hostname 规则、0600/0700、原子替换、锁冲突、损坏文件、重置与 TOML 优先级
- [ ] 6.2 `cmd/webmux` 测试：网页设置跨重启恢复、网页 `enabled=false` 覆盖 TOML、`--tunnel` 不写状态、短根 Token 软失败、cloudflared 不可用不阻止启动、切换配置的 generation 与旧进程事件
- [ ] 6.3 `internal/api/tunnel_test.go`：四个端点的鉴权、Origin、状态码、凭据不泄露、请求字段校验、`DELETE` 恢复默认、并发保存
- [ ] 6.4 回归 `GET /api/config`、推送来源判定、托盘菜单与线上快照；确认 `hostname` 运行时变化后推送资格与 `PublicOrigin` 同步
- [ ] 6.5 E2E 使用 fake cloudflared 覆盖保存、启用、切换 Quick/Named、关闭与重启恢复；不连接真实 Cloudflare，不影响已有会话
- [ ] 6.6 运行 `go test -race ./internal/tunnelsettings ./internal/api ./cmd/webmux`、目标 Vitest/浏览器测试、`make check` 与 `pnpm --dir web check:arch`

## 7. 文档、用户故事与验收

- [ ] 7.1 `docs/cloudflare-tunnel.md`：把配置步骤改为网页表单，说明持久化优先级、`--tunnel`、恢复配置文件、切换 `hostname` 的断线与重新订阅影响；TOML 作为无网页/运维方式保留
- [ ] 7.2 `docs/protocol.md`：新增 `/api/tunnel` 系列端点、请求/响应字段、状态码、凭据只写语义与 `/local/v1/tunnel` 的持久化变化
- [ ] 7.3 `docs/usage.md`、`docs/deployment.md`、`deploy/config.example.toml`：更新设置页入口、Windows 托盘说明、`[tunnel]` 注释与配置文件/网页优先级
- [ ] 7.4 `SECURITY.md`：说明网页隧道设置等同扩大 shell 暴露面、凭据存储位置与权限、根 Token 要求
- [ ] 7.5 `docs/user-stories.md`：新增/更新网页配置、运行时应用、启动恢复、托盘入口与推送引导故事；同步索引、来源链接与核对记录
- [ ] 7.6 `openspec validate add-web-tunnel-settings --strict --no-interactive` 通过；UI 变更的 PR 附桌面/手机与亮/暗主题截图
