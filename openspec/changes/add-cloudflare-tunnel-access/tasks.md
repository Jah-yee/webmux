## 1. 前置验证与固定版本

- [x] 1.1 用真实 cloudflared 跑一次 Quick Tunnel（`cloudflared tunnel --no-autoupdate --url http://127.0.0.1:<端口>`），指向一个打印请求头的临时 HTTP 服务，确认以下几点，并把结论写回 design.md 的 Risks：
  - stderr 中公网地址行与 `Registered tunnel connection` 行的格式
  - origin 收到的 Host 头是否为公网主机名，以及 Origin 头的值
  - 存在 `~/.cloudflared/config.yml` 时 Quick Tunnel 是否受影响
  - 若需要 `--config` 空文件规避，同步修订 design 第 2 节
- [x] 1.2 选定固定的 cloudflared 版本，从 GitHub Release API 记录四个平台资产（`linux-amd64`、`linux-arm64`、`darwin-amd64.tgz`、`darwin-arm64.tgz`）的 `digest`。逐个下载并用 `sha256sum` 复核一致，再把校验值写入 `internal/tunnel` 的常量表。
  - **结果**：固定为 2026.10.2，四个 `digest` 与下载后的 `sha256sum` 一致；另记录 darwin `.tgz` 解包后可执行文件的 SHA-256，用于校验缓存。
  - **验证**：复核记录与常量逐项一致。
- [x] 1.3 用 `curl -fL --max-time 60` 分别通过 `ghfast.top`、`gh-proxy.com`、`ghproxy.net` 下载 1.2 选定版本的 `cloudflared-linux-amd64`，确认这三个镜像的前缀形式可用、会跟随重定向、内容 SHA-256 与常量一致。不可用的镜像从默认列表中移除或替换，并更新 design 第 3 节。
  - **结果**：国内直连时 ghfast.top 约 1.8 MB/s、gh-proxy.com 约 2.5 MB/s，SHA-256 一致；ghproxy.net 只有约 10 KB/s，已移出默认列表。
  - **验证**：记录每个镜像的结果，默认列表只保留实测可用的镜像。

## 2. 配置与 Token

- [x] 2.1 在 `internal/config` 新增 `TunnelConfig{Enabled, Token, Hostname, Cloudflared, DownloadMirrors}`，并解析 `[tunnel]` 节。`DownloadMirrors` 要能区分 nil（使用内置默认镜像）与 `[]`（禁用镜像）。`Validate` 校验以下规则：
  - Named Tunnel 必须配置纯主机名 `hostname`，并统一转为小写；末尾的点被去掉，拒绝通配符与非 ASCII（IDN 用 punycode）
  - `cloudflared` 路径展开 `~`
  - 每个镜像前缀为 `https://`，不含凭据、查询参数或片段，末尾的 `/` 被去掉

  **验证**：在 `config_test.go` 增加表驱动用例，覆盖以下情况：
  - Quick 模式无需 `hostname`
  - Named 模式缺少 `hostname`，或 `hostname` 含协议、端口、路径、`@` 时报错
  - 大写主机名与末尾的点被规范化；通配符与非 ASCII 主机名报错
  - 未配置镜像时为 nil，`[]` 保持为空，非法前缀报错，末尾 `/` 被规范化

- [x] 2.2 在 `cmd/webmux/main.go` 的 `loadConfig` 新增 `--tunnel` 布尔参数，显式出现时覆盖 `tunnel.enabled`。
  - **验证**：在 `main_test.go` 现有表格中补充两行：参数开启时覆盖配置；`--tunnel=false` 时关闭配置中的开关。
- [x] 2.3 隧道启用且 Token 为空时，用 `crypto/rand` 32 字节加 `base64.RawURLEncoding` 生成 Token，写入 `c.Token`；已配置的 Token 少于 43 字符时拒绝启动；非隧道模式不生成。
  - **验证**：单元测试断言以下几点：隧道模式生成 43 字符 URL 安全 Token，两次生成结果不同，已配置的 43 字符 Token 不被覆盖，过短的已配置 Token 报错，非隧道模式保持空 Token。

## 3. 访问 URL 与二维码输出

- [x] 3.1 实现 `accessURL(base, token)` 与本地地址推导，规则如下：
  - 通配地址改为 `127.0.0.1`
  - IPv6 地址加方括号
  - 使用 `listener.Addr()` 的实际端口

  `net.Listen` 后向 stdout 打印本地 URL，只有隧道模式附带 `#token=` 片段。**验证**：表驱动测试覆盖 access-link 规格中的所有场景：
  - 隧道模式的 Token 片段与无 Token
  - 非隧道模式即使配置了 Token 也不含片段
  - `0.0.0.0:0` 动态端口
  - `[::1]`
  - Token 中的特殊字符编码为 `%20` / `%26` / `%23`

- [x] 3.2 引入 `rsc.io/qr`，实现半块字符终端渲染（纠错级别 L，4 模块静区）。
  - **验证**：单元测试用 `qr.Encode` 得到的矩阵与渲染输出逐模块比对，确认可逆还原且尺寸正确；`CGO_ENABLED=0 go build ./...` 通过。

## 4. cloudflared 获取

- [x] 4.1 在 `internal/tunnel` 实现可执行文件解析，顺序为：显式路径、`LookPath`、`<state_dir>/cloudflared/<版本>/cloudflared`（缓存存在时先校验 SHA-256）。显式路径无效时直接报错，不再回退。
  - **验证**：单元测试使用临时目录与伪造的 `PATH`，覆盖以下情况：
    - 显式路径优先，且失败时不回退
    - 命中 `PATH` 时不访问网络
    - 缓存校验通过时直接使用
    - 缓存被篡改时进入下载分支
- [x] 4.2 实现下载：
  - 按 `GOOS/GOARCH` 选择资产；darwin 平台解开 `.tgz`
  - 限制响应大小
  - 先写临时文件，校验通过后 `chmod 0755` 并 rename
  - 校验失败时删除临时文件并报错
  - 不支持的平台给出“请自行安装 cloudflared”的错误

  GitHub 基地址与镜像列表可注入，供测试使用。**验证**：用 `httptest` 提供伪造资产，测试以下情况：
  - 正确资产会落盘并可执行
  - 错误校验值不落盘、不执行
  - 超过大小上限时报错
  - tgz 解包得到正确文件

- [x] 4.3 实现来源回退：先 GitHub 直连，再按顺序尝试镜像；每个来源设 10 秒响应头超时与 20 秒读取空闲超时；遇到非 2xx、超出大小上限或校验失败时换下一个来源；GitHub 成功时不访问镜像；全部失败时返回汇总错误，并提示手动安装或配置 `tunnel.cloudflared`。超时值可注入，测试用毫秒级。
  - **验证**：用多个 `httptest` 服务端模拟 GitHub 与镜像，覆盖以下情况：
    - GitHub 挂起不返回响应头时回退到镜像，并断言镜像请求路径为 `/https://github.com/...`
    - GitHub 发出部分数据后停滞时回退，部分数据不落盘
    - 第一个镜像返回 404、第二个成功
    - 第一个镜像返回篡改内容、第二个成功
    - GitHub 成功时镜像服务端收到 0 次请求
    - `download_mirrors = []` 且 GitHub 失败时报错
    - 全部失败时错误信息包含每个来源
    - 根 ctx 取消后立即返回

## 5. 隧道子进程

- [x] 5.1 实现 `tunnel.Start(ctx, opts)`：
  - argv 拼装：Quick 模式为 `tunnel --no-autoupdate --url`；Named 模式为 `tunnel --no-autoupdate run`，`TUNNEL_TOKEN` 经环境变量传入
  - 逐行扫描 stderr，解析 Quick 地址与 `Registered tunnel connection`
  - 提供 `Ready()`、`Done()`、`Close()`
  - 60 秒就绪超时
  - 使用 `exec.CommandContext`，SIGTERM 取消，`WaitDelay` 5 秒

  **验证**：Go 测试用 shell 脚本伪造 cloudflared，覆盖以下情况：
  - 输出地址行与注册行后，`Ready` 收到正确 origin
  - argv 不含 Named Tunnel 凭据，环境变量中含凭据（脚本把 argv 与环境写入文件后断言）
  - 只输出地址、不输出注册行时，超时且不发出 Ready
  - 超时错误日志之后才输出注册行时，仍发出 Ready
  - 脚本立即以非零状态退出时，`Done` 返回包含退出码的错误
  - ctx 取消后子进程被终止，且无 goroutine 泄漏（`go test -race`）

## 6. 公网 origin 白名单

- [x] 6.1 在 `internal/api` 新增 `PublicOrigin`（基于 `atomic.Pointer`，提供 `Set`、`Clear`、查询）。`api.New` 新增 `public *PublicOrigin` 参数，并更新所有调用方：`cmd/webmux`、`cmd/baseline/fixture.go` 与各 `_test.go`，测试中传 nil。`hostCheck` 与 `originAllowed` 按 design 第 4 节接入。
  - **验证**：在 `security_test.go` 新增表驱动用例：
    - 公网 Host + `https://` 公网 Origin + 有效 Token：200
    - 公网 Host + `http://` 公网 Origin：403
    - 未 Set 时公网 Host：403
    - `Clear` 后公网 Host：403
    - 无关 Host：403
    - 直连路径的原有用例结果不变
- [x] 6.2 WebSocket 经公网 Host/Origin 握手。
  - **验证**：在 `security_test.go` 中用 `websocket.Dial` 设置自定义 Host 与 Origin 头，已 Set 时握手成功并收到快照，Clear 后被拒绝。

## 7. 启动流程接线

- [x] 7.1 在 `run()` 中接线：
  - 启用隧道时，在 `net.Listen` 之前解析或下载 cloudflared，失败则以非零状态退出
  - 监听后启动隧道
  - 就绪后依次执行 `public.Set`，打印公网 URL 与二维码
  - `Done` 后执行 `public.Clear` 并记录错误日志，不终止服务
  - 关闭时先关隧道再 `m.Shutdown`

  **验证**：Go 测试用伪造的 cloudflared 驱动 `run` 级别的启动函数，断言以下几点：
  - stdout 依次出现本地 URL、公网 URL 和二维码
  - 携带 Token 的请求经公网 Host 成功
  - 伪造进程退出后服务仍可经本地地址访问
  - 关闭后伪造进程已退出

## 8. 前端片段认证

- [x] 8.1 在 `web/src/api.ts` 实现 `consumeTokenFragment()`，在 `App.svelte` 首次 `check()` 之前调用，同步组件的 `token` 状态；`ws.ts` 以 `webmux.token.<base64url>` 子协议携带 Token，不再拼进 WebSocket URL，也不写 Cookie。服务端只选择 `webmux` 子协议，移除 WebSocket 的 `?token=` 入口。
  - **验证**：在 `web/test/` 新增 Vitest 单元测试，覆盖以下情况：
    - `#token=` 解码后存储，片段被 `replaceState` 移除，路径与查询参数保留
    - `#quick-commands` 不被处理
    - 非法百分号编码时不存储，但片段被移除
    - 已有 Token 被覆盖
- [x] 8.2 E2E：
  - 带 Token 的后端下，在全新浏览器上下文打开 `/#token=<有效>`，直接进入会话列表，地址栏不含片段，`page.goBack()` 不回到带 Token 的地址
  - 登录后前端的 `/ws/sessions` 握手成功并收到帧，请求 URL 不含 `token`，`document.cookie` 不含 Token；不带子协议的裸握手失败
  - 打开 `/#token=wrong` 显示 Token 表单与现有的认证失败提示

  **验证**：在 `web/test/e2e/` 新增或扩展 spec，运行 `pnpm --dir web exec playwright test` 指定该文件并通过。

## 9. 文档与示例

- [x] 9.1 在 `deploy/config.example.toml` 增加注释掉的 `[tunnel]` 示例。在 `docs/deployment.md` 增加隧道章节，内容包括：
  - Quick 与 Named 的用法
  - Named Tunnel 需要在 Cloudflare 侧把公共主机名指向本地端口
  - 自动下载与 `state_dir` 缓存；GitHub 直连失败时回退到镜像，用 `tunnel.download_mirrors` 覆盖或禁用镜像
  - 隧道模式的 Token 会进入 journal 的安全提示，以及显式 Token 的最低长度
  - `#token=` 链接只在隧道模式打印

  **验证**：照文档执行 `webmux --tunnel`，能得到可扫码访问的公网地址。

## 10. 集成检查

- [x] 10.1 运行 `make check` 与 `make test`，确认全部通过。
  - **结果**：`make test` 全部通过；`make format-check` 与 `go vet` 通过。`golangci-lint` 报告 26 个本变更之前就存在的问题（errcheck / ST1005，CI 不运行该目标），本变更新增与修改的文件没有问题。
- [x] 10.2 在本机用真实 cloudflared 执行 `./bin/webmux --tunnel`，确认以下几点：
  - 终端打印公网 URL 与二维码
  - 用手机扫码，直接进入会话列表并能打开终端交互
  - 进程列表中的 cloudflared argv 不含凭据
  - `Ctrl-C` 后不残留 cloudflared 进程

  **结果**：在隔离的 HOME 中运行。GitHub 直连 10 秒内没有返回响应头，系统回退到 ghfast.top 下载，校验通过后启动 Quick Tunnel；终端打印公网 URL 和二维码，二维码用 ZXing 解码得到同一 URL。用无头 Chromium 以手机视口打开该链接，直接进入会话列表，并通过 wss 创建 Shell，执行 `echo` 后收到输出。外部 kill cloudflared 后，公网 Host 返回 403，本地访问仍可用。再次启动直接复用缓存，没有重新下载；SIGTERM 后不残留 cloudflared。没有用实体手机扫码。
