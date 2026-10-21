# Spec Delta

## Purpose

让 Windows 托盘用户在 WebMux 运行时一键开启或关闭 Cloudflare 公网隧道，看到并复制公网地址，同时在开启前明确知晓公开 shell 的风险。

## ADDED Requirements

### Requirement: 隧道开关

WebMux 运行且健康时，托盘菜单 SHALL 显示“公网隧道（Cloudflare）”勾选项，勾选状态反映 `starting` 或 `ready`。实例不支持运行时隧道控制（状态中没有 `tunnel` 字段）时 SHALL 隐藏该项。外部实例 SHALL 同样可以开关隧道，因为开关不重启实例。

#### Scenario: 开启后显示地址

- **WHEN** 用户勾选“公网隧道”并确认风险
- **THEN** 菜单先显示“公网隧道：正在连接”，就绪后显示公网地址，会话不受影响

#### Scenario: 旧版服务端

- **WHEN** WSL 中运行的 webmux 不报告 `tunnel` 状态
- **THEN** 菜单中没有“公网隧道”项

### Requirement: 开启前确认

每次从关闭到开启时，托盘 SHALL 先弹出确认，说明“任何获得链接和 Token 的人都能以你的身份执行命令”。用户取消时 SHALL NOT 发出开启请求。

#### Scenario: 取消确认

- **WHEN** 用户勾选“公网隧道”后在确认框中取消
- **THEN** 隧道保持关闭，没有 cloudflared 进程

### Requirement: Token 不足时的引导

服务端因 Token 缺失或过短拒绝开启时，托盘 SHALL 显示原因；配置文件不存在时提供“初始化配置（生成 Token）”，否则提示修改配置后重启。

#### Scenario: 未配置 Token

- **WHEN** 实例未配置 Token，用户确认开启隧道
- **THEN** 托盘通知开启失败及原因，勾选项保持未勾选

### Requirement: 公网地址复制

隧道 `ready` 时，托盘 SHALL 提供“复制公网链接”，复制内容 SHALL NOT 包含 Token；Token 仍只能通过“复制 Token”获得。

#### Scenario: 复制链接

- **WHEN** 隧道就绪，用户点击“复制公网链接”
- **THEN** 剪贴板内容为 `https://<公网主机>/`，不含 `#token=`

### Requirement: 隧道失败通知

隧道从 `starting` 或 `ready` 变为 `failed` 时，托盘 SHALL 发出一次通知并在菜单中显示原因；托盘 SHALL NOT 自动重新开启。

#### Scenario: 隧道断开

- **WHEN** 隧道就绪后 cloudflared 意外退出
- **THEN** 托盘通知“公网隧道已断开”及原因，勾选项变为未勾选

### Requirement: 隧道不随托盘持久化

托盘 SHALL NOT 保存隧道开关状态；WebMux 重启（包括托盘的自动重启）后隧道 SHALL 处于关闭状态，除非服务端配置自身启用了隧道。托盘因 WebMux 重启而关闭隧道时 SHALL 在通知中说明。

#### Scenario: 自动重启后

- **WHEN** 隧道开启时 WebMux 崩溃并被托盘自动重启
- **THEN** 新实例隧道关闭，托盘通知中说明公网隧道已关闭
