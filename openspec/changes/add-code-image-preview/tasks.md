## 1. 服务端识别与契约

- [x] 1.1 新增 `internal/codeview/media.go`：扩展名白名单、光栅签名表（PNG/JPEG/GIF/WebP/AVIF/BMP/ICO）、基于 `encoding/xml` 首元素的 SVG 判定，以及仅按扩展名推导的节点 `mediaType`
- [x] 1.2 `types.go` 增加 `MaxImageBytes`（20 MiB）、`MaxDownloadBytes`（100 MiB）；`Document` 增加 `mediaType`、`size`、`downloadable` 与未导出的工作树 `identity`；`Node` 增加 `mediaType`
- [x] 1.3 `binding.read()` 对图片候选只读 `stat` 与 4 KiB 头部，生成 `image` / 超预览上限 `too-large` / 空文件与无法识别的 `binary` 文档及身份版本；非候选保持原逻辑并补齐 `size`、`downloadable`；SVG 在文本路径上设置 `mediaType`
- [x] 1.4 `internal/workspace` 新增 `StreamGit`，复用 `ReadGitInput` 的只读覆盖、环境、进程组终止与 stderr 上限，stdout 写入调用方 writer、无内部 10 秒超时；为其补充取消、写出错误终止与覆盖参数的测试
- [x] 1.5 `binding.blob()` 对图片候选用 `cat-file -s` + `StreamGit` 头部读取分类，版本取 OID；非候选保持原逻辑并补齐新字段
- [x] 1.6 树节点列举填充 `mediaType`；确认 `Check` 对图片走轻量版本比较且不读取全量内容
- [x] 1.7 表驱动测试：各格式签名、大写扩展名、签名与扩展名不一致、空文件、损坏头部、`.bin` 内的 PNG、ZIP/PDF、合法/非法 SVG、20 MiB 边界、文本 2 MiB/20000 行行为不变、工作树改写后版本变化

## 2. Diff 图片分类

- [x] 2.1 `Diff()` 增加 `image` 分类（两侧图片或图片+empty；SVG 对 SVG 仍为文本；图片与非图片混合为 `metadata` 并给出原因；冲突仍优先 `metadata`）
- [x] 2.2 测试：未暂存/已暂存修改的 PNG、新增与删除图片、PNG→文本、SVG 修改仍为文本 Diff，以及 `Check` 检测工作树新侧图片改写

## 3. 原始字节端点

- [x] 3.1 `codeview.Service.Raw`：服务槽内完成会话/快照/侧校验、empty 与 100 MiB 上限判断、打开来源并比对身份或 OID 大小，返回 `Transfer` 后释放服务槽；容量 4 的非阻塞传输信号量，满时 429
- [x] 3.2 `Transfer.WriteTo`：工作树 `io.CopyN` 后二次 `fstat` 与重新 `open` 校验，索引/HEAD 经 `StreamGit cat-file blob`；发送中变化或字节数不符时中断连接
- [x] 3.3 `internal/api/code.go` 注册 `GET …/code/snapshots/{snapshotId}/raw`（不经 10 秒 JSON 包装）：打开阶段 10 秒 ctx、写期限按块续期 30 秒、HEAD 仅头部；设置 `Content-Type`、`Content-Length`、`no-store`、`inline`/`attachment` 与编码文件名、SVG CSP；首字节前错误按 JSON 返回
- [x] 3.4 `internal/codeview` 测试：工作树/索引/HEAD 字节与 SHA-256 一致、打开后改写 409、发送中改写导致中断、过期 410、跨会话拒绝、Diff 缺失或非法 side 400、empty 侧 404、超过 100 MiB 413、传输槽满 429、取消停止读取且不泄漏 Git 进程
- [x] 3.5 `internal/api/code_test.go`：无/错误 token 401、Origin 拒绝、PNG inline 与 attachment 头、非 ASCII 与控制字符文件名、ZIP 一律 attachment + octet-stream、SVG CSP、`nosniff`/`no-store`、HEAD 无正文、超过 10 秒的慢读取不被 JSON 超时截断

## 4. 前端契约与图标

- [x] 4.1 `web/src/schemas.ts`：`CodeDocumentSchema` 增加 `mediaType`、`size`、`downloadable` 与 `kind: "image"`，`CodeNodeSchema` 增加 `mediaType`，`CodeDiffSchema.kind` 增加 `"image"`；更新受影响的单元、组件与 Storybook 夹具
- [x] 4.2 `web/src/api.ts` 新增基于 `request()` 的 `apiBlob(path, signal)`，保持 Bearer、`APIError` 与 401 事件处理
- [x] 4.3 引入 `unplugin-icons` 与 `@iconify-json/tabler` 开发依赖（`pnpm --dir web add -D`，确认无需新增安装脚本白名单），在 `vite.config.ts` 与类型声明中启用 Svelte 编译器；迁移 `CodeDocumentView.svelte`、`CodePanel.svelte` 中的 `@tabler/icons-svelte` 引用

## 5. 图片预览与下载 UI

- [x] 5.1 新增 `CodeImageView.svelte`：Blob 加载与回收、适应/1:1/放大/缩小、拖动、双指捏合、`ctrl+wheel` 缩放、`ResizeObserver` 适应、格式/像素尺寸/大小元信息、解码失败与无固有尺寸提示；图标按钮具备 `aria-label` 与可见提示
- [x] 5.2 下载按钮：`apiBlob(raw?download=1)` 生成临时 `<a download>`，原文件名保存；忙碌状态、切换或卸载时中止并回收；按 409/410/413/429/401/网络错误给出说明与刷新入口
- [x] 5.3 `CodeDocumentView.svelte`：图片文档渲染预览；SVG 提供按标签保存的“预览 / 源码”切换（默认预览、失败回退源码）；超预览上限与无法识别的图片显示元信息与下载；图片 Diff 显示变更类型与大小变化并用 `side` 切换预览侧；预览时隐藏行引用、保留整文件引用
- [x] 5.4 `CodeNavigator.svelte` 与 `CodePanel.svelte` 标签：按 `mediaType` 显示装饰性图片图标（`aria-hidden="true"`）
- [x] 5.5 浏览器组件测试（`web/test/browser/`）：Blob 加载与回收、缩放与适应、下载文件名与中止、解码失败、超限状态、SVG 预览/源码切换与回退、图片 Diff 侧切换；必要时补 Storybook 故事

## 6. 端到端验证

- [x] 6.1 `web/test/e2e/codeview.spec.ts`：在隔离工作区放入 PNG/JPEG/GIF/WebP/SVG，打开即预览，下载事件内容与源文件哈希一致；含脚本与外部资源的 SVG 不执行脚本且无外部请求；修改并暂存 PNG 后 Diff 两侧预览分别对应索引与工作树；在桌面与 Pixel 7 项目运行
- [x] 6.2 运行 `go test -race ./internal/codeview ./internal/workspace ./internal/api`、目标 Vitest 浏览器测试、目标 Playwright 规格与 `make check`

## 7. 文档与用户故事

- [x] 7.1 `docs/protocol.md`：补充 raw 路由、参数、响应头、上限与错误码，`Document`/`Node`/`Diff` 新字段与 `image` 类型
- [x] 7.2 `docs/usage.md`：代码工作区补充图片预览、下载、20 MiB 预览与 100 MiB 下载上限、SVG 安全、扩展名+签名识别规则与图片 Diff
- [x] 7.3 `docs/user-stories.md`：I 组新增 US-I15（预览与下载图片），更新 US-I11 的受限说明、分组与覆盖索引（US-I01–US-I15）、来源链接与核对记录，并检查 ID 唯一与链接可达
- [x] 7.4 PR 描述附桌面/手机、亮/暗主题的图片预览、SVG 切换、图片 Diff 与下载截图（使用隔离样例数据）
