## 1. 运行时基础（`internal/runtime`）

- [x] 1.1 实现记录结构（v1，含套接字路径）、`<state_dir>/run/` 0700 目录、0600 原子替换写入、读取与格式版本校验。验证：`go test -race ./internal/runtime` 覆盖权限、原子替换、未知版本、损坏 JSON
- [x] 1.2 实现 `server.lock` 独占锁获取/释放；锁被占用时返回可识别错误。验证：同进程两次获取与子进程持锁的测试
- [x] 1.3 实现 `ProcessIdentity(pid)`（Linux `/proc/<pid>/stat` starttime 与 `/proc/<pid>/exe` 去 ` (deleted)`；darwin `proc_pidinfo`）与 `PeerCred(conn)`（Linux `SO_PEERCRED`；darwin `LOCAL_PEERCRED` 与 `LOCAL_PEERPID`）。验证：对自身、已退出子进程与 Unix 套接字对端的测试在 Linux 与 macOS CI 上通过
- [x] 1.4 实现 Token 指纹、启动方识别（`WEBMUX_LAUNCHER`、`INVOCATION_ID`、`XPC_SERVICE_NAME`）与套接字路径规则（默认路径、超长时 `$XDG_RUNTIME_DIR` 退回、都不可用时禁用）。验证：表驱动单元测试

## 2. 契约与本机 API 客户端（`internal/contract`、`internal/localclient`）

- [x] 2.1 新增无依赖的 `internal/contract`：`StatusV1`/`StopV1`/`ConfigV1`、健康分类、`/local/v1` 路径常量。验证：`GOOS=windows go build ./internal/contract` 成功，`go list -deps ./internal/contract` 只含标准库
- [x] 2.2 新增 `internal/localclient`：`Dialer` 接口、基于它的 `http.Client` 与 WebSocket 拨号、`Status`/`Shutdown`/`Token`、`UnixDialer`。验证：`GOOS=windows go build ./internal/localclient` 成功；以 `net.Pipe` 模拟的拨号器对接 `httptest` 处理器的单元测试

## 3. 服务端接线（`cmd/webmux`、`internal/api`、`internal/session`）

- [x] 3.1 子命令分发：无参数/`-` 开头 → serve，`serve`、`version`、`status`、`stop`、`bridge`、`config`，未知子命令退出码 2。验证：`cmd/webmux` 测试覆盖 `webmux --config x`、`webmux serve --listen ...`、`webmux statsu`
- [x] 3.2 `serve --log-file`：`~/` 展开、`.1` 轮转、0600/0700、fd 1/2 重定向（linux `dup3`/darwin `dup2`）；`--config` 支持 `~/`。验证：子进程测试中关闭父端管道后服务继续写日志并存活
- [x] 3.3 `internal/api` 拆分路由注册与策略中间件；新增 `NewLocal`：跳过 Host/认证/Origin，注册 `/local/v1/status|shutdown|token`；TCP 上 `/local/*` 返回 404。验证：既有 `internal/api` 安全测试全部不变通过；新增测试断言 TCP 上 `/local/v1/token` 为 404
- [x] 3.4 `run()` 增加第二个 `http.Server` 服务 Unix listener：`ConnContext` 校验对端 UID（不符即关闭）并记录对端 PID；遗留套接字清理；退出时两个 server 依序关闭并删除套接字；`/local/v1/shutdown` 取消信号上下文。验证：集成测试启动 `--listen 127.0.0.1:0`，经套接字免 Token 访问 `/api/sessions` 与 `/ws/s/{id}`（终端输入输出）、`shutdown` 后记录与套接字消失、systemd 启动方返回 409
- [x] 3.5 会话内进程判定（Linux 环境标记 + 父进程链；macOS 父进程链）对 `shutdown`/`token` 返回 403。验证：在真实会话中运行的测试程序请求 `token` 得到 403、请求 `POST /api/sessions` 成功
- [x] 3.6 监听就绪后写记录、退出清理后删除；锁被占用时警告并跳过记录与套接字。验证：第二实例不覆盖记录、不创建套接字且 TCP 照常服务
- [x] 3.7 `sessionEnv` 过滤 `WEBMUX_LAUNCHER`。验证：`internal/session` 测试断言会话环境不含该变量

## 4. 客户端命令（`internal/client`、`cmd/webmux`）

- [x] 4.1 `Locate`：复用 `config.Load`/`Validate` 解析配置，确定状态目录、记录与套接字路径；退回路径所需的 Token 读取。验证：单元测试覆盖缺失配置、显式路径不存在、失效记录（PID 不存在、启动标识不符）
- [x] 4.2 `Links`：本机/远程链接、Tailscale 置顶、排除回环/链路本地/停用网卡与 WSL NAT 虚拟网卡、通配无 Token 警告；服务端 `/local/v1/status` 与退回路径共用。验证：表驱动测试覆盖 `127.0.0.1`、`localhost`、`[::1]`、`0.0.0.0`、`::`、`:7681`、具体地址，结果不含 Token
- [x] 4.3 `status`（本机 API 优先、退回记录 + TCP 健康检查、人类可读与 `--json` v1、`--reveal-token`、`drift`、`warnings`、退出码 0/1/3）。验证：golden JSON 测试分别覆盖本机 API 与退回路径；断言无 `--reveal-token` 时 stdout/stderr 均不含 Token
- [x] 4.4 `stop`（本机 API 优先、卡死时退回 SIGTERM → `--timeout` → SIGKILL、systemd/launchd 拒绝与 `--force`、幂等、无记录不发信号、`--json`）。验证：真实 `webmux serve` 子进程测试正常停止、阻塞本机 API 的替身进程被信号停止、无记录时不发信号
- [x] 4.5 `bridge`：双向复制与半关闭、忽略 SIGPIPE、连接失败不写 stdout 且退出码 3/1。验证：以子进程运行 `webmux bridge`，经其 stdin/stdout 用 `localclient` 完成多个 keep-alive 请求与一次 WebSocket 终端会话；服务未运行时 stdout 为空且退出码 3

## 5. 配置命令

- [x] 5.1 `config path` 与 `config show [--json]`（契约 v1，复用 `config.Load`/`Validate`，不含 Token）。验证：golden JSON 测试覆盖无文件、默认值与自定义预设，断言输出无 Token
- [x] 5.2 `config validate [--json]`（TOML 行列号、校验错误、不写文件）。验证：语法错误与非法预设样例测试
- [x] 5.3 `config init [--listen] [--force]`：经新增的 `deploy/embed.go` 嵌入模板、`crypto/rand` 32 字节 base64url Token、0600/0700、已存在拒绝、`--force` 生成 `.bak`、输出只含路径。验证：单元测试断言权限、Token 长度与唯一性、模板其余内容不变、标准输出不含 Token；`config validate` 能通过生成的文件

## 6. 文档与验收

- [x] 6.1 `docs/architecture.md` 增加“客户端与通讯方案”一节（决策 0 摘要与信任边界）；`docs/protocol.md` 增加本机套接字、`/local/v1/*` 与 `status`/`stop`/`config` JSON 契约及兼容规则；`docs/deployment.md` 增加 `status`/`stop`/`config init`/`--log-file`/`bridge` 用法。验证：`make format-check` 通过
- [x] 6.2 运行 `make format-check`、`go vet ./...`、`go test -race ./...`（Linux 与 macOS CI）全部通过
