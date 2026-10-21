# 安全策略

## 安全模型

WebMux 以启动服务的系统用户身份执行任意命令，**等同于把一个 shell 暴露到网络上**。它不是进程沙箱，也不提供多用户隔离。安全依赖于网络隔离：

- 默认只监听 `127.0.0.1:7681`。远程访问请绑定 Tailscale / WireGuard 等可信 VPN 地址，并用防火墙限制可访问的接口。
- 设置随机的共享 `token`。监听所有接口（如 `0.0.0.0`）且未设置 token 时，服务仍会启动，但会打印警告。
- **不要把 WebMux 直接暴露到公网。** 确需公网访问时，只使用 `--tunnel` 提供的入口，并接受下面的隧道风险模型。

### Cloudflare Tunnel

`--tunnel` 通过 Cloudflare Tunnel 把同一个 shell 以 HTTPS 暴露到公网。此时 Token 是唯一入口屏障：

- Quick Tunnel 没有 Cloudflare Access 或额外身份验证。任何拿到链接、二维码或 Token 的人都能以服务用户身份执行命令。
- 启用隧道且未配置 `token` 时，每次启动生成 256 位随机 Token；显式配置的共享 `token` 至少需要 43 个字符，否则拒绝启动。建议用 `openssl rand -base64 32` 生成。
- 隧道模式会在终端打印带 `#token=` 的本地与公网链接和二维码。以 systemd / launchd 运行时，这些内容会写入 journal 或日志文件，请限制日志访问权限；自动生成的 Token 每次重启都会更换。
- 公网主机名只接受 Cloudflare 转发的 Host 与该主机的 `https://` Origin，其他 Host 与 `http://` Origin 会被拒绝；cloudflared 退出后立即撤销授权。
- Token 不进入请求 URL（HTTP 使用 Bearer 头，WebSocket 使用 `webmux.token.*` 子协议），也不写入 Cookie（Cookie 不按端口隔离，同一主机其他端口的页面可以读取）；但 Bearer、子协议头与终端输入输出仍会经过 Cloudflare 边缘。
- 不再需要公网访问时，立即停止 webmux 或移除 `--tunnel`。

内置的防护措施：

- 所有 API 与 WebSocket 都需要认证（启用 token 时），token 以常量时间比较。
- 带 Origin 的请求必须同源或位于 `allowed_origins` 中，防止其他网站通过浏览器发起连接。
- 终端输入输出不会写入日志；配置接口不返回 token。
- 响应头包含 `X-Content-Type-Options: nosniff` 与 `Referrer-Policy: no-referrer`。

其他注意事项：

- WebMux 不内置 TLS；需要 HTTPS 时请使用 `tailscale serve` 或反向代理。
- token 保存在浏览器 localStorage 中，只在可信设备上登录。
- 登记的 Git 仓库必须可信：Git 会执行仓库配置中的 hooks 与 filters。
- 关闭会话会清理其进程树，但完全脱离终端会话的自守护进程可能不受控制。

## 支持的版本

只有最新发布版本会接收安全修复。

## 报告漏洞

请**不要**通过公开 Issue 报告安全漏洞。请使用 GitHub 的 [私密漏洞报告](https://github.com/mzdluo123/webmux/security/advisories/new) 功能，并提供：

- 受影响的版本与平台；
- 复现步骤或概念验证；
- 可能的影响范围。

我们会尽快确认并处理。
