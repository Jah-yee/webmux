## Context

- `internal/api/public.go` 的 `PublicOrigin` 只保存一个不允许端口的 HTTPS origin；`internal/api/router.go` 的 Host 检查先剥掉端口，再要求 Origin 精确匹配。Tailscale Serve/Funnel 在非 443 端口上会同时带端口，现有模型无法安全表达“只授权这个 host:port”。
- `cmd/webmux/tunnelctl.go` 把 cloudflared 当作 WebMux 的子进程：启动 `--tunnel` 时定位可执行文件，退出或失败时结束进程并撤销来源。Tailscale `serve --bg` 是 tailscaled 的持久配置，进程退出后仍然存在，不能照搬这个生命周期。
- `cmd/webmux/main.go` 的 `ensureToken` 目前只在 Cloudflare 隧道开启或非回环监听时生成 Token；`--tunnel` 与网页托管隧道都要求至少 43 字符。新增 Tailscale Serve/Funnel 同样是网络入口，必须纳入同一判定。
- `docs/deployment.md` 已建议手工使用 `tailscale serve`，但没有集成；WSL 段落已说明 NAT 模式下 Windows Tailscale 流量不会进入 WSL，并提供 mirrored、WSL 内安装 Tailscale 与托盘转发三种方案。
- `internal/client/wsl_linux.go` 已经用 `/proc/sys/fs/binfmt_misc/WSLInterop`、`/proc/sys/kernel/osrelease` 与 `wslinfo --networking-mode` 识别 WSL 与 NAT；服务端不能导入客户端包，应把纯检测抽到共享层复用。
- Tailscale 官方建议 WSL 用户只在 Windows 主机运行 Tailscale；WSL 内同时运行 Tailscale 会造成 Tailscale 封装在 Tailscale 内与 MTU 问题。Windows 的 `tailscale serve/funnel` 只支持代理 `http://127.0.0.1`；NAT 模式下 Windows 回环到 WSL 的转发不保证、可能命中其他发行版或其他服务，并存在已知的 IPv6/MTU 故障。mirrored 网络模式让 Windows 与 WSL 共享回环与网卡，是托管模式的可靠前提。
- `tailscale status --json` 的 `CertDomains` 为空表示 tailnet 未启用 HTTPS；此时 `tailscale serve --https` 可能无限等待证书。Tailscale 官方文档明确说明该交互行为，因此必须在执行前检查 `CertDomains`，并给所有 CLI 调用设置超时。
- `tailscale serve status --json` 用 `Web["<dns>:<port>"].Handlers["/"].Proxy` 描述反向代理，用 `AllowFunnel["<dns>:<port>"]` 描述该入口是否公开。命令成功退出不足以证明配置生效，必须回读这两项。

## Goals / Non-Goals

**Goals:**

- 在 Linux、macOS 与 WSL 中只读识别 Tailscale 的可用性、provider、节点 DNS 名、HTTPS 证书状态、现有 Serve/Funnel 入口与阻塞原因。
- 让 `[tailscale] mode = "serve"` 在 tailnet 内提供自动证书的 HTTPS，`mode = "funnel"` 在明确选择后提供公网 HTTPS。
- 只修改 WebMux 自己创建的精确端口目标；对账崩溃、WSL 关闭或配置变更后留下的 `--bg` 入口；绝不调用 `reset`。
- 让 Host/Origin 校验精确到 443 的无端口 authority 与任意非 443 端口的带端口 authority，并允许 Cloudflare 与 Tailscale 来源并存。
- 在 WSL 中正确区分 WSL 内 Tailscale 与 Windows 主机 Tailscale；Windows provider 必须显式选择、要求 mirrored、失败关闭、绝不自动提权。
- 在不存在本实例遗留入口时，任何 Tailscale 故障都不阻止本地 HTTP 服务、已有会话或 Cloudflare 功能；失败时撤销对应来源，不残留本次创建的公开入口。若无法证明遗留入口已关闭，宁可拒绝启动。
- 在设置页诚实地展示“已检测但未配置”“已配置但失败”“NAT 不可托管”“端口冲突”等状态，不提供不会生效的假开关。

**Non-Goals:**

- 不实现自签名 TLS，也不把 `http://` 伪装成安全上下文。
- 不提供运行时之外的第二套配置入口：网页只修改 `mode`、`port`、`provider`，CLI 路径 `command` 仍只由 TOML 管理。
- 不把 Tailscale HTTPS 加入 Web Push 来源判定；当前推送仍只接受 Cloudflare Named Tunnel 与回环，后续变更单独设计并同步托盘说明。
- 不安装或登录 Tailscale，不修改 tailnet ACL、HTTPS 开关、节点属性、`.wslconfig`、Windows 防火墙或系统代理。
- 不替换 Cloudflare Tunnel，不改变 cloudflared 的定位、下载、Token 或现有网页设置。
- 不结束或重启 tailscaled，不把 `--bg` 入口当作子进程，也不管理用户已有的其他 Serve/Funnel 目标。

## Decisions

### 1. 检测是只读的，平台识别先于 CLI 选择

新增 `internal/tailscale` Service，只负责检测与执行 Tailscale CLI；`cmd/webmux` 的控制器负责状态、所有权与生命周期。`GET /api/tailscale` 需要现有认证与 Host/Origin 中间件，返回脱敏后的状态，不返回 Token、其他 Serve 目标的本地路径或完整命令输出，并设置约 2 秒缓存与并发合并，避免设置页反复打开时反复派生进程。

平台矩阵：

| 环境 | 识别方式 | provider | 是否可托管 |
| --- | --- | --- | --- |
| Linux/macOS | `tailscale` 在 `PATH`，状态可读 | `linux` | 是 |
| WSL 内安装了 Tailscale | WSL 判定 + `tailscale` 可运行且已登录 | `wsl-native` | 是 |
| WSL，Tailscale 在 Windows | WSL 判定 + `tailscale.exe` 可运行且已登录 | `windows` | 仅显式 `provider = "windows"` 且 networking mode 为 `mirrored` |
| 未安装或不可互操作 | 找不到 CLI 或 `/mnt` 不可用 | `none` | 否 |

WSL 判定复用共享层的 `WSLInterop` / `osrelease` 检查，`wslinfo --networking-mode` 失败按未知处理。`provider = "auto"` 在 WSL 中优先选择正在运行的 WSL 原生 Tailscale；如果只有 Windows Tailscale 可用，只报告检测结果，`mode != "off"` 时失败并提示显式设置 `provider = "windows"`。WSL 原生与 Windows 同时运行时，`auto` 不切换；选择 Windows 必须显式配置。非 WSL 环境配置 `provider = "windows"` 是配置错误。

Windows CLI 解析顺序：显式 `command`；`PATH` 中的 `tailscale.exe`；默认安装路径 `C:\Program Files\Tailscale\tailscale.exe` 对应的 `/mnt/<drive>/...`。所有调用使用 argv、独立超时与不继承交互式 stdin；不经过 shell，不拼接用户输入。`/mnt` 未挂载、WSL interop 关闭或 CLI 不存在时报告明确原因。

只读检测调用 `tailscale status --json`、`tailscale serve status --json`；Funnel 状态可从同一份 serve 配置的 `AllowFunnel` 读取，必要时再调用 `tailscale funnel status --json`。解析只依赖文档化字段，缺失字段不崩溃。其他 Serve 入口只展示 host、port 与是否公开，不展示目标路径或 URL 路径。

### 2. 托管由 `[tailscale]` 配置驱动，网页设置可覆盖并立即生效

新增配置：

```toml
[tailscale]
mode = "off"          # off | serve | funnel
port = 443            # 0 表示自动；Serve 可 1-65535，Funnel 仅 443/8443/10000
provider = "auto"     # auto | linux | windows
# command = "/usr/bin/tailscale" 或 WSL 中的 "/mnt/c/Program Files/Tailscale/tailscale.exe"
```

`mode`、`port`、`provider` 与 `command` 的非法值属于配置错误，启动时以非零状态退出：Serve 的端口可以是 `0` 或 `1-65535`，Funnel 只能是 `0`、`443`、`8443` 或 `10000`。运行期前置条件（未安装、未登录、证书未就绪、NAT、权限不足、端口冲突）不是配置错误，只把 Tailscale 状态标为 `failed`，本地服务继续运行。这样 `serve`/`funnel` 不会因为 Tailscale 暂时不可用而阻断 Shell。

`listen` 约束：Tailscale Serve/Funnel 只能代理 `http://127.0.0.1`。托管模式要求实际监听地址覆盖 IPv4 回环；`127.0.0.1:0` 使用监听后的真实端口；`0.0.0.0` 通常覆盖回环，`localhost` 取决于解析结果，二者都必须用启动后的 `127.0.0.1:<port>` 可达性检查确认；`[::1]`、仅特定网卡地址或不覆盖回环的监听必须失败并提示把 `listen` 改为 `127.0.0.1:<port>` 或 `0.0.0.0:<port>`。这是对现有“监听 Tailscale IP”部署的明确迁移要求，文档必须说明改用 Serve URL。

`ensureToken` 把 `mode != "off"` 视为网络入口：Token 留空时生成 256 位随机 Token；显式 Token 少于 43 字符时拒绝启动；Funnel 与 Serve 使用同一强 Token 规则。若 Token 是本次进程生成，就绪后可以打印带 Token 的链接与二维码；若来自配置，只打印不含 Token 的 URL，避免把配置 Token 写入服务日志。`GET /api/tailscale` 永远不返回 Token。

### 3. Serve/Funnel 生命周期：精确目标、所有权、对账

`serve --bg` 与 `funnel --bg` 不是 WebMux 的子进程，WebMux 退出不会自动撤销。生命周期按以下规则：

启动顺序先清理再监听：在配置与 Token 校验通过后、绑定监听前，读取 `tailscale.json` 与 serve 配置；若存在本实例记录，且新配置为 `off` 或模式/端口/target 与记录不同，先在绑定监听前关闭该精确目标（按需 `funnel off` + `serve off`）。无法移除时 MUST 拒绝启动并给出精确手工命令，避免旧入口在新进程开始监听后继续暴露。若状态文件损坏或缺失，但 serve 配置中有入口指向当前监听端口与 target，也视为所有权未知并拒绝启动；没有匹配入口时才允许本地 HTTP 继续。清理完成或确认入口可复用时才绑定监听并应用。若保留的匹配入口在随后绑定监听时失败（例如端口被其他进程占用），系统 MUST 在退出前关闭本次记录的入口；无法关闭时 MUST 报告公开风险与精确手工命令，避免旧入口指向另一个进程。

1. 清理通过后再绑定监听；HTTP 本地访问可用后异步应用 Tailscale；状态先为 `starting`。应用命令前先把本次意图（provider、节点 DNS、端口、模式、target 与 `pending` 标记）写入状态文件并落盘；命令与回读成功后标记 `active`。这样即使进程在命令成功后、标记前崩溃，下次启动仍知道该入口属于本次实例。
2. 应用前读取 serve 配置。目标端口已有入口时：只有状态记录中属于本实例、且 `host:port`、唯一 `/` handler 与 `Proxy` 精确等于当前 target 的入口可以复用；没有记录时即使精确匹配也视为冲突，状态 `failed`，不覆盖、不删除。`tailscale.json` 是唯一的所有权依据。命令或回读失败时 SHALL 撤销来源并尝试移除与 `pending` 记录 target/端口精确匹配的入口；不匹配的冲突入口必须保持不动。清理失败时保留 `pending` 记录供下次启动对账，清理成功后删除记录或标记失败。
3. `port = 0` 时按 443、8443、10000 顺序选择第一个没有非本人入口的端口；三者都被占用时 Funnel 失败，Serve 失败并提示显式指定其他空闲端口。显式端口冲突直接失败，不静默换端口。选中端口写入状态文件并展示 URL。
4. Serve 命令：`tailscale serve --bg --https=<port> http://127.0.0.1:<实际端口>`。执行前要求 `CertDomains` 包含节点 DNS 名；否则不执行命令，避免等待证书而挂起。
5. Funnel 命令：`tailscale funnel --bg --https=<port> http://127.0.0.1:<实际端口>`。Funnel 只允许 443/8443/10000；执行前检查证书与端口，命令失败时把 stderr 映射为可操作原因（HTTPS 未启用、缺少 `funnel` 节点属性、权限不足、端口冲突）。
6. 命令成功不等于生效。回读 `serve status --json`，Serve 要求目标端口有且仅有指向当前 target 的 `/` handler 且 `AllowFunnel` 不为真；Funnel 要求同一 handler 且 `AllowFunnel[host:port] == true`。不满足时撤销来源并保持 `failed`。
7. 移除只针对本次记录：执行 `tailscale serve --https=<port> off`。该命令同时移除 handler 与 Funnel 标记；先执行 `funnel ... off` 会让随后的 `serve off` 因 `handler does not exist` 失败。重复移除同一端口视为成功。绝不调用 `tailscale serve reset` 或 `tailscale funnel reset`，因为它们会清除用户的其他入口。
8. 启动时按 `<state_dir>/tailscale.json` 对账：记录存在但入口缺失则重建；target 或端口变化则移除旧精确目标后再应用；模式变化（serve↔funnel）先移除旧目标，确保旧端口不再公开；入口存在且与记录精确匹配则复用。记录丢失或入口与记录不匹配时失败关闭，等待人工处理；不得仅凭 target 相同就把用户手工创建的入口当作自己的。
9. 退出时在短超时内移除本次入口；移除成功后删除状态记录，CLI 不可用或超时则记录警告并保留记录（含 `pending`/`active`），下次启动对账。不结束 tailscaled，不阻塞会话清理超过既定超时。
10. 状态文件 `tailscale.json` 使用 0600、版本字段、原子替换与独立 flock。文件损坏、不可读或锁冲突时失败关闭：不删除任何入口、不应用新目标；如果没有入口指向当前监听端口与 target，本地 HTTP 继续并报告存储故障；如果有匹配入口，按上述启动清理规则拒绝启动。不要用状态文件推断其他 Serve 入口的所有权。

`tailscale serve --bg` 的持久性意味着 WebMux 崩溃后入口会短暂指向死端口。文档必须说明这一点，并给出等价的手工检查与移除命令；启动对账是主要修复路径。

### 4. WSL Windows provider：mirrored + 显式选择

Windows 主机上的 Tailscale 是 WebMux 进程之外的全局状态，serve/funnel 配置影响 Windows 上所有用户与应用。因此：

- 只有显式 `provider = "windows"` 才允许对 Windows 执行写命令；`auto` 永远不代管 Windows，只报告检测结果与需要显式选择的提示。
- 托管要求 `wslinfo --networking-mode` 为 `mirrored`。NAT 下即使 Windows CLI 可用也失败关闭，不尝试用 `127.0.0.1` 猜测 localhostForwarding，因为转发不保证、可能命中其他发行版/服务，并存在 IPv6 与 MTU 已知问题。
- NAT 的替代方案写入错误与文档：把 `%USERPROFILE%\.wslconfig` 的 `[wsl2]` 设为 `networkingMode=mirrored` 后执行 `wsl --shutdown`（会结束会话）；在 WSL 内安装 Tailscale 并使用 `provider = "linux"`；或继续使用 Windows 托盘的 Tailscale 转发并手工配置 Serve。WebMux MUST NOT 自动修改 `.wslconfig`、重启 WSL 或修改 Windows 防火墙。
- Windows CLI 权限不足时，错误提示在 Windows 管理员终端执行一次 `tailscale set --operator=<Windows 用户>`；不在 WSL 内使用 `sudo`，不自动提权。
- URL、Host 与 Origin 使用 Windows `tailscale status --json` 的 `Self.DNSName`（去掉末尾点），不使用 WSL 主机名、WSL IP 或 `hostname` 命令的结果。Windows 与 WSL 节点可能同名不同后缀，必须展示实际节点。
- Windows 的 `--bg` 状态跨 WSL 关闭与重启保留；WSL 关闭时 WebMux 无法清理，下一次启动对账。多个 WSL 发行版共用同一个 Windows Tailscale 时，端口或 target 冲突按第 3 节失败关闭，不覆盖其他发行版的入口。
- 同时检测到 WSL 原生 Tailscale 与 Windows Tailscale 时，`auto` 选择正在运行的 WSL 原生；设置页同时列出 Windows 可用但未托管，避免用户误以为已经使用 Windows 节点。

### 5. 端口感知的多来源 Origin 注册表

把 `api.PublicOrigin` 改为 `api.PublicOrigins`，内部用 copy-on-write map 保存 `source → {origin, authority}`，至少支持 `cloudflare` 与 `tailscale` 两个来源；`Set(source, origin)` 只替换该来源，`Clear(source)` 只撤销该来源。Cloudflare controller 使用 `cloudflare`，Tailscale controller 使用 `tailscale`，两者可同时存在。

- 443 的 origin 为 `https://<dns>`，authority 为 `<dns>`；其他端口的 origin 为 `https://<dns>:<port>`，authority 为 `<dns>:<port>`。
- Host 检查比较完整的 authority（host 或 host:port），不再先剥端口后放过其他端口；Origin 检查要求 `Origin` 精确等于该来源的 origin，且 `r.Host` 精确等于对应 authority。
- 来源在 Tailscale 命令成功且回读确认后登记；停止、失败、模式切换前先撤销。撤销后旧公网请求立即 403。
- `allowed_origins` 仍是手工部署的后备；托管 Tailscale 不需要用户添加，但文档说明手工配置时必须写带端口的完整 origin。
- 不信任 `X-Forwarded-Host` / `X-Forwarded-Proto`。Tailscale 终止 TLS 后到达 WebMux 的是 http，但 Host 与 Origin 仍是 ts.net；判定只依赖已登记的 authority 与 origin。
- 推送来源判定保持现状：Quick Tunnel 仍返回 `quick_tunnel`，Tailscale 返回 `unsupported_origin`（Tailscale 的推送支持留待后续变更）；不要把 Tailscale 来源加入推送白名单。

### 6. 设置页：只读状态而不是假开关

在现有“公网访问”设置分区增加 Tailscale 卡片，并提供 `/settings/tailscale` 详情页。页面展示：

- 检测状态：未安装、未运行、需要登录、运行中、无法确定；provider 为 Linux、WSL 原生或 Windows。
- 配置状态：`off` / `serve` / `funnel`、端口、节点 DNS 名、公开 URL、运行状态与错误。
- `Serve` 与 `Funnel` 的显著区别：Serve 仅 tailnet 内设备可访问；Funnel 是公网 HTTPS，任何拿到链接与 Token 的人都能执行命令。
- WSL NAT 的具体阻塞原因与三种替代方案；Windows provider 的显式选择提示与 operator 权限提示。
- 现有入口冲突：显示 host、port、是否公开，不显示其他目标路径；说明 WebMux 不会覆盖。
- 检测刷新按钮；复制不含 Token 的 URL，以及复用现有剪贴板约定复制带 Token 链接（Token 来自当前浏览器，不进入 DOM 之外的持久化）。
- 页面显示配置来源，提供模式（关闭/Serve/Funnel）、端口与 provider 的编辑、保存并应用与“恢复配置文件设置”；开启 Funnel、恢复配置文件设置与替换当前访问的入口前确认。网页设置写入 `<state_dir>/tailscale-settings.json`（0600、原子替换、独立锁），优先于 TOML；损坏或锁冲突时禁用写入并把有效模式视为 `off`。保存后控制器取消当前应用、移除不再符合新设置的本实例入口，再应用新设置；与 Cloudflare 设置页一致，恢复配置文件设置只关闭当前入口，TOML 在下次启动生效。

移动端单列、键盘可操作、亮暗主题与窄屏溢出按现有前端守卫检查；新增 Zod schema，API 失败只禁用该卡片/详情，不影响终端。实现 PR 需要按 AGENTS.md 提供桌面/手机、亮/暗截图。

### 7. 安全边界

- Funnel 必须显式配置，不得因为检测到 Tailscale 就自动公开；`mode = "funnel"` 本身就是用户选择，仍要在设置页和文档中持续显示公网风险。
- 任何 `set`、`serve reset`、`funnel reset`、`sudo`、自动提权、修改 `.wslconfig` 或 Windows 防火墙的命令都不在实现范围内。权限不足时只显示提示：Linux/macOS 为 `tailscale set --operator=$USER`，WSL Windows 为在管理员终端执行 `tailscale set --operator=<Windows 用户>`；由用户自行执行。
- 只允许操作记录中的精确端口与 target；不覆盖、不删除未知入口；状态文件损坏时宁可失败关闭，也不猜所有权。
- 命令输出进入错误信息前去掉 Token；`GET /api/tailscale`、日志、URL 与二维码都不包含 Token（生成 Token 的启动链接除外，并遵守现有日志边界）。
- Funnel 失败或从 Funnel 切回 Serve 时，必须确认旧端口的 `AllowFunnel` 已关闭；若无法确认，状态 `failed` 并在文档给出人工移除命令。不能把“命令失败”误报成“已关闭”。
- Tailscale CLI 的 JSON 格式可能随版本变化；只解析文档化字段，字段缺失按未知/失败处理，不 panic。

### 8. 测试策略

- `internal/tailscale` 使用临时目录中的 fake `tailscale`（POSIX 脚本）与 fake `tailscale.exe`（在 Linux 测试中作为普通可执行文件，通过 `command` 指定）；`wslinfo` 用 PATH 覆盖，覆盖 mirrored、nat、缺失与超时。
- 控制器测试覆盖：首次 Serve、端口自动选择、端口冲突、证书缺失不执行命令、命令失败、命令成功但回读不匹配、Serve/Funnel 切换、退出移除、崩溃后对账、状态文件损坏/锁冲突、`--bg` 残留、状态文件丢失冲突、拒绝未知入口。
- API 测试覆盖：`GET /api/tailscale` 的 401/403/200、缓存与并发合并、脱敏；`PublicOrigins` 的 443、8443、10000 与任意 Serve 端口、错误端口、http Origin、无关 Host、撤销后 403、Cloudflare 与 Tailscale 并存。
- WSL 专项测试：Windows provider 未显式选择时不写；NAT 失败关闭；mirrored 允许；权限错误提示；Windows DNS 名进入 URL/Host；WSL 关闭后的残留对账；多个 provider 的确定性选择。
- 前端：Zod schema、检测状态、NAT 指引、Funnel 警告、冲突、错误降级、刷新、复制、键盘与窄屏；浏览器测试使用 mock API，不连接真实 Tailscale。
- E2E 使用 fake CLI；真实 Tailscale Serve/Funnel、真实 Windows Tailscale 与 WSL mirrored 环境只做手工验收，不进入 CI。
- 运行 `go test -race` 相关包、`pnpm --dir web check`、`check:arch`、生产构建、`make docs-check` 与 `openspec validate add-tailscale-exposure --strict`。实现 PR 更新用户故事与协议快照。

## Risks / Trade-offs

- [网页可开启公网 Funnel] → 与 Cloudflare 网页开关同等的风险模型：需要认证与 Origin、强 Token、显式风险确认；会话内进程不能经本机 API 修改。修改 TOML 仍需重启，重启会结束会话。
- [`--bg` 入口在 WebMux 崩溃后仍指向死端口] → 启动对账是主要修复；文档给出手工检查与精确移除命令，绝不用 `reset`。窗口期内 Tailscale URL 返回 502，但不会泄露 shell。
- [NAT 下的 WSL 用户不能直接托管 Windows Tailscale] → 这是安全与可靠性取舍；提供 mirrored、WSL 内安装和托盘转发三种替代，比猜测 localhostForwarding 更可控。
- [Windows 状态是主机全局的] → 显式 `provider = "windows"`、只操作精确端口、状态文件与冲突失败关闭；仍无法阻止同一 Windows 用户手工修改 Tailscale，这属于主机管理边界。
- [Serve 只支持 `http://127.0.0.1` 目标] → 现有监听 Tailscale IP 的部署必须改为回环/wildcard；文档给出迁移，直连 Tailscale IP 的旧 URL 会失效，但 Tailscale URL 是 HTTPS 且更安全。
- [Funnel 是 beta，端口与带宽受限] → 仅显式开启，文档写明只能使用 443/8443/10000 与公网风险；不承诺带宽或稳定性。
- [多个 HTTPS 入口同时存在] → 注册表按来源隔离，Cloudflare 与 Tailscale 可并存；推送仍只支持 Cloudflare Named Tunnel 与回环，用户可能在 Tailscale 页面看到推送不可用，文档说明这是后续工作。
- [Tailscale CLI/JSON 版本差异] → 只解析文档化字段、全部调用设超时、未知字段忽略、状态不可读按失败处理；fake CLI 覆盖常见输出。
- [状态文件丢失后无法确定所有权] → 即使 target 精确匹配也失败关闭，宁可要求人工处理也不删除用户手工创建的入口。
- [Windows 节点 DNS 名或 tailnet 变化] → 每次检测重新读取；URL/Origin 随节点名变化，旧页面与旧推送订阅可能失效，文档提示重新打开。
- [与 Cloudflare 同时开启时 URL/来源增多] → 注册表按来源管理；用户需要知道当前页面来自哪个入口，设置页同时展示两者状态。

## Migration Plan

- 默认 `mode = "off"`；从未使用过 Tailscale 的现有部署行为完全不变，不创建状态文件、不调用 CLI。若状态目录中已有本实例记录（崩溃恢复或配置改回 off），启动时按对账规则清理其精确入口。
- 首次设置 `mode = "serve"` 或 `"funnel"` 时，若无 Token 则本次进程生成；若显式 Token 少于 43 字符则拒绝启动。用户需要在重启前保存工作。
- 现有 `listen` 为 Tailscale IP 的部署：托管 Serve/Funnel 会失败并提示改为 `127.0.0.1:<port>` 或 `0.0.0.0:<port>`；改为回环后通过 `https://<node>.ts.net` 访问，旧 `http://100.x.y.z:7681` 不再可用。文档提供迁移步骤。
- 首次尝试应用前创建 `<state_dir>/tailscale.json` 的 `pending` 记录，命令与回读成功后标记 `active`；回滚到旧版本会忽略该文件，但旧版本不会对账，可能留下 `--bg` 入口。回滚前应手工执行 `tailscale serve --https=<port> off`（同时清除 Funnel 标记），不要执行 `reset`。
- WSL Windows provider 首次使用需要显式 `provider = "windows"` 且 mirrored；修改 `.wslconfig` 后 `wsl --shutdown` 会结束所有会话，文档要求先保存工作。
- 配置校验失败或进程在清理阶段之前退出时不会进入清理阶段；若此前留下 `--bg` 入口，修复配置后下次启动会清理，或按文档手工移除。配置错误本身不应触发 Tailscale 写命令。
- 文档、协议、配置示例、架构包表与用户故事在同一实现 PR 更新；真实 Tailscale Serve/Funnel 与 WSL Windows 主机验收仍为手工步骤，CI 只使用 fake CLI。
