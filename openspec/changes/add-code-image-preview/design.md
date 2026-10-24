## Context

- `internal/codeview/service.go` 的 `document()` 只按“超过 2 MiB / 含 NUL / 非 UTF-8 / 超过 20000 行”分类；`binding.read()` 每次读取至多 `MaxBytes+1` 字节并以内容摘要作为 `contentVersion`。`binding.blob()` 先 `cat-file -s` 再整体读取，`git()` 通过 `workspace.ReadGit` 固定 `MaxBytes` 上限和 10 秒内部超时。
- `/code/file`、`/code/diff`、`/code/snapshots/{sid}/file` 都经 `codeRoutes` 的 `handle` 包装：10 秒超时、JSON 响应。服务器未设置 `WriteTimeout`，`baseHeaders` 已统一设置 `nosniff` 与 `Referrer-Policy: no-referrer`；认证接受 Bearer 头或 `webmux_token` Cookie。
- 快照只缓存 `Document`（含文本），总量受 `CacheBytes = 64 MiB` 与 10 分钟生存期限制；`Check` 每 5 秒对打开的工作树文档重新 `sourceFile` 并比较 `contentVersion`。
- `Diff()` 只要任一侧不是 `text` 就降级为 `metadata`。
- 前端 `CodeDocumentView.svelte` 对非 `text` 只渲染 `reason`；`api.ts` 的 `request()` 已统一 Bearer、`APIError` 与 401 事件；代码区图标直接使用 `@tabler/icons-svelte`，仓库约定新/改图标改用本地打包的 Iconify。
- #69（通用下载与临时目录根）尚无实现；本变更提供的原始字节端点需要能被它直接复用。

## Goals / Non-Goals

**Goals:**

- 图片识别、元信息和 Diff 分类在服务端完成，前端只按契约渲染。
- 快照不缓存图片字节；原始字节按需从来源重读并校验仍与快照一致。
- 一条原始字节通道服务所有来源与类型，供预览、下载与后续 #69 复用。

**Non-Goals:**

- 不做服务端图片解码、像素尺寸提取、缩略图或转码；尺寸由浏览器解码后得到。
- 不支持 Range / 断点续传、目录打包、批量下载。
- 不新增临时目录根、非图片文件的下载入口或下载前确认框（留给 #69）。
- 不改变文本分类、文本上限与文本版本语义。

## Decisions

### 1. 识别：静态签名表 + 扩展名白名单（`internal/codeview/media.go`）

- 扩展名白名单决定“候选”；候选文件只读取前 4 KiB 头部与 `stat`/`cat-file -s` 大小，按静态表匹配 PNG（`\x89PNG\r\n\x1a\n`）、JPEG（`FF D8 FF`）、GIF（`GIF87a`/`GIF89a`）、WebP（`RIFF....WEBP`）、AVIF（`ftyp` 盒的主品牌或兼容品牌含 `avif`/`avis`）、BMP（`BM`）、ICO（`00 00 01 00`）。签名优先于扩展名决定 `mediaType`，但扩展名必须在白名单中。
- 不使用 `http.DetectContentType`：它不识别 AVIF/ICO，并会把任意内容嗅探成某种类型，违背“非图片不改判”。
- SVG：扩展名 `.svg` 时走原有文本路径（≤2 MiB、UTF-8、≤20000 行），再用 `encoding/xml` 解码到第一个 `StartElement`，本地名为 `svg` 才设置 `mediaType: image/svg+xml`。`Kind` 保持 `text`，源码阅读与行引用不受影响。
- 候选但签名不匹配：空文件 `kind: "binary"`、`reason: "空文件"`；其他 `kind: "binary"`、`reason: "图片格式无法识别"`。不再继续当作文本，因为扩展名已表明用户期望图片。
- 备选方案“只按 magic 不看扩展名”被否决：会把嵌入图片头的任意二进制、`.bin` 产物改判为图片，issue 明确要求不误判。

### 2. 契约字段

- `Document` 新增 `mediaType`（`""` 表示未识别）、`size`（来源字节数，`empty` 来源为 0）、`downloadable`（来源存在、为常规文件/blob 且 `size ≤ MaxDownloadBytes`）。`kind` 新增 `image`（光栅图片且 `size ≤ MaxImageBytes`）；超过预览上限的光栅图片为 `too-large`，`reason: "超过 20 MiB 预览上限"`，保留 `mediaType`。
- 新常量 `MaxImageBytes = 20 MiB`、`MaxDownloadBytes = 100 MiB`（与上传上限对称）；`MaxBytes`/`MaxLines` 不变。
- `Node` 新增 `mediaType`，仅按扩展名推导（含 `.svg`），只用于图标，不读文件。
- `Diff.kind` 新增 `image`。
- 前端 Zod schema 字段设为必填并同步更新测试夹具，避免默认值掩盖后端缺字段。

### 3. 版本与检测不读全量图片

- 工作树图片文档的 `contentVersion = digest(dev, inode, size, mtime_ns, digest(header))`；索引/HEAD 图片为 `digest(oid)`。`Check` 继续调用 `sourceFile` 比较版本，图片只做 `open + stat + 4 KiB` 读取，5 秒周期检测不会反复读取 20 MiB。
- 快照为工作树文档另存未导出的 `identity{dev, inode, size, mtime}`，供原始字节读取校验；不序列化到 JSON。
- 文本、非图片二进制与现有 `too-large` 的版本计算保持原样。

### 4. 原始字节读取：`Service.Raw` + 流式写出

- `codeview.Service.Raw(ctx, id, sid, side) (*Transfer, error)`：在服务槽内完成 `bind`、`get`（会话归属、过期）、选择 `file.Document` 或 `diff.Old/New`（Diff 必须 `side=old|new`，`file` 快照忽略 `side` 以便 #69 复用）、拒绝 `empty` 来源（404）与 `size > MaxDownloadBytes`（413），然后打开来源并返回 `Transfer{Name, MediaType, Size, WriteTo}`。槽在返回前释放，慢速传输不占用 2 个代码服务槽。
- 并发传输另设容量 4 的非阻塞信号量；满时返回 429，不排队。
- 工作树：`binding.open`（逐段 `O_NOFOLLOW`、拒绝 `.git`/边界/特殊文件）→ `fstat` 与快照 `identity` 比较，不同返回 409；`WriteTo` 用 `io.CopyN(size)` 发送，完成后再次 `fstat` 并重新 `open` 比较 `os.SameFile`/size/mtime。发送中检测到变化或读到的字节数不符时，通过 `panic(http.ErrAbortHandler)` 中断连接，使 `Content-Length` 不满足、客户端得到失败而非截断成功。
- 索引/HEAD：新增 `workspace.StreamGit(ctx, cwd, w, args...)`，复用 `ReadGitInput` 的只读覆盖参数、环境清理、进程组终止与 stderr 上限，但把 stdout 直接写入 `w`、不设内部 10 秒超时（由调用方 ctx 与写期限约束）；`w` 返回错误时终止进程。`cat-file blob <oid>` 读取前用 `cat-file -s` 确认大小与快照一致。图片候选的 4 KiB 头部读取也复用它（限长 writer 返回哨兵错误并视为成功）。
- 备选方案“快照缓存图片字节”被否决：两张 20 MiB 图片即可占满 64 MiB 缓存的大半，且下载 100 MiB 仍需重读路径，形成两套逻辑。

### 5. HTTP 路由（`internal/api/code.go`）

- `GET /api/sessions/{id}/code/snapshots/{snapshotId}/raw?side=old|new&download=1` 直接 `mux.HandleFunc` 注册，不进 10 秒 `handle` 包装；Go 的 `GET` 模式同时匹配 `HEAD`，HEAD 只写头部。
- 打开阶段使用 10 秒 ctx；流式阶段使用 `r.Context()`，并通过 `http.ResponseController` 每写出一块后把写期限延长 30 秒，防止停滞客户端无限占用传输槽。
- 头部：`Content-Type` = 图片 `mediaType`，否则 `application/octet-stream`；`Content-Length`；`Cache-Control: no-store`；`Content-Disposition` 仅在 `download!=1` 且为图片时用 `inline`，其余 `attachment`，文件名为来源 basename，经 `mime.FormatMediaType` 编码（非 ASCII 使用 RFC 2231），含控制字符或编码失败时回退 `download`。SVG 额外 `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; sandbox`。
- 错误沿用 `codeview.Status`：400 非法 side 或快照不属于此会话（沿用 `get()` 现有语义）、404 该侧不存在、409 已变化、410 快照过期、413 过大、429 传输繁忙、504 取消/超时。错误在写出首字节前以 JSON 返回。

### 6. Diff 分类

- `capture` 后：两侧都是存在的文本（SVG 对 SVG、SVG 对普通文本）或都不是图片的文本 → 走原有文本 Diff；否则若每一侧要么 `mediaType` 为图片（含超预览上限的图片与 SVG），要么来源为 `empty`，且至少一侧为图片 → `kind: "image"`（因此新增或删除的 SVG 显示为图片差异），`reason` 为空；其他非文本组合 → `metadata`，图片与非图片混合时 `reason: "图片与非图片内容不能对比"`。
- 冲突（`U*`）仍优先为 `metadata`。新增/删除/修改由前端根据 `change.status` 与两侧来源呈现。

### 7. 前端

- `api.ts` 新增 `apiBlob(path, signal): Promise<Blob>`，复用 `request()` 的 Bearer 与 `APIError`；不新增独立 fetch 约定。
- 新组件 `CodeImageView.svelte`：输入 `sessionId`、`snapshotId`、`side`、`document`；可预览时请求 `raw` → `URL.createObjectURL` → `<img>`，在快照/侧切换和卸载时 `abort` 并 `revokeObjectURL`。缩放状态以 `$state` 保存 `scale/x/y`，用 CSS `transform` 渲染；适应比例 `min(1, cw/nw, ch/nh)`，缩放步进 ×1.25，范围 0.05–16；Pointer Events 处理单指/鼠标拖动与双指捏合，`ctrl+wheel`（含触控板捏合）缩放；容器 `touch-action: none`，并阻止事件冒泡到终端手势处理。`ResizeObserver` 在适应模式下重新计算。`onload` 读取 `naturalWidth/Height`（SVG 为 0 时显示“无固有尺寸”），`onerror` 显示解码失败；对 SVG 由父组件回退源码。
- 下载：`apiBlob(raw?download=1)` → 临时 `<a download={basename}>` 点击 → 下一帧 `revokeObjectURL`。进行中禁用按钮并显示“下载中…”，卸载/切换时中止。
- `CodeDocumentView.svelte`：`doc.kind === "image"` 渲染 `CodeImageView`；`kind === "text"` 且 `mediaType === "image/svg+xml"` 时显示“预览 / 源码”切换（每个标签独立保存，默认预览）；`too-large`/`binary` 且 `mediaType` 为图片时显示元信息与下载；`shownDiff.kind === "image"` 显示变更类型、`旧大小 → 新大小`，并用现有 `side` 状态切换预览侧。预览状态隐藏行引用，保留“引用整个文件”。
- `CodeNavigator.svelte` 与标签：`mediaType` 以 `image/` 开头的节点显示装饰性图片图标（`aria-hidden="true"`）。

### 8. 图标改用 Iconify

- 采用 `unplugin-icons` + `@iconify-json/tabler`（devDependencies），以 Svelte 编译器在构建期把 `~icons/tabler/<name>` 生成为组件，只打包实际导入的图标，运行时不访问 Iconify API。类型通过 `unplugin-icons/types/svelte` 声明。
- 本变更触及的 `CodeDocumentView.svelte`、`CodePanel.svelte`、`CodeNavigator.svelte` 和新组件中的图标全部迁移；其他组件不在本变更范围。
- 备选 `@iconify/svelte` + `addCollection` 需要在运行时代码中引入集合 JSON，难以只打包所需图标，故不采用。

## Risks / Trade-offs

- [工作树同大小、同修改时间的原地改写无法被身份校验发现] → 与现有超限文件的版本语义一致；Linux/macOS 修改时间为纳秒精度，实际窗口极小。
- [100 MiB 文件在浏览器内存中成为 Blob，低端手机可能吃紧] → 仅在用户点击下载时发生；预览上限 20 MiB；后续可由 #69 改为流式保存。
- [AVIF/BMP/ICO 依赖浏览器解码能力] → 解码失败提示“浏览器不支持或已损坏”并保留下载。
- [发送中中断依赖 `http.ErrAbortHandler`，部分代理可能缓冲并补全] → 部署建议为回环或可信 VPN；`Content-Length` 不满足时标准客户端判为失败。
- [新增构建期依赖受 pnpm 安装脚本白名单约束] → 实施时确认两包无需安装脚本，保持 `esbuild`-only 白名单。
- [仅凭扩展名的文件树图标可能与内容不符] → 图标只作提示，打开时以服务端识别为准。

## Migration Plan

契约为新增字段与枚举值，前后端随同一二进制发布，无数据迁移。回滚即回退本变更；旧前端不会请求新端点。
