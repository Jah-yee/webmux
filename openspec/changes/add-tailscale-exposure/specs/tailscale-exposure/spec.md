## Purpose

让部署者在 Tailscale 网络中直接获得自动证书的 HTTPS：Serve 仅 tailnet 内可达，Funnel 可显式公开到互联网。系统负责只读检测、配置驱动的托管、精确目标的所有权与启动对账，并正确处理 WSL 中 Tailscale 运行在 Windows 主机这一特殊情况；除无法清理本实例遗留入口的失败关闭外，Tailscale 故障不阻止本地 HTTP 与已有会话。

## ADDED Requirements

### Requirement: 只读检测 Tailscale 运行环境

系统 SHALL 提供需要认证的 `GET /api/tailscale`，返回脱敏的检测与运行状态：provider（`linux`、`wsl-native`、`windows`、`none`）、后端状态（运行中、已停止、需要登录、未安装、不可确定）、节点 DNS 名、HTTPS 证书是否就绪、Funnel 是否可用或未知、配置的模式与端口、当前运行状态、公开 URL、阻塞原因，以及现有 Serve/Funnel 入口的 host、port 与是否公开。配置侧的 provider 只有 `auto`、`linux`、`windows`；检测结果用 `wsl-native` 区分 WSL 内原生 Tailscale，它对应配置值 `linux`。检测 SHALL 只执行 `tailscale status --json`、`tailscale serve status --json` 及必要的 `tailscale funnel status --json`，SHALL NOT 执行 `serve`、`funnel`、`set` 或任何 `reset`。

检测 SHALL 使用短超时、无交互 stdin、并发合并与短缓存；CLI 缺失、超时、输出损坏或字段缺失时 SHALL 返回可解释的状态，不得 panic。响应 MUST NOT 包含 WebMux Token、其他 Serve 入口的目标路径或未脱敏的 CLI 输出。WSL 环境 SHALL 额外返回网络模式与 Windows provider 是否被检测到。

#### Scenario: Linux 上检测到可用 Tailscale

- **WHEN** 主机安装了 `tailscale`，`BackendState` 为 `Running`，`Self.DNSName` 为 `node.tailnet.ts.net.`，`CertDomains` 包含同一主机名
- **THEN** `GET /api/tailscale` 报告 provider=`linux`、运行中、DNS 名 `node.tailnet.ts.net`、HTTPS 就绪，并返回配置模式与现有入口

#### Scenario: 未安装或未登录

- **WHEN** `tailscale` 不存在，或 `BackendState` 为 `NeedsLogin`
- **THEN** 检测报告 `none`/`needs-login` 与可操作原因，不执行任何写命令，设置页不显示“已就绪”

#### Scenario: 检测不修改配置

- **WHEN** 用户打开设置页并多次刷新检测
- **THEN** 现有 Serve/Funnel 配置、端口、`AllowFunnel` 与用户其他入口保持不变，没有新增或删除任何入口

#### Scenario: 输出损坏或超时

- **WHEN** CLI 返回非 JSON、缺少 `Self`，或在超时内没有结束
- **THEN** 接口返回 `error`/`unavailable` 状态与原因，不把缺失字段当成已就绪，也不派生第二个并发调用

### Requirement: Tailscale 配置与模式

系统 SHALL 通过 `[tailscale]` 配置托管行为：`mode` 为 `off`、`serve` 或 `funnel`，默认 `off`；`port` 默认 `0` 表示自动，Serve 可显式使用 `1-65535`，Funnel 只能使用 `0`、`443`、`8443` 或 `10000`；`provider` 为 `auto`、`linux` 或 `windows`，默认 `auto`；可选 `command` 指定 CLI 路径。非法 `mode`、当前模式不支持的端口或非法 provider 时，系统 MUST 在启动时拒绝配置并以非零状态退出。

`mode != "off"` SHALL 被视为网络入口：Token 留空时系统 SHALL 生成本次进程的 256 位随机 Token；显式配置的 Token 少于 43 字符时 MUST 拒绝启动；Funnel 与 Serve 使用相同的强 Token 规则。配置合法但运行期前置条件不满足时，系统 SHALL NOT 因此停止本地 HTTP 服务，而是把 Tailscale 状态标为 `failed`。非 WSL 环境配置 `provider = "windows"` MUST 被视为配置错误。

`mode = "off"` 时系统 MUST NOT 创建新入口或修改未知入口，也不创建新的 Tailscale 状态文件；若状态文件记录了本实例崩溃前创建的入口，系统 SHALL 按所有权与对账要求在启动前移除该精确目标。没有记录且没有匹配入口时，系统 MUST NOT 调用任何会修改 Tailscale 的命令。

#### Scenario: 默认关闭

- **WHEN** 配置中没有 `[tailscale]` 或 `mode = "off"`，且状态目录没有 Tailscale 记录
- **THEN** 系统不执行 Tailscale Serve/Funnel，不创建 `tailscale.json`，本地访问与现有 Cloudflare 行为不变

#### Scenario: 非法模式或端口

- **WHEN** 配置 `mode = "public"`，或 `mode = "funnel"` 且 `port = 9000`，或任何模式使用 `port = 70000`
- **THEN** `webmux config validate` 与启动都返回可定位的错误，WebMux 不开始监听

#### Scenario: 空 Token 自动生成

- **WHEN** `mode = "serve"` 且配置 Token 为空
- **THEN** 系统生成本次进程 Token，未携带该 Token 的 API/WebSocket 请求返回 401

#### Scenario: 显式短 Token

- **WHEN** `mode = "funnel"` 且配置了少于 43 字符的 Token
- **THEN** 系统输出强 Token 要求并以非零状态退出，不开始监听，也不执行 Funnel

#### Scenario: 运行期失败不阻止本地服务

- **WHEN** 配置 `mode = "serve"`，启动时 Tailscale 未运行，且没有本实例遗留入口
- **THEN** 本地 HTTP 继续服务，Tailscale 状态为 `failed` 并说明原因，已有会话不受影响

### Requirement: 托管 Tailscale Serve

`mode = "serve"` 时，系统 SHALL 在监听开始后使用 argv（不经过 shell）执行 `tailscale serve --bg --https=<port> http://127.0.0.1:<实际端口>`，其中目标端口 MUST 来自实际监听端口。Serve 的 target MUST 是 IPv4 回环；`listen` 为 `[::1]`、仅特定网卡地址或不覆盖 IPv4 回环时，系统 MUST 拒绝托管并提示把 `listen` 改为 `127.0.0.1:<port>` 或 `0.0.0.0:<port>`。

执行前系统 SHALL 要求 `CertDomains` 包含节点 DNS 名；`CertDomains` 为空时 MUST NOT 执行会等待证书的 Serve 命令，而应失败并提示在 Tailscale 管理台启用 HTTPS。端口已有入口时，只有状态记录中属于本次实例、且精确匹配当前 target、端口并仅有一个 `/` handler 的入口可以复用；没有记录或其他入口 MUST NOT 被覆盖。`tailscale.json` 是唯一的所有权依据。`port = 0` 时 SHALL 按 443、8443、10000 选择第一个没有他人入口的端口；三者都被占用时 MUST 失败并提示在 Serve 下显式指定其他空闲端口。显式端口冲突 MUST 失败，不静默换端口。

命令成功退出后，系统 SHALL 回读 `serve status --json`，要求目标端口存在指向当前 target 的 `/` handler 且 `AllowFunnel` 不为真；确认后才 SHALL 把 `https://<dns>`（443）或 `https://<dns>:<port>`（其他端口）登记为受权来源并报告 `ready`。停止、失败或切换模式前 SHALL 先撤销来源，再只对本次记录执行 `tailscale serve --https=<port> off`（需要时先关闭 Funnel）。系统 MUST NOT 调用 `tailscale serve reset`。

#### Scenario: 首次开启 Serve

- **WHEN** `listen = "127.0.0.1:7681"`、`mode = "serve"`、HTTPS 证书已就绪，且 443 没有他人入口
- **THEN** 系统执行 Serve 命令，回读确认后状态为 `ready`，`https://node.tailnet.ts.net/` 可在 tailnet 内打开，普通 HTTP 本地访问与已有会话不受影响

#### Scenario: 443 被他人占用

- **WHEN** `port = 0` 且 443 已有不属于本实例的 Serve 入口，8443 空闲
- **THEN** 系统选择 8443，使用 `https://node.tailnet.ts.net:8443/`，不修改 443 的入口

#### Scenario: 监听地址不能满足回环代理

- **WHEN** `listen = "100.64.0.5:7681"` 或 `listen = "[::1]:7681"`，127.0.0.1 实际不可达
- **THEN** 系统不执行 Serve 命令，状态为 `failed` 并提示改为 `127.0.0.1:<port>` 或 `0.0.0.0:<port>`，本地现有监听与会话不受影响

#### Scenario: HTTPS 证书未启用

- **WHEN** `CertDomains` 为空
- **THEN** 系统不执行 `serve --https`，状态为 `failed`，提示先在 Tailscale 管理台启用 HTTPS，本地 HTTP 继续运行

#### Scenario: 命令成功但回读不匹配

- **WHEN** Serve 命令退出码为 0，但 `serve status --json` 没有目标 handler 或 `AllowFunnel` 为真
- **THEN** 系统撤销来源、状态为 `failed`，不把该端口当作已就绪

#### Scenario: 停止只移除精确目标

- **WHEN** Serve 运行中，用户停止 WebMux
- **THEN** 系统先撤销来源，再执行 `tailscale serve --https=<port> off`；同一 tailscaled 上其他端口的入口保持不变，且没有执行 `serve reset`

### Requirement: 托管 Tailscale Funnel

`mode = "funnel"` 时，系统 SHALL 在监听开始后使用 argv 执行 `tailscale funnel --bg --https=<port> http://127.0.0.1:<实际端口>`，端口只能是 443、8443 或 10000，默认 443。Funnel MUST 由显式配置开启，系统 MUST NOT 因为检测到 Tailscale 或之前运行过 Serve 就自动公开。配置为 Funnel 时 MUST 满足强 Token 要求。

执行前系统 SHALL 校验 HTTPS 证书、端口与已有入口；缺少证书或端口冲突时直接失败。命令失败时 SHALL 把 stderr 映射为缺少节点 `funnel` 属性、权限不足、HTTPS 未启用或端口冲突的可操作原因，并指向 Tailscale 管理台或 Windows operator 设置。命令成功不等于生效：系统 SHALL 回读 `serve status --json`，要求目标 handler 指向当前 target 且 `AllowFunnel[<dns>:<port>] == true`，确认后才登记公开来源并报告 `ready`。

从 Serve 切换到 Funnel、从 Funnel 切换到 Serve 或端口变化时，系统 SHALL 先关闭旧记录的精确目标（按需执行 `tailscale funnel --https=<旧端口> off` 与 `tailscale serve --https=<旧端口> off`），再应用新目标，避免旧端口继续公开。Funnel 启动失败或切换失败时，系统 SHALL 撤销公开来源，并尝试移除本次记录中可能残留的公开入口；无法确认移除时 MUST 保持 `failed` 并给出人工命令，不得报告 `ready`。系统 MUST NOT 调用 `tailscale funnel reset`。

#### Scenario: 显式开启公网 Funnel

- **WHEN** `mode = "funnel"`、`port = 443`、强 Token 已配置，Tailscale 节点具备 Funnel 权限
- **THEN** 系统执行 Funnel 命令，回读 `AllowFunnel` 为 true 后报告 `ready`，`https://node.tailnet.ts.net/` 可从公网访问，设置页持续显示公网风险

#### Scenario: 缺少 Funnel 权限

- **WHEN** 节点没有 `funnel` 属性
- **THEN** 命令失败，系统报告需要访问 Tailscale 管理台启用 Funnel 节点属性，不登记公开来源，本地 HTTP 继续运行

#### Scenario: 从 Funnel 切回 Serve

- **WHEN** 已记录的 Funnel 入口在 443，新配置改为 `mode = "serve"`
- **THEN** 系统先关闭 443 的 Funnel 与 Serve 精确目标，再按 Serve 重新应用；旧公网 URL 不再公开

#### Scenario: 失败不残留本次公开入口

- **WHEN** 启动时配置为 Funnel，但端口冲突导致无法应用
- **THEN** 系统确保本次记录中的公开入口已关闭或报告无法关闭，状态为 `failed`，不把旧入口继续当作本实例的公开入口

### Requirement: WSL 中由 Windows 承载 Tailscale

在 WSL 中，系统 SHALL 区分 WSL 内原生 Tailscale 与 Windows 主机 Tailscale。Windows provider MUST 通过显式 `provider = "windows"` 选择；`provider = "auto"` 只 SHALL 检测并展示 Windows Tailscale，MUST NOT 对 Windows 执行 `serve`、`funnel`、`set` 或 `reset`。同时检测到两者时，`auto` SHALL 优先使用正在运行的 WSL 原生 Tailscale，并明确展示 Windows 节点未托管。

使用 Windows provider 托管时，系统 SHALL 要求 `wslinfo --networking-mode` 为 `mirrored`。网络模式为 `nat` 或无法确定时，系统 MUST 失败关闭，不执行任何修改 Windows Tailscale 的命令，也不猜测 Windows 回环转发；错误与设置页 SHALL 提供三种替代：把 `.wslconfig` 改为 mirrored（并说明 `wsl --shutdown` 会结束会话）、在 WSL 内安装 Tailscale 并使用 `provider = "linux"`、或继续使用 Windows 托盘的 Tailscale 转发并手工配置 Serve。系统 MUST NOT 自动修改 `.wslconfig`、重启 WSL 或修改 Windows 防火墙。

Windows CLI 权限不足时，系统 SHALL 提示在 Windows 管理员终端执行一次 `tailscale set --operator=<Windows 用户>`；MUST NOT 在 WSL 内使用 `sudo` 或自动提权。URL、Host 与 Origin MUST 使用 Windows 节点 `Self.DNSName`，不得使用 WSL 主机名或 IP。Windows 的 `--bg` 入口跨 WSL 关闭与重启保留，系统 SHALL 在下次启动时按状态文件对账；WSL 关闭时无法清理的入口 MUST 在文档中说明并给出精确移除命令。

#### Scenario: WSL mirrored 上的 Windows provider

- **WHEN** WSL 内没有运行中的 tailscaled，Windows `tailscale.exe` 报告运行中，`wslinfo --networking-mode` 为 `mirrored`，配置 `provider = "windows"` 与 `mode = "serve"`
- **THEN** 系统使用 Windows 节点的 DNS 名执行 Serve，设置页显示 provider=`windows`，URL 指向 Windows 节点

#### Scenario: WSL NAT 阻塞托管

- **WHEN** 同一环境为 `nat`
- **THEN** 系统状态为 `failed`，不执行任何 Windows Serve/Funnel 命令，说明需要 mirrored 或改用 WSL 内 Tailscale/托盘转发，本地 HTTP 与会话正常

#### Scenario: 未显式选择 Windows

- **WHEN** WSL 中只有 Windows Tailscale 可用，`provider = "auto"`，`mode = "serve"`
- **THEN** 系统只报告检测结果并失败，提示显式设置 `provider = "windows"`，不修改 Windows 配置

#### Scenario: Windows 权限不足

- **WHEN** Windows CLI 返回 access denied 或 operator 权限错误
- **THEN** 系统显示在 Windows 管理员终端设置 operator 的指引，不尝试 sudo，不把状态误报为 ready

#### Scenario: WSL 关闭后残留

- **WHEN** Serve 运行中 WSL 被关闭，Windows 上的 `--bg` 入口保留；之后 WSL 重新启动且配置端口改变
- **THEN** 系统只移除状态文件中记录的旧精确目标，再应用新目标，不删除 Windows 上其他应用或发行版的入口

#### Scenario: 两个 provider 同时可用

- **WHEN** WSL 内 Tailscale 与 Windows Tailscale 都可用，`provider = "auto"`
- **THEN** 系统优先使用 WSL 内正在运行的 Tailscale，同时展示 Windows 节点可用但未托管；要使用 Windows 必须显式配置

### Requirement: 端口感知的 Host 与 Origin 授权

系统 SHALL 按来源维护多个 HTTPS 来源；Cloudflare 与 Tailscale 可以同时存在，撤销一个来源 MUST NOT 清除另一个。每个来源 SHALL 保存精确的 origin 与 authority：443 的 authority 为 `<dns>`，其他端口的 authority 为 `<dns>:<port>`。Host 检查 MUST 比较完整 authority；Origin 检查 MUST 要求 `Origin` 精确等于该来源的 origin 且 `r.Host` 精确等于对应 authority。系统 MUST NOT 接受同一 DNS 名但不同端口的 Host/Origin，也 MUST NOT 信任 `X-Forwarded-Host` 或 `X-Forwarded-Proto`。

Tailscale 来源 SHALL 只在命令成功并由 serve 配置回读确认后登记；停止、失败或模式切换前 SHALL 先撤销，之后旧公网请求返回 403。托管 Tailscale 时，用户不需要把 ts.net 地址写入 `allowed_origins`；手工部署仍可使用 `allowed_origins`，但必须写完整 origin（含非 443 端口）。

#### Scenario: 443 上的 Origin

- **WHEN** 浏览器从 `https://node.tailnet.ts.net/` 打开并通过 Serve 发起写请求，Origin 为 `https://node.tailnet.ts.net`
- **THEN** Host 与 Origin 检查通过，请求正常处理

#### Scenario: 8443 上的 Origin

- **WHEN** 浏览器从 `https://node.tailnet.ts.net:8443/` 打开，Host 为 `node.tailnet.ts.net:8443`，Origin 为 `https://node.tailnet.ts.net:8443`
- **THEN** 请求通过；若 Origin 为 `https://node.tailnet.ts.net` 或 Host 缺少 `:8443`，请求返回 403

#### Scenario: 同一主机名的错误端口

- **WHEN** Host 为 `node.tailnet.ts.net`，但 Origin 为 `https://node.tailnet.ts.net:8443`
- **THEN** 请求返回 403，不能因为主机名相同而放行

#### Scenario: 撤销后立即失效

- **WHEN** Serve 失败或停止
- **THEN** 系统先撤销 Tailscale 来源，旧 ts.net Host/Origin 的请求立即返回 403，Cloudflare 来源（若有）仍可用

#### Scenario: 并存来源

- **WHEN** Cloudflare Named Tunnel 与 Tailscale Serve 同时运行
- **THEN** 两个精确 origin 都通过检查；停止其中一个只撤销该来源，另一个继续工作

### Requirement: 所有权、对账与失败关闭

系统 SHALL 在 `<state_dir>/tailscale.json` 记录本次实例的入口意图与所有权：版本、provider、节点 DNS 名、端口、模式、target，以及 `pending`/`active` 状态。系统 MUST 在执行会修改 Tailscale 的命令前写入 `pending` 意图并落盘，命令与回读成功后标记 `active`；这样命令成功后、标记前崩溃也能在下次启动识别该入口属于本实例。文件 SHALL 使用 0600、原子替换与独立 flock。文件损坏、不可读或锁冲突时，系统 MUST 失败关闭：不删除任何入口、不应用新目标；没有入口指向当前监听端口与 target 时本地 HTTP 继续并报告存储故障，有匹配入口时按启动清理规则拒绝启动。

启动时系统 SHALL 在配置与 Token 校验通过后、绑定监听前先清理再监听：若记录中的本实例入口在新配置下应关闭（`mode = "off"`）或模式/端口/target 已变化，系统 MUST 在绑定监听前关闭该精确目标；无法关闭时 MUST 拒绝启动并给出精确手工命令，避免旧入口在新进程开始监听后继续暴露。若状态文件损坏或缺失，但 serve 配置中存在指向当前监听端口与 target 的入口，系统 MUST 视为所有权未知并拒绝启动。清理完成或确认入口可复用后才绑定监听，然后用当前 serve 配置对账：`pending` 或 `active` 记录中的入口缺失则重建；记录存在且入口精确匹配时可以复用；存在无记录或不匹配的入口时 MUST 失败关闭，不覆盖、不删除。若保留的匹配入口在绑定监听时失败（例如端口被其他进程占用），系统 MUST 在退出前关闭本次记录的入口；无法关闭时 MUST 报告公开风险与精确手工命令，避免旧入口指向另一个进程。

`tailscale.json` 是唯一的所有权依据，系统 MUST NOT 仅凭 target 相同就把用户手工创建的入口当作自己的。命令、回读或状态文件故障时 SHALL 进入 `failed` 并撤销来源，不自动重试。命令或回读失败后，系统 SHALL 只尝试移除与 `pending` 记录 target/端口精确匹配的入口；不匹配的冲突入口必须保持不动。清理成功则删除或更新记录，清理失败则保留 `pending` 供下次启动对账。系统 MUST NOT 结束或重启 tailscaled，MUST NOT 把 `--bg` 入口当作子进程，MUST NOT 使用 `serve reset` 或 `funnel reset`。

#### Scenario: 崩溃后入口仍在

- **WHEN** WebMux 在 Serve 运行中崩溃，`--bg` 入口仍指向旧端口；之后 WebMux 重启
- **THEN** 启动对账按状态记录识别该入口，按新 target/端口修正，不调用 reset，最终状态为 `ready` 或带原因的 `failed`

#### Scenario: 命令成功后、标记前崩溃

- **WHEN** Serve 命令成功，状态文件已写入 `pending` 但尚未标记 `active` 时进程崩溃；之后 WebMux 重启
- **THEN** 启动对账把 `pending` 记录视为本次实例的入口并复用或修正，不因为缺少 `active` 标记而删除或忽略

#### Scenario: 关闭模式清理旧入口

- **WHEN** 上一次运行创建了 Serve 或 Funnel 入口，本次配置为 `mode = "off"`，状态文件仍有记录
- **THEN** 系统在绑定监听前关闭该精确目标；成功后才开始监听，旧 ts.net URL 不再可达，本地 HTTP 正常

#### Scenario: 无法清理旧入口

- **WHEN** `mode = "off"` 或模式/端口已变化，状态文件中的旧入口无法关闭
- **THEN** 系统拒绝启动并输出精确的 `funnel ... off` / `serve ... off` 命令，不在旧入口仍指向本进程时开始监听

#### Scenario: 绑定监听失败

- **WHEN** 启动时保留了一个匹配的 Serve/Funnel 入口，但随后监听端口被其他进程占用，WebMux 无法绑定
- **THEN** 系统在退出前关闭本次记录的入口；无法关闭时明确报告公开风险与手工命令，不让旧入口指向另一个进程

#### Scenario: 状态文件损坏

- **WHEN** `tailscale.json` 内容损坏或锁被另一个实例持有，且 serve 配置中没有指向当前监听端口与 target 的入口
- **THEN** 系统不删除未知入口、不应用新目标，状态报告存储故障，本地 HTTP 与会话继续

#### Scenario: 状态损坏且入口指向当前端口

- **WHEN** `tailscale.json` 内容损坏，但 serve 配置中有入口指向当前监听端口与 target
- **THEN** 系统拒绝启动并报告所有权未知，要求修复状态文件或手工移除精确目标，避免意外暴露

#### Scenario: 状态文件丢失但入口精确匹配

- **WHEN** 状态文件不存在，serve 配置中目标端口只有一个指向当前 target 的 `/` handler
- **THEN** 系统仍失败关闭并报告所有权未知，不删除或接管该入口；用户需要恢复状态文件或手工移除精确目标后再启动

#### Scenario: 端口已有其他入口

- **WHEN** 目标端口已存在指向不同 target 或有多个 handler 的入口
- **THEN** 系统状态为 `failed`，保留该入口，显示 host/port/public 与人工处理提示，不覆盖

#### Scenario: 状态文件损坏时退出

- **WHEN** 状态文件损坏，WebMux 随后退出
- **THEN** 系统不会删除任何入口；下次启动仍按失败关闭处理，直到用户修复或删除状态文件，并在需要时手工移除精确目标

### Requirement: 设置页状态与风险说明

设置页 SHALL 提供 Tailscale 卡片与 `/settings/tailscale` 详情页，展示检测状态、provider、节点 DNS 名、HTTPS 就绪、Funnel 可用性、配置模式、端口、公开 URL、运行状态与阻塞原因。页面 SHALL 明确区分 `Serve`（仅 tailnet 内设备可访问）与 `Funnel`（公网 HTTPS，任何拿到链接与 Token 的人都能执行命令），并在 Funnel 模式下持续显示公网风险。

页面 SHALL 明确 Tailscale 由 `[tailscale]` 配置管理，修改后重启生效；本变更 MUST NOT 提供保存、启用、关闭或恢复默认的写控件。失败状态 SHALL 说明修复 Tailscale 后需要重启 WebMux 才会重新应用，刷新检测只更新状态、不触发写命令。WSL NAT 时 SHALL 显示三种替代方案；Windows provider 时 SHALL 显示显式选择与 operator 权限提示；入口冲突时 SHALL 只显示 host、port、是否公开，不显示其他目标路径。页面 SHALL 提供刷新检测与复制 URL；带 Token 链接复用现有剪贴板与 `localStorage` 约定，Token 不得进入页面可见文本或新的持久化位置。

#### Scenario: 检测到但未配置

- **WHEN** 设置页检测到运行中的 Tailscale，`[tailscale]` 为 `off`
- **THEN** 页面显示节点 DNS、HTTPS 就绪与“由配置文件启用 Serve/Funnel”的说明，不显示无法生效的开关

#### Scenario: WSL NAT 指引

- **WHEN** 页面检测到 WSL NAT 与 Windows Tailscale
- **THEN** 页面说明不能托管，列出 mirrored（会结束会话）、WSL 内安装 Tailscale 与托盘转发三种方案，不提供自动修改 `.wslconfig` 的按钮

#### Scenario: Funnel 风险

- **WHEN** `mode = "funnel"` 且状态为 `ready`
- **THEN** 页面显著显示公网 HTTPS 风险、端口与公网 URL，不把 Funnel 描述成“仅 tailnet”

#### Scenario: 失败时终端仍可用

- **WHEN** Tailscale 检测或启动失败
- **THEN** 卡片/详情页显示原因，其他设置与终端功能继续工作，不阻塞页面其他请求

#### Scenario: 入口冲突

- **WHEN** 目标端口已有不属于本实例的入口
- **THEN** 页面显示该 host、port 与公开状态，说明不会覆盖，并给出人工处理提示；不显示其他应用的目标路径

### Requirement: 安全边界与命令执行

系统 SHALL 以 argv 执行 Tailscale 命令，MUST NOT 经过 shell、MUST NOT 拼接用户输入、MUST NOT 使用交互式 stdin，并 SHALL 为每次调用设置超时与取消。命令输出进入错误信息或日志前 SHALL 去掉 Token 与敏感路径。`GET /api/tailscale`、日志、通知、普通 URL 与二维码 MUST NOT 包含 WebMux Token；只有本次进程生成的 Token 可以按现有启动链接规则出现在标准输出的访问链接中。

系统 MUST NOT 自动执行 `sudo`、自动提权、修改 `.wslconfig`、修改 Windows 防火墙、修改 tailnet ACL/HTTPS 开关/节点属性，或调用 `tailscale serve reset` / `tailscale funnel reset`。系统 MUST NOT 覆盖、删除或修改不归本次记录所有的 Serve/Funnel 入口。Funnel MUST 仅由显式配置开启；从 Funnel 切回 Serve 或 Funnel 失败时，系统 SHALL 尝试关闭本次公开入口，无法确认时保持 `failed`，不得报告成功。

#### Scenario: 不自动提权

- **WHEN** Linux 或 Windows 的 Serve 命令因权限不足失败
- **THEN** 系统显示设置 operator 的指引，不执行 sudo、不弹出提权、不重试写命令

#### Scenario: 不修改系统网络配置

- **WHEN** 检测到 WSL NAT、Windows 防火墙或 tailnet HTTPS 未启用
- **THEN** 系统只显示原因与操作步骤，不修改 `.wslconfig`、防火墙或 tailnet 设置

#### Scenario: 不清理他人入口

- **WHEN** tailscaled 上存在用户为其他应用创建的 Serve 入口
- **THEN** 检测可以显示其 host/port/public，但停止、切换或失败清理都只作用于本次记录的精确目标，其他入口保持不变

#### Scenario: 响应不含 Token

- **WHEN** 已认证客户端请求 `GET /api/tailscale`
- **THEN** 响应只包含状态、DNS、端口、URL 与原因，不包含 Token 或其他入口的目标路径

#### Scenario: 命令挂起

- **WHEN** Tailscale CLI 在超时内没有返回
- **THEN** 系统取消调用，状态为 `failed`，不继续等待，也不在后台重复调用写命令
