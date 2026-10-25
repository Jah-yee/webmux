## Why

#69 需要把会话中明确选中的普通文件下载到当前设备。现有只读文件视图可以浏览、阅读和预览，但它只覆盖工作区，且下载入口仍限于图片；用户拿到 agent 生成的 PDF、ZIP、日志或任意二进制产物后，仍要借终端命令转存。

#110（`add-code-image-preview`）已经提供通用快照原始字节通道、100 MiB 下载上限、`Document.size` / `downloadable` 和图片下载界面。本变更不再重复设计传输层，而是补齐 #69 尚未覆盖的部分：临时目录只读根、所有常规文件的显式下载入口，以及下载前确认、进度、取消与失败说明。

## What Changes

- 文件面板由单一工作区根扩展为 `workspace` 与 `temp` 两个只读根，默认工作区；导航顶部提供根切换器，`temp` 指向服务进程临时目录（遵循 `TMPDIR`），并包含 `webmux-upload-*` 上传目录。
- 在共享临时目录上只列出并打开当前服务用户拥有的条目；其他用户的条目不出现在列表中，直接请求也会被拒绝。
- 快照记录所属根，原始字节端点按快照根打开文件；复用 #110 的 `GET /api/sessions/{id}/code/snapshots/{snapshotId}/raw`、100 MiB 上限、身份校验、HEAD、认证与文件名编码，不新增第二套传输接口。
- 完整文件文档（文本、图片、二进制、编码不支持、超过文本阅读上限）统一提供“下载文件”入口；点击后先确认来源绝对路径、大小与不可下载原因，确认后显示进度并可取消。
- 下载失败按内容变化、快照过期、超过上限、认证、传输繁忙与网络错误给出可区分说明；409 / 410 标记文档需要刷新。
- 上传成功后提供“在临时目录中查看”，切到临时根并打开文件所在目录，不自动下载。
- 临时根不提供 Git 差异、忽略项或状态；文本 2 MiB / 20000 行和图片 20 MiB 预览上限保持不变。
- 不新增路径输入框、目录打包、批量下载、Range / 断点续传、公开分享或产物数据库；不自动扫描目录，也不从终端输出识别路径。

## Capabilities

### New Capabilities

- `terminal-file-download`：工作区与临时目录两个只读根、临时目录用户隔离、从文件视图显式下载任意常规文件、下载前确认、进度与取消、失败说明和上传后定位。

### Modified Capabilities

- `terminal-diff-review`：完整文件视图增加下载入口；代码读取边界覆盖临时根与原始字节端点。

## Impact

- 后端：`internal/codeview`（按 `(sessionId, rootId)` 绑定、临时根与所有权过滤、快照记录根、`Context.roots`、根感知的 `Raw` / `Locate`）、`internal/api/code.go`（`/code/context?root=` 与既有 raw 路由）。
- 前端：`web/src/schemas.ts`、`web/src/api.ts`（带进度的 Blob 读取）、`CodePanel.svelte`、`CodeNavigator.svelte`、`CodeDocumentView.svelte`、`CodeDownloadButton.svelte`、`Terminal.svelte`、`App.svelte` 与 `useUpload`。
- 依赖：本变更以 #110 合入后的原始字节端点与 `Document` 契约为基线；实施前先确认其最终路由、字段与上限。
- 文档：`docs/protocol.md`、`docs/usage.md`、`docs/user-stories.md`。
- 不涉及：持久化迁移、会话生命周期、终端 buffer 导出语义、公开分享或产物存储库。
