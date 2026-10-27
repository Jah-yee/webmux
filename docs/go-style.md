# Go 代码质量基线

## 适用范围与优先级

本仓库采纳 [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)
作为 Go 代码的规范基线，适用于 `cmd/`、`internal/`、`web/embed.go` 和
`deploy/embed.go`，包含测试代码。新代码 MUST 遵守，存量代码按 issue #148 的分期计划收敛。

发生冲突时，`AGENTS.md` 与 [分层架构](architecture.md) 优先；本文件集中登记
本地化条款，避免隐性漂移。指南不意味着引入 Uber 的依赖；类型化 `sync/atomic`
符合本仓库要求。不要为机械对齐重命名现有包或破坏协议。

## 本地化条款

| 指南条款                                | 本仓库决定与理由                                                                                                                                                                                                                       |
| --------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Prefix Unexported Globals with `_`      | 不采纳 `_` 前缀。保持现有 Go 命名习惯，用小写名称表达未导出性；不得以此前缀批量重命名包级变量与常量。                                                                                                                                  |
| Channel Size is One or None             | 保留有界队列，容量大于 1 时 MUST 使用命名常量并注释容量理由。终端输出、会话列表与输入队列吸收短时突发，慢客户端断开，不得阻塞 PTY 读取；托盘事件队列串行化状态变化。测试可按样例长度配置容量。                                         |
| Package Names                           | 冻结 `files`、`pathutil`、`tailscalesettings`、`tunnelsettings`、`service` 等现有名称；受架构清单及引用约束，不在本次改名。新包遵守单数、简洁、避免 util 的要求。                                                                      |
| Avoid Embedding Types in Public Structs | 冻结 `api.Server` 嵌入 `Deps`、`runtime.PeerConn` 嵌入 `net.Conn` 的存量例外。新公开结构体 MUST 使用具名字段，不增加公开嵌入。                                                                                                         |
| Errors                                  | 强化现有约定：服务返回 sentinel 错误种类，`internal/api/errors.go` 集中映射 HTTP 状态；服务不携带 HTTP 状态。包装错误保留 cause。不可向客户端暴露的内部错误应记录日志并返回固定文案，日志不得包含 token、终端输入输出或推送 endpoint。 |

必填响应数组 MUST 编码为 `[]`，不能因服务切片为 nil 变成 `null`；只有前端
Zod 明确允许 nullable 的字段才可返回 `null`。请求字段的缺省语义不受此规则改变。
必填响应对象（如启动环境和代码检查映射）同理编码为 `{}`。
修改协议时同步检查 `internal/contract`、`internal/wire`、前端 schema 与线上快照。
实现外部接口的具体类型 SHOULD 在实现旁加入编译期断言。

## 固定工具与本地执行

本机与 CI 固定使用 **golangci-lint v2.15.0**（v2 配置），Go 版本遵循 `go.mod`
及 `AGENTS.md`。安装：

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
```

确保 `$(go env GOPATH)/bin` 在 PATH 中，或通过 `GOLANGCI_LINT` 指定可执行路径。
`make go-lint` 检查版本，版本不一致会给出安装提示并失败。
Go 编译与分析需要嵌入的前端产物，干净检出先执行 `make web`。

```sh
make check                  # 前端构建、格式、文档、架构、vet 与固定 Go lint
make go-lint                # 已构建前端时，单独运行 Go lint
make test                   # Go race 与前端全套
GOOS=windows go vet ./cmd/webmux-tray ./cmd/webmux-windows ./internal/attach/...
```

CI 的 lint job 在前端构建之后运行固定版本的 golangci-lint；Go job 在嵌入产物
构建后运行 `go vet ./...` 与 race 测试，另行交叉构建和 vet Windows 客户端。
两者都参加现有的 `Test result` 汇总门禁。

## Linter 集合与存量收紧

`.golangci.yml` 显式关闭默认集合，只启用 `errcheck`、`govet`、`ineffassign`、
`staticcheck`、`unused`。告警数量与相同告警上限均为 0（不截断），不能依靠
默认上限隐藏问题。生产代码基线为零，不使用仅检查 PR 差异的模式。

唯一存量豁免是 `_test.go` 的 `errcheck` 路径规则，覆盖历史 fixture 清理和测试
输出；它不排除其他 linter，也不排除生产代码。新增测试仍 MUST 检查有意义的错误；
清理阶段逐文件收紧该规则，不得扩大路径或增加 linter 豁免。

**新增代码零新增告警**：不得通过 `nolint`、批量 `_ =` 或扩大基线掩盖应处理的
错误。写文件、flush、持久化与协议传输错误需要处理；对已报告错误后的清理、尽力
终止进程或 CLI 诊断输出，可以显式忽略，并在非显然情形说明原因。

增加 linter 或升级版本时，单独评估全部告警。先修复生产告警；确需冻结存量时，
逐规则/路径记录原因及清理范围，提交只收紧的 ratchet，不允许新增记录。`errorlint`、
`revive`、`misspell`、`copyloopvar` 暂不加入首发集合；它们需要独立审查已有告警，
不能为了启用推荐项让门禁恒红。

## Review checklist

- [ ] 命名、导入分组、零值、接收者与注释符合指南和本地条款。
- [ ] 错误已处理或有明确忽略理由；错误链、服务错误种类与 HTTP 映射保留。
- [ ] 接口实现有适当编译期断言；公开结构体没有新增嵌入。
- [ ] goroutine 有明确所有者、取消和等待路径；阻塞读可中断，资源在返回前释放。
- [ ] 锁不跨越无界 I/O；有界队列容量具名并解释，不阻塞 PTY 读取。
- [ ] 必填数组、nullable 字段、Zod schema 和 wire 快照一致。
- [ ] `make check` 与相关测试通过；没有新增告警或扩大豁免。
- [ ] 行为变化已更新用户故事；未把尚未实现的分期任务写成已完成。

## 分期与后续工作

Issue #148 的首批验收覆盖规范、可执行门禁和 P1：推送存储失败诊断、可取消并等待
的终端输入、必填数组契约、接口断言。实际告警以固定工具不截断的输出为准，不能
复用审计正文中受默认上限影响的计数。

Phase 3/4 的 options 参数、锁临界区、printf 包装、预览节流、state store 抽取、
fetch 测试、OSC/Preview fuzz 与大文件拆分已落地。后续维护沿用这些规则：

- 新增表驱动测试使用 `tests` 作为用例集合，`tt` 作为当前用例，字段优先命名为
  `give` / `want`；存量表不要求仅为改名而重排。
- import 分为标准库与其余依赖两组，中间空一行；本模块包与第三方包同组。
  修改 imports 后运行 `goimports -w`（不添加 `-local` 第三组，以免与存量 181
  个文件不一致）。
- 有界队列容量使用命名常量并注释理由；新增阻塞 I/O 不得在持有互斥锁时执行。
- 冻结例外及测试 `errcheck` 只收紧、不扩大。
