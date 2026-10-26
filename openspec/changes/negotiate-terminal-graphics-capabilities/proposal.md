## Why

#130 已实现浏览器端 Kitty 图片渲染，并为首次预置的 omp 配置设置 `PI_FORCE_IMAGE_PROTOCOL=kitty`；这证明显式启用后的渲染可用，不证明所有应用能在启动时自动发现图形能力。PTY 程序先于浏览器附着启动，当前查询应答依赖浏览器且回放不应补答，因此需要独立规定启动能力声明、查询时序和应答归属。

## What Changes

- 本变更仅提交未来实施的规划；本次合并保留 #130 的实现、omp 专用环境变量和既有配置不覆盖策略，不把下述要求标为已经实现。
- 规定 WebMux 的真实终端身份和显式启动变量优先级，不伪造 Kitty 身份，也不把图形协议等同于 Kitty 键盘等其他扩展。
- 为虚拟会话建立固定的基础图形能力边界：仅已实现且可验证的直接传输子集；首次启动、无浏览器、断线和接管不改变该边界。
- 让有界、无图像渲染的基础能力查询应答独立于浏览器就绪；同一输出流中 Kitty 查询必须先于后续 DA1 应答，不能用历史重放或迟到应答弥补启动探测。
- 为服务端接管的查询确定唯一应答方，阻止浏览器或原生 attach 再次提交重复应答；其他依赖当前屏幕或视口的设备查询继续遵循既有控制权规则。
- 明确环境提示、标准探测和应用专用强制开关的区别；不承诺仅凭新的终端身份就让所有应用自动启用图片。

## Capabilities

### New Capabilities

- `terminal-graphics-negotiation`：会话级图形能力边界、启动及离线查询、FIFO 应答、回放静默和多客户端兼容性。

### Modified Capabilities

- `session-launch-environment`：补充真实终端身份及显式环境优先的启动声明要求，不修改原有 argv、配置引用与环境验证契约。
- `terminal-control`：对服务端接管的基础查询设置单一应答权威，保留其余设备自动回复和输入/尺寸的控制端语义。

## Impact

- 未来实现涉及 `internal/session/env.go`、`session.go`、`io.go`、`control.go` 的生命周期和 PTY 输入顺序，以及 `internal/hub`、`internal/contract`、`internal/wire` 的自动应答边界；如新增 JSON 字段，须与 `web/src/schemas.ts` 和消费者同时迁移。
- 浏览器涉及 `web/src/kitty-graphics.ts`、`web/src/hooks/terminal-graphics.ts` 与 `terminal-socket.svelte.ts`；原生客户端涉及 `internal/attach` 的自动设备回复处理。
- 仅提取必要的有界查询识别，不在 Go 后端构建完整终端模拟器或图片渲染器。配置转换和服务组装继续只由 `cmd/` 完成。
- 不新增依赖，不改变会话持久性、输入权限、认证、Origin 或现有回放截断边界。未来实施须更新 US-D12、US-D06、US-B08 等受影响故事；本规划不新增“已实现”故事。
