## Why

代码工作区目前只按“是否含 NUL / 是否合法 UTF-8 / 是否超过 2 MiB”分类文件，agent 生成的截图、图表或素材只显示一行原因，既看不到图像也无法取回到当前设备；手机或远程使用时只能再借终端命令处理（#108）。服务端也没有任何原始字节通道，后续通用下载（#69）同样缺少基础。

## What Changes

- 服务端按“扩展名白名单 + magic bytes”识别 PNG、JPEG、GIF、WebP、AVIF、BMP、ICO，按“扩展名 + UTF-8 + `<svg>` 根元素”识别 SVG；文档和文件树节点携带媒体类型、字节大小和是否可下载，图片拥有独立的 20 MiB 预览上限，文本阅读的 2 MiB / 20000 行上限不变。
- 新增快照原始字节端点 `GET /api/sessions/{id}/code/snapshots/{snapshotId}/raw`：按快照返回任意来源（工作树、索引、HEAD、Diff 旧/新侧）的原始字节，最大 100 MiB，校验内容仍与快照一致；支持 `inline` 预览与 `attachment` 下载，带正确的 `Content-Type`、`nosniff`、`no-store`，SVG 附加禁止脚本与外部资源的 CSP。端点按通用文件设计，本变更的界面只为图片接入，#69 可直接复用。
- 代码面板新增图片预览：适应窗口、1:1、放大/缩小，鼠标与触摸拖动、双指缩放；显示格式、像素尺寸和文件大小；损坏、空文件、格式不支持、超限分别提示；超过预览上限仍显示元信息与下载入口。
- 动作栏为图片提供“下载”，保留原文件名，字节与来源一致；请求沿用 Bearer 认证生成 Blob，URL 中不出现 token。
- SVG 默认以 `<img>` + Blob 预览，可切换到源码阅读并保留按行引用；无法渲染时回退源码。
- Diff 新增 `image` 类型：图片新增、删除或修改时显示变更类型与大小变化，并可在旧侧（索引/HEAD）与新侧（工作树/索引）预览之间切换。
- 代码区新增/调整的图标改用本地打包的 Iconify `tabler` 图标，相关组件中的直接图标库引用一并迁移。

## Capabilities

### New Capabilities

- `code-image-preview`：代码工作区内的图片识别、在线预览、Diff 图片对比、SVG 安全渲染，以及基于快照的原始字节读取与下载。

### Modified Capabilities

- `terminal-diff-review`：“完整文件只读且可辨识”允许图片走预览而非只给原因；“代码读取沿用会话访问边界”覆盖新的原始字节端点，并允许 SVG 在受限图片上下文中渲染。

## Impact

- 后端：`internal/codeview`（识别、文档字段、版本与检测、Diff 分类、原始字节读取与校验）、`internal/workspace`（流式只读 Git blob 输出）、`internal/api/code.go`（不受 10 秒 JSON 包装的原始字节路由）。
- 前端：`web/src/schemas.ts`、`web/src/api.ts`（Blob 请求）、`CodeDocumentView.svelte`、新图片查看组件、`CodeNavigator.svelte`、`CodePanel.svelte`；`web/package.json` 新增 Iconify 构建期依赖。
- 契约：`Document` 新增 `mediaType`、`size`、`downloadable`，`kind` 新增 `image`；`Node` 新增 `mediaType`；`Diff.kind` 新增 `image`。既有字段与语义不变，非破坏性。
- 文档：`docs/protocol.md`、`docs/usage.md`、`docs/user-stories.md`（I 组新增故事）。
- 不涉及：临时上传目录作为第二个只读根、非图片文件的下载入口与确认框（#69），图床、缩略图缓存、EXIF 编辑或同步。
