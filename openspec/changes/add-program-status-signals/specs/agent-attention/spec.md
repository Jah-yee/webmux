## MODIFIED Requirements

### Requirement: 锁存式关注状态

会话信息 SHALL 包含可空字段 `attention`，非空时为 `{kind, source, since}`：

- `kind` ∈ `question`、`permission`、`done`、`error`、`notify`、`bell`。
- `source` ∈ `agent`（OSC 7501 根记录或 Warp 结构化事件）、`terminal`（普通通知或 BEL）。
- `since` 为 RFC 3339 时间。

信号来源 SHALL 按以下层级排序，层级只在服务端内部使用，SHALL NOT 出现在任何 API 负载中：

| 层级 | 来源                                    |
| ---- | --------------------------------------- |
| L1   | OSC 7501 根记录（见 `program-status`）   |
| L2   | Warp `warp://cli-agent` 结构化事件       |
| L3   | OSC 9 / OSC 777 普通通知                 |
| L4   | 裸 BEL                                  |
| L5   | 启发式（提示符正则、进程树等待、输出窗口）|

信号与关注种类的映射 SHALL 为：

| 信号                                                           | kind         | source     | 层级 |
| -------------------------------------------------------------- | ------------ | ---------- | ---- |
| 根记录 `blocked`，`kind` 为 `question`、`auth` 或缺省           | `question`   | `agent`    | L1   |
| 根记录 `blocked` + `kind=permission`                           | `permission` | `agent`    | L1   |
| 根记录 `done`                                                  | `done`       | `agent`    | L1   |
| 根记录 `error`                                                 | `error`      | `agent`    | L1   |
| 结构化 `question_asked`                                        | `question`   | `agent`    | L2   |
| 结构化 `permission_request`                                    | `permission` | `agent`    | L2   |
| 结构化 `stop`、`idle_prompt`                                   | `done`       | `agent`    | L2   |
| 结构化 `stop_failure`                                          | `error`      | `agent`    | L2   |
| OSC 9 / OSC 777 普通通知                                       | `notify`     | `terminal` | L3   |
| 裸 BEL                                                         | `bell`       | `terminal` | L4   |

种类优先级 SHALL 为 `question` = `permission` > `error` > `done` > `notify` > `bell`。合并规则如下：

1. **根记录存在时，L1 是唯一权威。**
   - 根记录的 `(state, kind)` 每变化为上表中的一项，关注 SHALL 被替换为对应种类，不受种类优先级限制。种类发生变化时，`since` 更新为当前时间；种类相同时，`since` 保持不变。
   - 根记录内容未变化的重复上报 SHALL NOT 改变关注；若此前关注已被清除，也 SHALL NOT 重新置位。
   - L2–L4 信号 SHALL NOT 改变关注。
2. **没有根记录时，按种类优先级合并 L2–L4 信号。** 新信号优先级低于当前关注时，SHALL NOT 改变当前关注；优先级不低于当前关注时 SHALL 替换，种类发生变化时 `since` 更新为当前时间。低层信号 SHALL NOT 替换由更高层置位的关注。
3. **启发式（L5）只参与 `status` 合成**，SHALL NOT 置位关注，也 SHALL NOT 触发推送。

关注置位后，任何后续 PTY 输出（包括 spinner、计时器和状态栏重绘）SHALL NOT 清除或降级关注；只有 L1 根记录的迁移与下文的清除条件可以改变它。

#### Scenario: 动画不掩盖等待回答

- **WHEN** omp 发出 `question_asked` 事件后，每 100 ms 持续重绘 spinner
- **THEN** 会话的 `attention.kind` 保持 `question`，`status` 保持 `waiting`，直到满足清除条件

#### Scenario: 弱信号不降级

- **WHEN** 会话没有根记录，已有由 `question_asked` 置位的 `question` 关注，随后收到一个裸 BEL
- **THEN** 关注仍为 `question`，`since` 不变

#### Scenario: 强信号升级

- **WHEN** 会话没有根记录，已有 `kind = done` 的关注，随后收到 `permission_request`
- **THEN** 关注变为 `permission`，`since` 更新为收到该事件的时间

#### Scenario: 根记录迁移可降级

- **WHEN** 根记录从 `blocked` + `permission` 依次变为 `working`、`done`
- **THEN** 关注依次为 `permission`、无、`done`，最后的 `since` 为收到 `done` 的时间

#### Scenario: 双协议 agent 不重复提醒

- **WHEN** omp 在同一回合结束时既发出 7501 `state=done`，又发出 Warp `stop` 事件，随后又响铃一次
- **THEN** 关注只置位一次，为 `done`，`since` 为 7501 上报的时间；推送只发送一次

#### Scenario: 根记录存在时忽略低层清除

- **WHEN** 根记录为 `done`，关注为 `done`，随后收到 Warp `prompt_submit`
- **THEN** 关注仍为 `done`

#### Scenario: 重复上报不复活已清除的提醒

- **WHEN** 根记录为 `done`，用户输入清除了关注，agent 随后再次上报相同的 `state=done`
- **THEN** 关注保持为空

#### Scenario: 根记录存在时普通通知不置位

- **WHEN** Codex 的根记录为 `idle`，关注为空，随后输出 `OSC 9` 回合完成通知
- **THEN** 关注保持为空

### Requirement: 关注的清除条件

系统 SHALL 在且仅在以下任一情况清除关注：

1. 一次成功写入 PTY 的用户输入中，至少含有一个非“终端应答”的字节。“终端应答”指仅由以下内容组成的输入：
   - OSC 序列、DCS 序列；
   - 终止字节为 `c`、`R`、`n`、`I`、`O`、`t` 或以 `$y` 结尾的 CSI 序列（设备属性、光标位置、状态报告、焦点事件、窗口尺寸报告、DECRQM 模式报告）。
2. 根记录迁移为 `working`、`idle` 或 `clear`。
3. 根记录被 `OSC 133;A` 或 RIS 丢弃，且当前关注由 L1 置位。
4. 会话没有根记录时，收到结构化事件 `prompt_submit`、`permission_replied` 或 `session_start`。
5. 会话退出。

以下情况 SHALL NOT 清除关注：

- 调整终端尺寸、浏览器附着或断开、终端输出。
- 服务端写入的 7501 探测应答。
- 结构化事件 `tool_complete` 和未知事件。
- 根记录存在时收到的结构化清除事件。

#### Scenario: 用户回答后清除

- **WHEN** 会话有 `question` 关注，用户在该会话键入字符并成功写入 PTY
- **THEN** 关注被清除，会话状态按状态合成规则重新确定

#### Scenario: agent 报告恢复工作

- **WHEN** 根记录为 `blocked` + `permission`，用户在 agent 自己的界面里批准后，agent 上报 `state=working`
- **THEN** 关注被清除

#### Scenario: 终端自动应答不清除

- **WHEN** 会话有关注，浏览器终端仅回送设备属性应答 `ESC [ ? 6 2 ; 2 2 c` 或焦点事件 `ESC [ I`
- **THEN** 关注保持不变

#### Scenario: 打开会话页回放的终端应答不清除

- **WHEN** 会话有 `question` 关注，回放缓冲区中含有程序启动时发出的 DECRQM、设备属性和光标位置查询；用户打开或重新打开该会话的终端页
- **THEN** 浏览器终端对回放查询发出的自动应答（如 `ESC [ ? 2026 ; 2 $ y`）写入 PTY 后，关注保持不变

#### Scenario: 断开重连不清除

- **WHEN** 会话有 `permission` 关注，所有浏览器断开后又重新附着
- **THEN** 重新附着收到的会话状态仍包含该关注

#### Scenario: agent 崩溃回到提示符

- **WHEN** 根记录为 `blocked`，关注为 `question`，agent 崩溃后 shell 输出 `ESC ] 133 ; A BEL`
- **THEN** 根记录与关注都被清除

#### Scenario: 退出清除

- **WHEN** 有关注的会话进程退出
- **THEN** `attention` 为 null，`status` 为 `exited`

### Requirement: 关注与活动状态合成

会话 `status` SHALL 按以下顺序确定，命中即止：

1. **已退出**：`exited`。
2. **存在根记录**：由根记录决定，完全不使用启发式。
   - `blocked` → `waiting`。
   - `done`、`error` → 存在关注时为 `waiting`，否则为 `idle`。
   - `idle` → `idle`。
   - `working` → `running`；若超过 `idle_after_seconds` 没有任何 PTY 输出，则为 `idle`。
3. **存在关注**：`waiting`。
4. **会话收到过结构化 agent 信号**：会话进程生命周期内收到过任何 L1 根记录上报或 L2 结构化事件。此时只按输出新旧判定：
   - 最近 2 秒内有输出，为 `running`；
   - 超过 `idle_after_seconds` 无输出，为 `idle`；
   - 否则为 `running`。
   - 提示符正则与进程树等待判定 SHALL NOT 使用，因为这类 agent 自己会报告等待。
5. **其他情况**：沿用既有探测规则（最近输出窗口、提示符模式、进程树睡眠与空闲阈值），既有规则的判定条件 SHALL 保持不变。

普通通知（L3）与 BEL（L4）SHALL NOT 使会话进入第 4 条规则。`status` 的取值集合 SHALL 保持 `running`、`waiting`、`idle`、`exited` 不变。关注、关注种类或由根记录决定的状态发生变化时，SHALL 触发会话列表 `updated` 事件和终端连接上的会话状态帧。

#### Scenario: 有输出时仍为等待

- **WHEN** 会话存在关注且最近 2 秒内有 PTY 输出
- **THEN** `status` 为 `waiting`

#### Scenario: 7501 工作中不受静默影响

- **WHEN** 根记录为 `working`，agent 在等待模型响应期间已有 10 秒没有任何输出，`idle_after_seconds` 为默认值
- **THEN** `status` 为 `running`

#### Scenario: 7501 空闲立即生效

- **WHEN** 根记录变为 `idle`，最近 1 秒内仍有输出
- **THEN** `status` 为 `idle`

#### Scenario: 已查看的完成

- **WHEN** 根记录为 `done`，用户输入清除了关注
- **THEN** `status` 为 `idle`

#### Scenario: 结构化 agent 不触发启发式等待

- **WHEN** 只发出 Warp 事件的 agent 会话在 `prompt_submit` 后静默 3 秒，其 TUI 进程正在读终端，末行恰好匹配提示符正则
- **THEN** `status` 为 `running`，不显示“可能在等你”

#### Scenario: 无信号程序沿用旧规则

- **WHEN** 普通 shell 会话从未发出任何结构化信号
- **THEN** 其 `status` 与本变更前的探测结果一致，`attention` 为 null

#### Scenario: 普通通知不关闭启发式

- **WHEN** 只发出 OSC 9 与 BEL 的程序会话中没有关注，末行匹配提示符正则且静默超过 2 秒
- **THEN** `status` 为 `waiting`
