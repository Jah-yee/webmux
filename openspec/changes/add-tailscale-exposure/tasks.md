## 1. 依赖、契约与配置

- [x] 1.1 确认本变更不依赖未归档的 `add-web-tunnel-settings` 或 `add-server-settings-sync`；只在现有 `tunnel.json` 之外新增独立状态文件，不改写 Cloudflare 设置
- [x] 1.2 在 `internal/config` 增加 `TailscaleConfig`：`mode`（`off`/`serve`/`funnel`）、`port`（0 自动；Serve 1-65535，Funnel 443/8443/10000）、`provider`（`auto`/`linux`/`windows`）与可选 `command`；写清默认值、按模式的端口校验与非 WSL 环境使用 `windows` 的错误
- [x] 1.3 在 `Config.Validate` 中校验 `[tailscale]` 的枚举与端口；由启动装配/检测拒绝非 WSL 的 `provider="windows"`；运行期前置条件不阻止本地服务
- [x] 1.4 在 `cmd/webmux/access.go` / `main.go` 把 `mode != "off"` 纳入 `ensureToken`：空 Token 生成 256 位 Token，显式短 Token 拒绝启动；不改变 Cloudflare 现有规则
- [x] 1.5 在 `internal/contract` 增加 `TailscaleStatus`、`TailscaleEntry` 与阻塞原因字段；不包含 Token、其他入口的 target 路径或原始 CLI 输出
- [x] 1.6 在 `internal/wire` 增加服务类型到 contract 类型的转换；`GET /api/tailscale` 只经 wire 输出 JSON
- [x] 1.7 在 `internal/archtest/layers_test.go` 把 `internal/tailscale` 归为 Service，更新 `docs/architecture.md` 的包表、分层表与装配说明

## 2. 只读检测服务

- [x] 2.1 新增 `internal/tailscale` 的 WSL/平台检测：把 WSL 判定抽取到共享层并复用，读取 `wslinfo --networking-mode`，区分 Linux、WSL 原生、WSL Windows 与不可用
- [x] 2.2 实现 provider 解析：`auto` 在 WSL 优先运行中的 WSL 原生，Windows 只检测不代管；显式 `windows` 才允许写；同时检测到两者时展示确定的选择
- [x] 2.3 实现 CLI 定位：显式 `command`（用 `pathutil.Expand` 展开）、`PATH` 中的 `tailscale`/`tailscale.exe`、默认 Windows 安装路径；验证可执行、处理 `/mnt` 未挂载与 interop 关闭
- [x] 2.4 实现 `status --json` 解析：`BackendState`、`Self.DNSName`、`Self.Online`、`CertDomains`；末尾点归一化，缺失字段按未知处理
- [x] 2.5 实现 `serve status --json` 解析：`Web` 的 host:port、`/` handler、`Proxy` 与 `AllowFunnel`；其他入口只保留 host、port、public，不保留目标路径
- [x] 2.6 给所有 CLI 调用加超时、无交互 stdin、argv 传参与 stderr 脱敏；`CertDomains` 为空时标记 HTTPS 未就绪，绝不执行会等待证书的 Serve
- [x] 2.7 实现检测缓存与并发合并（约 2 秒）并支持显式刷新；缓存只影响检测，不影响控制器状态
- [x] 2.8 新增 fake CLI 测试：未安装、未登录、未运行、证书未启用、正常、超时、畸形 JSON、Windows provider、NAT/mirrored、权限错误

## 3. Serve 生命周期与所有权

- [x] 3.1 在 `cmd/webmux` 新增 tailscale 控制器：状态 `off`/`starting`/`ready`/`failed`、generation、取消与串行 apply；启动时先清理应关闭/已变化的旧入口与未知匹配入口，再绑定监听，监听开始后异步应用
- [x] 3.2 实现 target 推导：使用监听后的真实端口；仅接受覆盖 IPv4 回环的监听；`[::1]` 或仅特定网卡地址失败并提示修改 `listen`
- [x] 3.3 实现端口选择：显式端口冲突失败；`port = 0` 按 443、8443、10000 选第一个无他人入口的端口，三者都冲突时 Serve 提示显式指定其他空闲端口；记录最终端口
- [x] 3.4 在 `<state_dir>/tailscale.json` 保存版本、provider、节点 DNS、端口、模式、target 与 `pending`/`active` 状态；0600、原子替换、独立 flock；损坏/锁冲突失败关闭
- [x] 3.5 应用前读取 serve 配置：只有状态记录匹配且 target 精确、唯一 `/` handler 可复用；无记录、其他入口、额外 handler 或不匹配 target 一律不覆盖并失败
- [x] 3.6 执行写命令前先持久化 `pending` 意图，再用 argv 执行 `tailscale serve --bg --https=<port> http://127.0.0.1:<port>`；不使用 shell，不传用户拼接字符串
- [x] 3.7 应用到 Serve 后回读 `serve status --json`：要求目标 handler 指向当前 target 且 `AllowFunnel` 不为真；不满足则撤销并失败
- [x] 3.8 实现精确移除：执行 `tailscale serve --https=<port> off`，同时清除 Funnel 标记；把重复移除的 `handler does not exist` 视为成功；绝不调用 `serve reset` 或 `funnel reset`
- [x] 3.9 启动对账：`mode=off` 或模式/端口/target/provider 变化时在绑定监听前移除旧精确目标，失败则拒绝启动；移除前用 `serve status --json` 复核 provider、DNS/端口与 target，读取失败时失败关闭并在错误中给出状态文件路径；端口已无入口或指向其他 target 时判定记录过期，保持该入口不动并删除记录；匹配入口复用后绑定失败也要关闭并报告风险；`pending`/`active` 记录中的入口缺失则重建；记录匹配才复用；无记录或不匹配则失败关闭，禁止仅凭 target 相同接管入口
- [x] 3.10 退出时在短超时内移除本次入口；成功后删除状态记录，CLI 不可用或超时保留 `pending`/`active` 记录供下次启动对账；不结束 tailscaled、不阻塞会话清理
- [x] 3.11 就绪后打印 `WebMux tailscale: <url>`；Token 为本次生成时附 `#token=<Token>` 与二维码，配置 Token 只打印 URL；结构化日志不含 Token
- [x] 3.12 测试写命令前落盘、命令成功后标记前崩溃、失败清理成功/失败、退出清理成功/失败的 `pending`/`active` 状态转换，以及 `mode=off`/端口变化/损坏状态/绑定失败下的启动前清理与拒绝启动

## 4. Funnel 托管

- [x] 4.1 实现 `mode = "funnel"`：只允许 443/8443/10000，要求强 Token；不因检测到 Tailscale 就自动公开
- [x] 4.2 执行前校验 HTTPS 证书、端口与入口冲突；命令失败时把 stderr 映射为 HTTPS 未启用、缺少 `funnel` 节点属性、权限不足或端口冲突的可操作提示
- [x] 4.3 用 argv 执行 `tailscale funnel --bg --https=<port> http://127.0.0.1:<port>`，回读要求 `AllowFunnel[host:port] == true` 且 handler 指向当前 target
- [x] 4.4 从 Serve 切到 Funnel、Funnel 切到 Serve 或端口变化时，先关闭旧精确目标再应用；失败时确认旧端口不再公开，无法确认则保持 `failed` 并给出人工命令
- [x] 4.5 启动失败或前置条件不满足时，移除本次记录中可能残留的公开入口；不自动重试，不影响本地 HTTP 与 Cloudflare
- [x] 4.6 测试 Funnel 公开/失败/切换/残留清理/证书缺失/无节点属性/端口冲突；fake CLI 覆盖 `AllowFunnel` 回读

## 5. WSL Windows provider

- [x] 5.1 检测 WSL 与 networking mode；`provider = "windows"` 且 mode 非 off 时，NAT 直接失败关闭，不执行任何 serve/funnel 命令
- [x] 5.2 NAT 错误与设置页给出三种替代：mirrored（`.wslconfig` + `wsl --shutdown`，会结束会话）、WSL 内安装 Tailscale、Windows 托盘转发；不自动修改 `.wslconfig`
- [x] 5.3 `auto` 在只有 Windows Tailscale 时只检测；mode 非 off 时提示显式 `provider = "windows"`；非 WSL 环境配置 windows 在启动时失败
- [x] 5.4 Windows CLI 权限错误提示在 Windows 管理员终端执行 `tailscale set --operator=<用户>`；不调用 `sudo`、不自动提权
- [x] 5.5 URL、Host、Origin 使用 Windows 节点的 `Self.DNSName`，不使用 WSL 主机名/IP；设置页同时展示 WSL 原生与 Windows 检测结果
- [x] 5.6 处理 Windows `--bg` 入口跨 WSL 关闭/重启保留：启动对账、旧 target 移除、状态文件丢失失败关闭、多个发行版端口冲突失败关闭
- [x] 5.7 测试 fake `tailscale.exe` + fake `wslinfo`：mirrored 成功、nat 阻塞、权限失败、DNS 名选择、互操作不可用、WSL 重启后的残留对账

## 6. 来源注册表与 API

- [x] 6.1 用 `api.PublicOrigins` 替换单值 `PublicOrigin`，按来源 copy-on-write 保存 origin 与精确 authority；Cloudflare controller 改用 `cloudflare` 来源
- [x] 6.2 调整中间件：Host 比较完整 authority，Origin 比较对应 HTTPS origin；443 无端口，其他端口带端口；撤销后立即 403
- [x] 6.3 Tailscale 控制器在命令成功且回读确认后登记来源，停止/失败/切换前撤销；Cloudflare 与 Tailscale 可并存
- [x] 6.4 新增 `GET /api/tailscale`：认证、Host/Origin、缓存/合并、脱敏、状态码 200/401/403；不返回 Token 或其他入口 target
- [x] 6.5 保持推送判定现状并写测试：Quick Tunnel 仍为 `quick_tunnel`，Tailscale 仍为 `unsupported_origin`，避免本次悄悄扩大推送来源
- [x] 6.6 重新录制受影响的线上快照；补充 443、8443、10000 与任意 Serve 端口、错误端口、http Origin、无关 Host、撤销、并存来源的测试

## 7. 前端设置页

- [x] 7.1 `web/src/schemas.ts` 增加 `TailscaleStatusSchema` 与相关类型；`web/src/api.ts` 复用 Bearer、`APIError`、401 事件与 `AbortSignal`
- [x] 7.2 `web/src/App.svelte` 增加 `/settings/tailscale` 懒加载路由；`Settings.svelte` 在“公网访问”分区增加 Tailscale 卡片与入口
- [x] 7.3 新增 Tailscale 详情页/组件：检测状态、provider、节点 DNS、HTTPS 就绪、模式、端口、URL、错误、阻塞原因与刷新
- [x] 7.4 明确区分 Serve（仅 tailnet）与 Funnel（公网）并显示 Funnel 风险；显示“由配置文件管理，修改后重启”
- [x] 7.5 WSL 指引：NAT 阻塞原因、mirrored 会结束会话的提示、WSL 内安装、托盘转发；Windows operator 提示
- [x] 7.6 现有入口冲突只显示 host/port/public，不显示其他目标路径；复制 URL 与带 Token 链接复用现有剪贴板约定
- [x] 7.7 移动端单列、键盘、可访问名称、亮暗主题与窄屏溢出；浏览器测试覆盖加载、失败、NAT、Funnel、冲突、刷新与复制

## 8. 文档、用户故事与验收

- [x] 8.1 新增 `docs/tailscale.md`：Serve vs Funnel、端口、证书、Token、手工/托管命令、精确移除、故障排查、WSL Windows/NAT/mirrored
- [x] 8.2 更新 `docs/deployment.md` 的 HTTPS、手机访问、WSL 段落；`docs/cloudflare-tunnel.md` 增加入口对比与选择建议
- [x] 8.3 更新 `docs/protocol.md` 的 `GET /api/tailscale`、`SECURITY.md` 的公网 shell 与 Funnel 风险、`deploy/config.example.toml` 的 `[tailscale]` 示例
- [x] 8.4 更新 `docs/user-stories.md`：新增 Tailscale 访问故事组，覆盖检测、Serve、Funnel、WSL NAT 阻塞、Windows provider 与失败关闭；核对 ID、索引与来源路径
- [ ] 8.5 手工验收 Linux/macOS 的 Serve 与 Funnel、端口回退、证书未启用、端口冲突、已有入口、重启对账、崩溃残留
- [ ] 8.6 手工验收 WSL：Windows provider mirrored 成功、NAT 阻塞、权限不足提示、WSL 关闭后残留对账、WSL 内 Tailscale 优先级
- [x] 8.7 确认真实 Tailscale 操作不进入 CI；E2E 与单元测试只使用 fake CLI 和隔离状态目录

## 9. 验证

- [x] 9.1 `go test -race ./internal/tailscale ./internal/config ./internal/api ./internal/wire ./cmd/webmux`（按最终包名调整）
- [x] 9.2 `go test ./internal/archtest ./internal/docguard` 与 `go vet` 相关包
- [x] 9.3 `pnpm --dir web check`、`pnpm --dir web test` 定向用例、`pnpm --dir web check:arch`、生产构建
- [x] 9.4 按 AGENTS.md 运行格式检查与 `make test` 的适用子集；确认没有真实 Tailscale/cloudflared 网络依赖
- [x] 9.5 `openspec validate add-tailscale-exposure --strict --no-interactive` 与 `make docs-check`
- [x] 9.6 实现 PR 附桌面/手机、亮/暗截图；说明未做真实 Windows 主机与真机验收的部分

## 实施记录（2026-10-26）

- Go：`go test -race ./internal/tailscale ./internal/config ./internal/api ./internal/wire ./cmd/webmux`、`go test ./internal/archtest`、`go vet ./...` 通过。
- 前端：`svelte-check`、Oxlint、`check:arch`、生产构建与新增 `test/browser/TailscaleSettings.test.ts` 通过；未运行完整浏览器/E2E 套件与截图采集。
- 配置/接口：`openspec validate add-tailscale-exposure --strict --no-interactive` 与 `make format-check` 通过。
- 手工验收（WSL2 mirrored + 真实 Windows Tailscale，`provider = "windows"`，Serve）：显式 8444 与自动端口（443 已被用户入口占用时选 8443）就绪；经 tailnet 访问页面、未认证 401、同源 POST 201、无端口或 http Origin 403、WebSocket 同源可连且异源拒绝；正常退出只移除本实例端口并删除记录，用户 443 入口保持不变；SIGKILL 后 `mode = "off"` 重启在监听前移除残留；过期记录不动他人入口并删除记录；端口冲突 `failed` 不覆盖；无记录但入口指向本 target 时拒绝启动；`auto` 只检测 Windows 节点；CLI 缺失时拒绝启动并给出状态文件路径。
- 未完成：8.5 的 Linux/macOS 与真实 Funnel（公网）验收、8.6 的 NAT 阻塞与权限不足真机验收。CI 与单元/浏览器测试只使用 fake CLI，不连接真实 Tailscale。
