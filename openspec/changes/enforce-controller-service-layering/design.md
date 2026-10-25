## Context

动机见 proposal.md，规则见 `specs/controller-service-layering/spec.md`。下面只列出影响实现方式的现状。

- 现在只有 `internal/contract` 是专门的线上类型包，内容是本机控制面 v1（`StatusV1`、`TunnelStatus`、`TokenResponse`、`ErrorResponse` 等）。REST 响应大多直接序列化 Service 类型：`session.Info`、`session.Preset`、`workspace.Repository`/`Worktree`/`Branches`/`Preview`/`Capability`、`launch.Config`/`Summary`、`push.Status`/`TestReport`、全部 `codeview.*` 结果。请求体直接解码到 `workspace.CreateRequest`/`RemoveRequest`/`Reference`、`launch.Input`、`codeview.CheckRequest`。`TunnelInfo` 定义在 `internal/api`。错误体、健康检查、上传、推送订阅等返回的是匿名 map。
- `workspace.Repository`/`Worktree`（`repositories.json`）和 `launch.Config`（`launch-configs.json`）的 json 标签同时决定磁盘格式。`push.Subscription` 只用于持久化，不上线。
- 终端 JSON 帧由 `internal/session` 自己编码：`jsonFrame` 生成 2（会话信息）、4（`Position`）、5（`SubmissionStatus`）、6（`ControlState`）号帧；`json.Marshal(map[string]int{"exitCode"})` 生成 3 号帧。`session.Event.MarshalJSON` 决定 `/ws/sessions` 的形状。`Subscriber.Out` 是 `chan []byte`，里面是带类型字节的成品帧。输出帧（0）和回放快照帧（1）是共享、不可变的字节块。Hub 按 `frame[0]` 合并输出帧，遇到 3 号帧时以 4410 关闭连接。
- `internal/hub` 自己解码入站的 1 号帧 `{cols,rows}`（匿名结构）和 4 号帧 `{ids}`。
- `workspace` 与 `codeview` 的 `problem(status int, …)` 用到的状态码有 400/403/404/409/410/413/429/500/503/504。`launch` 已经采用"包级哨兵错误 + Controller 映射"（`launchStatus`）。
- 服务器外消费 JSON 的 Go 代码：`sessioncmd` 维护一份 `sessionInfo` 本地副本；`attach` 用匿名结构解码 6 号帧和 3 号帧；`tray` 的 `Watch` 解码 `/ws/sessions` 事件；`cmd/baseline` 用 `session.Frame` 构造入站帧；多个测试解码 `session.Info`/`session.Event`。
- 共享层与客户端对 Service 的依赖：`internal/config` 使用 `session.ExpandPath`、`session.Preset`（TOML 字段 `presets`）、`session.Attention*` 常量（校验推送配置中的注意力种类），并通过 `Config.SessionOptions()` 构造 `session.Options`；`internal/client` 只用了 `session.ExpandPath`，另外通过 `api.ManagerHint` 依赖 Controller。

## Goals / Non-Goals

**Goals:**

- 每条规则都由类型解析实现，换别名、点导入或函数值都绕不过。
- 线上 JSON 逐字节语义不变，前端 Zod 契约零改动。
- 输出热路径（0/1 号帧）不增加分配和复制。
- 规则落地时不带"历史违规白名单"：同一变更内修完所有违规。

**Non-Goals:**

- 不改变 REST 路径、状态码、二进制帧编号、回放与重连语义。
- 不重构 `internal/tray`、`internal/tunnel` 等服务器外客户端的内部结构，只把它们解码的线上类型换成 `contract`。
- 不把磁盘持久化格式迁移到独立类型：持久化 json 标签保持原样，只是不再被 Controller 直接序列化。
- 不引入跨包共享的错误种类包。

## Decisions

### D1 用 `golang.org/x/tools/go/packages` 加载类型信息

`internal/archtest` 只包含 `_test.go`。它以 `NeedName|NeedFiles|NeedImports|NeedDeps|NeedSyntax|NeedTypes|NeedTypesInfo`、`Tests: false` 加载 `github.com/rainchan/webmux/...`，再遍历 `types.Info.Uses`/`Types`，把标识符解析成 `*types.Func`、`*types.TypeName`，并取 `Pkg().Path()`。

- 备选：只用标准库 `go/parser`。它只能按导入名匹配，无法解析函数值、方法接收者和表达式的静态类型，R5、R7 和线上类型规则都做不了。
- 依赖只出现在 `_test.go`，`go build` 不会把它链接进发布二进制。CI 的 `go test` 需要模块缓存，与现有依赖的处理方式相同。

### D2 分层清单是测试代码里的一张表

`layers_test.go` 是一张 `map[包路径]Layer`，外加两张显式例外表：出站 HTTP 登记（`push`）、JSON 编码登记（`push` 中的 VAPID JWT 与通知载荷函数，`codeview.version` 摘要），每条附理由。依赖方向没有例外表。测试先比对 `go list` 得到的包集合和清单：多出或缺失的包都报告。

- 备选：在源码注释里加 `//arch:layer` 标记。分散在各处，难以评审，也难以发现没加标记的包。

### D3 规则编号与检查方式

| ID  | 检查                                                                                                                                                                                                                                                       |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| R0  | 清单完整：多出或缺失的包                                                                                                                                                                                                                                   |
| R1  | 按层检查禁止导入：Service 不得导入 Controller、`cmd/`、`coder/websocket`、`net/http/httptest`；共享层不得导入 Controller/Service；服务器外客户端不得导入 Controller/Service。都没有例外                                                                    |
| R2  | 只有 `cmd/` 能导入 `internal/api`                                                                                                                                                                                                                          |
| R3  | Controller 不得导入 `os/exec`、`syscall`、`golang.org/x/sys/...`、`creack/pty`、`internal/statefile`                                                                                                                                                       |
| R4  | Controller 不得调用 `os` 文件系统函数（`ReadDir`、`Stat`、`Lstat`、`Open*`、`Create*`、`Mkdir*`、`Remove*`、`Rename`、`Chmod`、`Chown`、`ReadFile`、`WriteFile`、`Symlink`、`Link`、`Readlink`、`Truncate`）。判断依据是被调对象为 `os` 包的 `*types.Func` |
| R5  | Controller 使用 Service 包级函数时报告，除非函数在 `pure` 表中附理由登记；登记必须真实存在并被 Controller 使用，构造、状态访问与 I/O 不得登记为纯函数                                                                                                      |
| R6  | Service 引用了服务端 HTTP 符号，或函数签名的参数含 `*http.Request`；已登记的出站包按客户端符号白名单放行                                                                                                                                                   |
| R7  | Service 错误中整数 Status/StatusCode/HTTPStatus/HTTPStatusCode 字段（不分大小写），错误相关整数状态方法或函数，以及显式 HTTP 状态方法或函数；不禁止业务整数和非错误对象的领域 Status                                                                       |
| R8  | 线上类型：见 D4                                                                                                                                                                                                                                            |
| R9  | Service 声明 MarshalJSON，或未在登记函数体中直接调用 JSON 编码器；检查包括包级初始化及函数值/方法值别名，登记函数也不得保存编码器别名                                                                                                                      |
| R10 | `internal/contract` 不得导入 Service/Controller（R1 的特例，单独编号以便报告清楚）                                                                                                                                                                         |

每条违规记录为 `位置 [Rn] 消息 — 修复提示`，统一收集后由一个 `t.Errorf` 按文件和行号排序输出。

### D4 线上类型检查（R8）的汇点模型

Controller 里能写出 JSON 的入口是有限的，测试把它们登记为"汇点"：

- `encoding/json`：`Marshal`、`Unmarshal`、`(*Encoder).Encode`、`(*Decoder).Decode`，取第 1 个参数（Unmarshal 取第 2 个）。
- Controller 内部的包装函数：`api.reply` 的第 3 个参数，`api.decode`/`api.decodeOptional` 的第 3 个参数，hub 新增的帧编码函数 `hub.jsonFrame(kind, v)` 的第 2 个参数。

规则：

1. 每个汇点调用处，实参的静态类型去掉指针、切片和映射后，必须是 `internal/contract` 中的具名类型。
2. 实参静态类型是接口（`any`）时，只有当它出现在已登记包装函数的函数体内才放行，因为包装函数的调用点会按规则 1 检查；其他位置报告。
3. 单独检查 `internal/contract` 中所有类型的字段（递归），不得引用 Service 包的具名类型。
4. `[]string`、匿名 map 等非 contract 类型一律报告，所以目录列表、`{"ok":true}` 之类也要在 `contract` 中具名定义。

`api/code.go` 现在用闭包返回 `any` 再交给 `reply`，需要改成每个路由返回具体的 contract 类型，比如泛型 helper `serve[T any](fn func(...) (T, error))`，并在调用点检查 `T`。

- 备选：要求所有响应走同一个泛型 `reply[T]`，按类型参数检查。处理不了 hub 和 `json.Unmarshal`，汇点登记更通用。

### D5 线上类型的位置与映射归属

- 线上类型放在 `internal/contract`，按资源分文件：`sessions.go`、`terminal.go`（帧载荷）、`workspace.go`、`launch.go`、`push.go`、`code.go`、`api.go`（健康检查、配置、错误、上传、目录列表、通用确认）。现有本机控制面类型不动。`contract.Health` 已被 CLI 健康探测占用，HTTP 健康检查命名为 `APIHealth`。其他同名冲突一律加资源前缀，不改动现有导出名。
- Service 与线上类型之间的映射放在新的 Controller 层包 `internal/wire`。`api` 和 `hub` 都要把 `session.Info` 转成 `contract.SessionInfo`，所以映射集中一处；`wire` 归入 Controller 层，规则与 `api`/`hub` 相同。
  - 备选 1：映射放在 Service 包里（`Info.Wire()`）。Service 就得导入 `contract`，并感知线上形状，违背目标。
  - 备选 2：分别在 `api` 和 `hub` 各写一份。会重复。
- `contract.CreateSessionRequest` 的可选字段带 `omitempty`：只影响 Go 客户端（`sessioncmd`）发送的请求体，使其与原来的 map 请求一致；服务端解码时缺省字段与零值等价。
- 请求类型（`contract.CreateSessionRequest`、`contract.CreateWorktreeRequest`、`contract.RemoveWorktreeRequest`、`contract.LaunchConfigInput`、`contract.CodeCheckRequest`、`contract.PushSubscriptionRequest` 等）由 `wire` 转成 Service 的输入结构。Service 输入结构去掉 json 标签，持久化类型除外。
- 线上类型照搬原有的 json 标签、`omitempty`、指针与 nil 语义。空集合的序列化（如 snapshot 的 `sessions: []`、launch `env: {}`）由映射函数保证非 nil，或由 contract 类型自己的 `MarshalJSON` 保证，不靠调用方。

### D6 终端帧：Service 发出类型化消息，Hub 负责编码

`Subscriber.Out` 改为 `chan session.Message`：

```go
type Message struct {
	Frame      []byte            // 0/1 号原始帧（含类型字节），共享且不可变
	Info       *Info             // 2
	Exit       *int              // 3
	Position   *Position         // 4
	Submission *SubmissionStatus // 5
	Control    *ControlState     // 6
}
```

- `session` 继续在同一把锁下按原顺序把消息放进同一个有界队列，回放与实时订阅的无缝衔接、慢客户端断开、排空后再发布退出等语义都不变。0/1 号帧仍然是共享字节块，不复制。
- Hub 写循环按字段分派：`Frame` 照旧参与输出合并；其余字段经 `wire` 转成 contract 类型，再由 `hub.jsonFrame` 编码后写出。`Exit` 写出后以 4410 关闭连接。
- 入站 1 号帧 `{cols,rows}` 和 4 号帧 `{ids}` 都在 hub 中解码到 `contract.TerminalResize`、`contract.SubmissionQuery`，再调用 `session` 的类型化方法。`session` 里删除 JSON 解析代码。
- `session.Frame` 删除。帧类型字节在 `contract` 中定义为具名常量 `Frame*`（前后端编号不变），`contract.Frame(kind, payload)` 负责加类型字节；`hub.jsonFrame` 用它编码 JSON 帧，`cmd/baseline`、`attach` 和测试构造入站帧时也用它。放在只依赖标准库的 `contract` 而不是 hub，是因为服务器外的 `attach` 也要构造帧，而客户端不能导入 Controller。
- `session.Event.MarshalJSON` 删除，形状迁到 `contract.SessionEvent` 及其 `MarshalJSON`（snapshot 时总是输出 `sessions`）。
- 代价：2/5/6 号帧原来按订阅者共享一份已编码字节，现在每个订阅者编码一次。这些帧低频（状态变化、提交回执），订阅者数量也小。
  - 备选：`session` 产出 contract 类型。这会让 Service 依赖线上类型，违反 R10 的方向。

### D7 错误：沿用 `launch` 的哨兵模式

`workspace`、`codeview` 各自声明 `ErrInvalid`、`ErrForbidden`、`ErrNotFound`、`ErrConflict`、`ErrGone`、`ErrTooLarge`、`ErrBusy`、`ErrInternal`、`ErrUnavailable`、`ErrTimeout`，每个包只声明实际用到的。`problem` 改成 `problem(kind error, format, args...)`，返回 `&Error{kind, msg}`：`Error()` 返回原 `msg`，所以错误体文本不变；`Unwrap()` 返回 `kind`。Controller 在 `api/errors.go` 用 `errors.Is` 映射状态码，`launchStatus` 也搬到这里，三个 Service 共用一个映射文件。删除 `workspace.Status`、`codeview.Status`。

- 备选：新建共享 `fault` 包。会和 `launch` 现有模式形成两套约定，`launch` 的错误文本也得跟着改。

### D8 迁出越界逻辑

- `configId`：`workspace.Service.CreateSession` 改为接收 `workspace.SessionInput`（含 `ConfigID *string`）。互斥规则、`launch.Service.Get` 解析和 `session.ValidateEnv` 都在 Service 内，顺序保持"先校验，再做任何文件系统或 Git 操作"。`workspace.New` 增加 `*launch.Service` 参数，允许 nil，nil 时 `configId` 返回 `launch.ErrUnavailable`，即原来的 503 语义。
- `internal/files`：`Service{defaultCwd}` 提供 `Complete(prefix)`、`Mkdir(path)`、`StoreUpload(name, io.Reader, limit) (*Upload, error)`。`uploadFilename` 校验、临时目录 0700、文件 0600、`O_EXCL`、按 `limit` 的有界复制、失败清理都迁过去；`Upload.Discard()` 让 Controller 在之后的检查失败时删除已落盘文件。Controller 保留 multipart 读取、用请求上下文包装读取器以支持取消、请求体大小限制和状态码映射。`files` 的错误同样用哨兵错误（`ErrInvalid`、`ErrTooLarge`、`ErrRead`、`ErrInternal`），`ErrRead` 保留读取错误本身，Controller 据此区分 408/413/400。
- `push.Validate` 保持包级纯校验函数，在 `pure` 中登记理由，不再用无状态的 `CheckSubscription` 方法包装。`Register` 内部继续校验。Controller 保持"推送不可用 409 → 订阅格式 400 → 来源 409 → 保存"的顺序，来源判断仍留在 Controller。
- `codeview.New` 由 `cmd/webmux/main.go` 和 `cmd/baseline` 构造。`api.New` 的可变参数 `codes ...*codeview.Service` 改为必填，`files.Service` 也通过参数注入。参数过多时改为 `api.Deps` 结构体，以免位置参数继续增长。
- `ManagerHint` 移到 `internal/runtime`。`api/local.go` 和 `client/stop.go` 都改用新位置。
- `os.Hostname()` 不属于 R4 的文件系统函数，保留在 Controller 中。
- `ExpandPath` 移到共享包 `internal/pathutil`（`pathutil.Expand`），`session`、`config`、`client`、`files` 都改为调用它，不在 `session` 中保留转发。
- 注意力种类 `AttentionKind` 及其常量移到共享包 `internal/attentionkind`（`attentionkind.Kind`），`session` 和 `config` 共用，不再在两处各维护一份枚举。
  - 备选：`config` 自己维护一份种类字符串列表，再用一个测试保证与 `session` 一致。两份枚举仍需同步，只是把漂移推迟到测试时才发现。
- `config` 定义自己的 `Preset` 类型（TOML 标签不变，配置文件格式不变）。`Config.SessionOptions()` 删除，改为装配根内的共享包 `cmd/internal/assemble` 中的 `SessionOptions(config.Config) session.Options`，在其中把 `config.Preset` 转成 `session.Preset`；`cmd/webmux` 与 `cmd/baseline` 都调用它（两个 `main` 包不能互相导入，放在 `cmd/internal` 下仍属于装配根，且 Go 的 internal 规则保证只有 `cmd/` 能导入）。`cmd/` 以外的测试各自写测试用的转换。`/api/presets` 由 `wire` 把 `config.Preset` 映射为 `contract.Preset`。AGENTS.md 中"配置转换为 `session.Options`"的接线描述改为由 `cmd/webmux` 完成。

### D9 线上兼容的验证

- 在改动开始前，先新增 `internal/api` 的线上快照测试：用固定时钟或归一化时间字段的夹具，对每个 REST 端点、`/ws/sessions` 事件和每种终端 JSON 帧录制 JSON，存到 `internal/api/testdata/wire/*.json`。改动全程这些快照必须保持不变。快照是对前端 Zod 契约的回归保护，作为永久测试保留。
- 有意的协议变更通过 `go test ./internal/api -run TestWireSnapshots -update` 重新录制，差异进入评审。按端点/帧的明确 JSON 路径归一化时间、随机 ID 和临时路径，不全局替换业务正文或字段名，保留确定性 Git OID、摘要和固定输入；路径替换检查边界。六组场景各自持有夹具、期限和归一化状态，可单独运行。异步 detach 完成后才记录客户端计数，不将竞态归一化。
- 现有 HTTP 级测试（`launch_test`、`router_test`、`upload_test`、`workspace_test`、`code_test`、`push_test`、`continuity_test`）保持断言不变，只把解码目标改为 contract 类型。
- 前端 Vitest 与 Playwright E2E 不改代码，直接运行，作为端到端兼容的证据。
- `session.Message` 集中构造且恰有一个载荷，原始帧只允许带 0/1 前缀；Hub 校验失败或编码失败时关闭连接（1011），不记录载荷。输出仍只复制一次并共享，回放不增加复制。聚合循环的 pending 使用值加布尔标记，避免接收变量取地址逃逸；真实 PTY → Session → Attach → WebSocket 基准验证连续输出、控制边界和大块输出。

## Risks / Trade-offs

- [映射遗漏字段，线上形状悄悄改变] → D9 的快照在重构前录制并全程比对；contract 类型逐字段照搬标签；评审时对照 `web/src/schemas.ts`。
- [`Subscriber.Out` 改为结构体后破坏回放与实时衔接或退出顺序] → 入队位置和锁范围保持不动，只换元素类型；`continuity_test`、`replay` 相关测试和 race 测试覆盖。
- [汇点模型有盲区，比如新写一个返回 `any` 的包装函数] → 规则 2 拒绝在未登记函数中以接口类型调用汇点，新的包装函数必须登记后才能用；fixture 覆盖这种绕过方式。
- [规则过严，导致后续改动摩擦] → 例外只能写在清单表里并附理由，评审时可见。
- [`go/packages` 加载耗时增加测试时间] → 整个模块只加载一次，所有规则共享，预计为秒级；不开 `-race` 也能跑，CI 的 race 任务照常包含。
- [`api.New` 签名变化影响多个调用点] → 一次性迁移 `cmd/webmux`、`cmd/baseline`、`internal/tray/forward` 测试及其他调用点，不保留旧签名。

## Migration Plan

原计划分 3 个 PR；按维护者要求改为在同一个 PR（#117）中交付，但仍按下面三个阶段分别提交，每个阶段的提交自身测试全绿、可以单独回滚。规则随对应迁移一起加入 archtest 和 fixture，不设"规则开关"或历史违规白名单：

1. **PR1 边界基础**：录制线上快照；加入 archtest 框架、R0–R7 及其 fixture；完成错误种类迁移、`internal/files`、`configId` 迁移、`push.Validate`、Service 构造移到 `cmd/`、`ManagerHint`、`pathutil`/`attentionkind`/`config.Preset`/`sessionOptions` 迁移。合并时 R0–R7 全绿，快照不变。
2. **PR2 REST 线上类型**：新增 contract REST 类型和 `internal/wire`；`internal/api` 全部改为收发 contract 类型；`sessioncmd` 改用 `contract.SessionInfo`。加入 R8（汇点限定在 `internal/api`、`internal/wire`）和 R10。
3. **PR3 WebSocket**：`Subscriber.Out` 改为类型化消息，终端帧和 `/ws/sessions` 事件改由 Hub 编码；`attach`、`tray` 和测试改用 contract 帧类型。R8 的汇点扩展到 `internal/hub`，并加入 R9。

部署没有特殊步骤：二进制行为、磁盘格式、TOML 配置和线上协议都不变。回滚就是还原对应 PR；还原 PR1 必须先还原 PR2/PR3。
