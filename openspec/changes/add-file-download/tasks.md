## 1. 依赖与契约对齐

- [ ] 1.1 确认 #110（`add-code-image-preview`）已合入；核对 `GET /code/snapshots/{snapshotId}/raw`、`MaxDownloadBytes`、`Document.size/downloadable` 与现有下载组件
- [ ] 1.2 若 #110 最终路由、字段或上限与本规格不同，先更新本变更的 Context 与 Decisions，再开始实现
- [ ] 1.3 确认临时根实现不复制 raw 传输、身份校验、HEAD 或文件名编码逻辑

## 2. 服务端双根

- [ ] 2.1 `internal/codeview/types.go`：`Context` 增加 `rootId`、`label` 与 `roots`；定义根描述结构，保留现有字段供单根客户端使用
- [ ] 2.2 `Service` 改为按 `(sessionId, rootId)` 保存绑定、按 `contextId` 查找；会话结束与 `Close` 同时清理两个根与相关快照
- [ ] 2.3 实现 `temp` 根：`EvalSymlinks(os.TempDir())`、根身份记录、`available` / `reason`、与工作区同路径去重、`diffAvailable=false` 与 `ignoreAvailable=false`
- [ ] 2.4 实现临时根所有权谓词：列表隐藏非当前服务用户条目，`binding.open` 每一段再次检查；根自身不检查；上传目录正常可见
- [ ] 2.5 临时根继续拒绝符号链接、`.git`、Git 管理目录、嵌套仓库边界与特殊文件；`FileSource(source=index|head)` 与 `Diff` 拒绝，`Changes` 返回空能力
- [ ] 2.6 `Locate` 按根调整相对基点：工作区保持 shell 报告目录 / 启动目录，临时根使用临时根路径；绝对路径仍需位于根内
- [ ] 2.7 Go 测试：双根绑定与清理、`?root=` 上下文、roots 去重与不可用原因、同 UID 过滤谓词、临时根替换 409、跨根快照拒绝

## 3. 根感知快照与原始字节

- [ ] 3.1 `snapshot` 记录所属根；`Raw` 先取快照再解析对应绑定，不固定使用工作区根
- [ ] 3.2 保持 #110 的服务槽释放时机、4 个传输槽、写期限续期、工作树发送前后身份校验与发送中中断
- [ ] 3.3 `/code/context?root=`、临时根错误映射与 `Content-Disposition` / HEAD / 413 / 429 行为接入现有 raw 路由
- [ ] 3.4 Go 测试：工作区与临时根字节一致、打开后改写 409、发送中改写中断、过期 410、跨会话 / 跨根拒绝、100 MiB 边界、HEAD 无正文

## 4. 前端契约与下载

- [ ] 4.1 `web/src/schemas.ts`：`CodeContextSchema` 增加 `rootId`、`label`、`roots`；同步更新 `web/test/browser` 与 `web/test` 中的上下文夹具
- [ ] 4.2 `web/src/api.ts`：在 `request()` 基础上增加带进度的 Blob 读取（累计 `response.body` 字节，可计算时报告百分比），保留 Bearer、`APIError`、401 事件与 `AbortSignal`
- [ ] 4.3 新增下载确认对话框：文件名、`absolutePath`、大小、所属根、不可下载原因；取消不发请求；确认后显示进度、取消与错误，支持独立代码窗口的 `ownerDocument`
- [ ] 4.4 `CodeDocumentView.svelte`：为所有 `source !== "empty"` 的完整文件提供统一下载入口；非文本状态同样可用；`downloadable=false` 时禁用并说明原因
- [ ] 4.5 下载失败映射：409 → `updated`、410 → `expired`、413 / 429 / 401 / 网络错误给出说明与刷新入口；保存后回收对象 URL
- [ ] 4.6 浏览器测试：确认框元数据、取消不请求、进度、失败状态、非文本与文本下载入口、对象 URL 回收

## 5. 根切换、文件面板与上传定位

- [ ] 5.1 `CodeNavigator.svelte`：根据 `context.roots` 渲染 `工作区 | 临时目录` 切换器；不可用根显示原因；临时根隐藏“修改”与“显示忽略项”
- [ ] 5.2 `CodePanel.svelte`：保存 `rootId`，切换时重新加载上下文并清空目录、搜索、修改、标签、选择与待处理请求；标题显示根标签与路径
- [ ] 5.3 `CodeDocumentView.svelte`：接收 `diffAvailable` 或等价属性，临时根文件不显示“查看 Diff”；引用路径与绝对路径仍按现有规则输出
- [ ] 5.4 `Terminal.svelte`：顶栏“代码”改为“文件”，桌面展开态显示“折叠文件”，移动端保持“终端”；同步 `aria-label`
- [ ] 5.5 `useUpload` / `Terminal.svelte`：批量上传成功后显示“在临时目录中查看”；触发时打开文件面板、切到临时根，对上传路径与临时根做 `EvalSymlinks` 后定位到所在目录，不自动打开或下载
- [ ] 5.6 浏览器测试：根切换与状态重置、临时根能力隐藏、上传提示与定位、路径不在临时根时的说明

## 6. 端到端与回归

- [ ] 6.1 `web/test/e2e/codeview.spec.ts`：在隔离 `TMPDIR` 写入随机二进制文件，从临时根打开、确认并下载，比对 SHA-256
- [ ] 6.2 覆盖工作区文本 / ZIP / 超过文本上限文件下载，以及上传后定位到临时目录再下载
- [ ] 6.3 确认下载不修改源文件、mtime 与 Git 状态；文件变化、快照过期、超过 100 MiB 与取消均有可解释结果
- [ ] 6.4 在桌面与 Pixel 7 项目运行目标 E2E；运行 `go test -race ./internal/codeview ./internal/api`、目标 Vitest 与 `make check`

## 7. 文档、用户故事与验收

- [ ] 7.1 `docs/protocol.md`：补充 `/code/context?root=`、`roots`、raw 下载参数与响应头、100 MiB 上限及错误码
- [ ] 7.2 `docs/usage.md`：在“文件上传”后增加“文件下载”，说明工作区 / 临时目录两个根、确认与错误、三套限制
- [ ] 7.3 `docs/user-stories.md`：I 组新增文件下载与临时目录故事，更新故事索引、来源链接与核对记录
- [ ] 7.4 `openspec validate add-file-download --strict --no-interactive` 通过
- [ ] 7.5 UI 变更的 PR 描述附桌面 / 手机与亮 / 暗主题截图，使用隔离样例数据
