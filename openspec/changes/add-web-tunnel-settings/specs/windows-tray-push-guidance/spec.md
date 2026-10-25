## MODIFIED Requirements

### Requirement: 推送提示行

WebMux 运行且本机状态同时包含 `tunnel` 与 `push` 字段时，托盘 SHALL 在公网隧道项下方显示一行不可点击的推送提示：

- 推送停用：“手机推送：已在配置中停用”；
- 推送存储不可用：“手机推送不可用：<原因>”；
- 未配置 Named Tunnel：“手机推送：需要 Named Tunnel”；
- 已配置 Named Tunnel：订阅数为 0 时显示“手机推送：尚未有设备开启”，否则显示“手机推送：N 台设备已开启”。

状态缺少 `push` 字段时 SHALL 隐藏该行。

#### Scenario: Quick Tunnel 已开启

- **WHEN** 实例运行 Quick Tunnel，且未配置 Named Tunnel
- **THEN** 菜单显示“手机推送：需要 Named Tunnel”

#### Scenario: Named Tunnel 已有订阅

- **WHEN** 实例配置了 Named Tunnel，`push.subscriptions` 为 2
- **THEN** 菜单显示“手机推送：2 台设备已开启”

#### Scenario: 旧版服务端

- **WHEN** WSL 中的 webmux 状态不含 `push` 字段
- **THEN** 菜单不显示推送提示行与设置说明项

### Requirement: 推送设置说明

显示推送提示行时，托盘 SHALL 同时提供“手机推送设置说明…”菜单项，点击后弹出说明对话框。未配置 Named Tunnel 时，说明 SHALL 列出：在 Cloudflare 控制台创建 Tunnel 并把公共主机名指向 WebMux 本地地址、在 WebMux 设置 → 公网访问中选择 Named Tunnel 并填写隧道凭据与 `hostname`、保存并启用公网访问。之后两种情况都 SHALL 列出手机端步骤：用“复制公网链接”在手机打开，iOS 添加到主屏幕后从图标打开并输入 Token，进入设置 → 通知推送 → 开启。

#### Scenario: 未配置 Named Tunnel

- **WHEN** 实例未配置 Named Tunnel，用户点击“手机推送设置说明…”
- **THEN** 对话框先说明 Quick Tunnel 地址每次启动都会变化、无法用于推送，再列出网页配置 Named Tunnel 的步骤与手机端步骤

#### Scenario: 已配置 Named Tunnel

- **WHEN** 实例配置了 Named Tunnel，用户点击“手机推送设置说明…”
- **THEN** 对话框只列出手机端开启推送的步骤，并显示公网地址 `https://<hostname>`

### Requirement: 说明不泄露凭据且不改配置

推送提示行与说明对话框 MUST NOT 包含 WebMux Token、隧道凭据或订阅 endpoint。托盘 MUST NOT 以程序方式修改配置文件、隧道凭据或 `hostname`；除公网隧道勾选项的开关外，隧道配置由用户在网页“公网访问”设置中完成。

#### Scenario: 说明内容

- **WHEN** 用户打开“手机推送设置说明…”
- **THEN** 对话框中不出现 Token 或隧道凭据的值，配置文件、隧道凭据与 `hostname` 未被修改
