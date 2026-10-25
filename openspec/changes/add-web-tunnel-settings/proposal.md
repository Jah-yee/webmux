## Why

#123 要求把 Cloudflare Tunnel 的配置和启停从配置文件与 Windows 托盘迁移到网页。当前实现里，`GET /api/config` 只返回只读的 `TunnelInfo`，设置页的“公网访问（Cloudflare Tunnel）”卡片只能解释如何编辑 `config.toml`；运行中开关只存在于本机套接字 API `/local/v1/tunnel` 和托盘菜单，浏览器无法修改 `enabled`、隧道凭据或 `hostname`。用户必须手动编辑配置并重启 WebMux（重启会结束所有会话），Linux 与远程用户也没有托盘的临时开关。

本变更让网页成为隧道配置的主入口：配置保存到服务端状态目录，保存和启停立即生效，不重启 WebMux、不影响现有会话；Windows 托盘保留快捷开关，但不再承担配置说明或配置文件编辑，推送与设置说明同步指向网页。

## What Changes

- 新增 `/settings/tunnel` 页面：展示隧道状态、模式、`hostname`、凭据是否已配置、当前设置的来源，提供 Quick / Named 模式表单、保存并应用、启用 / 关闭、恢复配置文件设置、复制访问链接和风险说明。凭据只写不读，页面不会回填或展示明文。
- 新增服务端持久化设置 `<state_dir>/tunnel.json`（0600、原子替换、独立 flock）：保存 `enabled`、`mode`、`hostname` 与隧道凭据。文件存在时覆盖 TOML 中的 `enabled`、`token`、`hostname`；`cloudflared` 与 `download_mirrors` 仍由 TOML 管理；只有确认文件不存在才沿用 TOML。文件损坏、不可读或锁失败时保持隧道关闭，但 HTTP 继续运行，读状态显示存储故障。
- 新增浏览器可用的 `GET /api/tunnel`、`PUT /api/tunnel/settings`、`PUT /api/tunnel/enabled` 与 `DELETE /api/tunnel/settings` 接口。它们复用现有 Token、Host 与 Origin 校验，响应与日志不包含隧道凭据；本机 `/local/v1/tunnel` 与网页开关写入同一份持久化状态。
- 运行时应用：保存设置或切换开关后，按新设置撤销旧公网来源、结束旧 generation 的 cloudflared、按新配置启动；请求立即返回，状态经 `starting` 到 `ready` 或 `failed`。旧 generation 的进程退出或迟到事件不能重新授权公网来源。关闭即使持久化失败也撤权并停止，返回 503 与 `notPersisted=true`；其他修改仍先保存后应用。
- 启动恢复：持久化设置存在时，在监听开始后按设置启动隧道；cloudflared 不可用、下载失败或 WebMux 登录 Token 不足时只把隧道标为 `failed`，不阻止服务启动。没有持久化设置时，TOML `enabled` 的启动失败语义保持不变；`--tunnel` 只覆盖本次进程，不写入持久化设置。
- 动态联动：`GET /api/config` 的 `Named`/`Hostname`、推送来源判定与托盘状态使用当前生效设置；修改 `hostname` 后公网页面会断开、旧推送订阅点开仍指向旧来源，页面在应用前给出确认与说明。
- 托盘简化：保留公网隧道勾选项、状态、复制链接与失败通知；勾选使用含命令行覆盖的当前进程期望开关，可选字段缺失时回退实际状态；只有显式 `settingsAvailable=true` 才显示“打开公网访问设置…”入口与网页推送引导，旧服务端降级。移除新服务端隧道配置文件编辑与重启引导。根 WebMux 登录 Token 仍由现有配置命令或配置文件管理。
- 不修改 `config.toml`，不新增第二套认证，不把隧道凭据写入日志、URL、`GET` 响应或错误文本。

## Capabilities

### New Capabilities

- `web-tunnel-settings`：网页公网访问设置页、字段校验、服务端持久化与 TOML 优先级、浏览器 API、运行时应用、启动恢复、凭据与确认安全要求。

### Modified Capabilities

- `runtime-tunnel-control`：运行中开关与配置来自当前生效设置并持久化；服务结束仍终止 cloudflared，重启按持久化设置恢复。
- `windows-tray-tunnel`：托盘与网页共享服务端持久化开关，新增网页设置入口，移除隧道配置文件编辑与重启引导。
- `windows-tray-push-guidance`：推送前置条件说明改为在网页“公网访问”中配置 Named Tunnel，不再要求编辑 `[tunnel]` 并重启。
- `push-notification-client`：Quick Tunnel 提示改为引导到网页“公网访问”设置，而不是修改配置文件并重启。

## Impact

- 后端：新增 `internal/tunnelsettings`（设置存储、校验、原子写入与锁）；`internal/contract`（新请求/响应类型）；`internal/api`（`/api/tunnel*` 路由、动态 `tunnelInfo` 与 `pushOrigin`、错误映射、线上快照）；`cmd/webmux`（启动加载与合并、可变更的 `tunnelController`、`--tunnel` 覆盖、本机状态）；`internal/tray` 与 `internal/tray/winui`（菜单项与文案）；`internal/archtest/layers_test.go`（新 Service 包归类）与 `docs/architecture.md`。
- 前端：`web/src/App.svelte`（新路由）、`web/src/pages/Settings.svelte`（入口卡片）、新 `TunnelSettingsPage` 相关组件、`web/src/schemas.ts`、`web/src/api.ts` 与对应浏览器测试。
- 协议：新增 `/api/tunnel` 系列端点；`GET /api/config` 的 `tunnel.named` / `tunnel.hostname` 改为反映当前生效设置；`/local/v1/tunnel` 的开关开始持久化。线上 JSON 快照与 `web/src/schemas.ts` 需同步。
- 数据：新增 `<state_dir>/tunnel.json` 与 `tunnel-config.lock`；不迁移或改写已有 `config.toml`。回滚后旧版本会忽略新文件，继续按 TOML 启动。
- 文档与用户故事：`docs/cloudflare-tunnel.md`、`docs/protocol.md`、`docs/usage.md`、`docs/deployment.md`、`deploy/config.example.toml`、`docs/user-stories.md`、`SECURITY.md`。
- 不涉及：PTY、回放、会话生命周期、终端二进制帧、上传/下载、Cloudflare 账号创建流程与 cloudflared 下载实现。
