# 部署指南

## 运行要求

- Linux 或 macOS（amd64 / arm64）。
- 一个可执行的 shell，以及打算使用的 CLI agent（Claude Code、Codex CLI 等）。
- 可选：Git，用于仓库与 worktree 管理（需支持 `git worktree list --porcelain -z`）。

运行端不需要 Node.js、Go、tmux 或数据库。

## 配置

```sh
webmux config init                 # 生成 0600 的 ~/.config/webmux/config.toml，含随机 Token
# 或：mkdir -p ~/.config/webmux && cp deploy/config.example.toml ~/.config/webmux/config.toml && chmod 600 ~/.config/webmux/config.toml
```

示例见 [`deploy/config.example.toml`](../deploy/config.example.toml)。加载规则：

- 默认路径为 `~/.config/webmux/config.toml`，不存在时全部使用默认值；通过 `--config` 显式指定的文件不存在时启动失败。`--config` 与 `--log-file` 支持 `~/`。
- 命令行 `--listen`、`--token` 优先于配置文件，显式传入空 token 也会覆盖配置。
- `webmux version` 输出版本号。

```sh
webmux --config ~/.config/webmux/config.toml   # 等同于 webmux serve --config ...
curl -H 'Authorization: Bearer YOUR_TOKEN' http://127.0.0.1:7681/api/health
```

### 命令行

无子命令或第一个参数以 `-` 开头时启动服务，与 `serve` 相同；未知子命令退出码 2。客户端子命令都接受 `--config`，按与服务相同的规则找到状态目录、运行时记录与本机套接字。

| 命令                                             | 说明                                                                                                                           |
| ------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------ |
| `webmux serve [--log-file PATH]`                 | 启动服务；`--log-file` 把标准输出与标准错误写入文件（0600），旧文件保留为 `.1`，启动方关闭管道后服务照常运行                   |
| `webmux status [--json] [--reveal-token]`        | 优先经本机 API，退回运行时记录 + TCP 健康检查；退出码 0 健康、3 未运行、1 其他；只有 `--json --reveal-token` 输出 Token        |
| `webmux stop [--timeout 15s] [--force] [--json]` | 优先经本机 API 停止，卡死时退回 SIGTERM，超时 SIGKILL；systemd / launchd 实例需 `--force`；没有记录时不向任何进程发信号        |
| `webmux bridge`                                  | 把本机套接字桥接到标准输入输出，供 Windows 客户端经 `wsl.exe --exec webmux bridge` 使用                                        |
| `webmux config path\|show\|validate\|init`       | 定位、查看生效配置（不含 Token）、校验（含 TOML 行列号）、以模板和随机 Token 初始化（已存在需 `--force`，原文件保留为 `.bak`） |

JSON 输出契约见 [协议文档](protocol.md#控制面-json-契约)。配置只在启动时读取；`status` 的 `drift` 列出已修改但尚未通过重启生效的 `listen`、`token`。

### 配置项

| 配置项               | 默认值                                                     | 说明                                                                                        |
| -------------------- | ---------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| `listen`             | `127.0.0.1:7681`                                           | HTTP / WebSocket 监听地址                                                                   |
| `token`              | 空                                                         | 共享 token，空表示不认证；启用隧道且为空时每次启动随机生成，隧道模式显式 token 至少 43 字符 |
| `scrollback_bytes`   | `2097152`                                                  | 每会话保留的原始终端字节数（不是行数）                                                      |
| `default_cwd`        | `~`                                                        | 新建会话的默认目录，支持 `~` 展开                                                           |
| `state_dir`          | `$XDG_STATE_HOME/webmux`，未设置时 `~/.local/state/webmux` | 仓库清单目录                                                                                |
| `shell`              | `$SHELL` 或 `/bin/bash`                                    | Shell 预设使用的程序，以 `-l -i` 启动                                                       |
| `idle_after_seconds` | `30`                                                       | 无等待特征时判定为空闲的秒数                                                                |
| `prompt_patterns`    | 常见提示符 / 确认模式                                      | 判定“可能在等你”的 Go 正则列表                                                              |
| `agent_protocol`     | `true`                                                     | 在会话环境中设置 `WARP_CLI_AGENT_PROTOCOL_VERSION=1`，让 agent 发出结构化提醒事件           |
| `bell_attention`     | `true`                                                     | 把终端响铃（输入 1 秒后）视为需要介入                                                       |
| `allowed_origins`    | `[]`                                                       | 额外允许的完整 Origin，例如 `https://myhost.example`                                        |
| `presets`            | shell / claude / codex / omp                               | `name`、`label`、`cmd`（argv 数组）、`allow_args`；配置后替换默认列表                       |
| `tunnel`             | 未启用                                                     | Cloudflare Tunnel，见下文“公网访问（Cloudflare Tunnel）”                                    |

`custom` 是保留预设名。追加参数按引号与反斜杠规则拆分为 argv，不经 shell 求值。要改变 shell 的启动参数，可为 shell 预设显式设置 `cmd = ["/bin/zsh", "-l", "-i"]`。

### 状态目录

`state_dir` 保存仓库清单 `repositories.json` 与运行时子目录 `run/`（运行时记录 `server.json`、锁 `server.lock`、本机套接字 `webmux.sock`），不保存会话、终端内容或 token。目录权限 0700、文件 0600，通过原子替换写入。

- 同一状态目录只能由一个实例使用；多个实例请配置不同的 `state_dir`。第二个实例照常提供终端服务，但不写运行时记录、不创建本机套接字，并在日志中警告。
- 清单损坏、文件锁冲突或存储不可用时，仓库管理功能被禁用，但终端服务照常运行，损坏的清单不会被覆盖。

## systemd 用户服务（Linux）

```sh
make install
mkdir -p ~/.config/systemd/user
cp deploy/webmux.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now webmux
journalctl --user -u webmux -f
```

用户未登录时也保持运行：

```sh
loginctl enable-linger "$USER"
```

服务使用 `KillMode=mixed`：WebMux 先自行清理会话，超时后由 systemd 清理整个 cgroup。

## launchd 用户代理（macOS）

```sh
make install
mkdir -p ~/Library/LaunchAgents ~/Library/Logs
sed "s#/Users/YOU#$HOME#g" deploy/com.webmux.plist > ~/Library/LaunchAgents/com.webmux.plist
plutil -lint ~/Library/LaunchAgents/com.webmux.plist
launchctl bootstrap "gui/$(id -u)" ~/Library/LaunchAgents/com.webmux.plist
tail -f ~/Library/Logs/webmux.log
```

修改 plist 后重新加载：

```sh
launchctl bootout "gui/$(id -u)/com.webmux"
launchctl bootstrap "gui/$(id -u)" ~/Library/LaunchAgents/com.webmux.plist
```

- launchd 不展开 `~` 和 `$HOME`，plist 中必须使用绝对路径。
- plist 设置了 `LANG=en_US.UTF-8`；缺少 UTF-8 locale 时中文会显示为转义字符。
- 代理在用户登录后运行，`KeepAlive` 在异常退出后重启。
- 系统睡眠会暂停所有会话进程；长时间运行 agent 时可使用 `caffeinate` 防止睡眠。

## agent 的 PATH

systemd 和 launchd 都不会加载交互 shell 的初始化文件。服务模板的 `PATH` 已包含 `~/.local/bin`、pnpm / npm 全局目录（macOS 另含 Homebrew）。如果 agent 安装在 nvm 等其他位置：

```sh
which claude codex
systemctl --user edit webmux    # Linux：添加 drop-in
```

```ini
[Service]
Environment=PATH=/home/YOU/.local/bin:/home/YOU/.nvm/versions/node/v24.20.0/bin:/usr/local/bin:/usr/bin:/bin
```

macOS 直接编辑 plist 的 `EnvironmentVariables` 后重新加载。

## 手机访问（Tailscale）

1. 在服务器和手机上安装 Tailscale 并加入同一网络。
2. 将 `listen` 设置为服务器的 Tailscale 地址（如 `100.64.0.5:7681`），并设置 `token`。
3. 手机浏览器打开 `http://<Tailscale IP 或主机名>:7681` 并输入一次 token（保存在浏览器 localStorage 中）；也可以打开启动时打印的 `WebMux local:` 链接（把主机换成 Tailscale 地址）。
4. 可“添加到主屏幕”以独立模式运行。

防火墙应只允许可信 VPN 接口访问该端口。

## 访问链接

每次启动都会在标准输出打印 `WebMux local: http://<主机>:<端口>/`。启用隧道时会附带 `#token=<token>`；非隧道模式不附带片段，以免固定的已配置 token 被 service manager 捕获并写入 journal 或日志文件。片段不会发送到服务器，前端读取并保存 token 后立即从地址栏和当前历史记录中移除。WebSocket 握手通过子协议携带 token，token 不出现在请求 URL 或 Cookie 中。浏览器的自动补全或同步历史仍可能记住带片段的原始链接。

## 公网访问（Cloudflare Tunnel）

```sh
webmux --tunnel
```

或在配置中设置：

```toml
[tunnel]
enabled = true
token = "" # 空 = Quick Tunnel；填写 = Named Tunnel
hostname = "" # Named Tunnel 的公共主机名（ASCII/punycode，不支持通配符）
cloudflared = "" # 可选：cloudflared 路径
# download_mirrors = ["https://ghfast.top", "https://gh-proxy.com"]
```

- **Quick Tunnel**：无需账号，每次启动得到随机 `https://*.trycloudflare.com` 地址，无 SLA，适合临时访问。
- **Named Tunnel**：在 Cloudflare 控制台创建隧道，把公共主机名指向 `http://127.0.0.1:<端口>`，再把隧道 token 写入 `tunnel.token`、主机名写入 `tunnel.hostname`。凭据经环境变量传给 cloudflared，不出现在进程命令行中。
- 启用隧道时 webmux 强制认证：未配置 `token` 则每次启动生成 256 位随机 token；显式配置的 webmux `token` 至少 43 个字符，否则拒绝启动。隧道连通后终端打印 `WebMux public: https://<公网主机>/#token=<token>` 与二维码，手机扫码即可进入。
- Quick Tunnel 没有 Cloudflare Access：token 是唯一入口屏障，不要把链接或二维码发给不可信的人。
- cloudflared 依次取 `tunnel.cloudflared`、`PATH`、`<state_dir>/cloudflared/<版本>/` 缓存；都没有时下载 webmux 固定的版本，先连 GitHub，10 秒无响应或 20 秒无数据时依次改用 `download_mirrors` 镜像（默认 `ghfast.top`、`gh-proxy.com`；`[]` 表示只用 GitHub）。任何来源的文件都必须与内置 SHA-256 一致才会执行。镜像是第三方服务，能看到请求者 IP 和所下载的文件；不希望回退到它们时设置 `download_mirrors = []`。
- webmux 以 `--config /dev/null` 运行 cloudflared，`~/.cloudflared/config.yml` 不会影响隧道。
- cloudflared 意外退出时公网地址立即失效（返回 403），本地访问和会话不受影响；webmux 退出时 cloudflared 随之结束。Linux 上即使 webmux 被 SIGKILL 或 OOM 终止，内核也会结束 cloudflared；macOS 没有等价机制，依赖 launchd 结束作业的全部进程，手动运行时需自行确认没有残留。

> 隧道模式打印的链接与二维码包含 token；以 systemd / launchd 运行时它们会写入 journal 或日志文件，请限制日志访问权限；自动生成的 token 每次重启都会更换。非隧道模式不打印固定的已配置 token。

## HTTPS

WebMux 不内置 TLS。需要浏览器安全上下文（系统剪贴板 API）时，可使用 `tailscale serve` 或 Caddy 反向代理 HTTPS：

- 保留原始请求 `Host`；
- 把公开的 HTTPS Origin 加入 `allowed_origins`。

## 升级

替换二进制后重启服务。**重启会结束所有终端会话**，升级前请保存工作；仓库清单和磁盘上的 worktree 会保留。只替换磁盘上的文件不会让正在运行的进程加载新版本。

## 平台差异

Linux 与 macOS 同等支持。进程状态在 Linux 读取 `/proc`，在 macOS 使用 `sysctl` 与 `proc_pidinfo`。

macOS 上：

- 没有 subreaper，脱离父进程的后代由 launchd 回收。
- 内核不公开其他进程的环境变量，`WEBMUX_SESSION_ID` 无法用于清理；关闭会话时按终端会话 ID 与进程树查找后代。先 `setsid` 再让父进程退出的自守护程序不会被清理。
- 临时目录位于 `/var → /private/var` 符号链接下，WebMux 报告规范化后的真实路径。
- Apple 自带的 `/bin/bash` 为 3.2 版本；默认 Shell 预设使用 `$SHELL`（通常为 `/bin/zsh`）。
