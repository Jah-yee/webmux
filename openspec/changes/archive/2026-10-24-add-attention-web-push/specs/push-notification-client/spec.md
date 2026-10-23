# Spec Delta

## Purpose

让用户在浏览器或手机主屏幕应用中为本设备开启、关闭和测试注意力推送，收到通知后一键回到对应会话；不满足前置条件时，给出可执行的分步操作提示，而不是提供必然失败的开关。

## ADDED Requirements

### Requirement: Service Worker 职责

前端 SHALL 提供作用域为 `/` 的 Service Worker `/sw.js`。除安装时立即激活、激活时接管已打开的同源页面外，它只处理 `push` 与 `notificationclick` 事件。它 MUST NOT 拦截 `fetch`、缓存资源或发起网络请求，也 MUST NOT 读取或持有 Token。

#### Scenario: 不影响页面请求

- **WHEN** Service Worker 已注册，页面请求 `/api/sessions` 与静态资源
- **THEN** 请求直接到达服务端，Service Worker 不参与，响应与未注册时相同

### Requirement: 收到推送时显示通知

Service Worker 收到推送后 SHALL 立即用负载中的 `notification.title`、`notification.body` 与 `notification.tag` 显示通知，显示前 MUST NOT 等待任何网络请求。负载缺失或无法解析时，SHALL 显示标题“WebMux”、正文“有会话需要关注”的通知，以免浏览器因静默推送撤销订阅。

#### Scenario: 正常负载

- **WHEN** 收到会话 `abcdefgh` 的 question 推送
- **THEN** 设备显示标题为会话标题、正文为“需要回答”的通知，同一会话的后续通知替换前一条

#### Scenario: 无法解析的负载

- **WHEN** 收到内容不是合法 JSON 的推送
- **THEN** 仍显示“WebMux / 有会话需要关注”的通知

### Requirement: 点击通知回到会话

用户点击通知时，Service Worker SHALL 关闭该通知；若负载中的会话 ID 匹配 `^[a-z2-7]{8}$`，则优先聚焦已打开 `/s/<ID>` 的窗口，其次让同源的已打开窗口导航到 `/s/<ID>`，都没有时打开新窗口。会话 ID 不合法时 SHALL 打开 `/`。Service Worker MUST NOT 打开负载给出的其他来源或任意路径。

#### Scenario: 应用未打开

- **WHEN** 手机上的 WebMux 主屏幕应用已关闭，用户点击会话 `abcdefgh` 的通知
- **THEN** 应用打开并显示 `/s/abcdefgh` 终端页

#### Scenario: 已有会话列表窗口

- **WHEN** 浏览器中已打开 WebMux 会话列表页，用户点击会话 `abcdefgh` 的通知
- **THEN** 该窗口获得焦点并导航到 `/s/abcdefgh`，不新开窗口

#### Scenario: 非法会话 ID

- **WHEN** 负载中的会话 ID 为 `../../evil`，`navigate` 为 `https://evil.example/`
- **THEN** 点击后打开本站 `/`，不访问 `evil.example`

### Requirement: 设置页推送卡片

设置页 SHALL 提供“通知推送”卡片，显示本设备的推送状态（已开启、未开启、不可用及原因）与服务端已订阅的设备数。只有当浏览器支持 Service Worker、Push API 与 Notification、页面处于安全上下文，且 `GET /api/push` 报告可用时，卡片才 SHALL 提供开启开关。

#### Scenario: 条件满足

- **WHEN** 手机从 Named Tunnel 公网地址打开的主屏幕应用进入设置页
- **THEN** “通知推送”卡片显示“本设备未开启”与开启开关

#### Scenario: 不满足条件时不提供开关

- **WHEN** 任一前置条件不满足
- **THEN** 卡片不显示可点击的开启开关，只显示原因与操作提示

### Requirement: 开启本设备推送

用户在卡片中开启推送时，前端 SHALL 在该次用户操作中请求通知权限，注册 Service Worker，用服务端公钥创建推送订阅并登记到服务端，然后在本机保存返回的订阅 ID。任一步失败时 SHALL 显示失败原因，卡片保持未开启。

#### Scenario: 首次开启

- **WHEN** 用户点击开启并在系统弹窗中允许通知
- **THEN** 服务端订阅数加 1，卡片显示“本设备已开启”和向全部设备发送测试通知的按钮

#### Scenario: 服务端拒绝来源

- **WHEN** 登记请求返回 409
- **THEN** 卡片显示服务端返回的原因与对应操作提示，浏览器侧订阅被取消

### Requirement: 关闭本设备推送

用户关闭推送时，前端 SHALL 取消浏览器侧订阅，并请求服务端删除本机保存的订阅 ID；服务端删除失败时 SHALL 显示错误，但浏览器侧订阅仍保持取消。

#### Scenario: 关闭

- **WHEN** 已开启推送的设备在卡片中关闭推送
- **THEN** 服务端订阅数减 1，之后该设备不再收到提醒

### Requirement: 测试通知按钮

服务端已有设备开启推送时，卡片 SHALL 提供“向全部 N 台设备发送测试通知”按钮，无论本设备是否开启推送或能否开启推送，以便在一台设备上检查其他设备在后台收到的通知。按钮调用测试接口并显示发送结果；本机保存的订阅 ID 出现在 `removed` 中时，前端 SHALL 清除该 ID、取消浏览器侧订阅，并提示本设备需要重新开启。服务端没有任何设备时 MUST NOT 显示该按钮。

#### Scenario: 从桌面测试手机

- **WHEN** 手机已开启推送，用户在未开启推送的桌面浏览器中点击“向全部 1 台设备发送测试通知”，推送服务接受
- **THEN** 手机收到测试通知，卡片提示“已提交给全部 1 台设备的推送服务，若数秒内未收到，请检查各设备的系统通知设置”

#### Scenario: 本设备订阅已失效

- **WHEN** 测试结果的 `removed` 包含本机保存的订阅 ID
- **THEN** 卡片提示本设备需要重新开启推送，开关回到未开启

### Requirement: 订阅同步

应用启动和打开设置页时，若通知权限已授予且已有推送订阅，前端 SHALL 把当前订阅幂等登记到服务端，并更新本机保存的订阅 ID。若浏览器订阅使用的公钥与服务端公钥不同，前端 SHALL 取消该订阅，卡片提示“推送密钥已变更，请重新开启”。没有已有订阅时，同步 MUST NOT 注册 Service Worker 或请求权限。

#### Scenario: 服务端订阅丢失

- **WHEN** 服务端推送存储被重建后，已开启推送的设备打开应用
- **THEN** 若公钥未变，订阅被重新登记；若公钥已变，卡片提示重新开启

#### Scenario: 未开启的设备

- **WHEN** 从未开启推送的浏览器打开应用
- **THEN** 不注册 Service Worker，不弹出权限请求

### Requirement: 非安全上下文提示

页面不在安全上下文时，卡片 SHALL 说明推送需要 HTTPS，并给出操作步骤：用 Cloudflare Named Tunnel 的公网链接（托盘“复制公网链接”或启动时打印的二维码）打开 WebMux，再在该地址开启推送。

#### Scenario: 通过 VPN 地址访问

- **WHEN** 手机通过 `http://100.64.0.2:7681` 打开设置页
- **THEN** 卡片说明需要 HTTPS，并列出改用 Named Tunnel 公网链接打开的步骤

### Requirement: Quick Tunnel 提示

`GET /api/push` 报告 `quick_tunnel` 时，卡片 SHALL 说明 Quick Tunnel 每次启动更换地址，无法持续接收推送，并列出改用 Named Tunnel 的步骤：

1. 在 Cloudflare 控制台创建 Tunnel，把公共主机名指向 WebMux 本地地址；
2. 在 WebMux 配置的 `[tunnel]` 中写入 `token` 与 `hostname`；
3. 重启 WebMux 并开启公网隧道；
4. 用新的公网链接打开 WebMux 并开启推送。

#### Scenario: 从 Quick Tunnel 打开

- **WHEN** 用户从 `https://abc.trycloudflare.com` 打开设置页
- **THEN** 卡片显示上述步骤，提及 `tunnel.token` 与 `tunnel.hostname`，不提供开启开关

### Requirement: 不支持来源提示

`GET /api/push` 报告 `unsupported_origin` 时，卡片 SHALL 说明推送只支持 Cloudflare Named Tunnel 公网地址与本机回环地址，并提示改用 Named Tunnel 公网链接打开。

#### Scenario: 其他 HTTPS 反向代理

- **WHEN** 页面经其他 HTTPS 反向代理打开，服务端报告 `unsupported_origin`
- **THEN** 卡片显示该说明与改用 Named Tunnel 的提示，不提供开启开关

### Requirement: iOS 主屏幕提示

在 iOS 或 iPadOS 上，页面不是以独立显示模式（主屏幕应用）运行时，卡片 SHALL 说明需要 iOS 16.4 以上，并给出步骤：在 Safari 中点“分享 → 添加到主屏幕”，从主屏幕图标打开 WebMux 并重新输入 Token（主屏幕应用不共享 Safari 的登录），再在设置页开启推送。

#### Scenario: Safari 标签页

- **WHEN** iPhone 在 Safari 标签页中从 Named Tunnel 地址打开设置页
- **THEN** 卡片显示添加到主屏幕的步骤，并说明需要重新输入 Token，不提供开启开关

### Requirement: 权限与服务端状态提示

通知权限被拒绝时，卡片 SHALL 说明需要在浏览器或系统设置中允许本站通知后刷新页面。服务端报告 `disabled` 时 SHALL 说明推送已在配置中停用（`[push] enabled = false`）；报告 `storage` 时 SHALL 说明服务端推送存储不可用并提示查看服务日志。

#### Scenario: 权限被拒绝

- **WHEN** 用户此前拒绝了本站的通知权限
- **THEN** 卡片说明需要在系统设置中允许通知，不再弹出权限请求

#### Scenario: 配置停用

- **WHEN** 服务端 `[push] enabled = false`
- **THEN** 卡片说明推送已在配置中停用，并给出 `[push] enabled` 配置项名

### Requirement: 送达限制说明

卡片 SHALL 简要说明推送是尽力送达：锁屏、省电、勿扰或设备离线超过有效期时可能延迟或丢失；无 Google Play 服务的 Android 设备不支持；桌面 Chrome、Edge 与 Firefox 需要浏览器进程在运行。会话列表中的状态仍是唯一准确来源。

#### Scenario: 查看说明

- **WHEN** 用户打开“通知推送”卡片
- **THEN** 卡片中可见上述送达限制说明
