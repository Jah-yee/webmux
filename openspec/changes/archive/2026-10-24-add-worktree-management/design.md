## Context

动机与产品边界见 [proposal.md](proposal.md)。本设计基于以下已读取的实现，而不是假设已有工作区抽象：

- `internal/session/manager.go` 通过 `Create(cwd, preset, cmd, args, name)` 创建 PTY；路径展开后验证目录，预设与自定义 argv 互斥。会话列表和事件均在内存中。
- `internal/api/router.go` 注入配置与 session manager；`POST /api/sessions` 接受目录和命令，`DELETE /api/sessions/{id}` 关闭，`?remove=1` 只移除已退出记录。所有 API/WS 经过认证与 Origin 中间件。
- `cmd/webmux/main.go` 显式构造依赖，并在停机时清理会话。Git 与仓库状态存储尚不存在。
- `web/src/pages/SessionList.svelte` 用 `/ws/sessions` 快照/增量更新平铺卡片；`NewSessionSheet.svelte` 已有桌面 Dialog、手机 Drawer、目录选择、预设、自定义命令和失败提示。
- `web/src/schemas.ts` 是现有 JSON 合同；Git 状态不能混入终端二进制帧。`web/test/e2e/fixtures.ts` 已提供真实隔离服务和临时目录模式。
- `README.md` 的“不落盘”和 `WebMux.md` 的平铺首页描述需要在实施时修订；仅仓库清单落盘，不改变会话重启即结束的约束。当前没有可复用的主 OpenSpec 能力文件。

### Confirmed Product Decisions

用户两轮选择已确认：完整日常 worktree 管理；同工作树多个会话；仓库分组总览；显式注册且持久保留仓库；沿用终端会话级 agent 管理；允许二次确认强制删除；创建新分支或使用已有本地分支；默认工作树目录位于仓库旁。

具体默认策略：不删除分支，不自动 fetch，不自动复制 `.env`/依赖目录，不注入 agent 专属参数；locked worktree 必须先在终端解锁，主工作区及含子模块/嵌套仓库的目录不通过页面删除。以上属于保护边界，不是 Git 全功能管理。

## Goals / Non-Goals

**Goals:**

- 用 Git 作为仓库/worktree 事实来源，用轻量清单保存用户注册意图，不复制 Git 数据库。
- 在目录启动和删除之间建立同一服务内的明确互斥顺序，包括没有仓库标签的直接会话。
- 保持现有 PTY 传输和进程清理；扩展会话元数据而非另建 agent 生命周期。
- 通过局部失败和明确刷新让无 Git、不可访问仓库与正常终端共存。

**Non-Goals:**

- 不扫描整台机器、不 clone/init、不做 Git commit/merge/push、分支删除或 GitHub 集成。
- 不实现 worktree move/repair/prune、锁定管理、子模块清理或 detached worktree 创建；已有 detached worktree 仍可发现和启动。
- 不做任务派发、agent 对话恢复、自动依赖安装/初始化脚本、进程沙箱、跨服务重启 PTY 恢复或多终端分屏。
- 不追踪终端运行后 `cd` 的真实目录；删除保护只覆盖 WebMux 已知的启动目录，不声称能隔离或停止外部进程。

## Decisions

### 1. 一个可选 workspace 服务，不把 Git 放进 PTY 读写路径

新增 `internal/workspace`，负责仓库清单、Git CLI 调用、快照及操作协调。由 `main.go` 构造后注入 API，显式持有 session manager 所需的创建、枚举、关闭和等待退出能力。`internal/session` 只增加可比较的关联元数据与必要的完成等待能力，不依赖 Git 包；`internal/hub` 继续转发 session 信息和原有终端帧。

API 的所有会话创建入口通过同一 workspace 协调层，直接启动也不绕过删除保护；无 Git 时这层只执行路径保护和原会话创建。Git 命令、存储 I/O、关闭等待都不持有 session manager/session 内部锁。会话列表状态探测中不执行 Git 命令。

不采用 libgit2/go-git：系统 Git 对 worktree、common directory 和现有配置的语义更完整，且单二进制服务仍可在无 Git 时提供终端。Git 能力探测是局部功能探测，不是启动失败条件；所需命令能力不足返回明确原因。

### 2. 注册清单持久化，Git 状态按需读取

增加 TOML `state_dir`，默认 `$XDG_STATE_HOME/webmux`，未设置时使用 `~/.local/state/webmux`。清单存于 `repositories.json`，包含版本号与 `{id, commonDir, anchorPath, displayName}` 条目；不存 token、终端输出、会话记录或 dirty 缓存。`anchorPath` 是可用于 Git 命令的主工作区或 bare 仓库路径。

- 通过规范化绝对路径、解析符号链接及 `git rev-parse --path-format=absolute --git-common-dir` 识别仓库，而非判断 `.git` 是否为目录。
- 仓库 ID 在注册时分配并持久保存；common directory 去重。从 linked worktree/子目录注册仍定位同一仓库。外部移动 common directory 不猜测新身份，显示不可用并让用户重新注册。
- 清单通过同目录临时文件、同步、原子 rename 发布，目录权限 `0700`、文件 `0600`；串行化注册变更，落盘失败不发布内存成功态。一个状态目录只供一个服务实例使用，使用进程级文件锁防止多个实例覆盖；无法取得锁仅禁用该实例仓库管理。
- 文件不存在表示首次使用；损坏/权限错误表示注册服务故障，不覆盖原文件。当前进程没有可信清单时禁用注册写入，保留直接启动，并在配置/仓库响应中报告原因。
- 移除注册仅写清单并重新分组会话，不执行 Git 或关闭进程。

不采用 SQLite 或浏览器 localStorage：前者对单用户清单过重，后者无法跨设备一致保留。

### 3. 数据身份、状态与现有会话的关联

|对象|新增合同|
|---|---|
|Repository|`id`、`name`、`commonDir`、`anchorPath`、`availability`、`error`、`checkedAt`、`worktrees`|
|Worktree|`id`、`repositoryId`、`path`、`kind: main/linked/bare`、`branch`（可空）、`head`（可空）、`detached`、`locked`、`lockReason`、`prunable`、`available`、`dirty`（可空）、`hasUntracked`、`hasIgnored`、`error`|
|SessionInfo|保留全部现有字段，新增 `workspace: {repositoryId, worktreeId, worktreePath} \| null`；`cwd` 始终是启动目录|
|Workspace capability|`gitAvailable`、`registryAvailable`、对应原因；作为现有 `/api/config` 的新增对象返回，不暴露凭据|

Worktree ID 从仓库 ID 与规范化路径确定，用于 UI/API 引用；不以分支名标识（分支可切换），不承诺外部 move 后 ID 不变。删除预览另保存 Git 管理目录/文件系统身份，不能只比较可重用的路径 ID。

匹配会话先解析实际启动路径，再查其 Git 根/common directory 与已注册清单，避免字符串前缀把 `/repo-a` 匹配到 `/repo`。Git 检测失败时直接启动依然成功，关联为 null。删除保护另按规范化目录及路径分隔边界检查所有启动 `cwd`，不依赖可空标签。

注册变化、仓库刷新时，在锁外计算关联，再以会话 ID 和原启动目录为条件提交元数据更新，复用 `/ws/sessions` 的 `updated` 事件。失去工作树的既有会话保留原位置展示于“已不可用工作区”；注册移除后清空公开关联，进入直接会话。嵌套 Git 仓库按实际 Git 根匹配，不归错外层仓库。

### 4. Git 调用使用稳定机器输出

- 使用 `git -C <anchor> worktree list --porcelain -z` 解析 NUL 分隔记录，接受空格、换行路径和附加未知字段，不解析人类表格输出。
- 分支列表来自本地 refs，标明已被哪些工作树占用；新分支基线可选本地分支、已有的 remote-tracking ref 或可验证的本地提交，不触发网络。默认基线是主工作区当前可解析 HEAD；bare 仓库要求明确选择可用基线。
- 修改摘要使用 `git status --porcelain=v1 -z --untracked-files=normal --ignored=matching`；忽略项单独呈现，默认删除也保护它们，避免 `.env`/构建文件被“干净”标签掩盖。不解析完整 diff，不计算 ahead/behind。
- 通过 `exec.CommandContext` 直接传 argv，分支按 Git 规则验证并拒绝选项型输入；基线解析成提交 OID 后传给创建命令。路径先转绝对路径，适用处使用 `--` 分隔；不拼接 shell，不静默修改 `safe.directory`，不全局信任所有目录。
- Git 子进程禁用交互提示并受操作期限约束；读取最多 10 秒，写操作最多 30 秒，关闭/强删共享该写操作期限。取消须回收启动的命令进程，返回有上下文的错误并重新读取实际状态，不以超时推定没有副作用。
- 快照按仓库串行读取各工作树摘要；同仓库并发读取合并，UI 请求取消或旧响应不能覆盖新状态。不新增常驻全磁盘 watcher 或高频 Git 轮询。

Git 是可执行仓库配置/hooks/filter 的本机工具，不是沙箱。用户添加的目录必须可信，延续当前“以服务用户运行任意命令”的部署边界，不承诺抵御恶意本地仓库。

### 5. 创建 worktree 与启动 agent 分成两个可观察结果

新建表单包含分支模式、分支、基线与独立目录名/目标路径字段。分支名 `feature/foo` 不直接拼接为目录路径；默认目录名用最后一个分支段，完整路径可编辑，冲突要求用户更改而不是覆盖或静默改名。默认父目录为主工作区/bare 仓库旁的 `<basename>.worktrees`；拒绝已存在目标及与任何已知工作树相互嵌套的目标，避免在 `.git`、主工作区或另一个工作树内部创建托管目录。

显式传入新分支或已有本地分支，不使用 `-B`、force 或远程猜测。路径和分支预检后仍以 Git 执行结果为准，应对外部竞争。不自动修改 `.gitignore`、复制未提交文件或执行 WebMux 自定义初始化脚本。

“创建并启动”在 UI 中依次完成创建 worktree 和创建会话两个请求。前一步成功后立即更新页面目标；后一步失败保留工作树，重试只启动会话，不能再次创建同名分支。Git 添加自身失败时不强制回滚可能已被外部进程使用的资源，刷新并显示实际残留。

### 6. 删除采用预览、确认、重新验证和独占执行

普通删除与强制删除都先获取预览。服务端保存短期、单次使用的随机 `confirmationId`（5 分钟有效），绑定仓库、目标规范路径、Git 管理身份、当前分支/HEAD、锁定/支持状态、关联会话 ID 集合及其是否已退出。它是防误删状态绑定，不是认证凭据，不绑定不断变化的输出时间戳。

普通确认提交 `mode: safe`；存在修改、未跟踪/忽略文件或活动会话则返回冲突。强制操作必须从普通预览进入第二个危险确认页，提交 `mode: force`、完整 `confirmPath` 和明确的 `closeSessions: true`；不提供默认勾选。无运行会话时仍需确认删除目录内容。

执行顺序：

1. 验证预览存在且未过期，预留目标删除权；重新读取身份、分支/HEAD、锁定、嵌套仓库/子模块及会话集合。变化则在关闭进程之前返回 `409`，要求重新预览。
2. 主工作区、bare、locked、缺失、状态未知、含子模块/嵌套仓库一律拒绝；不自动 prune、unlock 或执行双重 force。锁定条目仍可启动 agent，锁不是运行互斥。
3. 安全模式重新检查所有文件保护条件；强制模式关闭确认范围内的活动 WebMux 会话，等待实际退出发布（包括子进程清理和 PTY 排空），并保留记录。
4. 再检查目标身份/锁定/嵌套仓库和相关会话，调用 `git worktree remove` 或单个 `--force`；不调用 `rm -rf`，不删除分支。
5. 释放预留权、作废确认、刷新快照；失败如实呈现已经关闭的会话和仍存在的工作树。不自动重试破坏性操作，也不自动重启已关闭 agent。

协调层使用规范化路径的删除预留与在途启动计数：所有直接/关联启动先短暂登记目录，再执行创建；删除预留禁止目标或子目录的新启动，并等待已登记启动完成后比较会话集合。Git 命令和进程等待在互斥锁外运行。创建 worktree 的分支/目标变更按仓库串行化；重叠目录的删除请求互斥。无 Git 的直接启动仍通过路径预留检查，因此不能绕过正在执行的删除。

状态令牌不试图冻结用户编辑：强制确认授权的是“删除时目标目录全部内容”，而非某个文件哈希快照。页面说明外部进程及终端 `cd` 后访问目标的进程不一定被识别。外部 rename/写入的竞态无法被普通 Git CLI 与进程内锁完全排除；重新检查降低误操作风险，但不宣称提供本机文件系统事务。

### 7. REST 合同与一致性

|接口|行为|
|---|---|
|`GET /api/repositories`|返回持久仓库清单与逐仓库快照/错误；列表本身不因单仓库错误整体失败|
|`POST /api/repositories {path}`|注册，新增返回 `201`，同仓库幂等返回 `200`|
|`DELETE /api/repositories/{id}`|仅移除注册，成功 `204`|
|`GET /api/repositories/{id}/worktrees`|重新读取该仓库工作树快照，返回 `checkedAt`|
|`GET /api/repositories/{id}/branches`|本地分支、可选本地基线及占用信息，不 fetch|
|`POST /api/repositories/{id}/worktrees`|接受 `{path, mode: new, branch, base}` 或 `{path, mode: existing, branch}`，创建成功 `201`|
|`POST /api/repositories/{id}/worktrees/{worktreeId}/removal-preview`|只读预览，返回目标、阻塞原因、会话、删除内容类别和 `confirmationId`|
|`POST /api/repositories/{id}/worktrees/{worktreeId}/remove`|提交确认和 safe/force 模式；成功 `204`，失败不伪造回滚|
|`POST /api/sessions`|保留原合同；新增可选 `workspace: {repositoryId, worktreeId}`，与提供的 `cwd` 必须一致（允许该工作树内的实际子目录）；指定 workspace 且省略 cwd 使用其根目录|

错误继续返回 `{error}`，区分 `400` 非法输入、`404` 资源不存在、`409` 冲突/过期确认、`503` Git/注册存储不可用、`504` 操作超时；存储或非预期执行错误为 `500`，认证沿用 `401/403`。明确 workspace 启动失败不能退回默认目录；只传 cwd 的原请求继续有效。

前后端一次性更新 Zod 合同与消费者，不新增第二套 api/ws 工具。会话数据仍来自已有 WebSocket；仓库数据通过 REST 首次加载、显式刷新、返回首页/可见、重连及操作完成刷新。跨客户端注册变化在这些刷新点收敛，不承诺 Git 状态或注册清单实时广播，也不轮询会话列表。

### 8. 首页信息架构与交互

- 顶部保留品牌、连接状态、主题、新建会话；新增添加仓库与刷新入口。
- 正文按注册顺序展示仓库。仓库标题显示名称/路径、运行/等待/退出数量与操作菜单；折叠后仍显示数量。
- 仓库内主工作区优先，其余工作树按规范路径稳定排序。工作树标题显示分支/提交、状态标签、目录、启动入口及删除菜单；会话卡片在其下按活跃时间排序。无会话时显示轻量空行，而非隐藏工作树。
- 没有当前工作树的已关联会话放在仓库内“已不可用工作区”；未注册/普通目录会话在底部“直接会话”中按 cwd 分组。所有会话恰好出现一次。
- 多会话工作树显示“共享目录”提示；dirty/等待/连接状态分别呈现，不能把文件变化或提示符启发式状态当作任务完成。
- 桌面工作树内可用多列卡片，手机单列；仓库和工作树均可折叠，不新增常驻侧栏。刷新保留当前展开状态；整页重新打开默认展开有会话的组，空组折叠但保留操作。
- `NewSessionSheet` 增加直接目录/已有工作树/新工作树三种明确模式；全局默认直接目录，上下文入口预选目标。保留最近目录和预设记忆，复用 DirectoryPicker、Dialog/Drawer、AlertDialog 和 DropdownMenu。
- 路径可换行或通过明确点击查看完整值，危险确认直接显示完整路径；不依赖 hover。键盘焦点、表单错误和手机软键盘遵循现有组件模式。

## Risks / Trade-offs

- [强制删除会不可逆丢失修改和忽略文件] → 单独危险确认、完整路径输入、关闭会话授权、保留分支；拒绝主工作区/locked/嵌套仓库，并说明 detached 提交可能没有分支保护。
- [未被 WebMux 管理的进程或运行后 cd 无法完整追踪] → 明示启动目录关联边界，不把 worktree 描述为沙箱；不自动关闭外部进程。
- [外部 Git 操作与删除竞态] → 执行前后重新校验、使用 Git 自身保护、拒绝身份变化；承认无法提供跨进程原子性。
- [大型仓库状态读取较慢] → 按需刷新、请求合并和期限；显示读取时间及局部错误，不在 PTY 输出循环探测 Git。
- [分组较多导致移动端页面过长] → 两级折叠、稳定顺序、折叠标题保留等待数量；不引入全局排序跳动。
- [状态文件损坏或多实例覆盖] → 原子发布、文件锁、故障时不覆盖；Git 和终端功能分离，直接启动仍可用。
- [新建工作树缺少环境文件或依赖] → 提交前说明只检出版本控制内容；用户通过 Shell/agent 自行准备，本变更不执行隐式 setup。

## Migration Plan

1. 实施时先增加可选 `state_dir`、注册/Git 服务和协议，再切换首页；所有依赖通过构造函数注入，现有 cwd 请求不要求迁移。
2. 新部署仓库清单为空，不扫描文件系统、不从最近目录自动注册。首次显式添加后，现有运行会话按启动位置重新关联。
3. 更新 `README.md`、`WebMux.md`、`deploy/config.example.toml`，说明新布局、强制删除风险、状态目录权限与“清单保留/会话不保留”；生产构建重新嵌入前端。
4. 回退旧二进制前移除新增 TOML 配置项（若旧解析器需要）；旧版忽略独立状态文件。Git worktree 和分支无需迁移或删除，仍可通过普通 cwd 启动。回退/升级导致的服务重启仍会结束所有 PTY 会话。

## Validation Strategy

- 仓库/Git 层使用临时真实仓库覆盖主工作区、linked 子目录、符号链接、重复注册、bare/unborn/detached、空格/换行路径、分支占用、修改/忽略/锁定及持久化失败。
- API/生命周期测试覆盖认证/Origin、直接与关联启动、不可用降级、并发启动/删除、确认过期/身份变化、进程退出等待、失败后保留资源和分支不删除；沿用 Go race 检查。
- 真实浏览器组件验证分组、折叠状态、错误/空状态、长路径及危险确认；E2E 在隔离后端与临时 HOME/Git 仓库中启动真实 Shell，而非依赖已登录 agent。
- 必须实际走通：注册仓库 → 创建两个 worktree → 各启动 Shell/agent 命令 → 同 worktree 第二会话 → 输入并重连 → 脏状态默认删除拒绝 → 强制确认关闭并删除 → 分支仍存在 → 服务重启清单保留而会话消失。
- 独立无 Git 服务实例验证直接启动；移动 Chromium 验证新弹层与软键盘遮挡，iOS/Android 真机键盘检查仍是单独限制。
- 本规划阶段已用 Git 2.53.0 的临时仓库实测 common directory 去重基础、带空格路径的 NUL 输出、未跟踪文件删除保护、锁定时单 force 拒绝和删除后分支保留；不将这些实验宣称为 WebMux 功能已实现。

## References

- [Git worktree 官方文档](https://git-scm.com/docs/git-worktree)：主/linked worktree、porcelain NUL 格式、分支占用、remove/lock/force 边界。
- [Git rev-parse 官方文档](https://git-scm.com/docs/git-rev-parse)：仓库定位与 common directory，避免假设 `.git` 是目录。
- [Git status 官方文档](https://git-scm.com/docs/git-status)：机器状态输出与未跟踪/忽略文件区分。
- [Claude Code Desktop 文档](https://code.claude.com/docs/pt/desktop)：独立 worktree 支撑并行 agent 的参考，不照搬其会话自动清理策略。
- [Codex 手机远程工程工作流](https://developers.openai.com/blog/mastering-codex-remote-for-engineering)：项目上下文、当前 checkout 与独立 worktree 的选择参考，不引入其任务/线程模型。
