## ADDED Requirements

### Requirement: 真实终端身份与启动能力提示

默认会话环境 SHALL 在进程启动前提供真实的 WebMux 终端身份 `TERM_PROGRAM=webmux` 及运行服务版本 `TERM_PROGRAM_VERSION`，保持 `TERM=xterm-256color` 与 `COLORTERM=truecolor` 的默认兼容值。系统 SHALL NOT 继承宿主的 Kitty 或其他终端身份，也 SHALL NOT 为启用图形而伪造 `KITTY_WINDOW_ID` 或将默认 `TERM` 改为 `xterm-kitty`。

调用方或保存的启动配置显式提供且经过校验的变量 SHALL 按既有优先级覆盖默认值；系统 SHALL NOT 把应用专用图形变量添加到全部会话。环境提示 SHALL NOT 替代图形协议查询，也不保证未知应用自动识别 WebMux。

#### Scenario: 在真实 Kitty 中启动服务

- **WHEN** 用户从 Kitty 终端启动 WebMux 服务并创建未指定终端变量的会话
- **THEN** 子进程得到 WebMux 身份和 xterm 兼容默认值，不继承宿主的 `KITTY_WINDOW_ID` 或 Kitty 身份

#### Scenario: 显式终端变量

- **WHEN** 启动配置显式设置合法的 `TERM` 或 `TERM_PROGRAM`
- **THEN** 子进程在第一次执行应用代码前得到显式值，配置不被默认身份重写

#### Scenario: 保留现有配置与包装

- **WHEN** 升级后使用已经保存的 omp 或其他 agent 启动配置
- **THEN** 不重写配置文件，保持 argv、加载 `.bashrc` 的显式包装及用户的环境变量选择
