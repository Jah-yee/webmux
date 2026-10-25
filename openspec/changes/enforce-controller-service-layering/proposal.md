## Why

`internal/api` 名义上是 Controller 层，但边界只靠约定维持，已经出现多处越界：`POST /api/sessions` 的 handler 自己解析 `configId`、检查互斥规则、调用 `session.ValidateEnv`；`dirs`/`mkdir`/`upload` 直接调用 `os.ReadDir`、`os.MkdirAll`、`os.OpenFile`；`workspace`、`codeview` 的错误类型携带 HTTP 状态码；`api.newServer` 自己构造 `codeview.Service`；`internal/client` 反向导入 `internal/api` 并依赖 `internal/session`；共享的 `internal/config` 依赖 Service 包 `internal/session`；Service 包里的类型（如 `session.Info`、`workspace.Repository`、`launch.Config`）直接 JSON 序列化到 HTTP/WebSocket，内部字段一改，线上协议就跟着变。

本变更划定 Controller 层与 Service 层的边界，并用基于 Go AST/类型信息的架构测试强制执行，让违规在 `go test` 中失败，不再靠代码评审发现。

## What Changes

- 声明分层清单：Controller（`internal/api`、`internal/hub`、新增 `internal/wire`）、Service（`session`、`workspace`、`launch`、`push`、`codeview`、新增 `files`）、共享契约/基础设施、服务器外客户端、装配根 `cmd/*`。`internal/` 下的每个包都 MUST 被归类；未归类的包使架构测试失败。共享层和服务器外客户端都不得依赖 Service 或 Controller，没有例外。
- 新增 `internal/archtest`：使用 `golang.org/x/tools/go/packages` 加载带类型信息的包，检查依赖方向、Controller 禁止直接做 I/O 和调用未登记的 Service 包级函数、Service 禁止使用服务端 HTTP 符号和 HTTP 状态语义，以及 Controller 编解码的 JSON 值 MUST 是 `internal/contract` 中的线上类型。纯函数例外须显式登记并附理由；业务整数不等于 HTTP 状态。规则检查器自身由带故意违规的 fixture 模块验证。
- 新增 `golang.org/x/tools` 依赖，仅用于测试，不进入发布二进制。
- 把越界逻辑迁入 Service 层：
  - `configId` 解析、互斥规则和环境变量校验迁入 `workspace.Service`。
  - 目录补全、`mkdir` 和上传落盘迁入新的 `internal/files`。
  - `push.Validate` 保持包级纯校验函数，并在 R5 的 `pure` 表中附理由登记；移除无状态方法包装。
  - `codeview.Service` 的构造移到 `cmd/webmux`。
  - `ManagerHint` 移到 `internal/runtime`。
- 解除共享层与客户端对 Service 的依赖：`ExpandPath` 移到新的共享包 `internal/pathutil`；注意力种类常量移到新的共享包 `internal/attentionkind`；`config` 定义自己的 `Preset` TOML 类型；配置到 `session.Options` 的转换从 `config.SessionOptions()` 移到 `cmd/webmux`。
- `workspace`、`codeview` 的错误改为与传输无关的错误种类，由 Controller 统一映射为 HTTP 状态码；删除两个包的 `Status()`。
- 拆分 DTO：Controller 读写的每个 JSON 请求体、响应体和 WebSocket JSON 帧都定义为 `internal/contract` 中的线上类型，由 Controller/Hub 在 Service 类型与线上类型之间转换。`internal/client`、`internal/sessioncmd` 等线上消费者也改用这些线上类型。Service 类型上用于磁盘持久化的 json 标签保留。
- 线上 JSON 形状保持不变：字段名、`omitempty`、空集合（`[]`/`{}`）与 `null` 的表示、错误体格式和状态码都与变更前一致；`web/src/schemas.ts` 不需要修改。线上 JSON 快照在改动前录制，作为永久测试保留，有意的协议变更需显式重新录制。
- 合并后的加固：R9 检查包级初始化与编码器别名；消息保持单载荷不变量，非法内部消息以 1011 关闭当前连接；聚合 pending 使用值避免逃逸；快照仅归一化端点明确动态字段，并拆成六组独立场景。
- 按 3 个阶段交付（同一个 PR 中的三组提交，每组都能单独回滚），架构规则随对应迁移一起加入：PR1 线上快照 + 架构测试 R0–R7 + 越界逻辑迁移；PR2 REST 线上类型；PR3 WebSocket 事件与终端帧编码迁到 Hub。

## Capabilities

### New Capabilities

- `controller-service-layering`：分层清单与归类要求、依赖方向、Controller 与 Service 各自的禁止项、HTTP 状态映射归属、线上类型（DTO）边界与线上 JSON 兼容要求，以及架构测试在违规时的失败与报告方式。

### Modified Capabilities

（无。现有 REST/WebSocket 行为、状态码与 JSON 形状保持不变，`POST /api/sessions` 的 `configId` 互斥规则从 handler 迁入 Service 后语义不变。）

## Impact

- 后端：`internal/api`（router、upload、push、code、workspace、launch、local）、`internal/hub`、`internal/workspace`、`internal/codeview`、`internal/push`、`internal/session`、`internal/config`、`internal/runtime`、`internal/client`、`internal/sessioncmd`、`internal/attach`、`internal/tray`、`internal/contract`；新增 `internal/files`、`internal/wire`、`internal/pathutil`、`internal/attentionkind`、`internal/archtest`；`cmd/webmux/main.go`、`cmd/baseline` 及所有 `api.New(...)`/`workspace.New(...)`/`config.SessionOptions()` 调用点改为注入新依赖。PTY、回放、退出顺序和会话清理路径不变；TOML 配置格式不变。
- 协议：REST 与 WebSocket 的 JSON 形状和状态码不变；终端二进制帧不变。
- 依赖：`go.mod` 新增 `golang.org/x/tools`（只被 `_test.go` 引用）。
- 前端：无代码变更。
- 测试与 CI：`go test ./...` 和 `make test` 自动包含 `internal/archtest` 和线上快照测试；现有 HTTP 级测试作为线上兼容的回归保护保留。
- 文档：`docs/architecture.md` 说明分层与规则；`AGENTS.md` 更新目录说明与约定；`docs/user-stories.md` 增补贡献者工具类故事。
