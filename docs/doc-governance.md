# 文档与 spec 治理

WebMux 由 Agent 主导开发，单个 Markdown 文件过长会让模型一次读取的成本过高，也会掩盖主题边界。`internal/docguard` 用可执行的测试约束 `docs/` 与 `openspec/` 的规模与树状结构；失败信息直接写给下一个 Agent，说明要拆分、合并还是重新分组。

## 适用范围

| 路径                                 | 行数/体积 | 行数下限 | 目录扇出 |
| ------------------------------------ | --------- | -------- | -------- |
| `docs/**/*.md`                       | 校验      | 校验     | 校验     |
| `openspec/specs/**/spec.md`          | 校验      | 校验     | 见例外   |
| `openspec/changes/**/*.md`（未归档） | 校验      | 不校验   | 校验     |
| `openspec/changes/archive/**/*.md`   | 不校验    | 不校验   | 不校验   |

归档变更是不可改写的记录；OpenSpec 允许在途变更的 proposal、design、tasks 与 delta 很短，因此它们没有行数下限。

## 阈值

- **行数上限**：400 行。
- **体积上限**：48 KiB（49152 字节）。行数不是唯一成本，宽表格会让 400 行以内的文件超过 150 KiB。
- **行数下限**：20 行，避免把内容拆成难以导航的碎片。`README.md` 以及与同名目录配套的树状索引（例如 `docs/protocol.md` 对应 `docs/protocol/`）豁免下限。
- **目录扇出**：`docs/` 与 `openspec/` 的每一级直接子目录不超过 20 个。

## 树状组织

### 拆分过长文件

1. 以原文件名为目录名创建同级目录，例如 `docs/protocol.md` 拆到 `docs/protocol/`，`docs/user-stories.md` 拆到 `docs/user-stories/`。
2. 按主题命名子文件，而不是按序号；每个子文件保持 20–400 行且不超过 48 KiB。
3. 保留原路径作为短索引，列出各子文件及用途；与同名目录配套的索引文件豁免行数下限。更新仓库内所有指向旧文件与旧锚点的链接。
4. 拆分后的文件可能多一层目录，所有相对链接要相应增加 `../`；`docs/user-stories/` 下的 `../web/...` 应改为 `../../web/...`。

### 合并过短文件

把内容并入语义最近的同主题文档，更新引用后删除孤立文件。不要为了满足下限而添加空话，也不要把无关主题硬塞进一个文件。

### 整理目录扇出

某一级超过 20 个直接子目录时，按语义归并到上级分组，例如 `docs/<主题>/<子主题>/`，再更新所有链接。`docs/images/` 是截图资产树，`openspec/specs/` 是 capability ID 的固定平铺结构，`openspec/changes/archive/` 是历史记录，这三处不参与扇出检查。

### 拆分 OpenSpec spec

OpenSpec 要求主规格位于 `openspec/specs/<capability>/spec.md`，不能在原目录内拆成多个文件。超过上限时，按语义建立新的 capability 目录，各自保留一个 `spec.md`，在变更中迁移需求并更新所有引用。归档记录不改写。

## 校验与修复

```sh
make docs-check          # go test -count=1 ./internal/docguard
make check               # 同时覆盖格式、前端、Go 与文档治理
```

CI 的格式/构建 job 会单独运行 `make docs-check`，让失败点直接可读。校验失败时，`internal/docguard` 会为每项违规输出：

- 文件或目录路径、实测行数/字节数/子目录数与阈值；
- 应该拆分、合并或重新分组的目标位置；
- 保留旧路径为树状索引、更新引用以及子文件规模的要求。

`internal/docguard/doc_test.go` 中的 `TestRules` 用内存目录树固定了超长、超体积、过短、扇出以及各类豁免场景；修改规则时同步更新该样例与本文件。

## 维护约定

- 新增或调整阈值、例外与提示文本时，同时更新 `internal/docguard`、本文件与 `docs/user-stories/operations-and-tooling.md` 的 US-L08。
- 不要通过放宽阈值或增加忽略标记来绕过校验；确实需要调整时，在 PR 中说明原因并同步更新测试。
- 文档按主题分层，不按版本或临时任务分层；归档的 OpenSpec 记录保持不变。
