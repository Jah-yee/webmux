## MODIFIED Requirements

### Requirement: 推送存储故障隔离

推送存储损坏、版本不支持、超限或锁冲突时，系统 SHALL 保留原文件并禁用推送，SHALL NOT 改名重建密钥或清空订阅。`GET /api/push` SHALL 报告原因 storage。会话与直接终端 SHALL 继续可用。持久化提交不确定时 SHALL 冻结推送写入，等待修复并重启。

#### Scenario: 文件损坏

- **WHEN** push.json 不是合法的受支持文档
- **THEN** 原字节保留，推送不可用，不生成新 VAPID 密钥，终端仍可用

#### Scenario: 首次安装

- **WHEN** push.json 确认不存在且锁可用
- **THEN** 创建私有密钥和空订阅并持久提交；正常重启保留密钥

#### Scenario: 目录不可写

- **WHEN** 状态目录不可写，推送不能取得锁或提交文件
- **THEN** 推送报告 storage 并停止写入，保留现有文件，会话与终端继续可用
