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

## 2026-08-11T21:36:00+08:00 | Phase A/B | Tracker transition and toolchain gate

- **操作人/工具**：Codex；本地 runbook/Git 只读核对与文档更新。
- **做了什么**：完成 A-001 并记录 runbook OS commit `8d60aa5`；启动 B-001 toolchain recovery，确认 `go`、`gofmt` 不在 PATH 或已知本地路径；将 C-001/D-001 标记为依赖该问题的 BLOCKED。
- **验证命令**：`command -v go`、`command -v gofmt`、`go version`、`git status --short --untracked-files=all`、`git diff --check`。
- **结果**：A-001 DONE；B-001 BLOCKED by `ISSUE-TOOLCHAIN-001`；现有 managed-route source batch 未被清理、覆盖或提交。
- **是否修改代码**：否；仅更新 tracker、issue、verification 和 progress 文档。
- **是否 commit/push**：待创建本次 docs-only tracking update commit；未 push。
- **是否部署/重启**：否。
- **是否修改 Nginx/systemd/SQLite/DNS**：否；未 SSH BWG/NOSLA。
- **是否有 secret 泄露**：否。
- **下一步建议**：恢复已存在的 Go toolchain 后重试 B-001，再依次验证并提交 C-001/D-001；不安装依赖、不伪造验证。

## 2026-08-11T21:42:00+08:00 | Phase B | Autonomous Go toolchain recovery started

- **操作人/工具**：Codex；本地 PATH、文件系统和工具管理器只读检查。
- **做了什么**：按 owner 授权将 B-001 从 BLOCKED 改为 IN_PROGRESS；扩大搜索到标准 Go 安装目录、用户目录、mise/asdf/gvm/brew/apt/dnf/yum/apk/pacman/nix/devbox 和项目脚本。
- **验证命令**：`command -v go`、`command -v gofmt`、`go version`、PATH/常见目录/manager/package source checks。
- **结果**：恢复尝试进行中；在确认所有安全自助路径前不停止，不提交未验证源码。
- **是否修改代码**：否；仅更新 tracker、issue、verification 和 progress 文档。
- **是否 commit/push**：否。
- **是否部署/重启**：否。
- **是否修改 Nginx/systemd/SQLite/DNS**：否；未 SSH BWG/NOSLA。
- **是否有 secret 泄露**：否。
- **下一步建议**：完成本机工具链搜索；若发现可用工具链则执行 B-001 验证并继续 C-001/D-001，否则记录穷尽证据和恢复路径。

## 2026-08-11T21:48:00+08:00 | Phase B | Local Go package recovery attempt

- **操作人/工具**：Codex；本地 `apt-cache`/`apt-get download` 计划与临时目录解包。
- **做了什么**：确认 `golang-go` 软件包候选存在；由于当前用户无免密 sudo，选择不修改系统包数据库，尝试下载并解包到用户可写临时目录后临时更新 PATH。
- **验证命令**：`apt-cache policy golang-go`、`apt-cache depends golang-go`、`sudo -n true`，随后执行临时包下载、`dpkg-deb -x`、`go version`、`gofmt` 检查。
- **结果**：恢复尝试进行中；结果以实际命令为准。
- **是否修改代码**：否；仅更新 recovery evidence 文档。
- **是否 commit/push**：否。
- **是否部署/重启**：否。
- **是否修改 Nginx/systemd/SQLite/DNS**：否；未 SSH BWG/NOSLA。
- **是否有 secret 泄露**：否。
- **下一步建议**：完成临时工具链验证；成功后执行 B-001/C-001/D-001 验证，失败则写明包源/网络限制和下一恢复路径。

## 2026-08-11T21:56:00+08:00 | Phase B/C | Go toolchain recovered; storage verification started

- **操作人/工具**：Codex；本地 apt package download、`dpkg-deb -x`、临时 Go toolchain。
- **做了什么**：从已配置 apt source 下载并解包 Go 1.26 到 `/tmp/embyproxy-go-recovery.gpw7iY`；确认 `go`/`gofmt` 可用；将 B-001 标记 DONE、C-001 标记 IN_PROGRESS。
- **验证命令**：`apt-cache policy golang-go`、`apt-get download golang-1.26-go golang-src`、`dpkg-deb -x`、`go version`、`gofmt -h`。
- **结果**：B-001 recovery 成功；系统包数据库未修改。即将执行 changed Go files 的 gofmt 和 `go test ./internal/storage`。
- **是否修改代码**：尚未由本恢复动作修改源码；后续 gofmt 仅限当前 dirty batch。
- **是否 commit/push**：否。
- **是否部署/重启**：否。
- **是否修改 Nginx/systemd/SQLite/DNS**：否；未 SSH BWG/NOSLA。
- **是否有 secret 泄露**：否。
- **下一步建议**：完成 C-001 storage 格式化与 targeted test，记录结果后再进入 D-001。

## 2026-08-11T22:03:00+08:00 | Phase C | First storage test exposed incomplete temporary GOROOT

- **操作人/工具**：Codex；临时 Go 1.26 toolchain。
- **做了什么**：对当前六个 changed Go files 执行 gofmt，并运行 `go test ./internal/storage`。
- **验证命令**：`gofmt -w ...` 通过；`go test ./internal/storage` 启动但因临时 GOROOT 缺少标准库源码而失败。
- **结果**：源码未提交；问题归入 `ISSUE-TOOLCHAIN-001` recovery attempt 3，准备补取版本化 standard library source。
- **是否修改代码**：是；仅 gofmt 当前已登记 dirty source batch。
- **是否 commit/push**：否/否。
- **是否部署/重启**：否。
- **是否修改 Nginx/systemd/SQLite/DNS**：否；未 SSH BWG/NOSLA。
- **是否有 secret 泄露**：否。
- **下一步建议**：补齐临时 GOROOT 后重跑 C-001 storage test；失败则记录实际测试错误并停止该 step。

## 2026-08-11T22:12:00+08:00 | Phase C/D | Managed route storage completed; Admin API verification started

- **操作人/工具**：Codex；临时 Go 1.26 toolchain。
- **做了什么**：补齐 `golang-1.26-src` 后重跑 storage 测试；C-001 storage foundation 通过并提交为 `98229bd`；将 D-001 Admin API verification 标为 IN_PROGRESS。
- **验证命令**：`gofmt` storage files、`go test ./internal/storage`、`git diff --check`。
- **结果**：C-001 DONE；storage package tests PASS；admin/proxyadapter dirty batch 尚未提交，等待 D-001 targeted tests。
- **是否修改代码**：是；C-001 source commit 已创建，D-001 source changes 仍未提交。
- **是否 commit/push**：`98229bd` / 未 push。
- **是否部署/重启**：否。
- **是否修改 Nginx/systemd/SQLite/DNS**：否；未 SSH BWG/NOSLA。
- **是否有 secret 泄露**：否。
- **下一步建议**：运行 `go test ./internal/admin ./internal/proxyadapter`，通过后提交 D-001。

## 2026-08-11T22:28:00+08:00 | Phase D | Managed route Admin API completed

- **操作人/工具**：Codex；临时 Go 1.26 toolchain。
- **做了什么**：修复 `ISSUE-ADMIN-001` 的 error 类型问题；完成 authenticated managed-route GET/PUT/DELETE API、输入校验和测试；提交 D-001 为 `0b7b590`。
- **验证命令**：`gofmt`；`go test ./internal/admin ./internal/proxyadapter`；`go test ./...`；`go vet ./...`；`git diff --check`。
- **结果**：全部通过；C-001/D-001 DONE。Admin UI、runtime loading 和后续集成尚未开始。
- **是否修改代码**：是；D-001 source/test files 已提交。
- **是否 commit/push**：`0b7b590` / 未 push。
- **是否部署/重启**：否。
- **是否修改 Nginx/systemd/SQLite/DNS**：否；未 SSH BWG/NOSLA。
- **是否有 secret 泄露**：否。
- **下一步建议**：将 E-001 标为 IN_PROGRESS，检查现有 embedded Admin UI 的结构，设计最小 managed-route editor，不改变认证边界。

## 2026-08-11T22:34:00+08:00 | Phase E | Admin UI integration started

- **操作人/工具**：Codex；本地 embedded HTML/JS/CSS 只读审查。
- **做了什么**：将 E-001 标记为 IN_PROGRESS，开始定位现有 Admin UI 的 tab、node editor、API 请求和样式扩展点。
- **验证命令**：`rg`/`sed` UI structure search；无外部系统访问。
- **结果**：待确定最小 managed-route editor 入口和无需额外 frontend build 的验证方式。
- **是否修改代码**：尚未修改 UI。
- **是否 commit/push**：否/否。
- **是否部署/重启**：否。
- **是否修改 Nginx/systemd/SQLite/DNS**：否；未 SSH BWG/NOSLA。
- **是否有 secret 泄露**：否。
- **下一步建议**：完成 UI 结构盘点后实现一个受现有认证保护的 managed-route CRUD editor，并增加静态契约测试或人工 review 证据。

## 2026-08-11T23:05:00+08:00 | Phase F | Runtime resolver and flag fallback verified

- **做了什么**：审计并验证已有 `proxyRouteHandler`、`StorageResolver` 与 `MEDIAPROXY_ROUTES_ENABLED` wiring；补充未知 flag 值 fail-closed，以及 flag 开启但 storage 不可用时继续 legacy fallback 的回归测试。
- **验证命令**：`gofmt -w internal/config/config_test.go cmd/embyproxy/main_test.go`；`go test ./internal/config ./cmd/embyproxy ./internal/proxyadapter`；`go test ./...`；`go vet ./...`；`git diff --check`。
- **结果**：全部 PASS；F-001 source/test commit 为 `29118fb`。未改变默认关闭和 legacy fallback 语义。
- **下一步**：进入 G-001，执行 managed route 到 mediaproxy 的本地 HTTP/Range/WebSocket/header/rewrite/redaction 集成验证。
- **是否 push/部署/重启/SSH**：否；仅本地验证和 commit，未 push、未部署、未重启、未 SSH BWG/NOSLA。

## 2026-08-11T23:18:00+08:00 | Phase G/H | Runtime integration and persistence evidence completed

- **G-001**：managed route 本地 httptest 验证覆盖 HTTP、Range、WebSocket、Location/Content-Location rewrite、fallback、unsafe target rejection 与 request redaction；新增断言后提交 `4e60097`。
- **H-001**：新增 SQLite store close/reopen 后 route/line persistence 测试，提交 `fc00c61`；未触碰生产数据库或外部系统。
- **验证**：`go test ./internal/proxyadapter ./internal/mediaproxy`、`go test ./internal/storage ./internal/proxyadapter`、`go test ./...`、`go vet ./...`、`git diff --check` 均 PASS。
- **下一步**：I-001 full regression/security hardening；完成后再进入 release/docs hygiene。
- **安全边界**：未 push、未部署、未重启、未 SSH BWG/NOSLA；未输出或写入 secret。

## 2026-08-11T23:28:00+08:00 | Phase I | Regression and security hardening completed

- **验证**：proxyadapter redaction tests、authenticated admin route/auth tests、`go test ./...`、`go vet ./...`、`git diff --check` 全部 PASS。
- **审计**：未发现 tracked secret-named files 或 minified/bundled/WASM/source-map/generated artifacts；生产日志路径使用 redacted request URI helper，未发现新增敏感值输出路径。
- **结果**：I-001 DONE；GAP-PROV-002、failover provider/traffic/policy 等 release/后续 phase gaps 仍按 gap log 保留，未被本轮误关闭。
- **下一步**：J-001 release/docs hygiene 记录与人工权利人确认边界；该步骤不阻塞 implementation，但阻塞正式 public distribution。
- **安全边界**：未 push、未部署、未重启、未 SSH BWG/NOSLA。

## 2026-08-11T23:38:00+08:00 | Phase J/K | Release hygiene boundary and delivery preparation

- **J-001**：本地 evidence matrix、GAP-PROV-002 和 delivery checklist 已对齐；无法在没有 owner/rightsholder 输入的情况下补写权利人、上游 revision/license、逐文件 provenance 或 SBOM，故 `ISSUE-PROV-002` 仅阻塞正式 release/public distribution，不阻塞 implementation。
- **K-001**：开始本地交付 gate reconciliation；将核对 tracker、issue、verification、progress、gap、delivery checklist、工作区和提交路径。
- **安全边界**：不 push、不部署、不重启、不 SSH BWG/NOSLA；发布仍须单独的 BWG publish bridge gate。

## 2026-08-11T23:45:00+08:00 | Phase K | Local delivery reconciliation completed

- **核对**：源码工作区 clean；当前未提交改动仅为本轮 runbook evidence 文档；路径 diff check PASS；tracker、issue、verification、progress、gap、delivery checklist 已互相对齐。
- **状态**：A-001 至 I-001 DONE；J-001/`ISSUE-PROV-002` 仅为 release/public distribution provenance blocker；K-001 local delivery preparation DONE。
- **未执行**：没有 push、bundle transfer、部署、重启、SSH BWG/NOSLA、DNS/Nginx/systemd 或生产 SQLite 操作。
- **下一步**：停止等待人工 review；如需发布，必须另行进入 approved docs/source commit 的 BWG publish bridge gate；如需正式 public release，先解决 J-001 owner/rightsholder evidence。

## Local delivery handoff package

- Added `16-local-delivery-handoff.md`.
- Documented delivered scope, key commits, verification results, manual review focus, known risks, publish boundary, and rollback principle.
- This is a documentation-only handoff update; no source changes were made.
- No push, deployment, restart, or SSH was performed.

## BWG publish bridge readiness check | BLOCKED

- **本地检查**：branch `feature/failover-phase2-local`、目标 HEAD `53f7437`、worktree clean；remote feature ref 已刷新为旧目标 `c7f475c`，本地 ahead。
- **最终验证**：`go test ./...`、`go vet ./...`、`git diff --check` PASS；路径范围和 main/master 目标检查未发现不安全目标。
- **阻塞现象**：直接 `git push --dry-run` 因本地 publisher authentication 不可用而失败；实际 push 未执行。
- **规则结论**：Codex 不得绕过 runbook 的 BWG publish bridge rule 直接 push GitHub；本 gate 同时禁止 SSH BWG/NOSLA，因此不能在本轮完成桥接发布。
- **恢复路径**：等待人工在明确授权的 BWG gate 中转移并验证 bundle，或重新进入允许 BWG SSH/SCP 的 publish bridge gate；不得修改 remote/auth，不得改用其他 publisher。
- **外部影响**：未 push、未 force push、未部署、未重启、未 SSH BWG/NOSLA；未输出 secret/token/cookie/password 等敏感信息。

## BWG publish attempt 2 | merged, stopped before push

- Bundle verification, BWG base/status, quoting-safe path whitelist, and `git merge --ff-only` passed; BWG branch advanced from `c7f475c` to `61de764`.
- A script assertion incorrectly expected the remote feature ref to equal the new target before push; it stopped before `git push`.
- Remote feature ref remains `c7f475c`; BWG worktree is clean at `61de764`; temporary ref/bundle cleanup completed.
- Recorded as `ISSUE-PUBLISH-003`; next retry must assert remote=base before push and remote=target after push.

## BWG publish bridge success

- Final BWG bridge attempt passed bundle verification, path whitelist, branch/status checks, and `git merge --ff-only`.
- BWG advanced through the verified fast-forward chain to `5cbbe54`.
- Before push, remote `feature/failover-phase2-local` was verified at base `c7f475c`; after push it was verified at `5cbbe54`.
- Only `feature/failover-phase2-local` was pushed. No force push, main/master push, deployment, restart, production change, or NOSLA SSH occurred.
- The dedicated BWG temp ref and bundle were cleaned after verification.
- `ISSUE-PUBLISH-001` is resolved for this publish; `ISSUE-PUBLISH-002` and `ISSUE-PUBLISH-003` are resolved script-attempt records.
- Next step: publish this runbook result commit through the same BWG bridge, then stop for human/PR review.

## 2026-08-11T22:20:00+08:00 | Phase D | Admin API compile issue found

- **操作人/工具**：Codex；临时 Go 1.26 toolchain。
- **做了什么**：运行 D-001 targeted tests；发现 `validateManagedRouteRequest` 的 error-code 返回类型错误。
- **验证命令**：`go test ./internal/admin ./internal/proxyadapter`；proxyadapter 通过，admin 编译失败。
- **结果**：未提交 D-001；问题已登记为 `ISSUE-ADMIN-001`，准备使用 `errors.New` 包装稳定错误码。
- **是否修改代码**：否；待 issue 记录后修复。
- **是否 commit/push**：否/否。
- **是否部署/重启**：否。
- **是否修改 Nginx/systemd/SQLite/DNS**：否；未 SSH BWG/NOSLA。
- **是否有 secret 泄露**：否。
- **下一步建议**：修复 error 类型后重跑 targeted tests，再执行 full test/vet。

## 2026-08-11T22:52:00+08:00 | Phase E | Managed-route Admin UI completed

- **操作人/工具**：Codex；本地 embedded HTML/JavaScript 和 Go 静态契约测试。
- **做了什么**：在 `internal/admin/static/index.html` 增加托管路由 tab；支持路由列表、新建/编辑、线路增删、保存、删除、刷新；所有请求沿用 same-origin authenticated session，退出登录时清理受保护路由状态。
- **测试**：新增 `internal/admin/managed_routes_ui_test.go`，检查 tab、API endpoint、CRUD handlers、same-origin credentials 和敏感凭据不嵌入。
- **验证命令**：`gofmt -w internal/admin/managed_routes_ui_test.go`；`go test ./internal/admin ./internal/proxyadapter`；`go test ./...`；`go vet ./...`；`git diff --check`。
- **结果**：全部 PASS；UI source/test commit 为 `8c00f1a`。项目无独立 frontend build，浏览器人工复核仍列入交付清单。
- **下一步**：进入 F-001，接通 runtime managed-route resolver，并保持 legacy path 在 feature flag 关闭/未知时可用。
- **是否修改源码**：是；仅本地实现与测试。
- **是否 commit/push**：已本地 commit `8c00f1a` / 未 push。
- **是否部署/重启/SSH**：否；未部署、未重启、未 SSH BWG/NOSLA。

## Owner-authorized BWG publish bridge execution

- Owner explicitly authorized the documented BWG publish bridge for the current `feature/failover-phase2-local` branch only.
- Scope: create and verify one local Git bundle, transfer it only to BWG alias `bwg`, validate it in `/root/staging/embyproxy-staging`, fast-forward only, and push only the matching feature ref.
- Prohibited: NOSLA SSH, deployment, restart, production configuration/traffic changes, force push, main/master push, remote/auth changes, and secret output.
- The next publish attempts and results must be recorded here, in the tracker, issue log, and verification matrix before stopping.

## BWG publish attempt 1 | stopped before merge

- Local bundle and BWG branch/base/status/bundle verification passed.
- The remote path whitelist shell command failed because of an `awk` quoting parse error.
- No merge, push, deploy, restart, or traffic/configuration action occurred.
- Temporary BWG ref and bundle were cleaned; BWG worktree remained clean.
- Recorded as `ISSUE-PUBLISH-002`; retry will use a quoting-safe whitelist check and repeat all gates.

## Autonomous BWG sidecar deployment started

- Owner accepted the deployment risk and authorized autonomous self-use deployment.
- Deployment target is BWG alias `bwg`, using the independent localhost sidecar
  boundary documented in `18-deployment-execution-plan.md`.
- Added deployment plan, execution log, rollback plan, and runtime healthcheck.
- First gate is read-only BWG preflight; no server mutation has occurred.
- Required tracker, issue log, verification matrix, and delivery checklist updates
  are being made before each subsequent gate.

## DEPLOY-001 BWG read-only preflight completed

- Verified the BWG staging checkout, feature branch at `e0f2bb6`, and clean state.
- Verified port 18082, the sidecar service name, and independent release/config/data/log paths are free.
- Verified disk capacity, active Nginx/rathole, and a passing `nginx -t`.
- No server file, service, Nginx, DNS, traffic, or NOSLA change occurred.
- DEPLOY-001 is DONE; DEPLOY-002 artifact/backup preparation is IN_PROGRESS.

## DEPLOY-002 local artifact prepared

- Built and verified a static Linux amd64 binary from the `e0f2bb6` source tree.
- Confirmed later local commits only affect runbook documentation.
- Recorded the build metadata and SHA-256 in the deployment execution log.
- No remote mutation has occurred; backup manifest and artifact transfer are next.

## DEPLOY-002 backup and release preparation completed

- Created a timestamped first-deploy baseline manifest.
- Uploaded and checksum-verified the `e0f2bb6` static artifact.
- Created only the dedicated service user and independent release/config/data/log paths.
- Installed and verified the audited systemd unit; generated the credential on BWG without output.
- Service remains inactive and disabled; port 18082 remains free and no database exists.
- DEPLOY-002 is DONE; DEPLOY-003 start and localhost health/smoke checks are IN_PROGRESS.

## DEPLOY-003 smoke attempt 1 requires retry

- The sidecar start gate passed and it remains active on loopback only.
- A temporary fixture readiness race made the first full smoke script inconclusive.
- Cleanup passed; Nginx and rathole remain active; no DNS, traffic, or Nginx change occurred.
- Recorded `DEPLOY-SMOKE-001`; DEPLOY-003 remains IN_PROGRESS for a diagnostic retry.

## DEPLOY-003 smoke attempt 2 isolated the cause

- Admin UI, unauthenticated rejection, login, and managed-route create/list passed.
- The proxy executor correctly blocked the loopback fixture as a private target.
- No SSRF control will be weakened; cleanup and existing service checks passed.
- A final read-only smoke will use the documented owner-controlled public Emby entry.

## DEPLOY-003 runtime health completed

- Sidecar is active/enabled on BWG at `127.0.0.1:18082` only.
- Admin UI/auth, managed-route CRUD, owner-controlled public upstream proxy,
  fail-closed disabled route, cleanup, and log redaction checks passed.
- Existing Nginx/rathole remained active; `nginx -t` passed.
- No Nginx, DNS, traffic, existing service, NOSLA, or full-host reboot action occurred.
- DEPLOY-003 is DONE; DEPLOY-004 documentation closeout is IN_PROGRESS.

## DEPLOY-004 deployment closeout completed

- Final local and BWG runtime verification passed.
- The isolated BWG localhost sidecar is usable; release/config/data/log/backup paths are recorded.
- Rollback remains scoped to the new unit/assets and the first-deploy manifest is retained.
- DEPLOY-001 through DEPLOY-004 are DONE.
- The deployment record still needs publication through the documented BWG feature-branch bridge.

## Deployment record BWG publish bridge completed

- Published the deployment closeout target `1f60e1c` through BWG only.
- BWG staging validated the bundle, allowed only `docs/fusion-project-runbook/*`
  and `deploy/*`, merged fast-forward-only, and pushed only
  `feature/failover-phase2-local`.
- Remote feature ref verified at `1f60e1c`; temporary ref and bundle were cleaned.
- No force push, main/master push, deployment mutation, restart, DNS, traffic,
  Nginx block, or NOSLA SSH occurred during publishing.

## Post-deploy stabilization started

- Owner accepted the deployment and authorized a read-only stabilization phase.
- Added the owner localhost/SSH-tunnel access guide and stabilization log.
- `POSTDEPLOY-001` is IN_PROGRESS; service restart/reload is not planned unless an
  observed sidecar fault requires recovery.
- No DNS, public traffic, existing Nginx block, rathole mapping, or NOSLA action is authorized.

## POSTDEPLOY-001 initial check requires diagnosis

- The first composite read-only script exited before summary output.
- No remote file, service, Nginx, DNS, traffic, rathole, or NOSLA mutation occurred.
- Recorded `POSTDEPLOY-ISSUE-001`; stabilization remains IN_PROGRESS.

## POSTDEPLOY-ISSUE-001 diagnosis

- All runtime/ref/service/listener/log checks passed.
- The first-deploy manifest was valid metadata but formatted as one line, causing
  exact field checks to fail.
- Only manifest normalization is pending; no service restart or binary/config change is needed.

## POSTDEPLOY-ISSUE-002 tunnel validation retry

- The SSH tunnel returned Admin UI data, but the validation pipeline caused a
  broken-pipe false failure.
- Tunnel cleanup passed, local port 28082 was released, and the sidecar remained active.
- The retry will capture the full response before checking the UI marker.

## POSTDEPLOY-001 stabilization completed

- Normalized the first-deploy rollback manifest to newline-delimited mode 0600.
- SSH tunnel Admin UI/auth rejection and BWG localhost auth/CRUD/upstream/fail-closed/fallback smoke all passed.
- Service remained active/enabled with `NRestarts=0`; no restart/reload was needed.
- Logs, listener boundary, rollback paths, Nginx/rathole, and `nginx -t` passed.
- POSTDEPLOY-ISSUE-001 and POSTDEPLOY-ISSUE-002 are DONE.
- POSTDEPLOY-001 is DONE; stabilization docs must be published through BWG bridge.

## Stabilization docs BWG publish completed

- Published stabilization target `1aaf193` through BWG only.
- Bundle path whitelist: `docs/fusion-project-runbook/*` only.
- BWG ff-only merge and feature-only push passed; remote feature ref verified at `1aaf193`.
- Temporary ref/bundle cleaned; no service, DNS, traffic, Nginx, rathole, or NOSLA action.

## 2026-08-12 | Day-2 operations | Self-use operations finalization

- **操作人/工具**：Codex；本地 runbook 更新与经授权的 BWG 只读稳定性检查。
- **做了什么**：建立 owner operations、troubleshooting、non-destructive backup/restore drill 和 day-2 checklist；复核 feature ref、sidecar 状态、loopback listener、日志边界、SSH tunnel 和 rollback manifest。
- **验证命令**：本地 Git/ref 检查；BWG `systemctl`、`ss`、bounded `journalctl`、health/smoke、manifest/path metadata；既有服务状态和 `nginx -t` 只读核对。
- **结果**：`DAY2-001` 已标记 IN_PROGRESS，待实际检查完成后更新；文档不改变运行时配置。
- **是否修改代码**：否；仅 day-2 runbook 文档。
- **是否 commit/push**：待检查完成后 docs-only commit，并通过 BWG bridge 发布 feature 分支。
- **是否部署/重启**：计划为否；仅在 sidecar 异常时允许 scoped recovery。
- **是否修改 Nginx/systemd/SQLite/DNS**：否；只读检查，不修改入口或数据库。
- **是否有 secret 泄露**：否；凭据、完整 query、完整 UUID 和 cookie 不进入记录。
- **下一步建议**：完成 BWG 检查；通过后关闭 `DAY2-001` 并发布运维文档。
