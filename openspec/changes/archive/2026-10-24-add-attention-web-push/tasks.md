# Tasks

## 1. 配置

- [x] 1.1 在 `internal/config` 新增 `PushConfig` 与 `[push]` 默认值，并在 `Validate` 中校验以下规则：
  - `subject` 为 `https://` URL 或 `mailto:` 地址；
  - `ttl_seconds` 在 0 到 2419200 之间；
  - `kinds` 只能取自六种 attention 种类，且不得重复。

  验证：`go test -race ./internal/config` 用表驱动用例覆盖默认值和每种非法值；`webmux config validate` 对 `ttl_seconds = -1` 返回 1。
- [x] 1.2 在 `deploy/config.example.toml` 增加带注释的 `[push]` 示例，并在 `docs/deployment.md` 配置表中增加 `push` 行，说明 Named Tunnel 前置条件。验证：`go test ./cmd/webmux -run Config` 中解析示例配置的既有测试通过。

## 2. 推送存储

- [x] 2.1 实现 `internal/push` 的 `Store`：
  - 首次运行生成 VAPID P-256 密钥；
  - 由私钥推导 base64url 未压缩公钥；
  - 按 endpoint 幂等增删订阅，上限 32 个，订阅 ID 为 128 位随机值；
  - 记录订阅来源；
  - 写入时目录为 0700、文件为 0600，并原子替换。

  验证：单测覆盖重启后复用密钥与订阅、幂等登记返回同一 ID、第 33 个 endpoint 被拒绝、文件权限为 0600。
- [x] 2.2 实现存储故障处理：
  - JSON 损坏时把原文件改名为 `push.json.corrupt-<时间>` 并重建；
  - 目录不可写时返回 `storage` 不可用状态。

  验证：单测分别构造损坏文件和只读目录，断言重建后的内容、原文件被保留，以及不可用原因。

## 3. 去重 Tracker

- [x] 3.1 实现纯状态机 `Tracker`：
  - 首个 snapshot 只建立基线；
  - `created`/`updated` 比较 `(kind, since)` 并按 `kinds` 过滤；
  - attention 为空时清空记录；
  - `removed` 时丢弃记录；
  - 断线后的 snapshot 与保留的基线比较，并清除快照中不存在的会话。

  验证：表驱动单测覆盖规格中的全部情况，包括初始快照不推、重复 updated 不推、source 变化不推、清除后再次置位推送、done 升级为 permission 推送、bell 与不在 kinds 中的种类不推、启发式 waiting 与 exited 不推、断线补推只推一次。

## 4. 加密与发送

- [x] 4.1 用标准库实现 RFC 8291 `aes128gcm` 加密：注入 salt 与临时密钥，生成单条记录。验证：单测使用 RFC 8291 附录 A 测试向量，输出逐字节一致。
- [x] 4.2 实现 RFC 8292 VAPID 头：JWT 使用 ES256，`aud` 为 endpoint 的源，`exp` 为 12 小时后，`sub` 为配置的 `subject`，并生成 `Authorization: vapid t=…, k=…`。验证：单测用公钥验证签名，检查 claims 与 `k` 值。
- [x] 4.3 实现负载构造：
  - 包含 Declarative 字段与 `webmux` 字段；
  - 使用种类文案表；
  - 标题去除控制字符并截断到 80 字符；
  - 支持 `include_title`；
  - `navigate` 由订阅来源拼接，负载不超过 4096 字节。

  验证：单测覆盖 permission 负载字段、空标题与隐藏标题，并断言负载中不出现 cwd、cmd、preview 或 OSC 正文。
- [x] 4.4 实现 `Sender`：
  - 10 秒超时，不跟随重定向；
  - 设置 `TTL`、`Urgency`（question/permission/error 为 high，其余为 normal）与 `Content-Encoding`；
  - 404/410 时删除订阅，其他失败只记日志；
  - 日志不含完整 endpoint。

  验证：用 `httptest.Server` 作为推送服务，测试请求头、307 不跟随、410 删除订阅、超时；捕获日志并断言不含 endpoint 路径。

## 5. 推送服务与接线

- [x] 5.1 实现 `push.Service`：
  - 订阅 `Manager`，在 `Slow` 关闭后重新订阅并交给 Tracker；
  - 使用容量 64 的有界队列与 2 个 worker，队满时丢弃并告警；
  - `Close` 停止接收并等待所有 goroutine 退出。

  验证：`go test -race ./internal/push` 用真实 `session.Manager` 与假推送服务测试：会话发出 OSC 777 结构化 question 后只推送一次；推送服务阻塞时终端输出与列表更新不受影响；`Close` 后没有遗留 goroutine。
- [x] 5.2 在 `cmd/webmux/main.go` 中接线：按配置构造服务，传入 `api.New`、`api.NewLocal` 与本机状态，并在 `m.Shutdown` 之前调用 `Close`；同步更新 `cmd/baseline/fixture.go` 的调用。验证：`go build ./...` 与 `go test -race ./cmd/...` 通过；`enabled = false` 的启动测试断言 `state_dir` 中没有生成 `push.json`。

## 6. REST API

- [x] 6.1 在 `internal/api` 实现来源判定 `pushOrigin`，可能的结果为：Named Tunnel 配置的来源、回环来源、`quick_tunnel` 与 `unsupported_origin`；判定依据为 Origin，没有 Origin 时用 Host 推断。验证：表驱动单测覆盖 Named 主机（隧道开启与关闭）、Quick Tunnel 公网主机、`127.0.0.1`、`[::1]`、`localhost`、`allowed_origins` 中的 VPN 地址，以及本机套接字请求。
- [x] 6.2 实现 `GET /api/push`、`POST /api/push/subscriptions`、`DELETE /api/push/subscriptions/{id}` 与 `POST /api/push/test`：
  - 处理状态码 200、204、400、404、409、410、502；
  - 校验 endpoint 与密钥（`p256dh` 必须是曲线上的点，`auth` 为 16 字节）；
  - 服务为 `nil` 时报告 `disabled`。

  验证：`go test -race ./internal/api` 覆盖未带 Token 返回 401、错误 Origin 返回 403、Quick Tunnel 来源登记返回 409、非法 endpoint 返回 400、幂等登记、未知 ID 删除返回 204、测试通知遇推送服务 410 时返回 410 并删除订阅；所有响应体都不含 endpoint 或密钥。
- [x] 6.3 在 `docs/protocol.md` 记录四个推送接口、来源规则、负载字段与去重语义，并说明推送不重放断线期间已清除的信号、送达只是尽力而为。验证：文档中的请求与响应示例和 6.2 测试中的 JSON 一致。

## 7. 本机状态

- [x] 7.1 在 `contract.StatusV1` 增加 `Push *PushStatus`（`enabled`、`available`、`error`、`subscriptions`、`publicOrigin`），由 `localStatus` 填充；实例未运行时省略该字段。验证：`tunnelctl_test` 风格的集成测试检查运行实例的 `push` 字段、Named 配置下的 `publicOrigin`、未运行时没有 `push`，且编码后的状态不含 Token 或 endpoint；在 `docs/protocol.md` 的状态示例中加入该字段。

## 8. 前端：Service Worker 与推送模块

- [x] 8.1 新增 `web/public/sw.js`，除生命周期（安装即激活、激活时接管已打开页面）外只处理 `push` 和 `notificationclick`：
  - 立即调用 `showNotification`；
  - 负载无法解析时显示兜底通知；
  - 点击时校验 `^[a-z2-7]{8}$`，依次尝试聚焦已打开会话的窗口、让同源窗口导航、打开新窗口，ID 非法时打开 `/`；
  - 不处理 `fetch`。

  验证：`pnpm --dir web build` 后 `web/dist/sw.js` 存在；Vitest 单测把处理逻辑提取为可导入的纯函数，覆盖负载解析与 URL 选择，包括非法 ID 和外部 `navigate`。
- [x] 8.2 新增 `web/src/push.ts` 与 `schemas.ts` 中的 `PushStatusSchema`、`PushSubscriptionResultSchema`。`push.ts` 实现：
  - `detectSupport`（安全上下文、Service Worker、PushManager、Notification、iOS、standalone、权限）；
  - base64url 转换；
  - `enable`、`disable`、`sync`、`test`，复用 `api.ts`，订阅 ID 存入 `webmux.push.subscriptionId`；
  - `sync` 遇到公钥不一致时取消订阅并返回需要重新开启。

  验证：`pnpm --dir web exec vitest run --config vitest.config.ts test/push.test.ts` 覆盖能力检测分支、公钥转换、登记返回 409 时回滚浏览器订阅、测试返回 410 时清除 ID、没有注册时 `sync` 不注册 Service Worker。

## 9. 前端：设置页卡片

- [x] 9.1 在设置页新增 `id="push"` 的“通知推送”卡片（组件 `PushSettings.svelte`）：
  - 显示状态与订阅数；
  - 提供开关与测试按钮；
  - 按“非安全上下文 → iOS 非主屏幕 → 能力缺失 → 服务端原因 → 权限拒绝 → 可开启”的顺序展示操作提示（非安全上下文与 iOS Safari 标签页本身就隐藏 Push API，必须先于能力判断），提示文案按规格中的步骤编写；
  - 显示送达限制说明。

  验证：浏览器测试（`web/test/browser`）分别渲染 `quick_tunnel`、`unsupported_origin`、非安全上下文、iOS 非 standalone、权限拒绝、`disabled`、可开启这几种状态，断言显示对应提示并且只在可开启时出现开关；`pnpm --dir web check` 与 `pnpm --dir web lint` 通过。
- [x] 9.2 在 `App.svelte` 认证成功后调用 `sync`，仅在通知权限已授予且已有推送订阅时执行。验证：既有 Vitest、浏览器测试与 Storybook 套件不出现 Service Worker 注册或权限请求，相关测试保持通过。
- [x] 9.3 新增 E2E `web/test/e2e/push.spec.ts`，使用隔离后端、回环来源、新版 headless Chromium（chrome-headless-shell 不授予通知权限），并授予通知权限：
  - 开启后服务端订阅数为 1；
  - 测试接口在推送服务不可达时返回 502，且卡片显示错误；
  - 关闭后订阅数为 0；
  - 向 Service Worker 注入 push 事件后会显示通知；
  - 对通知触发点击后，页面导航到 `/s/<id>`（这两项在 macOS CI 上跳过：headless Chromium 不列出 Service Worker 的通知）。

  验证：`pnpm --dir web exec playwright test test/e2e/push.spec.ts` 在 desktop 项目通过。

## 10. Windows 托盘

- [x] 10.1 在 `internal/tray` 中由状态推导推送提示：
  - 新增纯函数 `PushEntry`，返回提示行与说明文本；
  - 新增纯函数生成 Quick/未配置与 Named 两种说明文本，Named 说明包含公网地址，两种说明都不含 Token。

  验证：`menuview_test.go` 覆盖停用、存储不可用、未配置 Named、Named 0 台、Named 2 台以及旧服务端没有 `push` 字段这几种情况，并断言说明文本含 `tunnel.token`/`tunnel.hostname` 的配置步骤或手机端步骤，且不含 Token。
- [x] 10.2 在 `internal/tray/winui/menu.go` 增加推送提示行与“手机推送设置说明…”菜单项，复用 `messageBox` 显示说明。验证：`GOOS=windows go build ./cmd/webmux-tray` 通过；在 Windows 上手动检查菜单与对话框显示，并截图附在 PR 中。
  - 2026-10-24 验收：Windows 托盘构建通过；Windows 11 上菜单显示“手机推送：需要 Named Tunnel”，“手机推送设置说明…”对话框显示 Named Tunnel 步骤与手机端步骤且不含 Token（深色截图 `windows/p1-push-hint-menu-dark.png`、`windows/p2-push-guide-dialog-dark.png`，见 `mzdluo123/webmux@63881974`）。

## 11. 集成与验收

- [ ] 11.1 运行 `make check` 与 `make test`，全部通过。
  - 2026-10-24 归档时（`main` d85ab21）：`make test` 通过（Go race；Vitest unit 167；browser 143，首轮 `CodeDocument.test.ts` 一例负载下失败、重跑通过；E2E 132）。`make check` 中前端构建、svelte-check、Oxfmt、Oxlint、`go vet` 通过，但 `golangci-lint`（2.15.0，仓库无配置文件，CI 不运行该步骤）报告 41 个既有问题（errcheck 37、staticcheck ST1005 3、unused 1），均不在推送相关文件中，因此本项未勾选。
- [ ] 11.2 在真实 Named Tunnel 上手动验收：
  - 设备：Android Chrome（有 GMS）、iOS 16.4 以上的主屏幕应用（如有，包括 18.4 以上）、桌面 Chrome 与 macOS Safari；
  - 场景：关闭页面或锁屏后收到 question/permission 提醒，点击后回到会话；spinner 持续输出与重连时不重复提醒；隧道关闭时仍能收到提醒；Quick Tunnel 地址上看到 Named Tunnel 提示；退订后不再收到提醒。

  验证：在 PR 描述中记录每个平台的结果，并附设置页浅色/深色、桌面/手机截图。
  - 未验收（2026-10-24 归档时）：没有可用的 Cloudflare Named Tunnel 账号与 Android / iOS / macOS 真机，本项未执行。
