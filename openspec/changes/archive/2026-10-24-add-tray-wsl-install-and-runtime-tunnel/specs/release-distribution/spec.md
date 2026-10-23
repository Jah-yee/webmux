# Spec Delta

## Purpose

定义 WebMux 正式版本的发布方式：一次标签发布产出所有平台二进制与校验和，并让 Windows 托盘内置同版本 Linux 二进制的摘要，使托盘可以安全地安装服务端。

## ADDED Requirements

### Requirement: 标签触发发布

推送形如 `v<版本>` 的 Git 标签时，发布流水线 SHALL 以该版本号构建全部发布产物，并创建同名 GitHub Release 上传它们。产物 SHALL 包括 `webmux-linux-amd64`、`webmux-linux-arm64`、`webmux-darwin-amd64`、`webmux-darwin-arm64`、`webmux-windows-amd64.exe`、`webmux-windows-arm64.exe`、`webmux-tray-windows-amd64.exe`、`webmux-tray-windows-arm64.exe` 与 `SHA256SUMS`。每个二进制 SHALL 报告该版本号。构建或测试失败时 SHALL NOT 创建或更新 Release。

#### Scenario: 发布新版本

- **WHEN** 维护者推送标签 `v0.2.0`
- **THEN** Release `v0.2.0` 包含上述全部文件，`webmux version` 输出 `webmux 0.2.0`

#### Scenario: 构建失败

- **WHEN** 推送标签后某个平台构建失败
- **THEN** 不出现该标签的 Release，也不上传任何部分产物

### Requirement: 校验和文件

`SHA256SUMS` SHALL 为 Release 中每个其他产物各列一行，格式与 `sha256sum` 输出一致，可用 `sha256sum -c` 直接校验。

#### Scenario: 用户校验下载

- **WHEN** 用户下载 `webmux-linux-amd64` 与 `SHA256SUMS` 后执行 `sha256sum -c --ignore-missing SHA256SUMS`
- **THEN** 该文件校验通过

### Requirement: 托盘内置服务端摘要

发布构建的托盘 SHALL 内置同一次构建产出的 `webmux-linux-amd64` 与 `webmux-linux-arm64` 的 SHA-256。托盘 SHALL 只信任内置摘要，SHALL NOT 以下载得到的 `SHA256SUMS` 作为校验依据。未内置摘要的托盘（如开发构建）SHALL 报告“此版本托盘不支持安装 webmux”，且不提供下载。

#### Scenario: 发布版托盘

- **WHEN** 用户运行 Release 中的托盘
- **THEN** 托盘内置的两个摘要与同一 Release 的 `SHA256SUMS` 中对应行一致

#### Scenario: 开发构建

- **WHEN** 用户运行本地 `make tray` 构建、未注入摘要的托盘，且 WSL 中缺少 webmux
- **THEN** 托盘显示缺少 webmux 及“此版本托盘不支持安装 webmux”，菜单没有可用的安装动作
