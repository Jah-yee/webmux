## Purpose

规定托盘程序如何在 WSL 发行版中启动、保持、监控、停止和重启 WebMux，使 Windows 用户登录后即可获得一个持续运行、可安全停止的 WebMux 服务，并兼容已用 systemd 用户服务部署的用户。

## ADDED Requirements

### Requirement: 启动方式选择

托盘 SHALL 支持两种启动方式：

- **托盘托管**：托盘在目标发行版中直接运行 WebMux 服务；
- **systemd 用户服务**：托盘通过目标发行版的 `systemctl --user` 管理名为 `webmux` 的用户单元。

设置为“自动”时，托盘 SHALL 在每次启动 WebMux 前检测目标发行版：systemd 为 PID 1 且存在 `webmux` 用户单元时使用 systemd 方式，否则使用托盘托管。显式选择 systemd 方式但条件不满足时，托盘 SHALL 进入异常状态并说明缺少的条件，SHALL NOT 自动退回托盘托管。

#### Scenario: 自动选择 systemd

- **WHEN** 发行版启用了 systemd 且已安装 `webmux` 用户单元，启动方式为“自动”
- **THEN** 点击“启动”通过 systemd 启动 WebMux，状态行显示“systemd”

#### Scenario: 自动选择托盘托管

- **WHEN** 发行版未启用 systemd，启动方式为“自动”
- **THEN** 点击“启动”由托盘托管启动 WebMux，状态行显示“托盘托管”

### Requirement: 托盘托管方式启动

托盘托管方式下，托盘 SHALL 在目标发行版中以当前 WSL 默认用户运行设置中的 `webmux`，参数为 `serve`、`--log-file <设置中的日志路径>`，以及设置了配置路径时的 `--config <路径>`，并 SHALL 让 WebMux 将启动方记录为 `windows-tray`。所有参数 SHALL 以参数数组传递，用户提供的路径 SHALL NOT 被 shell 解释为命令或通配符；仅允许 `~/` 前缀表示 WSL 用户主目录。

运行 WebMux 的 WSL 进程 SHALL 独立于托盘进程存在：托盘退出或崩溃 SHALL NOT 结束 WebMux。

#### Scenario: 正常启动

- **WHEN** 已停止状态下用户点击“启动”
- **THEN** 图标进入启动中，WebMux 在目标发行版中运行且其状态报告启动方为 `windows-tray`，就绪后图标变为运行中

#### Scenario: 路径包含空格与特殊字符

- **WHEN** 配置路径为 `~/my configs/webmux;$(id).toml`
- **THEN** WebMux 收到的配置路径为该字面值（`~/` 展开为主目录），不执行任何额外命令

### Requirement: systemd 方式

systemd 方式下，启动、停止、重启 SHALL 分别对应目标发行版中 `systemctl --user` 对 `webmux` 单元的 start、stop、restart；意外退出后的重启 SHALL 交给该单元自身的重启策略，托盘 SHALL NOT 另行重启。托盘 SHALL 在单元进入失败状态时通知用户并显示异常。

#### Scenario: 通过 systemd 停止

- **WHEN** systemd 方式下 WebMux 有 1 个会话，用户点击“停止”并确认
- **THEN** 托盘执行 systemd 停止，WebMux 退出，图标变为已停止

### Requirement: 保持发行版存活

在 WebMux 由托盘管理并运行期间（任一启动方式），目标 WSL 发行版 SHALL NOT 因“没有活动的 WSL 客户端”而被自动关闭；浏览器断开、关闭所有 Windows 终端窗口或“仅退出托盘” SHALL NOT 结束 WebMux。托盘托管方式下运行 WebMux 的进程即承担保活；systemd 方式下托盘 SHALL 另行维持一个独立于托盘进程的保活进程，并在用户停止 WebMux 后结束它。

#### Scenario: 关闭所有终端窗口

- **WHEN** WebMux 运行中，用户关闭所有 Windows Terminal 与 WSL 窗口并等待 5 分钟
- **THEN** WebMux 仍在运行，已有会话可从浏览器重新连接

#### Scenario: systemd 方式下仅退出托盘

- **WHEN** systemd 方式下 WebMux 运行中，用户选择“仅退出托盘”并等待 5 分钟
- **THEN** WebMux 仍在运行

### Requirement: 状态获取与就绪判定

托盘 SHALL 经桥接访问目标发行版中 WebMux 的本机 API，以 `/local/v1/status` 作为服务状态的依据（运行状态、PID、启动方、版本、会话数、链接、配置漂移），并在运行中保持一条 `/ws/sessions` 订阅，使会话数与会话变化无需轮询即可在 2 秒内反映到图标与菜单。桥接因服务未运行而无法连接时，托盘 SHALL 改用目标发行版中 `webmux status --json` 获取未运行时的信息（配置是否存在、遗留记录、旧版本实例）。

托盘另 SHALL 从 Windows 侧经 TCP 检查本机链接是否可达。运行中时托盘 SHALL 至少每 10 秒检查一次 Windows 侧可达性、至少每 60 秒刷新一次本机 API 状态；订阅断开时 SHALL 立即刷新状态并以退避重连；菜单打开、启动 / 停止完成后 SHALL 立即刷新。

#### Scenario: 会话数实时更新

- **WHEN** WebMux 运行中，用户在浏览器中新建一个会话
- **THEN** 2 秒内托盘悬停提示中的会话数加一，期间托盘没有新启动轮询用的 `wsl.exe`

启动后托盘 SHALL 在状态为运行中且健康时判定就绪。60 秒内未就绪时 SHALL 进入异常状态，在状态行给出最后一次失败原因（连接被拒、Token 不匹配、超时等）及“打开日志”提示，但 SHALL NOT 自动结束已启动的进程。健康检查报告未授权时 SHALL 显示“Token 不匹配”，而不是“未运行”。

#### Scenario: 启动超时

- **WHEN** WebMux 进程已启动但 60 秒内始终无法就绪
- **THEN** 图标显示异常，状态行提示“WebMux 未在 60 秒内就绪：连接被拒绝”，菜单“打开日志”可用

#### Scenario: 登录后 WSL 冷启动较慢

- **WHEN** Windows 登录后托盘立即启动，WSL 虚拟机需要 20 秒才能就绪
- **THEN** 托盘显示启动中直到 WebMux 就绪，不出现失败通知

### Requirement: 外部实例

WebMux 已在运行但不归当前启动方式管理时（托盘托管方式下启动方不是 `windows-tray`；systemd 方式下启动方不是 `systemd`；或状态报告为无运行时记录），托盘 SHALL 显示运行中并标记“外部实例”，SHALL NOT 再启动第二个实例；对外部实例 SHALL 只提供访问类操作与状态监控，“停止”“重启” SHALL 禁用并在状态行说明“由外部管理”。

配置端口已被占用但不是 WebMux 时，托盘 SHALL 进入异常状态并提示端口被占用，SHALL NOT 启动新进程。

托盘重新启动后，SHALL 能根据 WebMux 状态中的启动方恢复对此前由托盘管理的实例的停止与重启控制。

#### Scenario: 手动启动的实例

- **WHEN** 用户在 WSL 终端里手动运行了 `webmux`，然后启动托盘
- **THEN** 托盘显示运行中（外部实例），“打开 WebMux”可用，“停止”“重启”禁用

#### Scenario: 托盘重启后恢复控制

- **WHEN** 用户“仅退出托盘”后重新启动托盘
- **THEN** 托盘显示运行中，“停止”“重启”可用

### Requirement: 停止与重启

当前会话数大于 0 时，停止与重启 SHALL 先弹出确认，说明将结束的会话数量且不可恢复；会话数为 0 时 SHALL 直接执行。托盘托管方式下停止 SHALL 先经本机 API 请求正常退出，15 秒内未退出或本机 API 无响应时 SHALL 改用 WebMux 自身的停止命令强制结束；重启 SHALL 等价于停止完成后再启动。

用户主动停止后，托盘 SHALL NOT 自动重启 WebMux。

#### Scenario: 有会话时停止

- **WHEN** WebMux 有 4 个会话，用户点击“停止”
- **THEN** 托盘提示“停止 WebMux 将结束 4 个终端会话”，确认后 WebMux 正常退出、会话进程被清理，图标变为已停止

#### Scenario: 停止超时

- **WHEN** 托盘托管方式下 WebMux 收到停止请求后 15 秒仍未退出
- **THEN** WebMux 被强制结束，图标变为已停止，托盘日志记录强制结束

### Requirement: 意外退出与自动重启

WebMux 非因用户停止而退出（崩溃、被杀、`wsl --shutdown`、发行版终止）时，托盘 SHALL 发出 Windows 通知，说明 WebMux 已退出且会话已丢失。托盘托管方式下，设置允许自动重启时，托盘 SHALL 以指数退避（首次 2 秒，上限 60 秒）重新启动；10 分钟内连续失败 5 次后 SHALL 停止自动重启、进入异常状态，直到用户手动启动。

托盘启动时，若设置“托盘启动时自动启动 WebMux”为是且未检测到运行中的实例，托盘 SHALL 自动执行启动；WSL 尚在初始化导致的失败 SHALL 计入退避，而不是直接放弃。

#### Scenario: wsl --shutdown

- **WHEN** 托盘托管方式下 WebMux 运行中，用户在 PowerShell 执行 `wsl --shutdown`
- **THEN** 托盘通知 WebMux 已退出，约 2 秒后重新启动，图标恢复为运行中

#### Scenario: 持续崩溃

- **WHEN** WebMux 每次启动后立即以非零码退出
- **THEN** 托盘在 5 次失败后停止重试，图标显示异常，状态行提示“多次启动失败，请查看日志”
