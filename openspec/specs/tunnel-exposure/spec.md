# tunnel-exposure Specification

## Purpose
通过 Cloudflare Tunnel 把单用户 webmux 服务以 HTTPS 安全暴露到公网，并保证隧道入口始终受 Token 保护、可通过终端打印的链接与二维码快速访问。

## Requirements

### Requirement: 启用隧道

系统 SHALL 在 TOML 配置 `[tunnel] enabled = true` 或启动参数 `--tunnel` 存在时启用隧道。`--tunnel` 优先于配置文件。未启用隧道时，系统 MUST NOT 获取、下载或启动 cloudflared。

#### Scenario: 通过启动参数启用

- **WHEN** 配置文件未启用隧道，用户以 `webmux --tunnel` 启动
- **THEN** 系统启用隧道并启动 cloudflared

#### Scenario: 默认不启用

- **WHEN** 配置文件没有 `[tunnel]` 节，且启动时未带 `--tunnel`
- **THEN** 系统不查找、不下载、不启动 cloudflared，行为与引入本能力前一致

### Requirement: 隧道模式选择

`tunnel.token` 为空时，系统 SHALL 使用 Quick Tunnel，公网地址由 cloudflared 分配。`tunnel.token` 非空时，系统 SHALL 使用 Named Tunnel，公网地址为 `https://<tunnel.hostname>`。`tunnel.hostname` MUST 是不含协议、端口、路径或凭据的纯主机名，MUST 为 ASCII（IDN 使用 punycode 形式），MUST NOT 含通配符；末尾的点会被去掉。Named Tunnel 缺少合法主机名时，系统 MUST 拒绝启动。

#### Scenario: Quick Tunnel

- **WHEN** 启用隧道且未配置 `tunnel.token`
- **THEN** 系统启动 Quick Tunnel，并把 cloudflared 报告的 `https://*.trycloudflare.com` 地址作为公网地址

#### Scenario: Named Tunnel

- **WHEN** 启用隧道，且配置了 `tunnel.token` 与 `tunnel.hostname = "term.example.com"`
- **THEN** 系统用该隧道凭据启动 cloudflared，公网地址为 `https://term.example.com`

#### Scenario: Named Tunnel 缺少域名

- **WHEN** 启用隧道，配置了 `tunnel.token`，但 `tunnel.hostname` 为空或不是纯主机名（如 `https://term.example.com/x`）
- **THEN** 系统输出配置错误并以非零状态退出，不开始监听

#### Scenario: 无法匹配 Host 的域名

- **WHEN** `tunnel.hostname` 含通配符（如 `*.example.com`）或非 ASCII 字符（如 `终端.example.com`）
- **THEN** 系统输出配置错误并以非零状态退出，提示使用 Cloudflare 转发的具体 ASCII/punycode 主机名

### Requirement: 隧道凭据保密

系统 MUST NOT 把 `tunnel.token` 写入日志，MUST NOT 通过任何 HTTP/WebSocket 响应返回它，也 MUST NOT 把它作为子进程命令行参数传递，以免被同机其他用户从进程列表读取。

#### Scenario: 进程列表不含凭据

- **WHEN** Named Tunnel 运行中，同机用户查看 cloudflared 进程的命令行
- **THEN** 命令行中不出现 `tunnel.token` 的值

#### Scenario: 配置接口不含凭据

- **WHEN** 已认证客户端请求 `GET /api/config`
- **THEN** 响应中不包含 webmux Token，也不包含隧道凭据

### Requirement: 获取 cloudflared

系统 SHALL 按以下顺序确定 cloudflared 可执行文件：`tunnel.cloudflared` 显式路径、`PATH` 中的 `cloudflared`、`state_dir` 下 webmux 固定版本且 SHA-256 校验通过的缓存。都不可用时，系统 SHALL 按“下载来源回退”下载该固定版本的 Cloudflare 官方 Release 产物，并且无论来自哪个来源，都必须校验通过才缓存并执行。

#### Scenario: 使用 PATH 中的 cloudflared

- **WHEN** 未配置显式路径，且 `PATH` 中存在可执行的 `cloudflared`
- **THEN** 系统直接使用它，不发起下载

#### Scenario: 显式路径无效

- **WHEN** `tunnel.cloudflared` 指向不存在或不可执行的文件
- **THEN** 系统报错并以非零状态退出，不回退到 `PATH`、缓存或下载

#### Scenario: 首次自动下载

- **WHEN** 没有显式路径，`PATH` 中没有 cloudflared，`state_dir` 中也没有有效缓存
- **THEN** 系统下载与当前操作系统和架构对应的固定版本，校验 SHA-256 后写入 `state_dir` 并执行
- **AND** 下次启动直接复用该缓存，不再下载

#### Scenario: 单个来源校验失败

- **WHEN** 某个下载来源返回内容的 SHA-256 与内置值不一致
- **THEN** 系统丢弃该内容，不写入缓存、不执行，并改用下一个下载来源

#### Scenario: 缓存被篡改

- **WHEN** `state_dir` 中缓存的 cloudflared 与内置 SHA-256 不一致
- **THEN** 系统不执行该文件，而是重新下载并校验

#### Scenario: 不支持的平台

- **WHEN** 当前操作系统或架构没有内置的下载项，且没有显式路径和 `PATH` 可用
- **THEN** 系统报错说明需要自行安装 cloudflared，并以非零状态退出

### Requirement: 下载来源回退

系统 SHALL 先从 GitHub 直连下载 cloudflared，失败后按顺序改用镜像，镜像下载地址为 `<前缀>/https://github.com/<原路径>`。内置默认镜像为 `https://ghfast.top`、`https://gh-proxy.com`。配置了 `tunnel.download_mirrors` 时，该列表完整替换默认镜像，空数组表示只用 GitHub。以下情况视为当前来源失败：连接或响应头超过 10 秒、连续 20 秒未收到数据、非 2xx 状态、内容超出大小上限、校验失败。

#### Scenario: GitHub 不可达时回退到镜像

- **WHEN** GitHub 直连在 10 秒内没有返回响应头，第一个镜像返回正确内容
- **THEN** 系统使用镜像的内容，校验通过后缓存并执行，日志记录 GitHub 失败原因和最终成功的来源

#### Scenario: GitHub 下载中途停滞

- **WHEN** GitHub 已开始返回数据，但随后连续 20 秒没有新数据
- **THEN** 系统放弃该来源，丢弃已收到的部分，从下一个镜像重新下载

#### Scenario: GitHub 可用时不访问镜像

- **WHEN** GitHub 直连下载成功且校验通过
- **THEN** 系统不向任何镜像发起请求

#### Scenario: 配置覆盖镜像列表

- **WHEN** 配置 `tunnel.download_mirrors = ["https://mirror.example"]`，且 GitHub 不可用
- **THEN** 系统只尝试 `https://mirror.example/https://github.com/...`，不尝试内置默认镜像

#### Scenario: 禁用镜像

- **WHEN** 配置 `tunnel.download_mirrors = []`，且 GitHub 不可用
- **THEN** 系统不访问任何镜像，报错并以非零状态退出

#### Scenario: 所有来源失败

- **WHEN** GitHub 与全部镜像都失败
- **THEN** 系统报错，列出每个来源及其失败原因，提示可以手动安装 cloudflared 或配置 `tunnel.cloudflared`，并以非零状态退出

#### Scenario: 非法镜像前缀

- **WHEN** `tunnel.download_mirrors` 中有某项不是 `https://` 开头、或包含查询参数、片段、凭据
- **THEN** 系统输出配置错误并以非零状态退出，不开始监听

### Requirement: 隧道模式强制 Token

启用隧道时，系统 MUST 要求所有 `/api/` 与 `/ws/` 请求携带有效 Token。若配置和启动参数都没有提供 Token，系统 SHALL 在每次启动时生成至少 256 位熵、URL 安全编码的随机 Token，用于本次进程生命周期。若显式提供了 webmux Token，其长度 MUST 不少于 43 个字符（与 256 位 Token 的自动生成形式等长）；否则系统 MUST 拒绝启动，并提示留空自动生成或使用 `openssl rand` 创建。

#### Scenario: 自动生成 Token

- **WHEN** 启用隧道，且配置与 `--token` 均为空
- **THEN** 系统生成随机 Token，未携带该 Token 的 API 请求返回 401

#### Scenario: 使用已配置 Token

- **WHEN** 启用隧道且配置了 43 个字符以上的 `token`
- **THEN** 系统使用该 Token，不生成新 Token

#### Scenario: 过短的已配置 Token

- **WHEN** 启用隧道且配置了 `token = "mine"`
- **THEN** 系统输出配置错误并以非零状态退出，不开始监听

#### Scenario: 重启后生成新 Token

- **WHEN** 未配置 Token 的隧道模式 webmux 重启
- **THEN** 新进程使用不同的随机 Token，旧 Token 返回 401

### Requirement: 隧道公网地址通过 Host 与 Origin 检查

隧道公网地址确定后，系统 SHALL 接受 Host 为该公网主机名的请求，以及 Origin 恰好为 `https://<公网主机名>` 的请求，即使到达 webmux 的连接本身是明文 HTTP。其他未授权的 Host 和 Origin MUST 继续被拒绝。

#### Scenario: 经隧道打开页面并调用 API

- **WHEN** 浏览器通过 `https://abc.trycloudflare.com/` 打开 webmux，并携带有效 Token 发起 `POST /api/sessions`，Origin 为 `https://abc.trycloudflare.com`
- **THEN** 请求通过 Host 与 Origin 检查并被正常处理

#### Scenario: 经隧道建立 WebSocket

- **WHEN** 浏览器从隧道公网页面连接 `wss://abc.trycloudflare.com/ws/sessions` 并携带有效 Token
- **THEN** WebSocket 握手成功并收到会话快照

#### Scenario: 公网地址的 http Origin 被拒绝

- **WHEN** 请求的 Host 为隧道公网主机名，但 Origin 为 `http://abc.trycloudflare.com`
- **THEN** 写请求返回 403

#### Scenario: 无关主机仍被拒绝

- **WHEN** 隧道运行中，请求的 Host 为未配置的 `evil.example`
- **THEN** 请求返回 403

### Requirement: 就绪后打印公网访问地址与二维码

隧道与 Cloudflare 边缘建立首个连接并确定公网地址后，系统 SHALL 在标准输出打印一次公网访问 URL，格式为 `https://<公网主机名>/#token=<URL 编码的 Token>`，并紧接着打印编码同一 URL、可被手机扫描的终端字符二维码。

#### Scenario: Quick Tunnel 就绪

- **WHEN** Quick Tunnel 建立连接并报告 `https://abc.trycloudflare.com`
- **THEN** 标准输出出现 `https://abc.trycloudflare.com/#token=<token>` 与对应二维码，扫描二维码得到同一 URL

#### Scenario: 隧道启动超时

- **WHEN** cloudflared 启动后 60 秒内仍未建立连接或报告公网地址
- **THEN** 系统记录错误日志，此时不打印公网 URL 和二维码，本地服务继续运行

#### Scenario: 超时后仍建立连接

- **WHEN** 超时错误日志之后，cloudflared 才报告注册成功或 Quick Tunnel 地址
- **THEN** 系统仍打印公网 URL 与二维码，并把该公网地址加入 Host/Origin 白名单

### Requirement: cloudflared 生命周期

cloudflared 子进程 SHALL 在 webmux 关闭时终止。cloudflared 意外退出时，系统 SHALL 记录错误并撤销其公网地址的 Host/Origin 授权，但 MUST NOT 终止本地服务或现有会话。

#### Scenario: webmux 正常关闭

- **WHEN** webmux 收到 SIGTERM
- **THEN** cloudflared 子进程在 webmux 退出前终止，不残留

#### Scenario: cloudflared 意外退出

- **WHEN** 运行中的 cloudflared 进程被外部终止
- **THEN** webmux 记录错误日志，本地监听地址上的会话继续可用，经原公网主机名到达的请求返回 403

#### Scenario: cloudflared 启动失败

- **WHEN** cloudflared 立即以非零状态退出（例如 Named Tunnel 凭据无效）
- **THEN** webmux 记录包含退出状态的错误，本地服务继续运行
