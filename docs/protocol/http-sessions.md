# HTTP API：会话、启动配置与其他

## 会话

| 方法   | 路径                          | 说明                                             |
| ------ | ----------------------------- | ------------------------------------------------ |
| GET    | `/api/sessions`               | 会话列表 `SessionInfo[]`，按 `lastActiveAt` 倒序 |
| POST   | `/api/sessions`               | 创建会话，201 返回 `SessionInfo`                 |
| GET    | `/api/sessions/{id}`          | 单个会话；不存在 404                             |
| DELETE | `/api/sessions/{id}`          | 关闭会话（SIGHUP → SIGKILL），204                |
| DELETE | `/api/sessions/{id}?remove=1` | 移除已退出会话，204；仍在运行 409                |
| GET    | `/api/sessions/{id}/buffer`   | 下载完整原始缓冲（`application/octet-stream`）   |
| POST   | `/api/sessions/{id}/upload`   | 上传文件，见下文                                 |

创建请求有两种互斥形式：

```jsonc
// 显式自定义命令
{ "cwd": "/home/u/proj", "cmd": ["bash", "-lc", "make dev"], "name": "可选" }
// 已保存的启动配置
{ "cwd": "/home/u/proj", "configId": "k3m9q2x7", "name": "可选" }
```

自定义命令必须包含非空可执行程序，`cmd` 是完整 argv，不经 shell 展开；不再接收 `preset` 或单独的预设 `args`。可附加 `workspace: {repositoryId, worktreeId}`：省略 `cwd` 时使用工作树根目录，提供时必须位于该工作树内。

显式命令可附加可选的 `env: {"NAME": "value"}`：只为该会话在服务环境之上追加或覆盖显式列出的变量（名称须匹配 `[A-Za-z_][A-Za-z0-9_]*`，值不含 NUL，`WEBMUX_SESSION_ID` 保留），不复制调用方的环境，也不出现在 `SessionInfo` 中。

`configId` 形式按已保存配置的完整 argv 与环境变量启动。此时 `cmd` 须为空数组或省略，`env` 须省略或为 `{}`，否则 400；空字符串 `configId` 400，未知 ID 404，启动配置存储不可用 503。这些检查都在目录与 Git 操作之前完成。之后修改或删除该配置不影响已启动会话的命令与环境；会话信息保留 `configId`，首页按该配置当前保存的预览设置渲染，配置删除后回退浏览器全局设置。

**上传**：`multipart/form-data`，恰好一个文件字段 `file`。成功返回 201 `{"path": "<服务器绝对路径>"}`。单文件上限 100 MiB（超限 413），读期限 5 分钟（超时 408）；无效请求 400，会话不存在 404，会话已退出 409。

```ts
interface SessionInfo {
  id: string;
  generation: string; // 本次运行的代际，16 位十六进制；输出 offset 与提交都绑定它
  title: string;
  cwd: string;
  cmd: string[];
  status: "running" | "waiting" | "idle" | "exited"; // waiting 表示提示符、终端读取或 attention；计时休眠不是 waiting
  exitCode: number | null;
  preview: string; // 屏幕底部最多 100 条非空行，以 \n 分隔
  createdAt: string; // RFC 3339
  lastActiveAt: string;
  cols: number;
  rows: number;
  clients: number; // 当前附着的连接数
  workspace: {
    repositoryId: string;
    worktreeId: string;
    worktreePath: string;
  } | null;
  // 程序主动报告的“需要介入”，存在时 status 为 "waiting"；不含任何终端正文。
  attention: {
    kind: "question" | "permission" | "done" | "error" | "notify" | "bell";
    source: "agent" | "terminal"; // 结构化事件 / 普通通知或响铃
    since: string; // RFC 3339，种类变化时更新
  } | null;
  // 程序通过 OSC 7 报告的目录；null 表示未报告或进程已退出。
  // 只作导航线索，不改变 cwd、workspace、读取边界或命令执行环境。
  reportedCwd: {
    path: string; // 已解码的绝对路径
    host: string; // 报告中的主机名，可能为空
    remote: boolean; // 非本机（例如 SSH 远端）；不得用于本机文件定位
    since: string; // RFC 3339，路径或主机变化时更新
  } | null;
  // 创建时使用的已保存启动配置；临时命令省略该字段。
  configId?: string;
}
```

## 其他

| 方法 | 路径                   | 说明                                                                                                                |
| ---- | ---------------------- | ------------------------------------------------------------------------------------------------------------------- |
| GET  | `/api/health`          | `{ok: true, sessions: n}`                                                                                           |
| GET  | `/api/config`          | `{defaultCwd, hostname, workspace: {gitAvailable, registryAvailable, gitError, registryError}, tunnel}`，不含 token |
| GET  | `/api/fs/dirs?prefix=` | 匹配前缀的全部子目录（含符号链接目录），普通目录优先，按名称排序                                                    |
| POST | `/api/fs/mkdir`        | `{path}`，204                                                                                                       |

`tunnel` 是只读的运行时隧道状态：`{enabled, state, named, hostname, publicUrl?}`。`state` 为 `off`、`starting`、`ready` 或 `failed`；此处的 `enabled` 仅在 `starting`/`ready` 时为 `true`，与[公网隧道设置](http-tunnel.md)的启用期望不同。`named` 与 `hostname` 动态读取当前有效设置（Web 覆盖优先，否则使用配置文件），关闭时也保留模式信息；Named Tunnel 缺少 `hostname` 时为空字符串。`publicUrl` 仅在 `ready` 时出现且不含 token。响应不包含 Cloudflare 凭据。

## 启动配置

启动配置保存在服务端 `<state_dir>/launch-configs.json`，在所有已认证设备间共享。只提供用户自定义的启动配置，不再提供内置预设或 `/api/presets` 路由；新安装首次启动时服务端在该文件写入常见 agent 的普通配置，经同一接口列出、修改和删除。

| 方法   | 路径                       | 说明                                                                                              |
| ------ | -------------------------- | ------------------------------------------------------------------------------------------------- |
| GET    | `/api/launch-configs`      | 摘要数组（按创建顺序）：`{id, name, icon?, cmd, envNames, preview, createdAt, updatedAt}`         |
| GET    | `/api/launch-configs/{id}` | 完整配置：`{id, name, icon?, cmd, env, preview, createdAt, updatedAt}`                            |
| POST   | `/api/launch-configs`      | `{name, icon?, cmd, env?, preview?}`，201 返回完整配置                                            |
| PUT    | `/api/launch-configs/{id}` | `{name, icon?, cmd, env?, preview?}` 整体替换，省略的 `env` 与 `preview` 被清空，200 返回完整配置 |
| DELETE | `/api/launch-configs/{id}` | 204                                                                                               |

`icon` 为可选的本地图标 ID：`terminal`、`claudecode`、`codex`、`geminicli`、`opencode` 或 `pi`。省略或空字符串表示自动匹配；更新时同样恢复自动匹配。不接受 URL 或 SVG 内容。旧文件省略此字段仍可读取，浏览器按可识别的直接命令或标准 `.bashrc` 包装匹配图标，未匹配时显示通用终端；`terminal` 可显式覆盖自动匹配。

`preview` 为 `null` 或省略时沿用服务端同步的全局预览设置；否则为 `{lines, skip}`，`lines` 为 1–50 的整数、`skip` 为 0–50 的整数。会话卡片按创建时记录的 `configId` 读取该配置的当前预览值，配置被删除或不可用时回退全局设置。

列表只含环境变量名（字典序），值只由单条读取与创建/更新响应返回。限制：名称去首尾空白后 1–64 个字符且唯一（重名 409）；`cmd` 1–128 项、首项非空、单项不超过 8 KiB；`env` 最多 64 项，名称规则与会话 `env` 相同，值不超过 8 KiB；最多 100 条。校验失败 400，未知 ID 404，落盘失败 500（内存回滚）。文件损坏、目录不可读写或状态目录已被另一实例占用时返回 503，错误信息包含原因与文件路径。

`launch-configs.json` 当前版本为 `2`，新增 `preview` 字段。本次升级仅接受版本 `2`；版本 `1` 文件保留原样，启动配置 API 返回 503。升级前备份旧配置，通过支持 v1 的旧版本保存为 v2，或备份后移走旧文件并在新版重新创建；详见 [部署升级说明](../deployment.md)。
