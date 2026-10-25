## Purpose

规定 WebMux 后端 Controller 层（HTTP/WebSocket 传输）与 Service 层（业务规则、状态、文件系统与进程）之间的边界，以及线上类型（DTO）与内部类型的分离。边界由仓库内的架构测试强制执行：违规会让 Go 测试失败，而不是依赖代码评审。

## ADDED Requirements

### Requirement: 分层清单与完整归类

仓库 SHALL 维护一份分层清单，把 `internal/` 下的每个 Go 包归入以下类别之一：Controller（`internal/api`、`internal/hub`、`internal/wire`）、Service（`internal/session`、`internal/workspace`、`internal/launch`、`internal/push`、`internal/codeview`、`internal/files`）、共享契约与基础设施（`internal/contract`、`internal/config`、`internal/runtime`、`internal/statefile`、`internal/fetch`、`internal/pathutil`、`internal/attentionkind`）、服务器外客户端（`internal/client`、`internal/localclient`、`internal/attach`、`internal/sessioncmd`、`internal/tray` 及其子包、`internal/wslclient`、`internal/tunnel`、`internal/service`）、仅含测试的工具包（`internal/archtest`）。`cmd/` 下的包 SHALL 作为装配根，是唯一允许构造 Service 实例并注入 Controller 的位置，也是唯一把配置转换为 Service 选项的位置。

`internal/` 下出现未归类的包，或清单中列出的包已不存在时，架构测试 SHALL 失败，并在报告中给出包路径。

#### Scenario: 新增包未归类

- **WHEN** 贡献者新增 `internal/foo` 包但没有更新分层清单
- **THEN** `go test ./...` 失败，报告指出 `internal/foo` 未归类

#### Scenario: 清单残留已删除包

- **WHEN** 清单仍列出已被删除的包
- **THEN** 架构测试失败，报告指出该过期条目

### Requirement: 依赖方向

Service 包 SHALL NOT 导入 Controller 包、`cmd/` 下的包、WebSocket 库或 `net/http/httptest`。共享契约与基础设施包 SHALL NOT 导入 Controller 包或 Service 包，且没有例外。服务器外客户端 SHALL NOT 导入 Controller 包或 Service 包，只能导入共享契约与基础设施包和其他服务器外客户端包。除 `cmd/` 下的包外，任何包 SHALL NOT 导入 `internal/api`。规则只检查非测试源码。

#### Scenario: Service 导入 Controller

- **WHEN** `internal/workspace` 的非测试文件导入 `internal/hub`
- **THEN** 架构测试失败，报告给出文件、行号和违反的规则编号

#### Scenario: 客户端导入 Controller

- **WHEN** `internal/client` 导入 `internal/api`
- **THEN** 架构测试失败

#### Scenario: 配置包导入 Service

- **WHEN** `internal/config` 导入 `internal/session` 以复用路径展开或预设类型
- **THEN** 架构测试失败

#### Scenario: 客户端导入 Service

- **WHEN** `internal/client` 为展开路径导入 `internal/session`
- **THEN** 架构测试失败，修复提示指向共享路径工具包

### Requirement: Controller 不直接执行 I/O 与业务规则

Controller 包的非测试源码 SHALL NOT 导入 `os/exec`、`syscall`、`golang.org/x/sys` 系列、PTY 库或 `internal/statefile`，SHALL NOT 调用读取、创建、修改或删除文件系统条目的 `os` 函数。Controller SHALL NOT 调用 Service 包的包级函数（包括构造函数与校验函数），只能调用注入的 Service 实例的方法，并可引用 Service 包导出的类型、常量与哨兵错误。

规则 SHALL 基于类型解析来识别被调用的函数，使用导入别名、点导入或函数值变量都不能绕过。

#### Scenario: handler 直接读目录

- **WHEN** Controller handler 调用 `os.ReadDir`
- **THEN** 架构测试失败，报告给出调用位置

#### Scenario: handler 调用 Service 包级校验函数

- **WHEN** Controller 调用 `session.ValidateEnv`，即使通过导入别名调用
- **THEN** 架构测试失败

#### Scenario: handler 调用注入的 Service 方法

- **WHEN** Controller 调用 `s.Workspace.CreateSession(...)` 并用 `errors.Is` 比较 Service 导出的哨兵错误
- **THEN** 架构测试不报告违规

### Requirement: Service 不感知 HTTP 传输

Service 包的非测试源码 SHALL NOT 引用服务端 HTTP 符号，包括 `http.ResponseWriter`、`http.Handler`、`http.HandlerFunc`、`http.ServeMux`、`http.Server`、`http.Error`、`http.Redirect`、`http.MaxBytesReader`，也 SHALL NOT 声明以 `*http.Request` 作为参数的函数或方法。需要发起出站 HTTP 请求的 Service 包 SHALL 在清单中显式登记，并且只能使用客户端符号。

Service 包中实现 `error` 的类型 SHALL NOT 包含底层类型为整数的字段；Service 包 SHALL NOT 声明返回 `int` 的 `Status` 函数或方法。错误到 HTTP 状态码的映射 SHALL 只在 Controller 中进行。

#### Scenario: Service 错误携带状态码

- **WHEN** Service 包的错误类型含有 `Status int` 字段
- **THEN** 架构测试失败

#### Scenario: 已登记的出站客户端

- **WHEN** 已登记出站请求的 `internal/push` 使用 `http.Client` 并比较响应状态码 `http.StatusGone`
- **THEN** 架构测试不报告违规

#### Scenario: 未登记的 Service 使用服务端符号

- **WHEN** `internal/workspace` 声明一个接收 `http.ResponseWriter` 的函数
- **THEN** 架构测试失败

### Requirement: 线上类型边界

Controller 与 Hub 写入 HTTP 响应体的每个 JSON 值、写入 WebSocket 的每个 JSON 载荷（包括 `/ws/sessions` 文本帧，以及 `/ws/s/{id}` 二进制控制帧中类型字节之后的 JSON 部分），以及从请求体或入站控制帧解码的每个目标，SHALL 是 `internal/contract` 中声明的类型，或由这些类型组成的切片、映射与指针。Service 包中声明的类型 SHALL NOT 出现在这些编码值或解码目标的类型中，包括嵌套字段。`internal/contract` SHALL NOT 导入任何 Service 包或 Controller 包。

Service 包 SHALL NOT 自行编码对客户端可见的 JSON 载荷：Service 包 SHALL NOT 声明 `MarshalJSON` 方法，也 SHALL NOT 调用 JSON 编码函数，除非该调用所在的函数已在分层清单中登记并注明理由（出站协议载荷或内部摘要）。经共享持久化包写入磁盘的 JSON 不属于此限制。终端输出与回放快照等原始二进制载荷不受此限制。

在服务器外解码这些 JSON 的 Go 客户端 SHALL 使用同一批线上类型，而不是 Service 类型。

#### Scenario: 直接回传 Service 类型

- **WHEN** handler 把 `session.Info` 传给 JSON 响应写入函数
- **THEN** 架构测试失败，报告给出调用位置和违规类型

#### Scenario: 线上类型嵌套 Service 类型

- **WHEN** 某个 `internal/contract` 类型的字段引用了 `workspace.Reference`
- **THEN** 架构测试失败

#### Scenario: 匿名错误体

- **WHEN** Controller 写出错误响应
- **THEN** 错误体由线上类型表示，架构测试不报告违规

#### Scenario: Service 自行编码控制帧

- **WHEN** `internal/session` 调用 `json.Marshal` 生成发给终端客户端的提交状态帧
- **THEN** 架构测试失败

### Requirement: 线上 JSON 兼容

引入线上类型后，每个 REST 端点和 WebSocket JSON 帧的 JSON 形状 SHALL 与变更前保持一致，包括字段名、字段省略规则、空数组与空对象的表示、`null` 的出现位置、错误体格式和 HTTP 状态码。终端二进制帧 SHALL 保持不变。前端 Zod 契约 SHALL NOT 因本变更而修改。

#### Scenario: 会话列表形状不变

- **WHEN** 客户端在变更前后分别请求 `GET /api/sessions` 和 `GET /api/sessions/{id}`
- **THEN** 两次响应的 JSON 字段集合、值类型与空值表示一致

#### Scenario: configId 互斥规则保持

- **WHEN** 客户端提交同时带 `configId` 与 `cmd` 的 `POST /api/sessions`
- **THEN** 服务端仍以 400 拒绝，不触碰文件系统，不创建会话

#### Scenario: 错误状态码保持

- **WHEN** 请求触发工作区冲突、代码视图快照过期或启动配置不存在
- **THEN** 返回的 HTTP 状态码和 `{"error": ...}` 错误体与变更前一致

### Requirement: 永久线上快照

仓库 SHALL 保留一组永久的线上 JSON 快照测试，覆盖每个 REST 端点的成功与错误响应、`/ws/sessions` 的每种事件，以及 `/ws/s/{id}` 的每种 JSON 控制帧。快照 SHALL 先在变更前的实现上录制。任何线上 JSON 形状的变化 SHALL 导致这些测试失败；有意的协议变更 SHALL 通过显式的重新录制命令更新快照，并让快照文件的差异出现在评审中。

#### Scenario: 无意改变字段

- **WHEN** 某个线上类型的字段被改名或丢失 `omitempty`
- **THEN** 线上快照测试失败，并给出端点和 JSON 差异

#### Scenario: 有意的协议变更

- **WHEN** 贡献者有意新增响应字段并同步修改前端 Zod 契约
- **THEN** 贡献者运行重新录制命令更新快照，评审中可见快照文件的差异

### Requirement: 架构测试的报告与自检

架构测试 SHALL 作为常规 Go 测试运行，被 `go test ./...` 与 `make test` 自动包含，不需要额外工具或网络访问。一次运行 SHALL 报告全部违规，而不是在第一处停止；每条报告 SHALL 包含文件路径、行号、规则编号和修复提示。

每条规则 SHALL 在一个包含故意违规的固定样例上验证：样例中的每处违规都被检测到，合规代码不被报告。

#### Scenario: 多处违规一次报告

- **WHEN** 同一次提交同时引入 Controller 直接 I/O 与 Service 引用 `http.ResponseWriter`
- **THEN** 架构测试的单次运行同时列出这两处违规

#### Scenario: 规则失效被发现

- **WHEN** 某条规则的实现被改坏，不再检测到违规
- **THEN** 该规则的固定样例测试失败
