## Investigation Baseline

调查提交：`677c1a94dedf461487867e6b99c1cbba670208a0`（#145 及后续网页设置提交均已合入）。初次调查时尚无 tailscalesettings；创建 PR 前检测到主分支更新，已重新同步、调查并测试。PR 以该最新 main 为基线，只提交本任务改动。

## Existing Functionality

| 证据                                                   | 观察与影响                                                                                                                             |
| ------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/statefile/statefile.go`                      | 已有文件 fsync、同目录临时文件、rename/link、0600；目录同步默认尽力，`DirSyncError` 表示新文件已可见。应扩展而非再建平行写入包。       |
| `internal/launch/store.go`                             | 生命周期 flock、严格字段/EOF、v1/v2 读取；文件读取无共享字节上限，保存目录同步尽力。                                                   |
| `internal/workspace/registry.go`                       | 生命周期 flock，宽松 Unmarshal，强制目录同步；已有替换后同步失败即禁用清单的正确保守处理。                                             |
| `internal/tunnelsettings/store.go`                     | `tunnel.json` / `tunnel-config.lock`；已拒绝未知字段/尾随内容，损坏禁止入口；Save/Reset 与关闭入口有专门语义。                         |
| `internal/push/store.go`                               | mutex，无 flock；宽松 Unmarshal；损坏改名并生成新 VAPID 密钥。Add/Remove 回滚逻辑不能直接套到已发布的不确定提交。                      |
| `cmd/webmux/tailscalectl.go`                           | `tailscale.json` 是所有权证明，不是偏好；有 pending/active、精确目标核对与启动前对账；宽松 Unmarshal，目录同步尽力，删除未检查同步。   |
| `internal/runtime/record.go`、`cmd/webmux/local.go`    | server.json 不含明文 token；锁冲突仅禁用本机 socket/记录，TCP 服务可继续。此锁不替代领域锁。                                           |
| `cmd/webmux/configcmd.go`、`internal/config/config.go` | config show 只生成少量 TOML 视图；当前结构缺少逐字段显式来源，不能靠“值等于默认”推断来源。                                             |
| `cmd/webmux/local.go`、`internal/api/local.go`         | 已有同 UID 本机 API 与运行时状态接线；活配置快照应复用这条边界。                                                                       |
| `openspec/changes/add-server-settings-sync`            | 尚未实现的偏好同步，设计含 settings.lock、所有写失败回滚、浏览器兼容迁移。实施时需收敛到本规格，不能原样叠加。                         |
| `internal/tailscalesettings/store.go`                  | 新增 tailscale-settings.json/tailscale-settings.lock，严格字段/EOF、flock、强制目录同步；失败 mode off。沿用专用安全流程而非并入偏好。 |
| `internal/archtest`、`internal/docguard`               | 已有分层与文档尺寸守卫，但没有持久化清单完整性约束；复用现有 test-only 包，不增加产品服务。                                            |

## Primary Sources

资料访问日期：2026-10-26。下列为原始规范/实现文档；“应用”是针对 WebMux 的设计判断，不是规范要求本项目采用特定架构。

1. [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir/latest/) 区分 config、state、cache 和 runtime 生命周期。应用：保持引导配置与运行时状态分开；不把本项目已有 state_dir/run 布局声称为完全遵循 XDG_RUNTIME_DIR。
2. [fsync(2)](https://man7.org/linux/man-pages/man2/fsync.2.html) 明确文件同步不保证目录项持久化，需要额外同步父目录。应用：所有持久提交强制报告目录同步结果，rename 后错误不能等同于“未提交”。
3. [flock(2)](https://man7.org/linux/man-pages/man2/flock.2.html) 说明锁绑定打开的文件描述、非阻塞选项与描述符关闭释放。应用：锁独立文件、服务生命周期持有，不能锁住会被 rename 替换的数据 inode；不承诺 NFS/SMB 锁语义。
4. [SQLite Atomic Commit，§8](https://www.sqlite.org/atomiccommit.html#_testing_atomic_commit_behavior) 采用模拟崩溃、不同故障点和重新打开验证一致性。应用：借鉴故障注入测试方法，不引入 SQLite；单元测试不会被宣传为证明真实硬件掉电安全。
5. [Go encoding/json](https://pkg.go.dev/encoding/json) 的传统 v1 解码接受重复键、宽松字段名匹配等行为；`DisallowUnknownFields` 不等于完整严格解码。应用：在项目 Go 1.26 支持范围实现显式重复键、精确字段名、UTF-8、EOF 与深度/大小约束，不依赖未来版本默认行为。
6. [Tailscale Serve](https://tailscale.com/kb/1242/tailscale-serve) 的后台服务生命周期独立于调用进程。应用：所有权记录不能像可再生缓存一样删除，破坏性改名必须保留安全检测边界。

## Alternatives

| 方案                              | 评价                                                                                                                            |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| 所有内容合并 TOML                 | 引导顺序循环、网页写安全配置、需要重启；不采用。                                                                                |
| 一个巨大 settings.json            | 偏好与凭据/外部所有权耦合，一个损坏影响全部功能；不采用。                                                                       |
| SQLite                            | 可提供事务与并发协调，但仍需解决领域边界、外部副作用、来源与脱敏；当前小文档、低频写入、无跨领域事务需求，用户已选择统一 JSON。 |
| 只有文档清单/文件名正则           | 新入口可绕过、删除登记不被发现、不能验证写入故障；采用 AST 引用守卫加后续契约与故障测试。                                       |
| 每个存储统一加 revision/updatedAt | v 是磁盘格式版本，revision 是并发协议，时间是业务元数据；不强制给所有权/运行时记录加无用途字段。                                |
| config show 直接调用服务 Open     | 会加锁、chmod、初始化或触发恢复，不能作为只读观察；设计单独纯读取路径。                                                         |

## Investigation Limits

本次不执行真实 Cloudflare/Tailscale 管理命令，不触碰用户实际状态目录。调查依据是源码与既有测试，不声称已复现真实掉电或验证全部故障路径。机械校验器的范围和静态分析盲区明确记录在 docs/persistence.md。
