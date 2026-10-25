## Context

- 当前 `main` 的 `/api/sessions/{id}/code` 只有单一工作区根：`Service.roots` 以会话 ID 为键，`Context` 描述一个 `rootPath`，`binding.open` 逐段 `O_NOFOLLOW` 并拒绝 `.git`、Git 管理目录、嵌套仓库边界、符号链接与特殊文件；`Locate` 以 shell 报告目录或启动目录作为相对路径基点。
- #110（`add-code-image-preview`）是开放 PR，不是已合入代码。它在 `internal/codeview/raw.go` 中新增 `Raw` / `Transfer`：按快照侧定位、100 MiB 上限、工作树身份前后校验、索引 / HEAD 经 `workspace.StreamGit`、4 个传输槽；`internal/api/code.go` 的 `GET .../snapshots/{snapshotId}/raw` 在 10 秒 JSON 包装外流式响应，支持 HEAD、`download=1`、文件名编码与 SVG CSP。`Document` 新增 `size`、`downloadable`、`mediaType`，但下载 UI 只为图片和图片 Diff 提供。
- #110 的 `openRaw` 固定调用 `s.bind(ctx, id)`（工作区根），快照只保存 `context` 字符串；因此临时根文件无法直接复用 raw。
- 上传把文件写入 `os.MkdirTemp("", "webmux-upload-")`，返回绝对路径；现有上传完成只把路径插入终端或草稿，没有文件面板定位入口。
- `CodePanel.svelte` 持有单一 `context`，`CodeNavigator.svelte` 只有“文件 / 修改”两个标签，`CodeDocumentView.svelte` 的动作栏已有“引用整个文件 / 发送位置”，#110 在其中加入图片下载按钮。

## Goals / Non-Goals

**Goals:**

- 在文件面板内提供“工作区 + 临时目录”两个只读根，临时目录覆盖 agent 产物与上传文件。
- 复用 #110 的原始字节端点与快照一致性，不写第二套传输逻辑。
- 让任意已打开的常规文件都能先确认来源与大小，再下载、显示进度、取消并得到可解释的失败结果。
- 保持现有认证、Origin、路径边界、快照和会话生命周期语义。

**Non-Goals:**

- 不支持目录打包、批量下载、Range / 断点续传、公开分享链接或产物数据库。
- 不自动扫描临时目录、不从终端输出自动识别路径、不为文件树每行增加下载图标。
- 不改变文本 2 MiB / 20000 行和图片 20 MiB 预览上限，也不改变终端 buffer 导出。
- 不把临时目录加入 Git 差异、忽略项或仓库注册；不扩大工作区根的定义。
- 不实现后台下载、断点续传或服务端转存到另一个存储。

## Decisions

### 1. 双根模型与上下文契约

- 新增 `rootId` 枚举 `workspace` / `temp`，默认 `workspace`。`workspace` 保持既有绑定：会话启动目录或其 Git worktree 顶层。
- `GET /api/sessions/{id}/code/context?root=workspace|temp` 返回当前根，并新增：
  - `rootId`、`label`；
  - `roots: [{rootId, label, path, rootKind, browseAvailable, diffAvailable, ignoreAvailable, available, reason}]`。
- `Service` 同时维护按 `(sessionId, rootId)` 索引的绑定和按 `contextId` 索引的查找；`Context`、`Tree`、`Files`、`FileSource`、`Locate`、`Changes`、`Diff`、`Check` 继续用 `contextId` 区分根，不在每个请求里重复传 `root`。
- 每个绑定保存根路径与首次 `os.Stat` 的 `os.FileInfo`；读取时仍用 `SameFile` 校验。根被删除、移动或替换返回 409，不自动改绑同名目录。
- 会话结束或 `Service.Close` 时同时清理两个根及其快照。根切换本身不创建新的服务生命周期。

### 2. 临时根的范围与用户隔离

- `temp` 根取 `filepath.EvalSymlinks(os.TempDir())`，因此遵循 `TMPDIR`；整个目录可浏览，包含 `webmux-upload-*` 上传子目录。临时根不可解析或不可访问时，`roots` 中仍保留 `temp` 条目并给出 `available: false` 与原因，工作区根不受影响。
- 若 `temp` 与 `workspace` 解析为同一路径，`roots` 只返回工作区，避免重复标签。
- 临时根能力固定为 `rootKind: "directory"`、`diffAvailable: false`、`ignoreAvailable: false`；不运行 `git rev-parse`，不显示 Git 状态。`Changes` 返回空列表，`Diff` 与 `FileSource(source=index|head)` 拒绝，UI 不调用。
- Linux 共享 `/tmp` 场景下，列目录时隐藏 `stat.Uid != os.Geteuid()` 的条目；打开时逐段再次检查所有权，不能只依赖列表过滤。临时根本身可以属于 root（例如 `/tmp`），不受所有权检查。所有权判断抽成可单测的谓词，避免测试环境必须创建其他用户的文件。
- 临时根继续使用 `binding.open` 的逐段 `O_NOFOLLOW`、`.git` / Git 管理目录、嵌套仓库边界、符号链接与特殊文件拒绝规则；上传目录没有例外。

### 3. 根感知的快照与原始字节

- `snapshot` 记录 `rootId`（或绑定指针），不再只记录 `context` 字符串；`File` 文档仍用 `source: "worktree"` 表示磁盘文件，`temp` 由根上下文区分，不新增 `Document.source` 枚举值。
- `Raw` 先 `get` 快照，再用快照记录的根解析绑定并打开来源；不能像 #110 那样固定 `s.bind(ctx, id)` 到工作区。打开、`Stat`、身份比较在服务槽内完成，返回 `Transfer` 后释放服务槽，流式阶段只占传输槽。
- 工作树来源继续比较 `dev/inode/size/mtime_ns`，发送完成后再 `Stat` 并重新 `open` 校验；发送前变化返回 409，发送中变化中断响应。索引 / HEAD 仍按 OID 读取，不适用于临时根。
- `Check` 按 `contextId` 找到根；临时根的目录与文件检查只比较版本，不扫描 Git 修改。`Locate` 在临时根下把相对路径基点改为临时根本身，绝对路径仍需落在临时根内；工作区根保持 shell 报告目录 / 启动目录语义。
- 原始字节路由沿用 #110 的 `/api/sessions/{id}/code/snapshots/{snapshotId}/raw`，不新增第二个下载路由；`download=1` 对任意文件都使用 `attachment`，图片预览仍可用 `inline`。

### 4. 不新增 probe 接口，复用文档元数据

- #110 的 `Document` 已有 `path`、`absolutePath`、`size`、`downloadable`；确认框直接使用当前快照的这些字段，不再请求 `?probe=1`。
- 不可下载原因在客户端按以下顺序解释：来源 `empty` → 文件不存在；`size > 100 MiB` → 超过下载上限；`downloadable === false` → 不是常规文件或来源不可读；否则可下载。
- 这样避免为确认框创建第二个快照或绕过已打开文档的版本；文件在打开后变化由 raw 的 409 处理。若未来需要更细的服务端原因，再以独立字段扩展，不在 v1 增加探测请求。

### 5. 下载确认、进度与取消

- `CodeDocumentView` 的动作栏为任何 `doc.source !== "empty"` 的完整文件显示“下载文件”；文本、图片、二进制、编码不支持与文本超限文档共用同一入口。`doc.downloadable === false` 时按钮禁用并显示原因。
- 点击后打开确认对话框，显示文件名、`absolutePath`、可读大小与所属根标签；取消不发送请求。确认后对话框保持打开，显示已接收字节 / 总量（可计算时显示百分比）、取消按钮和错误说明。目录或 Diff 侧不新增下载入口；图片 Diff 继续使用 #110 的既有下载。
- 下载走 `request()` 的认证与错误处理，但读取 `response.body` 的流并累计字节，最后构造 `Blob`；这比 `apiBlob` 多了进度，比新增 XHR 约定更贴近现有 `api.ts`。`AbortController` 在取消、关闭对话框、切换文档或卸载时中止；401 继续触发 `webmux:auth`。
- 保存时用 `URL.createObjectURL` + 临时 `<a download>`，从按钮或对话框所属 `ownerDocument` 创建，以兼容 #110 的独立代码窗口；下一帧回收对象 URL。失败按 404 / 409 / 410 / 413 / 429 / 0 与消息显示；409、410 分别把文档标记为 `updated`、`expired` 并提示刷新。
- 100 MiB 文件会在浏览器内存中形成 Blob，低端手机可能吃紧；这是 v1 的已知取舍，不实现服务端流式落盘或 File System Access API。

### 6. 根切换与文件面板状态

- `CodePanel` 增加 `rootId` 状态；`CodeNavigator` 顶部用分段按钮或标签显示 `roots`，不可用的根显示原因且不可切换。标题显示 `标签 · 根路径`。
- 切换根时重新请求 `/code/context?root=...`，并清空当前目录、搜索、修改列表、打开标签、活动标签、选择与待处理控制器，避免旧 `contextId` 的响应写入新根。v1 不要求跨根保留标签。
- 临时根下隐藏“修改”标签与“显示忽略项”开关；`CodeDocumentView` 接收 `diffAvailable`（或等价属性），临时根文件不显示“查看 Diff”。`Locate` 的提示文案在临时根改为相对临时目录。
- 顶栏动作由“代码”改为“文件”（展开时桌面显示“折叠文件”，移动端仍显示“终端”），并同步相关 `aria-label`；这是发现性调整，不改变面板结构。
- `Terminal.svelte` 继续懒加载 `CodePanel`；根切换发生在面板内部，不重建终端或 PTY。

### 7. 上传后定位

- `useUpload` 在批量成功后保留本次上传的绝对路径（至少最后一个），`Terminal` 显示可关闭的成功提示和“在临时目录中查看”动作；现有错误提示与路径插入行为不变。
- 触发定位时打开文件面板（如尚未显示），把目标路径传给 `CodePanel`；面板切到 `temp` 根，先对上传路径与临时根做 `EvalSymlinks` 再转换为相对 `nodeId`，打开文件所在目录并让文件出现在当前列表。若路径不在临时根内，只切换根目录并说明无法定位，不打开工作区或其他路径。
- 定位不自动打开文件内容，也不自动下载；它只复用 `terminal-file-download` 的显式文件选择模型。

### 8. 限制与安全

- 传输上限沿用 #110 的 `MaxDownloadBytes = 100 MiB`；超过时 413 且不发送正文。文本 2 MiB / 20000 行与图片 20 MiB 预览上限保持独立。
- 认证接受 Bearer 头或既有 `webmux_token` Cookie；token 不进入 URL、Referer 或日志，不生成公开分享地址。Origin 与 Host 检查沿用 `/api` 中间件。
- 响应沿用 `Content-Length`、`Content-Disposition: attachment`、`Cache-Control: no-store` 与全局 `X-Content-Type-Options: nosniff`；非图片为 `application/octet-stream`，SVG 下载继续带禁止脚本与外部资源的 CSP。
- 目录列表、搜索与打开继续有界；临时根可能很大，沿用现有 200 条分页与 10000 条目录上限，不新增全盘扫描。
- 权限错误映射为“文件不可读”类 403，不泄漏底层系统错误细节；不记录文件内容。

### 9. 验证与文档

- Go 覆盖双根绑定与清理、`roots` 响应、临时根所有权谓词、临时根替换 409、根感知快照与 raw、临时根的 `source=index|head` 拒绝、`changes` 空能力，以及 401 / 403 / 404 / 409 / 410 / 413 / 429、HEAD 与响应头。
- 浏览器组件覆盖根切换与状态重置、非文本下载入口、确认框元数据、取消不请求、进度、409 / 410 / 413 / 429 / 401 / 网络错误、上传后定位。
- E2E 在隔离 `TMPDIR` 写入二进制文件，从临时根下载并比对 SHA-256；上传后再定位并下载；确认源文件、mtime 与 Git 状态不变，并在桌面与 Pixel 7 项目运行。
- `docs/protocol.md` 增加 `?root=`、`roots`、raw 下载与错误码；`docs/usage.md` 在“文件上传”后增加“文件下载”，说明两个根与三套上限；`docs/user-stories.md` 增加文件下载故事并更新索引。

## Risks / Trade-offs

- [依赖 #110 尚未合入] → 在 tasks 第 1 节先确认 #110 的最终契约；若路由或字段变化，先更新本规格再实现，避免临时根与 raw 形成两套逻辑。
- [临时根包含系统临时文件，列表可能很杂] → 只按服务用户过滤，不做白名单；用户通过根切换显式选择，符合“临时文件夹也应允许”的边界。
- [另一用户在列目录与打开之间替换条目] → 打开时逐段 `O_NOFOLLOW` 并再次检查 UID 与身份，列表过滤只用于展示。
- [100 MiB Blob 占用手机内存] → 只由用户确认后触发；保留 100 MiB 上限并避免自动下载，后续可单独评估流式保存。
- [发送中文件变化导致浏览器看到不完整下载] → 服务器在内容长度不满足时中断响应，界面按下载失败处理并提示重试；不会把截断内容标记为成功。
- [切换根清空标签可能丢失阅读位置] → v1 接受；标签与上下文强绑定，混用 `contextId` 的风险高于保留状态。

## Migration Plan

- 无数据迁移。两个根均在运行时从会话与 `os.TempDir()` 计算，不写入 `state_dir`；服务重启后重新绑定。
- 前后端随同一二进制发布，旧前端不会请求 `?root=`，默认仍得到工作区根。
- 回滚即回退本变更；#110 的 raw 端点与图片下载保持可用。

## Open Questions

- 无阻塞项。临时根是否未来收窄为仅 `webmux-upload-*` 子目录，留待实际使用反馈；本规格按“整个临时目录 + 当前用户过滤”实现。
