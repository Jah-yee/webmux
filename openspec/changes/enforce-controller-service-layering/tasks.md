## 1. PR1：线上快照基线

- [x] 1.1 新增 `internal/api/wire_snapshot_test.go` 和 `internal/api/testdata/wire/`：在当前代码上为每个 REST 端点录制规范化 JSON 快照（只归一化时间、随机 ID、临时路径），覆盖 `/api/health`、`/api/presets`、`/api/config`、会话 CRUD、`/api/fs/dirs`、上传、仓库与 worktree 全部路由、启动配置 CRUD、推送状态/订阅/测试、代码视图全部路由，以及 400/403/404/409/413/503 错误体
- [x] 1.2 为 `/ws/sessions` 的 snapshot（含空列表）、created、updated、removed 事件，以及 `/ws/s/{id}` 的 2/3/4/5/6 号 JSON 帧录制快照
- [x] 1.3 实现 `-update` 重新录制开关；比较失败时输出端点和 JSON 差异；在当前代码上通过后提交，作为永久测试

## 2. PR1：架构测试框架与 R0–R7

- [x] 2.1 在 `go.mod` 中加入 `golang.org/x/tools`，只被 `internal/archtest` 的 `_test.go` 引用；用 `go list -deps ./cmd/webmux` 确认发布二进制不依赖它
- [x] 2.2 新增 `internal/archtest/layers_test.go`：分层清单（含 `wire`、`files`、`pathutil`、`attentionkind` 等计划中的包；PR1 中尚不存在的包在其 PR 中再加入，保证 R0 始终准确）、出站 HTTP 登记（`push`）和 JSON 编码函数登记（`push` 的 JWT 与通知载荷函数、`codeview.version`），每条附理由
- [x] 2.3 新增 `internal/archtest/arch_test.go`：用 `go/packages` 一次加载整个模块（`Tests:false`），实现 R0–R7；收集全部违规，按文件和行号排序，以 `位置 [Rn] 消息 — 修复提示` 一次输出
- [x] 2.4 新增 `internal/archtest/testdata/fixture/`（独立 `go.mod` 的小模块）和 `rules_test.go`：R0–R7 各有至少一处违规和一处合规写法，覆盖导入别名、点导入、函数值变量这几种绕过方式；断言违规全部被报告、合规写法不被报告
- [x] 2.5 在当前代码上运行 `go test ./internal/archtest`，把报告的违规与 design.md 列出的越界点逐条对照，补齐遗漏的迁移项（此时测试失败是预期的）

## 3. PR1：错误种类迁移（R7）

- [x] 3.1 `internal/workspace`：用哨兵错误替换 `problem(status int, …)`，`Error` 保存 `kind error` 和原消息，实现 `Unwrap`；删除 `Status()`；包内测试改为 `errors.Is` 断言
- [x] 3.2 `internal/codeview`：同 3.1
- [x] 3.3 新增 `internal/api/errors.go`：集中映射 `workspace`、`codeview`、`launch`、`files` 的哨兵错误到 HTTP 状态码，搬入 `launchStatus`；替换全部 `workspace.Status`/`codeview.Status` 调用

## 4. PR1：迁出越界逻辑（R2–R5）

- [x] 4.1 新增 `internal/files`：`Complete`（排序、隐藏文件优先级、符号链接判断与原实现一致）、`Mkdir`、`StoreUpload`（文件名校验、临时目录 0700、文件 0600、`O_EXCL`、失败清理），带哨兵错误；把 `router_test`/`upload_test` 中的纯逻辑边界补成包内单元测试
- [x] 4.2 `internal/api` 的 `dirs`、`mkdir`、`upload` 改为调用注入的 `files.Service`；Controller 只保留 multipart 读取、上下文取消、大小限制和状态码映射
- [x] 4.3 `workspace.Service.CreateSession` 改为接收 `SessionInput`（含 `ConfigID`），把 `configId` 互斥规则、`launch.Service.Get` 和 `session.ValidateEnv` 迁入 Service，保持"校验先于任何文件系统和 Git 操作"；`workspace.New` 增加 `*launch.Service` 参数（nil 时返回 `launch.ErrUnavailable`）；为 `configId` 规则补包内测试
- [x] 4.4 保留包级纯校验 `push.Validate` 并在 R5 的 `pure` 表中附理由登记，移除 `CheckSubscription` 包装；保持"不可用 → 格式 → 来源"的错误优先级
- [x] 4.5 `codeview.Service` 和 `files.Service` 改由 `cmd/webmux/main.go`、`cmd/baseline` 构造并注入；`api.New`/`api.NewLocal` 改用 `api.Deps` 结构体，删除可变参数 `codes`；迁移所有调用点，包括 `internal/tray/forward` 测试和 `internal/api` 测试
- [x] 4.6 把 `ManagerHint` 移到 `internal/runtime`，`api/local.go` 与 `client/stop.go` 改用新位置

## 5. PR1：解除共享层与客户端对 Service 的依赖（R1）

- [x] 5.1 新增共享包 `internal/pathutil`，把 `session.ExpandPath` 及其测试迁过去；`session`、`config`、`client`、`files`、`api` 全部改为调用 `pathutil.Expand`，删除 `session.ExpandPath`
- [x] 5.2 新增共享包 `internal/attentionkind`，迁入 `AttentionKind` 类型和常量；`session` 与 `config` 改用它，删除 `session` 中的定义
- [x] 5.3 `internal/config` 定义 `Preset`（保留原有 TOML 与 json 标签，保证 `/api/presets` 快照在 PR1 中不变）；删除 `Config.SessionOptions()`，改为 `cmd/internal/assemble.SessionOptions(config.Config) session.Options`（`cmd/webmux` 与 `cmd/baseline` 共用），在其中把 `config.Preset` 转成 `session.Preset`；迁移 `cmd/baseline` 和所有调用点与测试
- [x] 5.4 确认 `internal/config`、`internal/client` 的非测试代码不再导入任何 Service 包或 Controller 包

## 6. PR1：收尾

- [x] 6.1 `go test ./internal/archtest` 全绿；临时在某个 handler 中加入 `os.ReadDir`，确认 R4 报告了正确位置后撤销
- [x] 6.2 线上快照不变；运行 `go test -race ./...`、`make test-unit`、`make test-browser`、`make test-e2e`、`make check`
- [x] 6.3 更新 `docs/architecture.md`（分层表、R0–R7、例外表维护方式、新增包的归类流程、快照重新录制方式）、`AGENTS.md`（Key Directories、Code Conventions、配置到 `session.Options` 的接线描述）；在 `docs/user-stories.md` L 节新增 `US-L05 分层架构约束`（US-L04 已被"基线评测与多平台发布"占用），同步功能索引、来源链接与计数，运行 Markdown 格式与空白检查

## 7. PR2：REST 线上类型（R8 限于 api/wire、R10）

- [x] 7.1 在 `internal/contract` 中按资源新增 REST 线上类型：`sessions.go`（`SessionInfo` 及嵌套、`Preset`、`CreateSessionRequest`）、`workspace.go`、`launch.go`、`push.go`、`code.go`、`api.go`（`APIHealth`、`ServerConfig`、`TunnelInfo`、`DirectoryList`、`UploadResult`、`PushConflict`、`Ack`、复用 `ErrorResponse`）；json 标签、`omitempty`、指针语义逐字段照搬原类型
- [x] 7.2 新增 Controller 层包 `internal/wire`：Service/config 类型与 contract 类型之间的双向映射，保证空切片和空映射按原形状输出；为空值和 nil 边界写单元测试；把 `wire` 加入分层清单
- [x] 7.3 `internal/api` 全部 handler 改为编码和解码 contract 类型：`reply`/`failure`/`decode`/`decodeOptional` 只接收 contract 值；`code.go` 的 `any` 闭包改为返回具体类型的泛型 helper；`TunnelInfo` 从 `api/config.go` 移到 `contract`
- [x] 7.4 Service 输入结构（`workspace.CreateRequest`/`RemoveRequest`/`Reference`、`launch.Input`、`codeview.CheckRequest`）和 `config.Preset` 去掉 json 标签；持久化类型（`workspace.Repository`/`Worktree`、`launch.Config`、`push` 存储结构）保留标签，确认磁盘格式测试不变
- [x] 7.5 `internal/sessioncmd` 删除本地 `sessionInfo` 副本，改用 `contract.SessionInfo`；`internal/api`、`cmd/webmux`、`internal/tray/forward` 测试中解码 `session.Info` 的地方改为 contract 类型
- [x] 7.6 在 archtest 中加入 R8（汇点：`encoding/json` 编解码函数、`api.reply`/`decode`/`decodeOptional`，范围是 `internal/api` 与 `internal/wire`）和 R10，fixture 增加对应违规与"未登记 `any` 包装函数"的绕过样例
- [x] 7.7 archtest 全绿、线上快照不变；运行 `go test -race ./...` 与前端三套测试；更新 `docs/architecture.md` 的 R8/R10 与 `docs/protocol.md` 中引用 Go 类型位置的描述，并同步 `US-L05`

## 8. PR3：WebSocket 事件与终端帧（R8 扩展到 hub、R9）

- [x] 8.1 在 `internal/contract` 新增 `terminal.go`（帧类型常量、`StreamPosition`、`SubmissionStatus`、`ControlState`、`ExitFrame`、`TerminalResize`、`SubmissionQuery`）和 `SessionEvent` 及其 `MarshalJSON`（snapshot 总是输出 `sessions`）；在 `wire` 中补齐映射
- [x] 8.2 `internal/session`：`Subscriber.Out` 改为 `chan Message`；`jsonFrame`、3 号帧的 `json.Marshal` 和 `event.go` 的 `MarshalJSON` 改为发出类型化消息，入队位置、锁范围和顺序不变；0/1 号帧继续共享字节块；1 号入站帧的 JSON 解析改为接收已解码 `cols, rows` 的类型化方法
- [x] 8.3 `internal/hub`：写循环按 `Message` 字段分派，`Frame` 参与原有输出合并，其余字段经 `wire` 转换后由 `hub.jsonFrame` 编码；`Exit` 写出后以 4410 关闭；入站 1/4 号帧解码到 `contract.TerminalResize`/`SubmissionQuery`；`/ws/sessions` 改为编码 `contract.SessionEvent`
- [x] 8.4 删除 `session.Frame` 的对外用途：`cmd/baseline`、`internal/api` 测试改用 contract 帧常量和 Controller 层的帧编码函数；`session` 包内测试改为断言类型化消息
- [x] 8.5 `internal/attach` 的 6 号帧与 3 号帧解码改用 `contract.ControlState`/`contract.ExitFrame`，1 号帧编码改用 `contract.TerminalResize`；`internal/tray` 的 `Watch` 改为解码 `contract.SessionEvent`；`internal/wslclient` 测试改用 contract 类型
- [x] 8.6 在 archtest 中把 R8 汇点扩展到 `internal/hub`（含 `hub.jsonFrame`），加入 R9，fixture 增加对应违规样例
- [x] 8.7 archtest 全绿、线上快照不变；`continuity_test` 与回放相关测试在 `-race` 下通过；运行 `go test -race ./...`、`make test-unit`、`make test-browser`、`make test-e2e`、`make check`；更新 `docs/architecture.md` 的 R8/R9 与终端帧编码归属，并同步 `US-L05`

## 9. 合并后加固

- [x] 9.1 R9 检查整个源码及编码器别名，补包级初始化与函数值回归；R7 允许业务整数，仅拦截 HTTP 状态语义；R5 登记 `push.Validate` 并检查过期纯函数条目
- [x] 9.2 聚合 pending 改用值加布尔标记；集中构造 `Message`，校验单载荷及原始帧前缀，异常以 1011 关闭连接；补真实 WebSocket 顺序、非法消息和共享缓冲回归
- [x] 9.3 快照按明确字段归一化，保留业务值并检查路径边界；拆成六组独立夹具，保留 95 份快照；等待异步 detach 后记录客户端数
- [x] 9.4 运行真实 PTY/WebSocket 前后分配基准、生产协议烟测、Go race 全套和六组独立快照；同步架构、协议与 US-D02 / US-L04 / US-L05，未宣称本次运行前端浏览器或真机验收
