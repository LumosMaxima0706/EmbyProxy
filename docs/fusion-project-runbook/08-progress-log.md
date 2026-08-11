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

## 2026-08-11T17:15:44+08:00 | Phase 0 | 本地 inventory 与 gap audit

- **操作人/工具**：Codex；本地 Git、源码、测试和 runbook 只读检查，随后仅更新 runbook。
- **做了什么**：在 fresh fetch 后确认 canonical 工作分支及关键 commit；盘点 mediaproxy、proxyadapter、managed routes、failover、admin API、traffic、DNS、redirect、WebSocket 和 feature flag；按 implemented/partial/mock-only/placeholder/unverified 分类并建立 gap。
- **验证命令**：`git branch --show-current`、`git rev-parse`、`git status`、`git branch -a --contains`、`git show --stat`、`git remote -v`、`git ls-tree`、`find`、`rg`、`sed`；文档变更后执行 `git diff --name-only`、`git diff --stat`、`git status --short --untracked-files=all`、`git diff --check` 和路径范围检查。
- **结果**：HEAD 与 origin 均为 `7d8ba77`；关键模块和大量 mock/unit 测试存在，但管理 API 到 managed route、production controller/scheduler、真实 health/traffic/DNS、policy 配置化及 provenance 仍有缺口。Phase 0 尚待人工 review 和 provenance 结论，未进入 Phase 3。
- **是否修改代码**：否；仅 `docs/fusion-project-runbook/04-inventory-checklist.md`、`08-progress-log.md`、`09-gap-log.md`。
- **是否 commit/push**：否。
- **是否部署/重启**：否。
- **是否修改 Nginx/systemd/SQLite/DNS**：否；未调用 admin 写接口。
- **是否有 secret 泄露**：否。
- **下一步建议**：人工 review inventory；随后在单独批准的 Phase 0 子 gate 完成 license/source provenance audit，并在进入 Phase 1 前为 High gap 指定处理顺序。
