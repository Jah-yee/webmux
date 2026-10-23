# windows-tray-server-compat Specification

## Purpose
定义 Windows 托盘如何判断 WSL 中的 `webmux`（已安装文件与正在运行的实例）与托盘是否兼容、版本是否对应，何时检查，以及如何在状态行展示结果并提供更新入口。

## Requirements

### Requirement: 检查时机

托盘 SHALL 在以下时机检查所选发行版中配置路径上的 `webmux`：托盘启动后首次取得状态时、切换发行版或修改 `webmux` 路径后、安装或升级完成后、以及处于缺失或不兼容状态时打开菜单。检查 SHALL 在后台进行，SHALL NOT 阻塞菜单弹出，单次检查 SHALL NOT 超过 30 秒。检查结果 SHALL 写入托盘日志，包括托盘与 WSL 两侧的版本与契约版本。

#### Scenario: 启动时检查

- **WHEN** 托盘 0.3.0 启动，WSL 中的 `webmux` 为 0.2.0
- **THEN** 托盘取得状态后完成检查，日志中出现两侧版本与契约版本，菜单在检查期间仍可打开

#### Scenario: 切换发行版

- **WHEN** 用户把目标发行版从 Ubuntu 切换到 Debian，Debian 中的 `webmux` 版本不同
- **THEN** 托盘重新检查并以 Debian 中的结果更新状态行

### Requirement: 兼容判定

托盘 SHALL 内置自己支持的控制契约版本集合。WSL 中的 `webmux` 报告的契约版本在集合内即为兼容；低于集合为“不兼容：WSL 过旧”，高于集合为“不兼容：WSL 过新”。不支持机器可读版本输出的 `webmux` SHALL 沿用现有探测：支持本机 API 时视为契约 v1；不支持本机 API 时为“过旧（缺少本机 API）”。无法执行时为“缺失”。

#### Scenario: 契约版本过高

- **WHEN** 托盘支持契约 v1，WSL 中的 `webmux` 报告契约 v2
- **THEN** 托盘判定“不兼容：WSL 过新”

#### Scenario: 旧版本没有机器可读版本输出

- **WHEN** WSL 中的 `webmux` 不认识 `version --json`，但支持本机 API
- **THEN** 托盘视其为契约 v1，判定为兼容，并以 `webmux version` 的文本输出取得版本号

### Requirement: 版本对应判定

兼容时，托盘 SHALL 只比较版本号：两侧相同为“对应”；否则按 `MAJOR.MINOR.PATCH` 比较归为“WSL 较旧”或“WSL 较新”。任一侧版本号不是 `MAJOR.MINOR.PATCH`（如 `dev`）时 SHALL 归为“无法判断”，按兼容处理且不提示更新。托盘 SHALL NOT 以构建提交区分版本号相同的构建。

#### Scenario: 发布版本对应

- **WHEN** 托盘与 WSL 中的 `webmux` 都报告 0.3.0
- **THEN** 托盘判定“对应”，状态行不显示版本提示

#### Scenario: 本地构建

- **WHEN** 两侧都是本地 `make` 构建、都报告 0.1.0，即使来自不同提交
- **THEN** 托盘判定“对应”

#### Scenario: 开发构建

- **WHEN** WSL 中的 `webmux` 报告版本 `dev`
- **THEN** 托盘判定“无法判断”，照常工作，不提示更新

### Requirement: 状态行提示

托盘 SHALL 只通过菜单状态行提示版本结果，SHALL NOT 为此发出系统通知或弹出对话框。兼容但不对应时状态行 SHALL 显示“托盘 <版本> · WSL <版本>”并指出较旧的一方；不兼容时 SHALL 显示方向：“WSL 中的 webmux 版本过旧，请升级”或“WSL 中的 webmux <版本> 比托盘新，请升级托盘”。对应或无法判断时 SHALL NOT 显示版本提示。

#### Scenario: WSL 较旧

- **WHEN** 托盘为 0.3.0，WSL 中的 `webmux` 为 0.2.0 且兼容
- **THEN** 状态行显示“托盘 0.3.0 · WSL 0.2.0（WSL 较旧）”，没有系统通知

#### Scenario: 不兼容

- **WHEN** WSL 中的 `webmux` 报告契约 v2，托盘只支持 v1
- **THEN** 状态行显示“WSL 中的 webmux 0.5.0 比托盘新，请升级托盘”，没有系统通知或对话框

### Requirement: 不兼容时仍可启动

契约不兼容时，托盘 SHALL 照常允许启动、停止与重启，同时显示不兼容提示与更新入口。托盘无法解析不兼容实例的状态时，SHALL 以 Windows 侧可达性判断其是否在运行，并在状态行注明“不兼容，功能受限”；SHALL NOT 因状态无法解析而判定启动失败或进入自动重启循环。WSL 中的 `webmux` 缺少本机 API 时，由托盘启动无法进行，SHALL 继续禁用“启动”，只提供更新入口。

#### Scenario: 启动比托盘新的 webmux

- **WHEN** WSL 中的 `webmux` 报告契约 v2，用户点击“启动”
- **THEN** WebMux 启动，Windows 侧可达后托盘显示“运行中（不兼容，功能受限）”，菜单同时提供“下载托盘 <版本>”

#### Scenario: 缺少本机 API

- **WHEN** WSL 中的 `webmux` 不支持本机 API
- **THEN** “启动”禁用，菜单提供“升级 WSL 中的 webmux”

### Requirement: 更新入口

托盘 SHALL 按方向提供更新入口：WSL 过旧或较旧时提供“升级 WSL 中的 webmux”（安装与托盘同版本的 `webmux`，行为见 `windows-tray-wsl-install`）；WSL 过新或较新且其版本为 `MAJOR.MINOR.PATCH` 时提供“下载托盘 <版本>”，用默认浏览器打开该版本的 Release 页面。托盘 SHALL NOT 在后台自动安装、自动更新自身或自动重启 WebMux。无法判断时 SHALL NOT 提供更新入口。更新完成后的下一次检查 SHALL 更新状态行。

#### Scenario: WSL 较新

- **WHEN** 托盘为 0.3.0，WSL 中的 `webmux` 为 0.4.0 且兼容
- **THEN** 菜单出现“下载托盘 0.4.0”，点击后浏览器打开 `v0.4.0` 的 Release 页面

#### Scenario: 升级后恢复

- **WHEN** WSL 较旧，用户点击“升级 WSL 中的 webmux”并安装成功
- **THEN** 托盘重新检查，判定为对应，状态行不再显示版本提示

### Requirement: 运行实例与已安装文件不一致

WebMux 正在运行时，托盘 SHALL 以本机状态中的版本识别运行中的实例，并对它同样做对应判定。运行中实例的版本与已安装文件不同时，托盘 SHALL 在状态行提示“WSL 中已安装 <版本>，重启 WebMux 后生效，重启会结束所有会话”；仅当实例由托盘管理时 SHALL 提供重启，SHALL NOT 自动重启。

#### Scenario: 升级后未重启

- **WHEN** 由 systemd 管理的 WebMux 0.2.0 正在运行，用户已把 WSL 中的文件升级到 0.3.0
- **THEN** 状态行提示重启后生效，菜单不提供由托盘重启
