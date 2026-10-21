# 产品能力 baseline

WebMux 的核心价值是从手机或浏览器持续操控服务器上的 CLI agent。baseline 优先测量这条链路：创建真实 PTY → 输入 → 读取输出 → 断开 → 回放 → 继续操控。通过率和延迟分开报告，不合成任意权重的总分。辅助能力覆盖 worktree 生命周期、认证与 Origin、需要人工处理的识别。

## 运行

在仓库根目录运行，要求 Go 1.26+、Node.js 24 LTS、pnpm 12.11.2、Git、Linux/macOS、`/bin/bash` 及 `stty`、`cat`、`dd`、`sleep`。`make baseline` 会先安装锁定的前端依赖并构建嵌入资源，再启动采样；直接执行 `go run ./cmd/baseline` 前需先运行 `make web`。无需下载或调用真实收费 agent，也无需启动开发服务。前端构建耗时不计入 baseline 的采样结果。

```sh
make baseline
# 快速验证评测工具；不能作为正式性能基线
make baseline BASELINE_SAMPLES=2 BASELINE_OUTPUT=/tmp/webmux-smoke.json
# 保存另一次正式采样，避免覆盖已入库的基准
make baseline BASELINE_OUTPUT=baseline/results/candidate.json
```

每个性能场景默认 30 次；仅回显有 3 次不计入统计的预热。认证使用 22 个固定用例，状态识别使用 8 个固定标注场景。整个 suite 顺序运行，使用临时目录和动态 loopback 端口，退出时关闭所建会话并清理目录。不要并行运行浏览器测试、构建或其他重任务再把结果当作空载基线。

API 服务与测量客户端处于同一 Go 进程（`httptest`），PTY 子进程实际运行。时间不包含 CLI 服务进程启动、浏览器加载和绘制。

评测开始时为整个进程设置临时 HOME/XDG 目录，清除继承的 `GIT_*` 设置并禁用系统/全局 Git 配置；元数据采集、初始化 helper 和 API 驱动的 worktree 操作使用同一环境。结束后恢复调用方的环境。清理会话超时或移除失败时，记录失败样本、关闭整个 fixture 并终止本轮评测，避免残留会话影响后续样本。

[CI 接入补丁](ci.patch) 可通过 `git apply baseline/ci.patch` 应用：在现有 Linux/macOS 测试步骤后运行 baseline（前序测试失败时也运行，取消时跳过），保留 30 天 JSON artifact。修改 GitHub Actions 工作流需要具有 workflow 写权限的凭据，本次 PR 保留补丁供维护者单独启用。字节、顺序、生命周期或访问控制用例失败会非零退出；状态识别语料作为质量诊断单独报告。初版不按共享 runner 的毫秒数卡合并。

## 核心能力与测量边界

| 指标                                  | 产品问题                     | 独立验证与计时范围                                                                                                                                   |
| ------------------------------------- | ---------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `session_create_single` / `loaded_8`  | 多快开始一个会话             | POST 到 201 的耗时；之后必须真的收到 PTY READY 才算成功。启动首屏时间另由浏览器测试覆盖，当前不计时                                                  |
| `input_to_output_single` / `loaded_8` | 操控是否及时                 | WebSocket 输入到收到完整 UTF-8 ACK；关闭 PTY echo，避免把输入回显当作执行结果                                                                        |
| `resize_single` / `loaded_8`          | 终端尺寸是否生效             | 发 resize 后请求 shell 执行 `stty size`，直到收到正确行列；包含一次终端回显往返                                                                      |
| `disconnect_survival_and_replay`      | 无客户端时能否继续运行       | 确认 clients=0 后通过文件放行 200 条带序号和中文的输出；回放逐字节对照，随后再验证在线输入和续流。计时仅含重连至完整首个回放帧                       |
| `reattach_during_live_output`         | 回放与实时流交界有无遗漏     | 100 条持续输出过程中断开并立即重连；重连后的 snapshot+live 到退出必须与完整独立序列完全相同。耗时含剩余生成时间，不能当纯重连延迟                    |
| `replay_2mib_after_wrap`              | 默认缓冲满了能否可靠恢复     | 输出超过 2 MiB+64 KiB 的 UTF-8/ANSI 数据，独立取最后 2 MiB 的 SHA-256 校验；计时从拨号至回放、状态、退出三帧收齐                                     |
| `live_stream_4mib`                    | 大量输出会否丢失或卡住       | raw PTY 精确传输约 4 MiB，独立校验长度和 SHA-256，必须在退出帧前收齐；计入客户端接收/拷贝/校验之前的传输耗时                                         |
| `worktree_create_launch_remove`       | 能否可靠把任务放到独立工作树 | 真实 Git 创建 worktree → 关联启动 shell → 校验 pwd/exit 7 → removal preview → safe remove → 确认路径消失；计时覆盖整个流程                           |
| `auth_origin_contracts`               | 访问边界能否保持             | 6 个 REST/WS 路径拒绝无 token、错 token、恶意 Origin；另加同源/合法正例和跨域写请求。仅是这 22 个用例的通过率，不是安全认证                          |
| `needs_human_synthetic_corpus`        | 列表里的 waiting 值得信任吗  | 8 个事先标注是否需要人操作的真实 shell/agent 侧信道场景，3.5 s 后观察 waiting；包括无人值守 sleep 负例。单独诊断，不要求所有预测正确才让传输基线通过 |

`loaded_8` 是在被测交互会话之外，8 个零客户端 PTY 各约 2 KiB/s 输出的争用场景；它不是最大并发容量，也不是慢网络客户端背压测试。负载、默认 2 MiB 缓冲、Go/OS/CPU、GOMAXPROCS、版本和 dirty 状态保存在 JSON。完整原始样本包含失败信息；性能分位数只使用成功样本，失败仍保留在通过率分母。

## 数学口径

- 延迟单位 ms：排序后 `q_p = x[ceil(p*n)]`（从 1 起算）。报告 p50、p95、均值、min/max；30 样本 p95 是第 29 个值，不声称能可靠估计 p99。
- 吞吐单位 MiB/s：`收到的正确字节数 / 2^20 / 秒数`。看 p50 和 **p05** 低端吞吐，不把偏高的 p95 当性能下限。
- 成功率：`通过次数 / 全部尝试次数`。95% Wilson 区间用 `z=1.96`：中心 `(p+z²/(2n))/(1+z²/n)`，半宽 `z*sqrt(p(1-p)/n+z²/(4n²))/(1+z²/n)`。
- 30/30 只说明此次全过，其 Wilson 下界约 88.65%。这些重复共享机器与进程，存在相关性；区间只是样本量的不确定性提示，不是生产可靠性保证。8 个固定语料的命中率也不能推广成真实 agent 准确率。
- 比较同机器、同环境、同参数的独立运行：延迟退化 `p95_new / p95_ref - 1`；吞吐退化 `1 - p05_new / p05_ref`；争用影响 `loaded_echo_p95 / single_echo_p95 - 1`。先比较正确率，再比较性能，失败样本不能用较低耗时抵消。

修复后的隔离评测见 [审查修复验证](REVIEW-FIXES.md)；首次实测的历史表格、语料误判与验证限制见 [首份评测](REPORT.md)，原始数据在 [results](results/)。后续可在专用机器积累多轮跨进程结果后，再决定可靠的性能阻断阈值。

## 尚未量化的边界

当前时间指标终点为 WebSocket 收到字节，未包含 xterm 解析/绘制、手机屏幕呈现、输入法、真实 iOS Safari、WAN RTT/丢包/切网、服务重启、长时间 soak、最大并发和进程 RSS。浏览器既有 E2E 的通过率只能补充功能证据，不能冒充这些延迟。服务重启导致会话结束是项目现有边界。baseline 不修改产品逻辑，也不因为观察到状态误判而把标签改成实现的答案。
