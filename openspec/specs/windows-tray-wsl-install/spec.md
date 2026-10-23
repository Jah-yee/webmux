# windows-tray-wsl-install Specification

## Purpose
让只安装了 Windows 托盘的用户无需进入 WSL，即可由托盘把与托盘同版本、经过摘要校验的 Linux 版 webmux 安装或升级到 WSL 中，并在问题解决后自动恢复正常状态。

## Requirements

### Requirement: 服务端可用性分类

托盘 SHALL 把所选发行版中配置路径上的 webmux 归为以下之一：缺失（无法执行）、不兼容（缺少托盘依赖的功能或状态契约）、较旧（可用，但版本号低于托盘）、可用。版本号 SHALL 按语义化版本比较；任一方为非发布版本（如 `dev`）时 SHALL 视为可用，不提示升级。WSL 中的 webmux 版本高于托盘时 SHALL 视为可用。

#### Scenario: 版本较旧

- **WHEN** 托盘为 0.2.0，WSL 中的 webmux 报告 0.1.0 且兼容
- **THEN** 托盘照常管理 WebMux，状态行附加“WSL 中的 webmux 0.1.0 可升级到 0.2.0”

#### Scenario: 开发版本

- **WHEN** WSL 中的 webmux 报告版本 `dev`
- **THEN** 托盘不提示升级

### Requirement: 一键安装与升级

处于缺失、不兼容或较旧状态时，托盘菜单 SHALL 提供“安装 WSL 中的 webmux”或“升级 WSL 中的 webmux”。托盘 SHALL NOT 在用户未选择该动作时下载或改写 WSL 中的文件。安装 SHALL 写入托盘设置中的 webmux 路径（默认 `~/.local/bin/webmux`），必要时创建父目录，文件权限为 0755。

#### Scenario: 缺失时安装

- **WHEN** WSL 中没有 `~/.local/bin/webmux`，用户点击“安装 WSL 中的 webmux”
- **THEN** 该路径出现与托盘同版本的可执行文件，托盘状态变为“已停止”，“启动”可用

#### Scenario: 不自动安装

- **WHEN** 托盘检测到 webmux 缺失，用户没有点击安装
- **THEN** WSL 中的文件系统没有任何变化

### Requirement: 架构选择

托盘 SHALL 在安装前检测所选发行版的 CPU 架构，`x86_64` 选择 `linux-amd64`，`aarch64` 选择 `linux-arm64`；其他架构 SHALL 报告“不支持的 WSL 架构 <名称>”并不下载。

#### Scenario: ARM 设备

- **WHEN** 发行版的 `uname -m` 为 `aarch64`
- **THEN** 托盘下载并安装 `webmux-linux-arm64`

### Requirement: 下载来源与完整性

托盘 SHALL 从 GitHub Release 中与托盘同版本的标签下载所需文件；GitHub 在 10 秒内无响应或 20 秒没有数据时 SHALL 依次尝试内置镜像。任何来源的文件 SHALL 在 SHA-256 与托盘内置摘要一致后才可安装；不一致 SHALL 丢弃并尝试下一个来源。全部来源失败时 SHALL 报告每个来源的失败原因，WSL 中原有的文件 SHALL 保持不变。

#### Scenario: GitHub 不可达

- **WHEN** GitHub 连接超时，第一个镜像返回正确文件
- **THEN** 安装成功，日志记录使用的来源

#### Scenario: 摘要不符

- **WHEN** 所有来源返回的文件摘要都与内置摘要不同
- **THEN** 安装失败并列出各来源原因，原有 webmux 不变，没有执行下载的文件

### Requirement: 安装过程不经 shell

托盘 SHALL 只以 argv 形式调用 WSL 中的程序完成安装，SHALL NOT 调用 shell 或拼接命令字符串。被替换的旧文件 SHALL 不影响正在运行的 WebMux 进程。

#### Scenario: 路径含特殊字符

- **WHEN** 托盘设置中的 webmux 路径为 `~/my bin/web;mux`
- **THEN** 文件被安装到字面路径 `~/my bin/web;mux`，没有命令被执行

### Requirement: 安装后的生效

安装成功后托盘 SHALL 立即重新检测并更新状态与菜单。WebMux 正在运行时，托盘 SHALL 提示“新版本在重启 WebMux 后生效，重启会结束所有会话”；仅当实例由托盘管理时 SHALL 提供重启，SHALL NOT 自动重启。

#### Scenario: 运行中升级

- **WHEN** 托盘管理的 WebMux 正在运行，有 2 个会话，用户完成升级
- **THEN** 会话继续运行，托盘提示重启后生效并提供重启；用户确认前不重启

### Requirement: 失败状态恢复

因缺失或不兼容进入失败状态后，托盘 SHALL 在用户打开菜单时与安装完成后重新检测；检测通过 SHALL 回到“已停止”（或检测到的运行状态）。失败期间 SHALL NOT 自动启动 WebMux，也 SHALL NOT 定时重复检测。

#### Scenario: 手动安装后恢复

- **WHEN** 托盘显示“找不到 webmux”，用户在 WSL 中自行安装后右键打开托盘菜单
- **THEN** 托盘状态变为“已停止”，“启动”可用

#### Scenario: 仍然缺失

- **WHEN** 托盘显示“找不到 webmux”，用户未做任何事再次打开菜单
- **THEN** 状态不变，不启动 WebMux
