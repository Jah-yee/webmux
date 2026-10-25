## MODIFIED Requirements

### Requirement: Quick Tunnel 提示

`GET /api/push` 报告 `quick_tunnel` 时，卡片 SHALL 说明 Quick Tunnel 每次启动更换地址，无法持续接收推送，并列出改用 Named Tunnel 的步骤：

1. 在 Cloudflare 控制台创建 Tunnel，把公共主机名指向 WebMux 本地地址；
2. 打开 WebMux 设置 → 公网访问，选择 Named Tunnel，填写隧道凭据与 `hostname` 并保存；
3. 启用公网访问；保存与启用无需重启 WebMux，终端会话不受影响；
4. 用新的公网链接打开 WebMux 并开启推送。

#### Scenario: 从 Quick Tunnel 打开

- **WHEN** 用户从 `https://abc.trycloudflare.com` 打开设置页
- **THEN** 卡片显示上述步骤，提及在“设置 → 公网访问”中配置 Named Tunnel，不再要求编辑配置文件或重启 WebMux，且不提供开启开关
