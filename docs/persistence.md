# 配置与持久化清单

本页记录当前实现，不把 OpenSpec 的目标行为当成已实现功能。本 PR 实现统一 JSON 基础设施与偏好同步；后续变更必须同步运行时描述符、清单和源码引用。

## 当前服务端存储

以下路径除 `config.toml` 外均相对于 `state_dir`；`none` 表示没有对应跨进程锁。TOML 位置由 `-config` 或默认配置目录决定。运行时目录中的 socket 不是持久化 JSON，不能用备份记录替代活进程身份验证。

<!-- persistence-inventory:start -->

| ID                  | 路径                     | 锁                            | 所有者                       | 损坏或冲突            | 网页可改 |
| ------------------- | ------------------------ | ----------------------------- | ---------------------------- | --------------------- | -------- |
| bootstrap           | config.toml              | none                          | internal/config + cmd/webmux | 配置错误阻止启动      | 否       |
| launch              | launch-configs.json      | launch-configs.json.lock      | internal/launch              | unavailable           | 是       |
| repositories        | repositories.json        | repositories.json.lock        | internal/workspace           | unavailable           | 是       |
| tunnel              | tunnel-settings.json     | tunnel-settings.json.lock     | internal/tunnelsettings      | fail_closed           | 是       |
| tailscale-settings  | tailscale-settings.json  | tailscale-settings.json.lock  | internal/tailscalesettings   | fail_closed           | 是       |
| tailscale-ownership | tailscale-ownership.json | tailscale-ownership.json.lock | cmd/webmux                   | block_start           | 否       |
| push                | push.json                | push.json.lock                | internal/push                | unavailable           | 是       |
| runtime             | run/server.json          | run/server.json.lock          | internal/runtime             | local_api_unavailable | 否       |
| settings            | settings.json            | settings.json.lock            | internal/settings            | unavailable           | 是       |

<!-- persistence-inventory:end -->

这份表的机器来源是 `internal/archtest/persistence_inventory.json`；`make persistence-check` 精确比较表格各单元格（允许 Markdown 对齐空格变化），拒绝遗漏、重复或过时条目。

## 统一行为与故障隔离

- 每个 JSON 存储通过 `statefile.Open[T]` 取得注册 handle 和独立生命周期锁；数据与锁均为 0600、目录 0700。正常关闭显式解锁但不删除锁文件。`state_dir` 可为指向真实目录的符号链接；数据文件与锁自身仍拒绝符号链接。
- 读取有大小限制，拒绝符号链接、非普通文件、重复/未知/大小写变体键、缺失必需字段、非法 UTF-8、错误版本、尾随值与过深嵌套。损坏原文件保留。
- Commit 和 Remove 强制目录同步。发布前失败保留旧状态；发布后同步失败报告 `commit_uncertain` 并冻结写入，不广播成功、不用旧内存覆盖已发布文件。
- `unavailable` 只禁用对应功能；push 不再改名重建密钥。首次确认文件不存在时仍可初始化。
- `fail_closed` 表示覆盖文件异常时关闭对应入口，不能从 TOML 回退为启用；紧急关闭仍可停止当前入口，但写盘失败必须报告未持久化。
- `tailscale-settings.json` 保存用户希望的 mode/port/provider；`tailscale-ownership.json` 保存本实例实际创建的外部入口（精确目标、端口、pending/active），供撤销和重启对账。它们的职责、权限和失败策略不同。
- 旧 `tailscale.json` 即使损坏或与新文件并存，也在监听前阻止启动。先停止旧版本并核实/撤销其精确外部入口，再整理旧文件；不执行全局 reset。旧 `tunnel.json` 使 Cloudflare 保持关闭。新版不自动迁移文件或旧浏览器偏好。
- `run/server.json.lock` 仅保护运行时记录和本机控制面；锁冲突退化为 TCP 服务，不能与 Tailscale 安全阻止启动混淆。
- 升级前停止全部旧进程并备份；新旧版本不得共用状态目录并发运行，改名后的锁无法与旧锁互斥。
- Windows 托盘、日志、下载缓存、上传文件与 baseline 结果属于独立生命周期，保持逐项登记的例外。

## 磁盘与活进程诊断

`webmux config show [--json]` 只读展示磁盘解析结果；不创建目录/锁、不 chmod、不修复。`--live` 经同 UID socket 查询已验证进程的内存快照；磁盘漂移不改变该快照，不可达则报错。字段来源为 default/toml/web/cli/generated；凭据只返回是否配置。存储健康另有状态、提交阶段与观察时间，离线锁标为 not_checked，不能据此声称可写。

## 机械校验

```sh
make persistence-check
```

守卫位于现有 `internal/archtest`，直接解析 `cmd/` 与 `internal/` 所有平台的生产 Go 文件，不受本机 GOOS 限制。测试文件、testdata 和前端输出不参与生产扫描。

- 每个 `.json` / `.lock` 字符串及选定的文件创建、替换、删除、底层打开和锁函数引用，都登记到具体文件、函数、符号与出现次数。
- 解析导入别名；引用函数值也计数；拒绝对受检查包使用点导入。新增入口、重复调用、删除后遗留登记、无主登记、无理由例外或缺失来源文件都会失败。
- 每个存储的数据路径、派生锁名、所有者与失败策略必须匹配运行时 descriptor；数据路径必须出现在登记引用中。临时 JSON 模式与设备文件也需要登记，不能凭命名自动豁免。
- 负向样例验证新存储、旧函数中的新增调用、别名、函数值、Windows 文件、清单漏行及错误策略漂移确实被拒绝。
- `make check` 和 CI 的格式/构建 job 显式执行，常规 Go 测试也会运行。失败时根据诊断更新具体登记与本表，不能扩大目录级白名单。

## 校验边界

这是当前架构的防漂移入口守卫，不是完整的数据流证明。它无法推断已经登记函数内部任意动态路径的新含义，也不能证明 chmod、fsync、锁时序、错误策略描述与所有执行路径一致；外部进程和反射写文件不在扫描能力内。更换未覆盖的 I/O 库必须扩展扫描规则与负向样例。

所有领域使用已注册 typed handle；除本机 socket 清理外，领域原始写/锁入口已经撤销。逐 descriptor 契约测试覆盖权限、锁与重开；共享故障注入覆盖创建、chmod、短写、写入、Sync、Close、rename 和目录同步阶段，以及删除后同步不确定。领域与 API 测试进一步验证广播时机、降级与脱敏。

## 变更流程

新增存储前明确所属层、生命周期、数据/锁名、所有者、敏感性、失败后果和网页可编辑边界。登记真实读写入口及对应测试后同步清单；仍在规划的能力只写 OpenSpec，不登记为已实现。

详细目标、破坏性改名和分阶段验收见 [配置与状态治理提案](../openspec/changes/govern-config-state/proposal.md)。部署总览保留在 [部署指南](deployment.md)，架构分层见 [架构说明](architecture.md)。
