# 页面性能与连接稳定性验证

核对日期：2026-10-25。基线为主分支 `32b2055`，与本 PR 使用相同的 Node.js 26.12.1、pnpm 12.11.2 和锁定依赖构建。测试连接独立后端及临时目录。

## 结果

| 指标                          |      基线 |                本次实现 |
| ----------------------------- | --------: | ----------------------: |
| 首屏 JavaScript gzip          | 195,695 B | 148,529 B（减少 24.1%） |
| 首屏 JavaScript 原始体积      | 685,439 B |               481,963 B |
| 1000 个会话的 1000 次更新计算 | 168.42 ms |                 2.85 ms |

首屏体积从 `vite build --manifest` 的入口开始，递归收集静态 `imports`，对每个唯一 JavaScript 文件 gzip 后求和。保留工作区在首屏，不将异步加载的设置、终端、代码面板或未打开的管理表单计入首屏闭包。可在基线与 PR 分支分别运行以下构建，再按此方法读取 `.vite/manifest.json`：

```sh
pnpm --dir web exec vite build --manifest
```

列表基准是 7 次运行的中位数：基线每次先移除旧会话、追加新会话，再按创建时间和 ID 排序；新实现替换已有位置，只在快照和插入时处理顺序。它仅测量列表计算，不包含 Zod、网络、Svelte 和浏览器渲染，不能作为整个页面加速比例。

## 行为与验证

- 设置与管理表单按需加载，加载中可取消；下载失败可关闭提示，取消后不会迟到弹出。
- 已有会话更新保留其他行的对象和创建顺序；最近活跃时间每 30 秒、快照与回到前台时校准。
- 搜索与折叠暂时隐藏已挂载的卡片，清除筛选直接恢复；隐藏节点不进入键盘和辅助技术浏览，删除会话仍释放节点。隐藏卡片继续接收数据更新，保留范围仅限当前列表。会话菜单首次打开时才初始化浮层，隐藏卡片或工作区时关闭已打开的菜单。
- 关闭、释放或处于退避中的连接忽略迟到事件；普通 `pageshow` 不替换健康连接，前台和 bfcache 恢复仍会续接。
- Svelte/TypeScript、Oxlint、Oxfmt、前端架构守卫、生产构建、216 个单元测试与 178 个 Chromium 组件测试通过。
- continuity、launch-configs、workspace-discovery、worktrees、quick-commands 和 page-stability 共 60 个 E2E 通过，2 个原有平台条件跳过；发布截图使用 `demo-host`。
- 16 个性能 E2E 通过，22 份阶段报告均无意外布局偏移，最高交互耗时为 72ms。新增 120 卡片场景使用独立单 worker、4 倍 CPU 降速，校验 60 次输出更新、排序、搜索、清除筛选、节点复用和移除、隐藏卡片关闭菜单，要求意外布局偏移为 0、交互 ≤200ms。

```sh
pnpm --dir web check
pnpm --dir web lint
pnpm --dir web test:unit
pnpm --dir web test:browser
pnpm --dir web exec playwright test test/e2e/page-stability.spec.ts test/e2e/launch-configs.spec.ts test/e2e/workspace-discovery.spec.ts test/e2e/continuity.spec.ts test/e2e/worktrees.spec.ts test/e2e/quick-commands.spec.ts
pnpm --dir web test:performance
```

本次更新 US-A01、US-C01、US-C02、US-C03、US-C04、US-D03。没有重新核验整个故事目录；Chromium Pixel 7 模拟不代表已完成真实设备的键盘、选词和原生菜单验证。

## 截图

首屏和表单状态使用无会话、无仓库的隔离数据；菜单状态使用 120 张合成会话卡片，筛选到 load-31，无私人终端内容。桌面 viewport 为 1280×720 CSS px，Pixel 7 为 412×839 CSS px；手机图片保留设备像素倍率。截图文件位于 [page-performance](images/page-performance/)，包括明暗主题的首屏前后对照，以及表单下载中、下载失败和会话菜单状态。
