## Purpose

在 WSL 网络无法被局域网或 Tailscale 设备直接访问的特殊情况下（典型为 NAT 网络模式），由托盘在 Windows 主机上提供受控的转发入口，使手机等设备能访问 WSL 中的 WebMux，同时保持与直连相同的认证、Host 与 Origin 安全边界。

## ADDED Requirements

### Requirement: 适用判定与推荐

托盘 SHALL 在“远程访问”子菜单中按以下顺序判断并显示当前推荐：

1. WSL 处于镜像（mirrored）网络模式，或 `webmux status` 报告了可用的远程链接：显示“无需转发”及这些链接，转发条目 SHALL 禁用并说明原因；
2. 否则：显示“可通过托盘转发”，并列出 Windows 主机上可选的转发地址。

转发 SHALL 默认关闭，只在用户显式勾选某个地址后启用；托盘 SHALL 在托盘设置中保存已勾选的地址，下次托盘启动并且 WebMux 运行时自动恢复。

#### Scenario: 镜像网络模式

- **WHEN** WSL 使用镜像网络模式
- **THEN** 子菜单显示“无需转发（镜像网络模式）”，转发地址条目全部禁用

#### Scenario: NAT 模式下的推荐

- **WHEN** WSL 使用 NAT 模式，WebMux 只监听回环地址，Windows 主机有 Tailscale 地址 100.64.0.9 与局域网地址 192.168.1.20
- **THEN** 子菜单列出 `100.64.0.9`（Tailscale，置顶）与 `192.168.1.20`（局域网）两个可勾选的转发地址，均未勾选

### Requirement: 启用条件

只有同时满足以下条件时，托盘 SHALL 允许启用转发：WebMux 正在运行且健康、配置了非空 Token、Windows 侧可经本机链接访问 WebMux。任一条件不满足时，转发条目 SHALL 禁用并说明原因；已启用的转发 SHALL 暂停监听，条件恢复后自动恢复。

未配置 Token 时，提示 SHALL 提供“初始化配置（生成 Token）”的入口（仅当 WebMux 配置文件不存在时），或说明需要在配置中设置 Token 并重启 WebMux。

#### Scenario: 未设置 Token

- **WHEN** WebMux 未配置 Token，用户打开“远程访问”子菜单
- **THEN** 转发地址条目禁用，并提示“转发会把 shell 暴露给网络，请先设置 Token”

#### Scenario: WebMux 停止

- **WHEN** 转发已在 `100.64.0.9:7681` 启用，用户停止 WebMux
- **THEN** 托盘停止在该地址监听；WebMux 再次就绪后自动恢复监听

### Requirement: 监听范围

转发 SHALL 只监听用户勾选的具体 Windows 主机地址，端口与 WebMux 实际端口相同；SHALL NOT 监听通配地址或回环地址。端口已被占用时 SHALL 显示该地址启用失败及原因，不影响其他地址。Windows 主机地址消失（例如断开网络）时 SHALL 停止该地址的监听并在地址重新出现后恢复。

#### Scenario: 端口被占用

- **WHEN** Windows 主机上已有程序监听 `192.168.1.20:7681`，用户勾选该地址
- **THEN** 该条目显示“启用失败：端口已被占用”，其他已勾选地址照常转发

### Requirement: 转发的安全边界

转发 SHALL 是对 WebMux HTTP 与 WebSocket 流量的透明中转，并 SHALL 满足：

- 只接受 `Host` 恰好为所监听地址与端口的请求，其他 `Host` 一律以 403 拒绝（防 DNS 重绑定）；
- 请求带 `Origin` 时，只接受与转发入口同源（`http://<监听地址>:<端口>`）的请求，否则以 403 拒绝；
- 通过校验的请求，转发 SHALL 把 `Host` 与同源的 `Origin` 改写为 WebMux 本机链接对应的值后转给 WebMux，其余请求头（含 `Authorization`、Cookie）与请求体原样传递；
- 转发 SHALL NOT 添加、读取或记录 Token，SHALL NOT 记录请求体、响应体或 WebSocket 消息内容；认证仍完全由 WebMux 判定；
- WebSocket 升级与二进制帧 SHALL 原样双向传递，不缓冲整条消息；任一方向关闭时 SHALL 关闭另一方向。

#### Scenario: 手机经转发访问

- **WHEN** 转发启用于 `100.64.0.9:7681`，手机浏览器打开 `http://100.64.0.9:7681` 并输入正确 Token
- **THEN** 手机可以列出会话、打开终端、输入并看到实时输出，与直连行为一致

#### Scenario: 错误的 Token

- **WHEN** 经转发的请求携带错误 Token
- **THEN** 返回 WebMux 的 401 响应，转发本身不改变结果

#### Scenario: 跨站请求

- **WHEN** 第三方网页向 `http://100.64.0.9:7681/api/sessions` 发起带 `Origin: https://evil.example` 的请求
- **THEN** 转发以 403 拒绝，请求不会到达 WebMux

#### Scenario: DNS 重绑定

- **WHEN** 请求的 `Host` 为 `evil.example:7681`，但连接到了转发地址
- **THEN** 转发以 403 拒绝

### Requirement: 资源与生命周期

转发 SHALL 限制并发连接数（默认 64）并在连接空闲 10 分钟后关闭非 WebSocket 连接；WebSocket 连接只在任一端关闭或 WebMux 停止时结束。托盘退出时 SHALL 关闭所有转发监听与连接；退出确认对话框 SHALL 说明“仅退出托盘”会停止转发。

托盘菜单中的远程链接 SHALL 同时列出转发地址并标注“经托盘转发”。

#### Scenario: 退出托盘提示

- **WHEN** 转发已启用，用户选择“退出”
- **THEN** 对话框说明转发将停止；选择“仅退出托盘”后手机无法再经转发地址访问，WSL 中的 WebMux 继续运行
