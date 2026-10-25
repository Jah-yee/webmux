## MODIFIED Requirements

### Requirement: 隧道开关

WebMux 运行且健康时，托盘菜单 SHALL 显示“公网隧道（Cloudflare）”勾选项，勾选状态 SHALL 反映服务端当前进程的期望开关，包括 `--tunnel`/`--tunnel=false` 覆盖、`failed` 但仍希望开启，以及持久化失败后的紧急关闭。`TunnelStatus.enabled` SHALL 为可选布尔值，旧服务端缺字段时回退 `starting`/`ready` 实际运行状态。实例不支持运行时隧道控制（状态中没有 `tunnel` 字段）时 SHALL 隐藏该项。外部实例 SHALL 同样可以开关隧道，因为开关不重启实例；托盘开关 SHALL 与网页设置写入同一服务端持久化状态。

#### Scenario: 开启后显示地址

- **WHEN** 用户勾选“公网隧道”并确认风险
- **THEN** 持久化开关变为开启，菜单先显示“公网隧道：正在连接”，就绪后显示公网地址，会话不受影响

#### Scenario: 旧版服务端

- **WHEN** WSL 中运行的 webmux 不报告 `tunnel` 状态
- **THEN** 菜单中没有“公网隧道”项

#### Scenario: 重启后保持

- **WHEN** 用户通过托盘开启隧道后重启 WebMux，服务端持久化开关仍为开启
- **THEN** 新实例按持久化设置恢复隧道，托盘勾选项显示实际开启状态

### Requirement: Token 不足时的引导

服务端因 WebMux 登录 Token 缺失或过短拒绝开启时，托盘 SHALL 显示原因，并在 `settingsAvailable=true` 时提供“打开公网访问设置…”入口。托盘 MUST NOT 在支持网页设置的新服务端隧道菜单中要求用户编辑隧道配置文件或重启 WebMux 来修改隧道凭据；若原因是 WebMux 登录 Token 不足，说明需要至少 43 个字符的登录 Token，并指向现有配置文件或配置命令。旧服务端 MUST NOT 显示不支持的网页入口。

#### Scenario: 未配置 Token

- **WHEN** 实例未配置 WebMux 登录 Token，用户确认开启隧道
- **THEN** 托盘通知开启失败及原因，提供网页设置入口，勾选项保持未勾选

#### Scenario: 登录 Token 过短

- **WHEN** 实例的 WebMux 登录 Token 少于 43 个字符，用户确认开启隧道
- **THEN** 托盘说明需要更长的登录 Token 且不能只靠网页修复，并保留网页设置入口用于查看隧道配置

### Requirement: 隧道失败通知

隧道从 `starting` 或 `ready` 变为 `failed` 时，托盘 SHALL 发出一次通知并在菜单中显示原因；托盘 SHALL NOT 自动重新开启。持久化开关保持开启时，勾选项 SHALL 继续表示用户希望开启，并允许用户关闭或重试。

#### Scenario: 隧道断开

- **WHEN** 隧道就绪后 cloudflared 意外退出
- **THEN** 托盘通知“公网隧道已断开”及原因，菜单显示失败原因，勾选项仍为开启（服务端设置未变）

### Requirement: 隧道不随托盘持久化

托盘 SHALL NOT 保存隧道开关或配置；开关与配置 SHALL 由服务端持久化。WebMux 重启（包括托盘的自动重启）后，托盘 SHALL 显示服务端恢复后的实际状态；托盘自身重启 MUST NOT 改写服务端设置。

#### Scenario: 自动重启后

- **WHEN** 隧道开启时 WebMux 崩溃并被托盘自动重启
- **THEN** 新实例按服务端持久化设置恢复隧道，托盘显示实际状态，不因托盘重启而清空开关

## ADDED Requirements

### Requirement: 网页设置入口

服务端显式报告 `tunnel.settingsAvailable=true` 时，托盘隧道菜单 SHALL 提供“打开公网访问设置…”项，在默认浏览器打开 `/settings/tunnel`。仅有 `tunnel` 字段不足以证明支持新页面。该项 SHALL 只用于导航，MUST NOT 在菜单或通知中展示、编辑或复制隧道凭据。

#### Scenario: 打开网页设置

- **WHEN** 服务端报告 `tunnel.settingsAvailable=true`，用户点击“打开公网访问设置…”
- **THEN** 默认浏览器打开 WebMux 的 `/settings/tunnel` 页面，菜单文本不含隧道凭据

#### Scenario: 旧版服务端

- **WHEN** 实例不报告 `tunnel` 状态，或报告 `tunnel` 但没有 `settingsAvailable=true`
- **THEN** 托盘不显示“打开公网访问设置…”项
