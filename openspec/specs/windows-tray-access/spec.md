# windows-tray-access Specification

## Purpose
让 Windows 用户从托盘快速获取 WSL 中 WebMux 的访问方式：打开浏览器、复制本机与远程链接、按需复制 Token，同时不降低 WebMux 既有的认证与暴露面约束。

## Requirements

### Requirement: 链接来源

托盘显示与复制的本机链接、远程链接及暴露警告 SHALL 直接取自 WebMux 本机 API 报告的状态，托盘 SHALL NOT 自行解析 WebMux 配置文件或另行推导链接。菜单与通知中显示的链接 SHALL NOT 包含 Token；复制规则见“复制链接带 Token”。

状态报告配置与运行实例不一致（`drift` 非空）时，状态行 SHALL 提示“配置已修改，重启 WebMux 后生效”，复制的链接仍对应当前运行实例。

#### Scenario: 默认配置

- **WHEN** WSL 中 WebMux 使用默认配置运行
- **THEN** “复制本机链接”写入 `http://127.0.0.1:7681`（未配置 Token），菜单不显示“复制 Token”和“复制远程链接”

#### Scenario: 运行中修改监听地址

- **WHEN** WebMux 运行中，用户把配置中的 `listen` 改为 `100.64.0.5:7681`
- **THEN** 状态行提示配置已修改需重启生效，“复制本机链接”仍为 `http://127.0.0.1:7681`

### Requirement: 打开与 Windows 侧可达性

“打开 WebMux”与左键单击 SHALL 用 Windows 默认浏览器打开本机链接。

托盘 SHALL 从 Windows 侧直接请求本机链接的健康检查以确认可达；该请求 SHALL NOT 携带 Token，返回成功或“需要认证”都视为可达。WebMux 在 WSL 中运行但 Windows 侧不可达时，状态行 SHALL 提示检查 WSL 的 localhost 转发或网络模式设置，图标 SHALL 显示异常而不是已停止。

#### Scenario: Windows 侧不可达

- **WHEN** WebMux 在发行版内健康运行，但 Windows 侧访问 `127.0.0.1:7681` 连接失败
- **THEN** 状态行提示“WebMux 已在 WSL 中运行，但 Windows 无法访问 127.0.0.1:7681，请检查 WSL localhostForwarding / networkingMode”

#### Scenario: 配置了 Token 时的可达性检查

- **WHEN** WebMux 配置了 Token，托盘从 Windows 侧检查可达性
- **THEN** 健康检查请求不含 Token，收到“需要认证”响应即判定可达

### Requirement: 远程链接

远程链接 SHALL 由状态报告的远程链接与已启用的托盘转发地址（标注“经托盘转发”，排在状态报告的链接之后）组成。只有一项时，菜单 SHALL 显示“复制远程链接”并直接复制该链接；有多项时 SHALL 展开为子菜单，每项显示链接、网卡名与类型（Tailscale / 局域网），状态报告部分的顺序与报告一致。状态报告包含暴露警告时，子菜单顶部（或单项旁）SHALL 显示“未设置 Token，任何能访问该端口的人都可获得 shell”。

#### Scenario: 通配监听且无 Token

- **WHEN** `listen = "0.0.0.0:7681"`、`token = ""`，发行版内有 Tailscale 地址 100.64.0.5
- **THEN** 远程链接子菜单顶部显示无 Token 警告，其下第一项为 `http://100.64.0.5:7681`

### Requirement: 复制链接带 Token

配置了 Token 时，每个“复制…链接”（本机、远程、经托盘转发、公网）SHALL 在点击时按“复制 Token”的方式取得 Token，复制 `<链接>/#token=<Token>`，并 SHALL 按与“复制 Token”相同的规则排除剪贴板历史与云剪贴板、60 秒后若未被替换则清空。未配置 Token 时 SHALL 复制原链接。

#### Scenario: 复制带 Token 的本机链接

- **WHEN** WebMux 配置了 Token，用户点击“复制本机链接”
- **THEN** 剪贴板内容为 `http://127.0.0.1:7681/#token=<Token>`，通知只显示 `http://127.0.0.1:7681`，60 秒后自动清除

### Requirement: 复制 Token

仅当状态报告配置了 Token 时，菜单 SHALL 显示“复制 Token”。托盘 SHALL 只在用户点击时经桥接请求本机 API 的 Token 接口，写入剪贴板后 SHALL 立即丢弃，不在托盘中缓存，也 SHALL NOT 写入任何 Windows 侧文件。复制的内容 SHALL 只有 Token 本身，写入剪贴板时 SHALL 标记为不进入 Windows 剪贴板历史与跨设备云剪贴板；托盘 SHALL 在 60 秒后若剪贴板内容仍为该 Token 则清空剪贴板。

托盘 SHALL NOT 在菜单、悬停提示、通知或日志中显示 Token 明文。

#### Scenario: 复制 Token 后自动清除

- **WHEN** 用户点击“复制 Token”且 60 秒内未复制其他内容
- **THEN** 60 秒后剪贴板为空，Windows 剪贴板历史（Win+V）中不出现该 Token

#### Scenario: 期间复制了其他内容

- **WHEN** 用户复制 Token 后 30 秒内又复制了一段普通文本
- **THEN** 60 秒到期时剪贴板中的普通文本保持不变

### Requirement: 复制反馈

每次复制操作成功后，托盘 SHALL 给出简短的 Windows 通知（例如“已复制 http://127.0.0.1:7681”）；复制 Token 与带 Token 链接的通知 SHALL NOT 包含 Token。剪贴板被其他程序占用导致写入失败时 SHALL 通知失败原因。

#### Scenario: 复制成功提示

- **WHEN** 用户点击“复制本机链接”
- **THEN** 出现“已复制 http://127.0.0.1:7681”的通知
