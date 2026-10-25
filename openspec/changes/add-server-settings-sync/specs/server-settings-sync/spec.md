## Purpose

让同一 WebMux 实例上的主题、字号、输出预览、快捷指令、代码阅读偏好与主页工作树选择脱离单个浏览器，统一保存在服务端并在所有已认证客户端之间同步；同时把 token、推送订阅、草稿与上次选择等必须留在设备的数据划出清晰边界，并在设置存储损坏、被占用或暂时不可达时保持终端与会话可用。

## ADDED Requirements

### Requirement: 配置分层与同步范围

系统 SHALL 把用户偏好划分为三类，并 SHALL 只把第一类放进服务端设置文档与设置 API：

- 跨客户端同步：`theme`、`fontSize`、`preview`、`codeReader.lineNumbers`、`codeReader.diffOverflow`、`quickCommands`、`homeWorktrees`、`forceSoftKeys`。
- 仅当前设备：浏览器中保存的 token 输入、Web Push 订阅标识、草稿与暂存正文、`draftPersistence` 开关、`recentDirs`、`lastLaunch`、`customCommand`、`clientId`、`clientLive`。
- 部署默认与全局策略：`listen`、`token`、`state_dir`、`allowed_origins`、`tunnel.*`、`default_cwd`、`scrollback_bytes`、`idle_after_seconds`、`prompt_patterns`、`agent_protocol`、`bell_attention`、`push.*`。

设置接口 SHALL NOT 返回或接受第二类与第三类字段；PATCH 中出现 `token`、`push`、`draft`、`listen` 等未知键时 SHALL 返回 400。服务端 SHALL NOT 因为本能力修改 `config.toml` 或改变上述 TOML 项的生效方式。

已有公网访问配置 SHALL 保持独立：`tunnel.*` 提供 TOML 部署默认值，现有专用服务通过 `tunnel.json` 持久化并即时应用开关、模式、凭据和主机名的覆盖，`cloudflared` 与下载镜像仍只由 TOML 管理。通用设置文档、快照和更新帧 SHALL NOT 包含隧道凭据，本能力 SHALL NOT 改变既有隧道入口或优先级。

#### Scenario: 跨设备读取同一偏好

- **WHEN** 用户在桌面浏览器把主题改为深色并新增一条快捷指令，随后在手机上打开同一实例
- **THEN** 手机在认证后取得同一份设置快照，显示深色主题与该快捷指令，不依赖手机本地曾经保存的值

#### Scenario: 设备本地项不进入设置接口

- **WHEN** 客户端读取 `GET /api/settings` 或服务端写入 `settings.json`
- **THEN** 响应与文件都不包含浏览器 token、Web Push 订阅、草稿正文、最近目录或上次启动选择

#### Scenario: TOML 策略不经 API 修改

- **WHEN** 客户端在 PATCH 中提交 `listen`、`allowed_origins`、`tunnel` 或 `push`
- **THEN** 服务端返回 400，`config.toml` 与当前服务行为保持不变

### Requirement: 服务端设置存储与原子持久化

设置 SHALL 保存于 `<state_dir>/settings.json`，文件权限 SHALL 为 0600，状态目录 SHALL 为 0700。服务端 SHALL 在启动时打开 `<state_dir>/settings.lock`（0600）并取得独占非阻塞文件锁，在进程生命周期内持有；无法取得锁时本实例的设置功能 SHALL 不可用，SHALL NOT 读取或写入设置文件，其他功能 SHALL 照常启动。

设置文件不存在时，服务端 SHALL 视为所有键为 `null`、`revision` 为 0，且 SHALL NOT 创建文件；第一次成功 PATCH 才创建。每次变更 SHALL 通过同目录临时文件写入、`fsync`、关闭、`rename` 与目录同步原子替换；写入失败时 SHALL 回滚内存状态并返回错误。

设置文件无法解析、版本不支持、字段非法或超过 4 MiB 时，服务端 SHALL 保留原文件，SHALL NOT 覆盖、改名、重建或自动修复，并 SHALL 让设置接口返回 503；修复、删除文件或停止占用锁的实例后，服务端 SHALL 需要重启才能重新加载。设置功能不可用 SHALL NOT 影响会话创建、终端连接、WebSocket、启动配置、推送或仓库操作。

#### Scenario: 两个实例共用状态目录

- **WHEN** 第一个实例已持有 `settings.lock`，第二个实例以同一 `state_dir` 启动
- **THEN** 第二个实例的设置接口返回 503 并说明状态目录已被另一实例使用，`settings.json` 只由第一个实例修改，第二个实例的终端仍可连接

#### Scenario: 损坏文件不影响会话

- **WHEN** `settings.json` 内容损坏，用户打开设置页并启动一个新会话
- **THEN** 设置接口返回 503 且原文件保持存在，会话仍可创建、连接和退出

#### Scenario: 保存失败回滚

- **WHEN** PATCH 通过校验，但临时文件写入或重命名失败
- **THEN** 接口返回 500，内存快照保持修改前内容，`settings.json` 不出现部分写入

#### Scenario: 首次启动不写设置文件

- **WHEN** 状态目录中没有 `settings.json`
- **THEN** 设置快照使用默认值，服务不创建该文件；锁文件按现有状态文件模式打开

### Requirement: 设置快照、修订号与校验

`GET /api/settings` 与成功的 PATCH SHALL 返回 `{revision, updatedAt, values}`。`revision` SHALL 为从 0 开始的单调递增整数，每次成功 PATCH 加 1；`updatedAt` SHALL 为 UTC RFC 3339 时间，未写入过时为 `null`。`values` SHALL 始终包含 `theme`、`fontSize`、`preview`、`codeReader`、`quickCommands`、`homeWorktrees`、`forceSoftKeys`，未设置的键为 `null`。

设置值 SHALL 满足以下约束：

- `theme`：`"light"`、`"dark"` 或 `null`；
- `fontSize`：整数 12–20 或 `null`；
- `preview`：`{lines, skip}`，`lines` 为 1–50 的整数，`skip` 为 0–50 的整数，或 `null`；
- `codeReader`：`{lineNumbers, diffOverflow}`，`lineNumbers` 为 `"show"` 或 `"hide"`，`diffOverflow` 为 `"scroll"` 或 `"wrap"`，或 `null`；
- `quickCommands`：0–50 条，每条 `id` 为 1–64 字符且列表内唯一，`name` 去首尾空白后不超过 40 字符，`command` 去首尾空白后非空且不超过 4096 字符，`enter` 为布尔，或 `null`；
- `homeWorktrees`：对象，最多 200 个仓库，仓库 ID 为 1–128 字符，每个仓库最多 1000 个工作树 ID，每个 ID 为 1–128 字符，全部条目合计最多 2000 个，或 `null`；
- `forceSoftKeys`：布尔或 `null`。

所有字符串 SHALL 是有效 UTF-8。快捷指令保存时 SHALL 把 CRLF/CR 统一为 LF 并移除正文末尾换行；服务端 SHALL 执行相同规范化。PATCH 请求体 SHALL 不超过 2 MiB；设置文件 SHALL 不超过 4 MiB。任意值越界、类型错误或包含未知字段时 SHALL 返回 400，且 SHALL NOT 写入任何部分内容。

#### Scenario: 默认快照

- **WHEN** 设置文件不存在，客户端首次读取设置
- **THEN** 返回 `revision: 0`、`updatedAt: null`，七个键全部为 `null`

#### Scenario: 快捷指令的未初始化与空列表

- **WHEN** 服务端 `quickCommands` 为 `null`，用户随后在设置页删除全部预置指令并保存
- **THEN** 第一次保存写入 `[]`，之后所有客户端显示空列表，不再自动补回 `/resume` 与 `/new`

#### Scenario: 越界与非法值

- **WHEN** PATCH 同时包含合法主题与 51 条快捷指令，或包含 `fontSize: 30`、重复的快捷指令 ID
- **THEN** 服务端返回 400，修订号与所有设置保持原值，后续读取看不到部分更新

#### Scenario: 请求体上限

- **WHEN** 客户端提交超过 2 MiB 的 PATCH
- **THEN** 服务端拒绝请求且不解析为设置，已有设置不变

### Requirement: 设置读取与部分更新 API

服务端 SHALL 提供已认证的 `GET /api/settings` 与 `PATCH /api/settings`。PATCH 请求体 SHALL 为 `{baseRevision, changes}`：`baseRevision` 为客户端读取到的修订号，`changes` 为非空对象，键为已知设置名；省略的键 SHALL 保持不变，值为 `null` 的键 SHALL 恢复默认。

`baseRevision` 不等于当前修订号时 SHALL 返回 409 且不修改任何内容。请求体出现 `baseRevision`、`changes` 之外的字段时 SHALL 返回 400。成功时 SHALL 返回 200 与完整快照。校验失败 SHALL 返回 400，落盘失败 SHALL 返回 500 并回滚，存储不可用、文件损坏或锁冲突 SHALL 返回 503。认证与 Host / Origin 校验失败 SHALL 分别返回 401 / 403，行为与现有 `/api/*` 一致。

一个 PATCH SHALL 原子地应用全部 `changes`；任一字段非法时 SHALL 拒绝整个请求。错误文本 SHALL 包含字段名、限制或状态文件路径，SHALL NOT 包含任何设置值或凭据。

#### Scenario: 部分更新保留其他键

- **WHEN** 当前设置为 `theme: "dark"`、`fontSize: 15`，客户端 PATCH `{baseRevision: 3, changes: {theme: "light"}}`
- **THEN** 返回 200，`theme` 变为 `"light"`，`fontSize` 仍为 15，`revision` 变为 4

#### Scenario: 修订号冲突

- **WHEN** 两个客户端都以 `baseRevision: 3` 提交修改，第一个成功
- **THEN** 第二个返回 409，服务端保留第一个客户端的值；第二个客户端重新读取后按操作语义重放

#### Scenario: 恢复默认

- **WHEN** 客户端 PATCH `{baseRevision: 4, changes: {preview: null}}`
- **THEN** 返回 200，`preview` 变为 `null`，客户端按默认 `{lines: 10, skip: 1}` 展示

#### Scenario: 未认证请求

- **WHEN** 未携带有效 token 或使用不允许的 Origin 访问设置接口
- **THEN** 分别返回 401 或 403，与现有 `/api/*` 行为一致

### Requirement: 跨客户端实时同步

服务端 SHALL 提供已认证的 `GET /ws/settings`。连接建立后 SHALL 先发送一条完整快照帧，`type` 为 `settings`；每次成功 PATCH 后 SHALL 向所有连接广播 `type` 为 `settings.update` 的帧，包含新的 `revision`、`updatedAt` 与本次变化的顶层键。

快照捕获与实时订阅注册 SHALL 原子完成，并 SHALL 与 PATCH 提交形成同一修订号顺序；并发提交的修改 SHALL 要么包含在首帧快照中，要么作为其后的更新帧送达，SHALL NOT 因连接建立时的订阅空窗丢失。首帧快照 SHALL 先于后续修订号的更新帧发送，且更新帧 SHALL 按修订号顺序发送。

客户端 SHALL 按 `revision` 合并：等于本地值加 1 时应用变化键；大于本地值加 1 或出现未知键时 SHALL 重新读取完整快照；小于等于本地值的重复帧 SHALL 忽略。客户端断线后 SHALL 指数退避重连并重新取得快照；恢复在线或页面重新可见时 SHALL 校验快照。

每个连接 SHALL 只有一个写协程，订阅队列 SHALL 有界。客户端读取过慢时服务端 SHALL 在写期限内关闭该连接，客户端重连后重新取得完整快照；服务端 SHALL NOT 阻塞 PATCH 或其他客户端。设置同步失败 SHALL NOT 影响终端 WebSocket、会话列表或终端输入输出。

#### Scenario: 两台设备实时同步

- **WHEN** 桌面端修改主题，手机端的 `/ws/settings` 已连接
- **THEN** 手机端在收到 `settings.update` 后无需刷新即可应用新主题，修订号与桌面端一致

#### Scenario: 建立连接时恰好发生一次修改

- **WHEN** 客户端建立 `/ws/settings` 连接的同时另一客户端成功 PATCH，之后不再发生任何修改
- **THEN** 新连接的首帧快照已包含该修订号，或紧随快照收到该修订号的更新；客户端无需等待下一次修改即可取得最新值

#### Scenario: 断线重连

- **WHEN** 手机端网络中断期间服务端发生多次设置修改，之后连接恢复
- **THEN** 客户端重新连接并收到完整快照，直接使用最新值，不要求重放中间事件

#### Scenario: 慢客户端不阻塞写入

- **WHEN** 某个设置 WebSocket 长时间不读取更新
- **THEN** 服务端关闭该连接，其他客户端仍能收到后续更新，PATCH 正常返回

### Requirement: 旧浏览器设置迁移

首次认证成功并取得服务端快照后，客户端 SHALL 读取旧键 `webmux.theme`、`webmux.fontSize`、`webmux.preview`、`webmux.codeLineNumbers`、`webmux.codeDiffOverflow`、`webmux.quickCommands`、`webmux.homeWorktrees`，逐键校验，并 SHALL 只把本地有效且服务端仍为 `null` 的键放入一次带 `baseRevision` 的 PATCH。

服务端已存在的非 `null` 值 SHALL NOT 被覆盖。409 时客户端 SHALL 重新读取快照，并按“服务端仍为 `null`”重新过滤后最多自动重试一次；网络错误、503 或校验失败时 SHALL 保留旧键，等待下次启动重试。成功后 SHALL 删除全部已识别的旧键（包括服务端已有值而未迁移的键）并写入本设备标记 `webmux.settingsMigratedAt`；取得快照且没有可迁移键时同样删除旧键并写入标记。迁移完成前 SHALL NOT 删除旧键；带标记的设备 SHALL NOT 重复迁移。旧键缺失或为空字符串时 SHALL NOT 视为设置；`codeReader` 的两个旧键分别存在时 SHALL 用另一项默认值补齐后迁移，两项都不存在时不迁移。

多台设备竞争同一键时 SHALL 由修订号保证只有一个成功，其他设备重新读取后放弃本地值；不同键 SHALL 可以由不同设备分别迁移。非法旧值 SHALL 被忽略，并只在同步成功后删除，SHALL NOT 阻止其他键迁移。服务端 SHALL NOT 预置用户数据，也不记录全局“迁移完成”状态。

#### Scenario: 升级后迁移快捷指令

- **WHEN** 浏览器中保存了 3 条快捷指令、深色主题和 16px 字号，升级后首次连接设置服务
- **THEN** 服务端写入这三项，页面删除旧键并显示迁移后的值；之后换设备可以看到相同内容

#### Scenario: 不覆盖已有服务端值

- **WHEN** 服务端主题已是 `"light"`，另一台设备的旧 `webmux.theme` 为 `"dark"`
- **THEN** 迁移不修改主题，服务端保持 `"light"`，该设备的旧键在同步成功后删除

#### Scenario: 迁移失败保留旧值

- **WHEN** 首次迁移时设置接口返回 503
- **THEN** 旧 `localStorage` 键保持不变，页面使用缓存或默认值，下次启动可以再次尝试

#### Scenario: 坏数据不阻塞其他键

- **WHEN** 旧 `webmux.preview` 无法解析，但 `webmux.fontSize` 合法
- **THEN** 字号仍被迁移，预览保持服务端 `null`，坏键在同步成功后删除

### Requirement: 服务端设置不可用时的降级

浏览器 SHALL 把最近一次成功同步的快照保存在本地只读缓存中，用于首屏主题与字号，以及设置服务不可用时展示最后已知值。缓存 SHALL 包含 `v`、`revision` 与 `values`，损坏、版本不符或字段非法时 SHALL 被忽略并删除。

设置接口返回 503 或网络不可达时，页面 SHALL 使用缓存或默认值只读展示；设置页 SHALL 显示不可用原因与服务端返回的状态文件路径，禁用写入并提供重新读取入口；快捷指令菜单 SHALL 使用缓存列表只读展示，没有缓存时显示预置指令并说明暂不可保存。认证失败时 SHALL 进入现有登录流程，登录页可以使用缓存主题与字号渲染，但 SHALL NOT 展示缓存中的快捷指令。设置不可用 SHALL NOT 阻止会话创建、终端连接、终端输入输出、草稿或主页浏览。

服务恢复后 SHALL 重新读取完整快照并覆盖缓存；SHALL NOT 自动重放离线期间的修改，避免过期意图覆盖其他设备的新值。缓存写入失败（隐私模式或配额不足）SHALL NOT 影响当前页面使用。

#### Scenario: 设置文件损坏

- **WHEN** `settings.json` 损坏，用户打开设置页和终端
- **THEN** 设置页显示不可用原因与文件路径，写入被禁用；终端仍使用缓存中的主题与字号，快捷指令菜单可用但不可保存

#### Scenario: 离线后恢复

- **WHEN** 手机断网期间用户打开设置页，随后网络恢复
- **THEN** 页面先显示缓存值，恢复后自动重新读取服务端快照并更新显示

#### Scenario: 首次使用就没有缓存

- **WHEN** 新浏览器第一次打开页面时设置接口不可用
- **THEN** 页面使用默认主题、设备默认字号与 `/resume`、`/new` 预置指令，不因设置失败而阻塞登录后的终端

### Requirement: 设置页与共享偏好入口

设置页 SHALL 说明服务端同步与设备本地的边界，并 SHALL 提供“外观与阅读”卡片：主题（跟随系统 / 浅色 / 深色）、字体大小 12–20、输出预览行数 1–50 与跳过行数 0–50、代码行号显示、Diff 换行以及强制底部工具栏。快捷指令卡片 SHALL 说明内容保存在服务端并在所有设备间共享，同时保留既有新建、编辑、排序、删除、上限和删除确认行为。

所有入口 SHALL 读写同一份设置快照：终端顶栏的主题切换与字体大小菜单、代码阅读器的行号与 Diff 换行开关、主页工作树的添加与移除、设置页控件。修改 SHALL 先更新本地状态并发送 PATCH；PATCH 失败时 SHALL 回滚显示并给出错误，409 时 SHALL 按操作语义重放一次。其他客户端通过 `/ws/settings` 更新到达时 SHALL 无需刷新即可反映到当前页面。

409 重放 SHALL 只保留用户本次操作的意图。对于 `preview` 与 `codeReader`，客户端 SHALL 记录用户修改的子字段及其目标值，重新读取快照后在最新对象上只覆盖这些子字段，并提交合并后的完整顶层对象；最新对象为 `null` 时 SHALL 先使用该组默认值。未修改的兄弟字段 SHALL 保留最新服务端值；同一子字段冲突时 SHALL 使用本次重试的目标值，一次操作明确修改两个子字段时 SHALL 重放两者。分组“恢复默认”SHALL 被视为显式整组重置，重试时仍提交该顶层键的 `null`，不做子字段合并。标量 SHALL 重试目标值，快捷指令 SHALL 按条目 ID 重放操作，主页工作树 SHALL 按本次添加或移除的 ID 重放；所有入口（包括设置页）SHALL 使用相同规则，最多自动重试一次，再次失败时保留编辑输入并显示错误。

设置页 SHALL 以简短文字显示同步状态（已同步或不可用原因），新增 `#appearance` 深链接；已有 `#quick-commands`、`#launch-configs`、`#drafts` 保持不变。“恢复默认”对单个分组 SHALL 使用 PATCH `null`；快捷指令删除全部仍需确认并保存为 `[]`。

#### Scenario: 一处修改多处生效

- **WHEN** 用户在设置页把字体改为 18px，随后在终端顶栏的主题菜单切换为浅色
- **THEN** 两个修改都写入服务端，设置页与终端立即显示新值，另一台已连接设备也收到更新

#### Scenario: 写入失败回滚

- **WHEN** 用户切换主题时设置服务端返回 503
- **THEN** 界面恢复切换前的主题，显示不可用原因，不把这次修改写入本地缓存为已同步值

#### Scenario: 恢复预览默认值

- **WHEN** 用户把预览改为 20 行、跳过 5 行后点击该分组的“恢复默认”
- **THEN** 发送 `preview: null`，服务端与所有客户端恢复 10 行、跳过 1 行

#### Scenario: 快捷指令编辑冲突

- **WHEN** 用户编辑快捷指令时另一台设备先保存了修改，当前保存返回 409
- **THEN** 客户端读取最新列表，按条目 ID 重放本次新增、修改或删除；目标条目已不存在时保留对话框输入并提示刷新

#### Scenario: 并发修改复合设置的不同子字段

- **WHEN** 两台设备基于相同快照，分别把 `preview.lines` 改为 20 和 `preview.skip` 改为 5，或分别把 `codeReader.lineNumbers` 改为 `"hide"` 和 `codeReader.diffOverflow` 改为 `"wrap"`，后提交者收到 409
- **THEN** 后提交者读取最新快照并只重放自己修改的子字段；最终预览为 `{lines: 20, skip: 5}`，代码阅读器为 `{lineNumbers: "hide", diffOverflow: "wrap"}`，设置页与各快捷入口行为一致

#### Scenario: 明确修改两个子字段或恢复默认

- **WHEN** 用户一次操作明确把预览改为 20 行、跳过 5 行，或选择预览分组“恢复默认”，提交前另一设备已修改预览导致 409
- **THEN** 客户端重读后分别重放本次明确修改的两个目标值，或重放整组 `preview: null`；未被该操作涉及的其他设置保持最新值

### Requirement: 主页工作树选择跨设备同步

主页工作树选择 SHALL 通过服务端设置中的 `homeWorktrees` 保存，表示每个仓库中被用户固定显示的工作树 ID。未出现在映射中的仓库 SHALL 默认显示主工作树；有会话的工作树 SHALL 始终显示，无论是否被固定。

读取时 SHALL 按当前仓库快照过滤不存在的仓库与工作树 ID 用于展示，SHALL NOT 因为读取或页面加载自动 PATCH 清理映射；只有用户显式添加或移除工作树时才写入。409 时 SHALL 重新读取快照并按本次添加或移除的 ID 重放。删除工作树或仓库 SHALL NOT 删除已运行会话。

#### Scenario: 桌面整理后手机可见

- **WHEN** 用户在桌面主页固定两个 linked worktree，随后在手机上打开同一实例
- **THEN** 手机主页显示相同的两个工作树与主工作树，不要求重新整理

#### Scenario: 已删除工作树不显示

- **WHEN** 服务端映射仍包含一个已被删除的工作树 ID
- **THEN** 主页按当前仓库快照忽略该 ID，不显示空分组，也不会仅因为打开页面就修改服务端映射

#### Scenario: 两台设备同时整理

- **WHEN** 两台设备分别固定不同工作树，第二个 PATCH 返回 409
- **THEN** 第二个客户端读取最新映射并只重放自己添加的 ID，最终两份选择都保留

### Requirement: 设置存储的安全与隐私

设置接口与设置 WebSocket SHALL 继承 `/api/*`、`/ws/*` 现有的认证、Host 与 Origin 校验；浏览器 HTTP 请求 SHALL 使用 `Authorization: Bearer <token>`，WebSocket 握手 SHALL 提供 `webmux` 与 `webmux.token.<base64url(token)>` 子协议，服务端 SHALL 只选择 `webmux`，SHALL NOT 回显 token 子协议或把 token 放进 URL。现有中间件对 `webmux_token` Cookie 的兼容回退 SHALL 保持不变，但浏览器 SHALL NOT 依赖 Cookie。本机 Unix 套接字 SHALL 继续按对端有效 UID 校验。服务端 SHALL NOT 新增匿名设置端点、放宽 CORS 或通过静态文件暴露设置文件。

`settings.json` 与 `settings.lock` SHALL 为 0600，状态目录 SHALL 为 0700。服务端 SHALL NOT 在日志、错误或诊断输出中记录快捷指令正文、完整请求体、Token 或 Web Push 订阅；错误可以包含字段名、限制和状态文件路径。设置接口返回的快捷指令正文 SHALL 只发送给已认证客户端，Token、Web Push 订阅与草稿 SHALL NOT 出现在设置文档、快照或更新帧中。

设置文件 SHALL 以明文 JSON 保存，本能力不提供应用层加密；文档 SHALL NOT 将其描述为加密存储。拥有服务账户文件读取权限的用户仍可读取设置内容，与现有启动配置环境变量的威胁模型一致。

#### Scenario: 未认证读取被拒绝

- **WHEN** 未携带有效 token 的客户端请求 `GET /api/settings` 或连接 `/ws/settings`
- **THEN** HTTP 请求返回 401，WebSocket 握手被拒绝；设置快照不泄露给该客户端

#### Scenario: 日志不包含快捷指令正文

- **WHEN** 保存快捷指令失败并写入服务日志
- **THEN** 日志只包含错误类型、字段限制或文件路径，不出现指令正文或请求体

#### Scenario: 设置文件不包含设备凭据

- **WHEN** 检查 `settings.json`
- **THEN** 文件不包含浏览器 token、Web Push 订阅端点和草稿内容，权限为 0600
