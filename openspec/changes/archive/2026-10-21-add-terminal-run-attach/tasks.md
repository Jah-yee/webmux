## 1. 实现

- [x] 1.1 `webmux run` 默认附着前在标准错误输出会话 ID、本机会话链接与分离键提示，标准输出保持为空。验证：`internal/sessioncmd` 单元测试断言 stderr 内容与 stdout 为空
- [x] 1.2 核对已落地的 `run/attach/ls/open`、`env` 字段与规格逐条一致（参数边界、cwd、`--detach` 输出、退出码 0/1/2/3/会话退出码、终端恢复、`--observe`）。验证：`go test -race ./internal/attach ./internal/sessioncmd ./cmd/webmux ./internal/session ./internal/api`

## 2. 文档与验收

- [x] 2.1 `docs/usage.md`“从终端启动与附着”说明附着前的提示。验证：`make format-check`
- [x] 2.2 端到端验收：原生终端 `webmux run` 启动 → `Ctrl+]` 分离 → 手机视口浏览器打开会话并输入取得控制 → 原生终端 `webmux attach` 接续，全程同一进程（PID 不变），尺寸、`Ctrl+C`、退出码与终端恢复正确，输出不含 Token
- [ ] 2.3 运行 `go vet ./...`、`go test -race ./...`、`make format-check`，CI 通过
