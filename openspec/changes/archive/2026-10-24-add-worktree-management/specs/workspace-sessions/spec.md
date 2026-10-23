## Purpose

将现有 PTY 会话与其启动时所在的仓库、工作树建立明确关联，使多个 agent 和辅助 Shell 能在不同或同一工作树中运行。关联用于展示与资源清理保护，不强制使用 Git、不追踪 agent 内部任务，也不改变终端连接和会话的生命周期。

## ADDED Requirements

### Requirement: 任意有效目录保留直接启动

系统 SHALL 保留按 `cwd` 启动预设或自定义命令的接口和 UI，不要求注册仓库、创建 worktree 或安装 Git。空 `cwd` SHALL 沿用配置默认目录。命令执行 SHALL 继续遵循预设与自定义 argv 的互斥规则；Git 检测错误 SHALL NOT 将直接启动变成 Git 必选流程。

#### Scenario: 无 Git 或普通目录启动
- **WHEN** 主机没有 Git，或选择的目录不属于 Git 仓库，用户提交有效的目录和命令
- **THEN** 系统在该目录启动会话，在直接会话分组展示，并允许连接、关闭和移除

#### Scenario: 只有主工作区的仓库
- **WHEN** 仓库没有 linked worktree，用户选择其主工作区启动 agent
- **THEN** 系统直接启动，不要求先新建分支或 linked worktree

#### Scenario: 未注册仓库启动
- **WHEN** 用户从一个未注册的 Git 仓库直接启动会话
- **THEN** 系统启动并展示直接会话，不自动将仓库加入持久清单

### Requirement: 关联反映启动位置而非实时进程目录

会话信息 SHALL 保留真实启动 `cwd`，并在能够匹配已注册仓库时提供仓库及工作树关联。从 worktree 子目录启动 SHALL 归入该 worktree，但 SHALL NOT 将实际 `cwd` 改为根目录。终端内的 `cd`、OSC title 或 agent 自行创建的 worktree SHALL NOT 让 WebMux 宣称会话已迁移。

#### Scenario: 在 linked worktree 子目录启动
- **WHEN** 用户在已注册工作树的 `packages/app` 目录直接启动 agent
- **THEN** 会话在该子目录执行，首页归入正确工作树，接口同时保留完整启动目录与关联信息

#### Scenario: 添加或移除注册后重新分组
- **WHEN** 已有会话所属仓库被显式添加或移除注册
- **THEN** 可识别的会话进入仓库分组或回到直接会话分组，既有进程、会话 ID 和缓冲不变

#### Scenario: 工作树被外部删除
- **WHEN** 已关联的 worktree 被外部删除或移动
- **THEN** 既有会话仍可访问，显示启动位置已不可用或不再属于当前清单，不悄悄绑定到另一个目录或隐藏会话

### Requirement: 一个工作树允许多个独立会话

系统 SHALL 允许同一工作树同时运行多个 agent、Shell 或自定义命令会话。各会话 SHALL 保持独立 PTY、状态、退出码和缓冲；UI SHALL 明示共享工作树意味着共享文件，并非 agent 间文件隔离。

#### Scenario: 同一工作树启动第二个会话
- **WHEN** 某工作树已有运行中的 agent，用户再启动一个 Shell 或 agent
- **THEN** 新会话独立创建，两者都展示在该工作树下；关闭其中一个不会关闭其他会话或删除工作树

### Requirement: 选择工作树后必须在明确目标启动

从工作树上下文启动 SHALL 使用服务端验证的目标目录；请求中工作树引用与 `cwd` 冲突、目标缺失或正在删除时 SHALL 拒绝，不能静默改用默认目录。创建 worktree 与启动 agent SHALL 是可区分的结果。

#### Scenario: 创建成功但 agent 不存在
- **WHEN** 新工作树创建成功，但所选 agent 可执行程序不存在或 PTY 启动失败
- **THEN** 系统报告 agent 启动失败，保留并展示已创建的 worktree，允许用户换命令重试，不自动删除工作树或分支

#### Scenario: 明确选择的工作树失效
- **WHEN** 提交启动时所选工作树已被删除、引用不匹配或处于删除中
- **THEN** 系统拒绝启动并要求刷新，不在仓库主工作区或其他目录创建会话

### Requirement: 保持终端管理边界

新增仓库关联 SHALL NOT 改变现有终端字节传输、断线后会话存活、回放连续性、退出顺序或慢客户端断开规则。agent 管理 SHALL 使用现有预设/参数/自定义命令和运行/等待/空闲/退出状态，不将等待状态解释为任务完成，也不自动追加 agent 专属 worktree 或 resume 参数。

#### Scenario: 断开并重连 worktree 中的 agent
- **WHEN** 用户关闭浏览器后重新进入某 worktree 会话
- **THEN** 连接回到原 PTY 并回放有界缓冲，不重新启动 agent，不创建新的 worktree

#### Scenario: 退出与移除会话
- **WHEN** agent 正常退出或被用户关闭，随后用户移除其会话记录
- **THEN** 原工作树、分支和其他会话不变，已退出记录只在显式移除前保留；服务重启仍会清除所有会话记录
