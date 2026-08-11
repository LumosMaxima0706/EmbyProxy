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

## 2026-08-11T17:57:01+08:00 | Phase 0 | 验证并固化 BWG publish bridge

- **操作人/工具**：Codex；本地 Git bundle、经明确批准的 BWG SSH/SCP、BWG Git 和 GitHub feature ref 验证。
- **做了什么**：将 approved docs-only commit `871cfc2` 打包，经 BWG 发布桥验证 bundle、临时 ref 和三文档路径白名单，随后 ff-only 更新 BWG feature branch 并 push GitHub 同名 feature branch；本 gate 把该流程写入操作守则。
- **验证命令**：本地 branch/HEAD/status/commit path 检查和 `git bundle verify/list-heads`；BWG branch/base/status、bundle/temp-ref/path whitelist、`git merge --ff-only`、目标 ref push 后只读核对。
- **结果**：BWG 从 `7d8ba77` fast-forward 到 `871cfc2`；GitHub `feature/failover-phase2-local` 到达 `871cfc2`；本次专用 BWG 临时 ref 和 bundle 已清理。未 force push，未 push `main`/`master`，未 SSH NOSLA。
- **是否修改代码**：否；本 gate 仅修改 `docs/fusion-project-runbook/07-codex-operating-rules.md` 和 `08-progress-log.md`。
- **是否 commit/push**：本 gate 未 commit、未 push；记录的上一独立 gate 已按批准 push `871cfc2`。
- **是否部署/重启**：否。
- **是否修改 Nginx/systemd/SQLite/DNS**：否。
- **是否有 secret 泄露**：否。
- **下一步建议**：人工 review 本规则；通过后单独批准 docs-only commit。Phase 0 的下一项技术任务仍是 license/source provenance audit。

## 2026-08-11T18:35:07+08:00 | Phase 0 | License/source provenance 只读审计

- **操作人/工具**：Codex；本地 Git history、tracked tree、依赖清单、source marker 和文件类型只读检查。
- **做了什么**：在 HEAD `a3c08dd` 上审计 root license/notice、既有 third-party review、上游致谢、mediaproxy 引入历史、Go dependency inventory、vendor/submodule/binary/generated 资产和强约束 license 线索；审计阶段没有修改文件。
- **验证命令**：branch/HEAD/status、`find`、`git ls-files`、`git log/show`、`git grep`、`rg`、`file`，以及最终 clean status 检查；未访问 GitHub 或其他外部网络。
- **结果**：未发现 vendored/minified/strong-copyleft 证据，但 root license/copyright、README 致谢上游 provenance、mediaproxy/proxyadapter 逐文件映射和 Go dependency license inventory 均不完整；结论为需要 docs-only gap update，不能宣称 provenance 已解决。
- **是否修改代码**：否；随后的 docs-only gate 仅更新 `04-inventory-checklist.md`、`08-progress-log.md` 和 `09-gap-log.md`。
- **是否 commit/push**：否。
- **是否部署/重启**：否。
- **是否修改 Nginx/systemd/SQLite/DNS**：否。
- **是否有 secret 泄露**：否。
- **下一步建议**：人工 review 本审计记录并取得 owner authorization decision；该审计时点尚未解除 `GAP-PROV-001`，后续决定见 owner authorization 记录。

## 2026-08-11T18:51:08+08:00 | Phase 0 | Provenance remediation skeleton

- **操作人/工具**：Codex；本地只读 evidence 结果与 docs-only 编辑。
- **做了什么**：新增 `10-provenance-evidence-matrix.md`，结构化记录 project license、README 上游致谢、mediaproxy/proxyadapter、Go direct/indirect dependencies 和 repository assets 的本地证据、缺失证据及人工确认项；同步更新 inventory 与 gap 引用。
- **验证命令**：branch/HEAD/status precheck；文档编辑后执行 `git status --short --untracked-files=all`、`git diff --name-only`、`git diff --stat`、`git diff --check`、路径白名单、blocker 关键词检查和完整 runbook diff review。
- **结果**：仅建立 evidence tracking 与 dependency inventory 占位结构；该时点所有未确认项尚待 owner decision，没有进入 Phase 3。后续状态由 owner authorization 记录更新。
- **是否修改代码**：否；仅修改 runbook 文档。
- **是否 commit/push**：否。
- **是否部署/重启**：否。
- **是否修改 Nginx/systemd/SQLite/DNS**：否；未 SSH BWG/NOSLA。
- **是否有 secret 泄露**：否。
- **下一步建议**：人工 review 本 skeleton；后续由权利人/作者逐项填充 release/docs hygiene evidence。Phase 3 状态以最新 owner authorization decision 为准。

## 2026-08-11T21:08:03+08:00 | Phase 0 / Phase 3 kickoff | Owner authorization decision

- **操作人/工具**：Project owner 授权；Codex 仅更新 runbook。
- **做了什么**：记录 owner 已授权为融合目标修改、重构、改写和集成相关项目源码，无需在 implementation 前逐文件等待 provenance 批准；将未完成的 license/provenance/SBOM/notice 工作迁移到 `GAP-PROV-002` release/docs hygiene tracker。
- **验证命令**：branch/HEAD/status、文档路径白名单、`git diff --check`、状态关键词检查和 docs-only cached diff review。
- **结果**：`GAP-PROV-001` 不再阻塞 Phase 3；Phase 3 implementation may begin。授权不表示所有 license/provenance 细节已解决，正式 release/public distribution 仍受 `GAP-PROV-002` 约束。
- **是否修改代码**：否；本记录所属 docs-only unblock commit 只包含四个 runbook 文档。
- **是否 commit/push**：计划创建 docs-only unblock commit；不 push。
- **是否部署/重启**：否。
- **是否修改 Nginx/systemd/SQLite/DNS**：否；未 SSH BWG/NOSLA。
- **是否有 secret 泄露**：否。
- **下一步建议**：提交 docs-only authorization decision 后，开始第一批最小、可测试的 Phase 3 implementation；release hygiene 并行跟踪但不阻塞 coding。

## 2026-08-11T21:15:43+08:00 | Phase 3 | Managed route admin API implementation attempt

- **操作人/工具**：Codex；本地源码编辑，未访问外部系统。
- **做了什么**：实现 managed route storage 的 list/save/delete 事务接口；新增认证后的 `/api/admin/managed-routes` GET/PUT/DELETE API；复用 proxyadapter 的 route/node/target 校验；新增 storage 与 admin API 测试。
- **验证命令**：`gofmt -w` 和 `go test ./internal/storage ./internal/proxyadapter ./internal/admin` 已尝试；当前环境没有 `go` 或 `gofmt` 命令，因此验证未执行，`git diff --check` 已通过。
- **结果**：源码改动仍未形成 commit；由于缺少 Go toolchain，不能声明测试通过，也不能安全提交 implementation commit。
- **是否修改代码**：是；`internal/storage/managed_routes.go`、`internal/storage/managed_routes_test.go`、`internal/proxyadapter/validation.go`、`internal/admin/admin.go`、`internal/admin/managed_routes_api.go`、`internal/admin/managed_routes_api_test.go`。
- **是否 commit/push**：否/否。
- **是否部署/重启**：否。
- **是否修改 Nginx/systemd/SQLite/DNS**：否；未 SSH BWG/NOSLA。
- **是否有 secret 泄露**：否。
- **下一步建议**：在具备现有 Go toolchain 的本地环境运行 gofmt、targeted tests、`go test ./...` 和 `go vet ./...`；若通过，再 review 并创建 implementation commit。不要安装依赖或绕过验证。

## 2026-08-11T21:32:12+08:00 | Phase A | 建立 full-project executable runbook

- **操作人/工具**：Codex；本地 Git、源码结构和 runbook 文档检查。
- **做了什么**：要求并建立从 repo normalization 到最终 delivery 的 master plan、step execution tracker、issue resolution log、verification matrix 和 delivery checklist；把项目从单点 Phase 3 切换为全项目 stepwise execution。
- **验证命令**：branch/HEAD/status/log、dirty path inventory、`command -v go`、`command -v gofmt`、`git diff --check` 和 runbook path checks。
- **结果**：所有阶段、任务依赖、验收标准、验证命令和 rollback 边界已落盘；A-001 当前 IN_PROGRESS。Go/gofmt 仍不可用，问题已登记到 `ISSUE-TOOLCHAIN-001`；现有 managed-route source batch 未被丢弃。
- **是否修改代码**：否；本轮 runbook 文档更新不修改源码内容。
- **是否 commit/push**：待创建 runbook-only commit；未 push。
- **是否部署/重启**：否。
- **是否修改 Nginx/systemd/SQLite/DNS**：否；未 SSH BWG/NOSLA。
- **是否有 secret 泄露**：否。
- **下一步建议**：提交 runbook operating system 后完成 A-001，进入 B-001 toolchain recovery；若仍不可用，保持源码步骤 BLOCKED 并等待具备 Go toolchain 的本地验证环境。
