# Gap Log

本文件记录尚未解决的实现、测试、设计、license、部署和证据缺口。一个 gap 一条记录，不合并无关问题。

## 状态值

- `OPEN`：已确认，未开始处理。
- `INVESTIGATING`：只读核对中。
- `PLANNED`：方案已 review，等待实现批准。
- `IN_PROGRESS`：处于获批实现 gate。
- `BLOCKED`：缺少输入、授权或外部条件。
- `VERIFIED`：修复及对应验证已完成。
- `CLOSED`：证据 review 完成，不再阻塞。

## Gap 模板

### `<GAP-ID> | <简短标题>`

- **Phase**：
- **严重程度**：Critical / High / Medium / Low
- **描述**：
- **影响**：
- **发现方式**：命令、测试、review 或现场行为
- **需要修改的文件**：`TBD` 或精确路径
- **需要新增的测试**：
- **修复前禁止进入的 phase**：
- **状态**：OPEN
- **证据/链接**：
- **备注**：

## 待 Phase 0 inventory 生成

当前不预填未经核对的技术 gap。`04-inventory-checklist.md` 完成后，把所有“未实现、部分实现、仅 mock、证据不足”项转换为独立 gap。

## GAP-RUNBOOK-001 | Runbook 被 docs ignore 规则隐藏

- **Phase**：Phase 0
- **严重程度**：High
- **描述**：仓库 `.gitignore` 包含 `/docs/`，因此 `docs/fusion-project-runbook/` 不出现在普通 `git status --short --untracked-files=all` 中，也不会被普通 `git add` 纳入。
- **影响**：如果不处理，runbook 只存在于当前本地 clone，未来 clone/session 可能无法读取唯一总控入口。
- **发现方式**：`git check-ignore -v docs/fusion-project-runbook/README.md`
- **需要修改的文件**：方案 A 为 `.gitignore` 增加精确 exception；方案 B 不改文件但在获批 commit gate 使用精确 `git add -f docs/fusion-project-runbook/*.md`。方案待人工选择。
- **需要新增的测试**：commit 前执行 `git ls-files docs/fusion-project-runbook` 并核对恰好 12 个 Markdown 文件。
- **修复前禁止进入的 phase**：Phase 1
- **状态**：BLOCKED
- **证据/链接**：`.gitignore:24:/docs/`
- **备注**：当前 gate 禁止修改目录外文件、stage、commit 或 push，因此不处理 ignore 规则。
