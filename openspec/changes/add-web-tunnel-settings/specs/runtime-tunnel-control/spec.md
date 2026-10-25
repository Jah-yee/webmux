## MODIFIED Requirements

### Requirement: 运行时开启隧道

运行中的 WebMux SHALL 接受本机客户端或网页设置开启隧道的请求，并按当前生效的隧道设置选择 Quick Tunnel 或 Named Tunnel、按与 `--tunnel` 相同的规则定位或下载 cloudflared；持久化设置存在时优先于 `[tunnel]`。请求 SHALL 立即返回并持久化 `enabled=true`，隧道状态先为 `starting`，cloudflared 注册连接后变为 `ready` 并带公网地址。开启过程 SHALL NOT 中断 TCP 服务、本机 API 或任何会话。

#### Scenario: 开启 Quick Tunnel

- **WHEN** 未启用隧道的实例有 2 个会话，当前生效设置为 Quick 且配置了 43 字符 Token，本机客户端请求开启隧道
- **THEN** 状态依次为 `starting`、`ready`，公网地址为 `https://*.trycloudflare.com`；两个会话保持运行，已连接的浏览器不断开，持久化开关为开启

#### Scenario: 重复开启

- **WHEN** 隧道处于 `starting` 或 `ready` 时再次请求开启
- **THEN** 请求成功，不启动第二个 cloudflared，公网地址不变

#### Scenario: 使用网页保存的设置

- **WHEN** 网页已保存 Named 凭据与 `hostname`，隧道仍为 `off`
- **THEN** 网页或本机客户端请求开启时使用该凭据与主机名，不读取旧的 TOML 隧道字段

### Requirement: 运行时关闭隧道

本机客户端或网页请求关闭隧道时，WebMux SHALL 尝试持久化 `enabled=false`，并无条件立即撤销公网主机名的授权（之后公网请求返回 403），再异步结束 cloudflared，状态立即变为 `off`。即使持久化失败也 MUST 撤权并停止，返回 503 与 `notPersisted=true`，明确说明关闭仅对当前进程生效。关闭 SHALL 同样适用于以 `--tunnel` 启动的隧道和网页持久化开启的隧道。隧道已关闭且存储可用时请求关闭 SHALL 成功且无副作用。

#### Scenario: 关闭后公网失效

- **WHEN** 隧道 `ready`，本机客户端请求关闭
- **THEN** 之后对原公网主机名的请求返回 403，cloudflared 进程退出，本地访问与会话不受影响，持久化开关为关闭

#### Scenario: 关闭启动时开启的隧道

- **WHEN** 实例以 `--tunnel` 启动，本机客户端请求关闭隧道后再请求开启
- **THEN** 隧道先变为 `off`，再变为 `ready` 并获得新的 Quick Tunnel 地址

### Requirement: 隧道随服务结束

WebMux 退出时 SHALL 结束 cloudflared。网页或本机客户端修改的隧道开关与配置 SHALL 持久化到状态目录，跨 WebMux 重启保留；下次启动 SHALL 在开始监听后按持久化设置恢复隧道，隧道启动失败 MUST NOT 阻止服务启动或影响会话。持久化设置不存在时 SHALL 沿用 `[tunnel]` 的启动行为。显式 `--tunnel` SHALL 只覆盖本次进程，MUST NOT 写入持久化设置。

#### Scenario: 重启后隧道关闭

- **WHEN** 运行时开启隧道后在网页关闭并重启 WebMux，持久化开关为关闭
- **THEN** 新实例的隧道状态为 `off`，持久化开关仍为关闭

#### Scenario: 重启后恢复开启

- **WHEN** 运行时开启隧道后直接重启 WebMux，持久化开关仍为开启
- **THEN** 新实例在监听开始后恢复隧道，最终为 `ready` 或带原因的 `failed`

#### Scenario: 一次性命令行覆盖

- **WHEN** 持久化开关为关闭，用户以 `--tunnel` 启动
- **THEN** 本次进程开启隧道，但不会改写持久化设置；下次普通启动仍为关闭

#### Scenario: 存储故障阻止自动暴露

- **WHEN** 网页设置损坏、不可读或锁冲突，即使 TOML 或 `--tunnel` 要求开启
- **THEN** HTTP 服务继续运行，当前隧道保持关闭；读状态报告存储不可用，不能以回退 TOML 的方式重新暴露公网
