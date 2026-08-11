# Progress Log

按时间追加，不改写历史结论。后续事实推翻旧结论时，应新增一条纠正记录并引用原记录。

## 记录模板

### `<时间> | <phase> | <简短标题>`

- **操作人/工具**：
- **做了什么**：
- **验证命令**：
- **结果**：
- **是否修改代码**：是/否；文件列表
- **是否 commit/push**：否，或列出 commit/ref
- **是否部署/重启**：否，或列出精确服务和次数
- **是否修改 Nginx/systemd/SQLite/DNS**：否，或列出精确范围
- **是否有 secret 泄露**：是/否；处置
- **下一步建议**：

## 2026-08-11T15:25:57+08:00 | Phase 0 | 创建融合项目总控手册

- **操作人/工具**：Codex，本地文件与 Git 只读命令
- **做了什么**：仅创建 `docs/fusion-project-runbook/` 及其 Markdown 手册；记录本机参考 clone 可验证事实和 UNKNOWN/TBD 边界。
- **验证命令**：本机 `git branch --show-current`、`git rev-parse`、`git status`、`git log`、`git branch --contains`、目录文件清单和范围检查。
- **结果**：runbook 已创建；canonical BWG 工作区因禁止 SSH 未现场核对。仓库 `.gitignore` 的 `/docs/` 规则会忽略本目录，普通 `git status` 不显示，已记录为 Phase 0 gap。
- **是否修改代码**：否
- **是否 commit/push**：否
- **是否部署/重启**：否
- **是否修改 Nginx/systemd/SQLite/DNS**：否
- **是否有 secret 泄露**：否
- **下一步建议**：人工 review 本 runbook，并决定后续通过精确 ignore exception 或显式 force-add 纳入版本控制；之后继续 Phase 0 inventory/gap audit，不直接进入部署。
