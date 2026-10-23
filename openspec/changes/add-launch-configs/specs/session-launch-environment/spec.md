## ADDED Requirements

### Requirement: 通过启动配置创建会话

`POST /api/sessions` SHALL 接受可选的 `configId` 字符串字段。提供该字段时，`preset` SHALL 为缺省或空字符串，`cmd` 与 `args` SHALL 为缺省或空数组，`env` SHALL 为缺省、`null` 或空对象 `{}`；违反互斥 SHALL 以 400 拒绝且不创建会话。空字符串 `configId` SHALL 以 400 拒绝，SHALL NOT 静默回退到预设或自定义命令。

服务端 SHALL 按 `configId` 读取已保存的完整 argv 与环境变量，并以二者启动会话；未知 ID SHALL 返回 404，启动配置存储不可用 SHALL 返回 503，二者都不创建会话。互斥、读取配置与环境变量校验 SHALL 在任何目录或 Git 操作之前完成。`cwd`、`workspace` 与 `name` 的语义 SHALL 保持不变，包括目录不存在、工作树失效与目标变化对应的既有错误。保存的 `env` SHALL 在启动前再次经过 `session.ValidateEnv`；不合法时 SHALL 返回 400 且不创建会话。

通过启动配置创建的会话 SHALL 在会话信息中记录 `preset: "custom"` 与完整 `cmd`；`env` SHALL NOT 出现在会话信息中，本期 SHALL NOT 增加配置 ID 字段。启动后更新或删除该配置 SHALL NOT 改变运行中的会话。

现有 `preset`/`cmd`/`args`/`env` 创建方式 SHALL 保持可用且行为不变。

#### Scenario: 使用配置的命令与环境变量

- **WHEN** 用户以 `{"configId": "k3m9q2x7", "cwd": "/home/u/proj"}` 创建会话，配置的 `cmd` 为 `["claude", "--resume"]` 且包含 `ANTHROPIC_BASE_URL`
- **THEN** 新会话执行该 argv，程序环境包含该变量，会话信息记录 `preset: "custom"` 与完整命令

#### Scenario: 字段互斥

- **WHEN** 请求同时包含 `configId` 与非空的 `preset`、`cmd`、`args` 或 `env` 中任意一项
- **THEN** 服务端返回 400，且不创建会话、不进行目录或 Git 操作

#### Scenario: 空对象环境变量不算冲突

- **WHEN** 请求包含有效的 `configId` 与 `"env": {}`
- **THEN** 服务端按该配置保存的环境变量启动会话

#### Scenario: 未知配置

- **WHEN** 请求引用不存在的 `configId`
- **THEN** 服务端返回 404，且不创建会话

#### Scenario: 删除配置后会话继续

- **WHEN** 用户通过某配置启动会话，随后删除该配置
- **THEN** 已运行会话继续使用启动时的 argv 与 env，终端连接与退出不受影响
