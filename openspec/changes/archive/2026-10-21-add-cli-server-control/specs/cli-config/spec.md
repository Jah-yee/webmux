## Purpose

让客户端（CLI、托盘）在不改写用户手写配置的前提下定位、检查和初始化 WebMux 配置，并以与服务端完全一致的规则得到生效配置，避免各客户端各自解析 TOML。

## ADDED Requirements

### Requirement: 定位与查看生效配置

`webmux config path` SHALL 输出将被使用的配置文件绝对路径（`--config` 指定时为其展开后的路径，否则为默认路径）及其是否存在。

`webmux config show` SHALL 按与服务启动相同的规则（默认值、缺失文件、显式路径不存在即失败、`Validate`）计算生效配置并输出；`--json` 时向标准输出写入恰好一个 JSON 对象，包含 `v`（本变更为 `1`）、`path`、`exists`、`listen`、`tokenConfigured`、`defaultCwd`、`stateDir`、`allowedOrigins`、`presets`（名称、标签、argv、是否允许追加参数）。输出 SHALL NOT 包含 Token。

#### Scenario: 无配置文件

- **WHEN** 默认路径下没有配置文件，用户执行 `webmux config show --json`
- **THEN** 输出 `"exists": false`、`"listen": "127.0.0.1:7681"`、`"tokenConfigured": false`，退出码 0

#### Scenario: 不输出 Token

- **WHEN** 配置了 Token，用户执行 `webmux config show` 或 `webmux config show --json`
- **THEN** 输出中只有 `tokenConfigured: true`，不出现 Token 字符串

### Requirement: 校验配置

`webmux config validate` SHALL 对配置执行与服务启动相同的解析与校验，成功时退出码 0；失败时向标准错误输出可定位的原因（TOML 语法错误的行列、非法字段值、预设冲突等），退出码 1。`--json` 时标准输出为包含 `v`、`ok`、`errors` 数组的 JSON 对象。

校验 SHALL NOT 修改任何文件，也 SHALL NOT 要求服务正在运行。

#### Scenario: 语法错误

- **WHEN** 配置文件第 3 行缺少引号
- **THEN** `webmux config validate` 输出包含行号的错误并以退出码 1 结束

### Requirement: 初始化配置

`webmux config init` SHALL 在配置路径不存在时，以内置的示例配置为模板创建配置文件：生成一个密码学安全的随机 Token（至少 256 位熵）写入 `token`，`--listen <addr>` 指定时同时写入 `listen`；文件权限 SHALL 为 0600，父目录不存在时 SHALL 以 0700 创建。命令输出 SHALL 只包含文件路径，SHALL NOT 输出 Token。

目标文件已存在时 SHALL 拒绝并以退出码 1 结束；传入 `--force` 时 SHALL 先将原文件保留为同目录下的 `.bak`，再创建新文件。

客户端 SHALL NOT 以程序方式修改已有配置文件中的单个字段；修改配置由用户在编辑器中完成。

#### Scenario: 首次初始化

- **WHEN** 配置文件不存在，用户执行 `webmux config init`
- **THEN** 生成 0600 的配置文件，其中 `token` 为新随机值、其余项与示例配置一致；标准输出只有文件路径

#### Scenario: 已存在时拒绝

- **WHEN** 配置文件已存在，用户执行 `webmux config init`
- **THEN** 原文件内容不变，命令提示使用 `--force` 并以退出码 1 结束

#### Scenario: 强制覆盖

- **WHEN** 配置文件已存在，用户执行 `webmux config init --force`
- **THEN** 原内容保存在 `config.toml.bak`，新文件包含新 Token
