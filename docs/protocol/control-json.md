# 控制面 JSON 契约

`webmux version|status|stop|config ... --json` 向标准输出写入恰好一个 JSON 对象，诊断只写标准错误。所有对象带 `v`（当前 `1`）；同一版本内只增加可选字段，不删除或改变既有字段含义，不兼容变更提升 `v`。退出码：`0` 运行中且健康或操作成功，`1` 其他错误，`2` 用法错误，`3` 未运行（`status`、`bridge`）。

`version`：`{"v": 1, "version": "0.3.0", "contract": 1}`。`contract` 是该构建支持的控制契约版本，不读取配置、不启动服务；客户端据此判断兼容。不带 `--json` 的 `webmux version` 与 `webmux --version` 仍输出 `webmux <版本>`。旧版本会忽略 `--json` 并输出文本，客户端应回退解析文本。

`status`（亦为 `/local/v1/status`）：

```jsonc
{
  "v": 1,
  "state": "running", // running | stopped | unresponsive | error
  "record": true, // 来自有效运行时记录或本机 API
  "localApi": true, // 经本机 API 获取
  "pid": 1234, // 未运行时为 null
  "version": "0.1.0",
  "launcher": "manual", // manual | systemd | launchd | windows-tray
  "listen": "127.0.0.1:7681", // 实际监听地址
  "sessions": 2, // 健康时的会话数，否则为 null
  "health": { "status": "ok", "message": "" }, // ok | unauthorized | refused | timeout | not-webmux | error
  "urls": {
    "local": "http://127.0.0.1:7681",
    "remote": [
      {
        "url": "http://100.64.0.5:7681",
        "interface": "tailscale0",
        "kind": "tailscale",
      },
    ],
  },
  "tokenConfigured": true,
  "drift": [], // 配置已修改但未重启的字段：listen、token
  "warnings": [],
  "config": {
    "path": "/home/u/.config/webmux/config.toml",
    "exists": true,
    "error": "",
  },
  "logFile": "",
  "socket": "/home/u/.local/state/webmux/run/webmux.sock",
  "startedAt": "2026-01-01T00:00:00Z",
  "token": "...", // 仅 --reveal-token 且配置了 Token 时出现
  "tunnel": {
    // 仅运行中实例经本机 API 返回；未运行或旧版本实例省略
    "state": "ready", // off | starting | ready | failed
    "publicUrl": "https://abc.trycloudflare.com", // 仅 ready，不含 Token
    "error": "", // 仅 failed，失败原因
    "named": false, // 是否 Named Tunnel
    "enabled": true, // 可选：当前进程启用期望（含 CLI 覆盖），不代表磁盘或 ready
    "settingsAvailable": true, // 可选：只有 true 才提供 Web 隧道设置页入口
  },
  "push": {
    // 仅运行中实例经本机 API 返回；未运行或旧版本实例省略。不含 endpoint 或密钥
    "enabled": true, // [push] enabled
    "available": true, // 推送存储可用
    "error": "", // 仅不可用时出现
    "subscriptions": 1, // 已登记设备数
    "publicOrigin": "https://term.example.com", // 仅当前有效 Named 设置具有主机名时出现，动态跟随设置
  },
}
```

链接不带路径、查询参数或 Token。回环监听的远程链接为空数组；通配监听列出启用中的非回环、非链路本地 IPv4 地址，Tailscale（100.64.0.0/10）在前，WSL NAT 模式下的虚拟网卡地址不列出；通配监听且未配置 Token 时 `warnings` 含暴露警告。

`stop`：`{"v": 1, "result": "stopped" | "killed" | "not-running" | "refused" | "error", "message": "..."}`。

`config show`：`{"v", "path", "exists", "listen", "tokenConfigured", "defaultCwd", "stateDir", "allowedOrigins", "view", "observedAt", "fields", "stores", "instance?"}`。默认 view 为 configured，`--live` 为 effective 并包含 PID/进程身份/启动时间；不可达报错。fields 为字段名到 `{value, source}` 的映射，stores 包含 `{id, path, status, phase?, observedAt, lock}`；敏感凭据仅为布尔，不返回正文。`config path`：`{"v", "path", "exists"}`。`config validate`：`{"v", "ok", "errors": [...]}`。
