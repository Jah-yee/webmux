## 0. 前置

- [x] 0.1 确认 #83（本机 API、`bridge`、`internal/contract`、`internal/localclient`）与 #67（`env` 字段、`run/attach` 语义、共享附着核心）已合入。验证：`GOOS=windows go build ./internal/contract ./internal/localclient` 成功；Linux 上 `webmux run --detach -- true` 可用

## 1. 共享客户端（`internal/wslclient`）

- [x] 1.1 `target.go`：发行版与 WSL 内 `webmux` 路径解析（参数 > 环境变量 > `tray.json` > 默认），`tray.json` 缺失或损坏时退回默认。验证：表驱动单元测试覆盖各优先级与损坏文件
- [x] 1.2 `bridge.go`：子进程 stdio 包装为 `net.Conn`、退出码 3 且无输出 → `ErrNotRunning`、`Close` 超时结束进程；Windows 进程创建在 `*_windows.go`（`CREATE_NO_WINDOW`、`WSL_UTF8=1`、argv）。验证：Linux 上以 `webmux bridge` 子进程替代 `wsl.exe` 对真实服务完成 REST 请求与 WebSocket 附着；服务未运行时得到 `ErrNotRunning`
- [x] 1.3 `paths.go`：Windows 路径分类与换算（盘符 → `wslpath -a -u`、同名发行版 UNC → Linux 路径、其他报错、`/`/`~` 原样）。验证：表驱动单元测试覆盖 `C:\a b\c`、相对路径、`\\wsl.localhost\Ubuntu\home\u`、`\\wsl$\ubuntu\tmp`、其他发行版、`\\server\share`、`\\?\C:\x`
- [x] 1.4 若 #82 已先实现桥接拨号器，则把它迁入 `internal/wslclient` 并让托盘改用；否则 #82 直接使用本包。验证：`go build ./...` 与两边单元测试通过

## 2. 附着核心与控制台适配

- [x] 2.1 确认 `internal/attach` 核心只依赖 `localclient` 与标准库，必要时把终端相关代码拆到 `term_unix.go`。验证：`GOOS=windows go build ./internal/attach` 成功，Linux 上 #67 的附着测试不变通过
- [x] 2.2 `term_windows.go`：进入/恢复控制台模式与 UTF-8 代码页、VT 输入读取、250 ms 尺寸轮询、`SetConsoleCtrlHandler` 恢复。验证：`GOOS=windows go vet` 通过；手动验收 4.2 中控制台相关条目

## 3. 子命令（`cmd/webmux-windows`）

- [x] 3.1 子命令分发、`--distro`/`--webmux` 全局参数、退出码约定（0/1/2/3 与会话退出码）、`version`；非 Windows 占位入口。验证：Linux 上 `go build ./...` 通过；Windows 交叉编译成功
- [x] 3.2 `run`：`--name`、`--cwd`、`--env`、`--detach`、`--` 后 argv 原样、cwd 换算、服务未运行退出码 3、`env` 不被支持时报错、默认附着。验证：以注入的拨号器对 `httptest`/真实 `api.New` 的单元测试断言请求体中的 argv/cwd/env 与 `--detach` 输出
- [x] 3.3 `attach`：非控制台 stdin 拒绝、默认取得控制、`--observe`、分离键、退出码透传、连接中断提示。验证：附着核心的单元测试 + 手动验收
- [x] 3.4 `ls [--json]`、`open <id> [--browser]`、`status [--json]`（服务未运行时改为 `wsl.exe ... webmux status --json`）。验证：单元测试断言 JSON 原样输出、链接不含 Token

## 4. 构建、文档与验收

- [x] 4.1 `Makefile` 的 `release` 增加 `webmux-windows-{amd64,arm64}.exe`；`.github/workflows/test.yml` 增加交叉编译与 `GOOS=windows go vet`。验证：`make release` 产出两个 exe，PR 上 CI 绿色
- [ ] 4.2 在真实 Windows 11 + WSL 2 手动验收：PowerShell 与 Windows Terminal 中 `run -- claude` 从 `C:\` 目录启动并附着、窗口缩放跟随、`Ctrl+]` 分离后手机接管再 `attach` 回来、`Ctrl+C` 发送给会话、会话退出码透传、关闭 localhostForwarding 后仍可用、服务未运行时退出码 3、`\\server\share` 目录报错、托盘设置的发行版被沿用、全部输出不含 Token
- [x] 4.3 `docs/usage.md` 增加“在 Windows 终端中使用”一节（安装位置、发行版选择、路径换算、PowerShell 引号注意事项）。验证：`make format-check` 通过
