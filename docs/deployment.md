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

无子命令或使用 `--listen` 等服务参数时启动服务，与 `serve` 相同；未知子命令退出码 2。客户端子命令都接受 `--config`，按与服务相同的规则找到状态目录、运行时记录与本机套接字。

| 命令                                              | 说明                                                                                                                           |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `webmux serve [--log-file PATH]`                  | 启动服务；`--log-file` 把标准输出与标准错误写入文件（0600），旧文件保留为 `.1`，启动方关闭管道后服务照常运行                   |
| `webmux status [--json] [--reveal-token]`         | 优先经本机 API，退回运行时记录 + TCP 健康检查；退出码 0 健康、3 未运行、1 其他；只有 `--json --reveal-token` 输出 Token        |
| `webmux stop [--timeout 15s] [--force] [--json]`  | 优先经本机 API 停止，卡死时退回 SIGTERM，超时 SIGKILL；systemd / launchd 实例需 `--force`；没有记录时不向任何进程发信号        |
| `webmux bridge`                                   | 把本机套接字桥接到标准输入输出，供 Windows 客户端经 `wsl.exe --exec webmux bridge` 使用                                        |
| `webmux config path\|show\|validate\|check\|init` | 定位、查看生效配置（不含 Token）、校验（含 TOML 行列号）、以模板和随机 Token 初始化（已存在需 `--force`，原文件保留为 `.bak`） |

`webmux config check` 是 `config validate` 的别名；`webmux help` 显示全部命令。

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

## 服务管理命令

先将二进制放到稳定位置（源码构建可执行 `make install`），再以日常用户运行，不要使用 sudo：

```sh
webmux service install
webmux service status
webmux service logs            # 最近 100 行
webmux service logs --follow   # 持续查看，Ctrl-C 退出；也支持 -f
webmux service stop
webmux service start
webmux service restart
webmux service uninstall
```

- `install` 写入用户服务定义并立即启用、启动；缺少默认配置时自动创建带随机 token 的配置。显式 `--config PATH` 指定的文件必须已经存在且有效。
- 服务使用当前可执行文件和配置文件的绝对路径，不复制二进制。移动二进制后，需在新位置执行 `service install --force` 更新定义。
- 已有服务定义需要 `service install --force` 才能替换，并会重启服务。`service install --dry-run` 只输出服务定义，不写文件、不创建配置、不操作服务；可与 `--config` 配合使用。
- `stop`、`restart`、覆盖安装和卸载会结束当前所有终端会话，滚屏不会在重启后恢复。`uninstall` 保留二进制、配置、日志和状态目录。
- 安装会保存当前 PATH 中的绝对目录，并补充常见 agent 安装目录，只保存 PATH、shell、locale 和 `XDG_STATE_HOME` 等指定环境变量。更新 agent 安装路径后可重新执行 `install --force`。
- 服务工作目录为用户主目录；配置中的 `default_cwd`、`state_dir` 建议使用绝对路径或支持的 `~` 路径；自定义 shell/预设的可执行程序使用绝对路径或 PATH 中的命令名，避免依赖安装时的工作目录。
- Linux 需要可用的 systemd 用户管理器；macOS 需要已登录的 GUI 会话。服务启用成功不代表 HTTP 端口已成功监听，遇到端口冲突等启动失败时查看 `status` 和 `logs`。

`webmux help` 显示命令概览；`webmux service install --help` 等显示子命令参数。

## systemd 用户服务（Linux）

推荐使用 `webmux service install`，生成位置为 `$XDG_CONFIG_HOME/systemd/user/webmux.service`（默认 `~/.config/systemd/user/webmux.service`）。也可手工使用模板：

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

推荐使用 `webmux service install`，自动生成 `~/Library/LaunchAgents/com.webmux.plist` 并填写绝对路径，日志位于 `~/Library/Logs/webmux.log`。也可手工使用模板：

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

## Windows（WSL）

WebMux 不原生运行在 Windows 上，而是运行在 WSL 2 发行版中，由托盘程序 `webmux-tray.exe` 在 Windows 通知区域启动、监控和停止。托盘只是 WebMux 的客户端：配置解析、链接推导和停止都由 WSL 中的 `webmux` 完成。

### 前置条件

- Windows 10 22H2 及以上或 Windows 11（amd64 / arm64），无需管理员权限。
- 从 Microsoft Store 安装的 WSL 2：托盘依赖 `wsl.exe --version`、`--exec`、`--cd` 与 `WSL_UTF8`，系统自带的旧版 WSL 不支持。在 PowerShell 中执行 `wsl --update` 可升级。
- 至少一个已安装的发行版。`wsl.exe --version` 失败或没有发行版时，托盘图标显示异常，状态行提示“未检测到 WSL 发行版”。

### 在发行版中安装 webmux

最简单的方式是让托盘安装：WSL 中的 `webmux` 缺失、不兼容或版本低于托盘时，菜单出现“安装 / 升级 WSL 中的 webmux”。托盘在 Windows 侧下载与托盘同版本、与发行版架构（`uname -m`）匹配的 Linux 二进制（GitHub 无响应时依次改用 `ghfast.top`、`gh-proxy.com` 镜像），只有 SHA-256 与发布构建时写入托盘的摘要一致才会使用，再以 `install -D -m 0755` 放到托盘设置中的 `webmux` 路径。托盘从不在后台自动安装。要求发行版能通过 `/mnt/<盘符>` 访问 Windows 文件（`/etc/wsl.conf` 未关闭 automount）；本地 `make tray` 构建的托盘不带摘要，不提供安装。WebMux 运行中升级时旧进程继续运行，重启后生效。

也可以在发行版的终端中手动安装到 `~/.local/bin/webmux`（托盘默认路径），并生成带随机 token 的配置：

```sh
install -m 755 webmux-linux-amd64 ~/.local/bin/webmux # arm64 设备用 webmux-linux-arm64
webmux config init
```

也可以跳过 `config init`，之后在托盘菜单中点击“初始化配置（生成 Token）”。托盘只在配置文件不存在时提供该入口，从不改写已有配置；修改配置请用“打开 WebMux 配置文件”。缺少 webmux 时托盘停在错误状态且不定时重试；手动安装后重新打开托盘菜单即恢复。

### 放置托盘与登录启动

把 `webmux-tray-windows-amd64.exe`（或 `-arm64.exe`）放到固定位置（例如 `%LOCALAPPDATA%\Programs\WebMux\`）后双击运行。同一 Windows 用户只运行一个实例，再次启动只会让已有实例弹出状态通知。勾选菜单中的“登录时自动启动”会在 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` 中登记当前 exe 路径；移动 exe 后复选项变为未勾选，重新勾选即可。

托盘设置保存在 `%APPDATA%\WebMux\tray.json`（“编辑托盘设置”用记事本打开，保存后 2 秒内自动生效；文件无效时托盘以默认值运行并在状态行提示，不会覆盖该文件）：

| 字段          | 默认值                             | 说明                                                   |
| ------------- | ---------------------------------- | ------------------------------------------------------ |
| `distro`      | 空（系统默认发行版）               | 目标发行版，也可在“发行版”子菜单中选择                 |
| `mode`        | `auto`                             | 启动方式：`auto` / `tray`（托盘托管）/ `systemd`       |
| `webmux`      | `~/.local/bin/webmux`              | WSL 中的可执行文件                                     |
| `config`      | 空（WebMux 默认配置路径）          | WSL 中的配置文件，绝对路径或 `~/` 开头                 |
| `logFile`     | `~/.local/state/webmux/webmux.log` | 托盘托管方式下的服务日志                               |
| `startWebMux` | `true`                             | 托盘启动时自动启动 WebMux                              |
| `autoRestart` | `true`                             | 托盘托管方式下意外退出后自动重启（10 分钟内最多 5 次） |
| `forward`     | `[]`                               | 已勾选的转发地址，由“远程访问”子菜单维护               |

路径类设置在下次启动 WebMux 时生效；切换发行版或启动方式会先停止由托盘管理的实例（有会话时先确认）。托盘日志位于 `%LOCALAPPDATA%\WebMux\tray.log`（1 MiB 轮转），不记录 token。

### 两种启动方式

- **托盘托管**：托盘在发行版中以参数数组（不经 shell）运行 `<webmux> serve --log-file <logFile> [--config <config>]`，WebMux 把启动方记为 `windows-tray`；该进程独立于托盘，并保持发行版存活。意外退出时托盘通知并按退避重启。
- **systemd 用户服务**：发行版启用了 systemd 且已按上文“systemd 用户服务（Linux）”安装 `webmux.service` 时，托盘通过 `systemctl --user start|stop|restart webmux` 管理该单元，重启策略交给单元自身；托盘另外维持一个 `wsl.exe --exec sleep infinity` 保活进程，避免发行版因没有 WSL 客户端而被关闭，停止 WebMux 时一并结束。
- **自动**（默认）：每次启动前检测，满足 systemd 条件时用 systemd，否则托盘托管。

在 WSL 终端中手动运行的实例、或启动方与当前方式不符的实例显示为“外部实例”：托盘只提供访问类操作，不会停止或重启它。“仅退出托盘”后 WebMux 继续运行，重新打开托盘即恢复控制；Windows 注销或关机时托盘直接退出。

### localhost 转发诊断

托盘从 Windows 侧直接请求 `http://127.0.0.1:<端口>/api/health`（不带 token、不经系统代理）。WebMux 在 WSL 中健康但 Windows 无法访问时，图标显示异常，状态行提示检查 WSL 的 `localhostForwarding` / `networkingMode`。常见原因：`%USERPROFILE%\.wslconfig` 中设置了 `localhostForwarding=false`、VPN 或安全软件劫持了回环地址、WebMux 只监听了 WSL 内部地址。修改 `.wslconfig` 后执行 `wsl --shutdown` 使其生效（会结束所有会话）。

### 远程访问

NAT 网络模式（WSL 默认）下，局域网与 Windows 上 Tailscale 的流量不会进入 WSL，手机无法直接访问 WebMux。三种途径，按推荐顺序：

1. **mirrored 网络模式**：在 `.wslconfig` 的 `[wsl2]` 下设置 `networkingMode=mirrored`，WSL 与 Windows 共享网卡，按上文“手机访问（Tailscale）”配置即可。托盘的“远程访问”子菜单会显示“无需转发（镜像网络模式）”。
2. **在 WSL 内安装 Tailscale**：发行版获得自己的 Tailscale 地址，WebMux 直接监听该地址；状态报告了远程链接时托盘同样提示无需转发。
3. **托盘转发**：在“远程访问”子菜单中勾选 Windows 主机的地址（Tailscale 地址置顶），托盘在该地址与 WebMux 相同的端口上监听，并把 HTTP 与 WebSocket 原样转给本机链接。转发默认关闭，只在 WebMux 运行且健康、配置了 token、Windows 侧可达时启用，条件不满足时暂停、恢复后自动继续；只接受 `Host` 与 `Origin` 恰为该地址的请求，认证仍由 WebMux 完成。勾选的地址保存在 `tray.json` 中，下次启动自动恢复；端口被占用时该条目显示失败原因。转发随托盘退出而停止，需要常驻远程访问时请改用前两种途径。

首次在非回环地址上监听时，Windows 防火墙可能弹出授权提示（允许需要管理员确认）；托盘不会自动修改防火墙规则。只为可信网络（如 Tailscale）放行。

### 公网隧道

WebMux 运行时，菜单中的“公网隧道（Cloudflare）”在不重启、不影响会话的情况下开启或关闭 Cloudflare Tunnel（按 WebMux 配置的 `[tunnel]` 选择 Quick Tunnel 或 Named Tunnel，cloudflared 的定位与下载规则见下文“公网访问（Cloudflare Tunnel）”）。每次开启前托盘都会确认风险；要求 WebMux 已配置至少 43 个字符的 Token（`webmux config init` 生成的即可），否则拒绝开启。就绪后菜单显示公网地址，“复制公网链接”与其他复制链接一样带 Token。隧道断开时托盘通知一次且不自动重开；开关状态不保存，WebMux 重启后隧道关闭。

### Token 与剪贴板

托盘不在 Windows 侧保存 token。点击“复制 Token”时才经本机 API 从 WSL 读取，写入剪贴板后立即丢弃；剪贴板内容标记为不进入 Windows 剪贴板历史（Win+V）与跨设备云剪贴板，60 秒后若剪贴板仍是该 token 则自动清空（期间复制了其他内容则保持不变）。所有“复制…链接”在配置了 token 时同样当场读取 token，复制 `<链接>/#token=<token>`（片段不会发送到服务器，打开即登录），并按同样规则排除剪贴板历史、60 秒后自动清除；通知中只显示不含 token 的地址。未配置 token 时复制普通链接。

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

Windows 托盘经 `wsl.exe --exec` 启动 WebMux，同样不经过登录 shell；托盘启动的实例会在 `PATH` 前补上服务模板中的 `~/.local/bin`、`~/.local/share/pnpm`、`~/.npm-global/bin`（已存在的保持原位）。安装在其他位置的 agent 请改用 systemd 启动方式并按上面添加 drop-in，或把它链接到 `~/.local/bin`。

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
