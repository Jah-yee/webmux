## 1. Repository Registry

- [x] 1.1 在 `internal/config` 和 `cmd/webmux/main.go` 增加可选 `state_dir` 与显式 workspace 服务构造/注入；验证无 Git 或状态存储不可用时真实服务仍能启动并创建直接 Shell 会话，配置响应不包含 token。
- [x] 1.2 在 `internal/workspace` 实现版本化仓库清单、稳定 ID、原子保存、目录/文件权限和单实例文件锁；用临时状态目录验证添加/移除跨重启保留、写失败不发布成功、损坏清单不被覆盖和第二实例不覆盖第一实例。
- [x] 1.3 实现仓库定位、common directory 去重和显式移除注册；用真实临时仓库验证主工作区、linked 子目录及符号链接添加返回同一条目，普通目录不被 init，移除注册不改变目录/分支或结束会话。

## 2. Git Worktree Lifecycle

- [x] 2.1 实现有期限且可取消的 Git argv 调用和 NUL 机器输出解析；真实仓库验证空格/换行路径、非法选项型输入、Git 缺失/权限/信任错误，确保不执行拼接命令或自动修改 safe.directory。
- [x] 2.2 实现 worktree/分支快照及修改、未跟踪、忽略、锁定、缺失状态；验证外部创建/切换/修改后刷新得到真实结果，并覆盖 main、linked、detached、bare、unborn 与未知状态不得显示干净。
- [x] 2.3 实现新分支和已有本地分支两种创建合同、显式基线及仓库旁默认路径；用真实 Git 验证创建结果、分支占用、目标已存在/嵌套冲突与不覆盖、不 fetch、不复制环境文件的行为。
- [x] 2.4 实现 Git 操作失败/取消后的实际状态刷新；通过受控失败场景验证残留分支或已创建工作树可见，不用递归删除/删除分支隐式回滚，且可重新选择已有工作树启动。

## 3. Workspace Sessions and Removal Coordination

- [x] 3.1 扩展 `internal/session` 会话信息和变更比较逻辑，增加可空 workspace 关联且保留真实 cwd；验证 linked 子目录启动位置正确、同工作树多个 PTY 独立，以及注册添加/移除重新分组不改变会话 ID、进程或缓冲。
- [x] 3.2 将 API 中全部直接/关联启动接入同一目录协调入口，实施目标验证、在途启动登记和删除预留；并发行为测试验证直接子目录与符号链接启动不能绕过删除，明确工作树失效时不会回退默认目录。
- [x] 3.3 实现安全删除预览和短期单次 confirmationId，绑定目标身份、分支/HEAD、锁状态和会话集合；验证安全删除拒绝 dirty/untracked/ignored/活动会话，过期或变化的确认在关闭进程前被拒绝。
- [x] 3.4 实现强制删除的完整路径确认、关闭授权、退出完成等待和单次 Git force；真实 PTY/Git 测试验证先清理子进程/排空 PTY 再删除，保留分支与退出记录，取消、关闭超时和 Git 失败不会被报告为完整成功。
- [x] 3.5 实现主工作区、bare、locked、缺失/未知、子模块/嵌套仓库的删除拒绝及所有操作后的预留释放；验证错误路径仍允许后续合法操作，不双重 force、不自动解锁，也不使用递归目录删除兜底。

## 4. API and Frontend Contracts

- [x] 4.1 在 `internal/api` 实现 design 中的仓库注册、工作树/分支查询、创建、删除预览与执行路由及错误状态；API 行为测试验证认证/Origin 拒绝无副作用、重复注册幂等、局部读取错误与 `400/404/409/503/504` 的区分。
- [x] 4.2 扩展 `/api/config`、会话创建请求、REST/WS 会话元数据及 `web/src/schemas.ts`，迁移现有消费者和受影响测试；验证旧 cwd 请求仍创建成功、workspace/cwd 冲突明确失败、所有新 JSON 在进入页面前通过 Zod，终端二进制协议不变。
- [x] 4.3 增加首页仓库读取/刷新状态管理，复用 `api.ts` 和 `ws.ts`；验证首次加载、返回可见、重连、显式刷新及操作完成能收敛到最新清单，旧响应不覆盖新状态、单仓库失败不丢会话。

## 5. Workspace Overview and Forms

- [x] 5.1 将 `SessionList.svelte` 改为仓库 → 主工作区/worktree → 会话的稳定分组总览，并保留直接会话/已不可用工作区；真实浏览器验证多仓库、多 worktree、多会话、空组和读取失败时每个会话恰好可见一次。
- [x] 5.2 复用 `SessionCard` 与 shadcn-svelte 组件实现两级折叠、数量汇总、Git 状态/读取时间、共享目录提示与完整路径查看；桌面/手机验证输出更新不重排上层分组，长路径不横向溢出，折叠组仍能看到等待数量。
- [x] 5.3 增加显式添加/移除仓库交互，复用 DirectoryPicker 和确认组件；真实浏览器验证重复路径去重、非 Git 错误、不可用条目移除，并确保有运行会话的仓库移除后仍能进入原终端。
- [x] 5.4 扩展 `NewSessionSheet` 的直接目录、已有工作树、新工作树三种模式和上下文入口；验证预设/参数/自定义 argv/名称及最近目录仍有效，创建 worktree 后启动失败可换命令重试且不会重复创建。
- [x] 5.5 实现普通删除预览与独立强制删除确认，明确区分会话关闭、仓库移除和目录删除；真实浏览器验证未确认/取消无副作用、完整路径和关闭授权缺一不可、陈旧确认重新预览、部分失败如实呈现。

## 6. Integration Acceptance

- [x] 6.1 扩展 `web/test/e2e` 隔离夹具，使用临时 HOME、状态目录、真实 Git 仓库和 Shell；桌面与 Pixel 7 流程验证注册 → 两个 worktree → 多 PTY 输入/重连 → 脏树拒绝 → 强制删除 → 分支保留，不连接现有开发服务或依赖已登录 agent。
- [x] 6.2 增加跨生命周期验收：真实重启后仓库/worktree 保留而会话消失；独立无 Git 环境仍可直接启动；外部 worktree 变更刷新后可见；记录对应实际命令输出/页面状态，不仅依赖 mock 测试。
- [x] 6.3 完成后统一运行相关 Go race、现有前端套件及 `make test`，并执行 `make build` 验证嵌入应用；通过实际浏览器检查新首页、桌面 Dialog/手机 Drawer、键盘焦点和长路径确认，记录真机 iOS/Android 键盘验证的可用证据或明确限制。

## 7. Documentation and Delivery

- [x] 7.1 实际运行验证后更新 `README.md`、`WebMux.md` 和 `deploy/config.example.toml`，补充 state_dir、Git 可选依赖、仓库分组、强制删除风险、环境准备边界与“清单持久/会话不持久”；核对文档示例和已验证的接口、配置及页面一致，不保留旧平铺首页或全部状态不落盘的冲突描述。

## Acceptance Evidence

2026-10-14 在隔离临时 HOME / 状态目录 / Git 仓库中验收，不连接开发服务或已有 agent 会话。

| 验证 | 实际结果 |
| --- | --- |
| `go test -race ./internal/workspace ./internal/session ./internal/config ./internal/api ./cmd/webmux` | 全部通过；真实 Git、PTY、子进程及独立服务进程覆盖持久化、身份、并发与降级 |
| `make test` | Go race 全部通过；Vitest 单元 16、浏览器组件 19 通过；Playwright 25 通过、3 按设计跳过（桌面项目不运行移动触控专属场景） |
| Worktree E2E | 8 个场景 × 桌面 / Pixel 7 全部通过：多工作树与独立终端、回放、拒绝脏树、强制删除、分支保留、启动失败重试、移除登记、重启、无 Git、局部失败、长路径、陈旧确认、旧响应竞争与直接目录创建 |
| `make build` | Svelte / TypeScript 零错误零警告，Vite 与 CGO 禁用的嵌入 Go 二进制构建通过；Vite 提示首页 minified chunk 505.34 kB 超过默认 500 kB 建议值，不影响构建 |
| `make format-check`、`go vet ./...` | 通过 |
| `openspec validate add-worktree-management --strict` | 通过 |
| 独立真实浏览器 | 桌面 Dialog 注册 / 创建并进入 Shell，PTY 输出 `visual-proof`；412px 窄屏 Drawer 新建 / 强制删除，完整路径可换行、动作可滚动到达，无横向溢出；缺少关闭授权时不可提交；删除后目录消失，`feature/mobile-visual` 分支保留 |
| 生产二进制 smoke | 直接运行 `bin/webmux` 的嵌入页面，缺失目录经确认后创建并启动；真实目录权限故障使登记删除返回 500、`registryAvailable=false`，随后直接命令仍返回 201 并输出 `STORAGE_FAILURE_DIRECT_OK` |
| 审查回归 | 原子替换后目录同步失败、Git 主进程退出后的取消子进程泄漏，均以失败前 / 修复后通过的真实故障用例确认；不通过测试桩替代 Git 或 PTY |

限制：本环境没有 iOS Safari / Android 真机，未证明系统软键盘、原生选择菜单、VPN 切网或真实锁屏行为；Chromium 窄屏与 Pixel 7 模拟不替代真机验收。强制删除保护仅覆盖已知启动 cwd，不保护外部进程或后续 cd，亦非文件系统事务。
