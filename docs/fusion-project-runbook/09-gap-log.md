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

## GAP-RUNBOOK-001 | Runbook 被 docs ignore 规则隐藏

- **Phase**：Phase 0
- **严重程度**：High
- **描述**：仓库 `.gitignore` 包含 `/docs/`，因此 `docs/fusion-project-runbook/` 不出现在普通 `git status --short --untracked-files=all` 中，也不会被普通 `git add` 纳入。
- **影响**：如果不处理，runbook 只存在于当前本地 clone，未来 clone/session 可能无法读取唯一总控入口。
- **发现方式**：`git check-ignore -v docs/fusion-project-runbook/README.md`
- **需要修改的文件**：`.gitignore` 和 `docs/fusion-project-runbook/*.md`（已完成）。
- **需要新增的测试**：commit 前执行 `git ls-files docs/fusion-project-runbook` 并核对恰好 12 个 Markdown 文件。
- **修复前禁止进入的 phase**：Phase 1
- **状态**：CLOSED
- **证据/链接**：commit `7d8ba77`；`git ls-tree -r --name-only HEAD docs/fusion-project-runbook .gitignore`。
- **备注**：`.gitignore` 已只放行 runbook 目录的 Markdown；本轮 fresh fetch 后 runbook 已在 HEAD 与 origin。

## GAP-PROV-001 | License 与 source provenance 证据不足

- **Phase**：Phase 0
- **严重程度**：High
- **描述**：两个项目的 Git tree 均未找到 LICENSE/COPYING/NOTICE。当前 EmbyProxy README 声称 MIT，但尚未核对上游 license 文本、copyright notice、融合代码是复制还是自研，以及逐文件来源。
- **影响**：无法确定 attribution 和再分发要求；不得凭 README 一句话宣称 provenance audit 完成。
- **发现方式**：remote、`git ls-tree`、license 文件搜索和 README 关键词只读检查。
- **需要修改的文件**：`docs/fusion-project-runbook/01-original-goal.md`、新增 provenance 清单 `TBD`；若确认直接复制，license/notice 路径由 audit 决定。
- **需要新增的测试**：不适用；需要 commit/file/source 映射证据。
- **修复前禁止进入的 phase**：Phase 1
- **状态**：OPEN
- **证据/链接**：当前 origin/upstream refs；参考项目 HEAD `74297fd`；两棵树的 license 文件搜索为空。
- **备注**：参考 clone worktree 自身为 tracked files deleted 状态，本轮没有修复或修改，只通过 Git object 读取。

## GAP-ROUTE-001 | Managed route 缺少管理 API 写入合同

- **Phase**：Phase 1 / Phase 2
- **严重程度**：High
- **描述**：managed route schema、query 和 production resolver 已存在，但没有发现 create/update/delete storage API 或 admin route API；现有测试通过直接 SQL 插入 fixture。
- **影响**：EmbyProxy 管理面不能驱动 managed route，未完成 management-plane 到 data-plane 的核心融合链路。
- **发现方式**：全仓搜索 managed route SQL、storage method 和 admin route。
- **需要修改的文件**：设计后确定；预计涉及 `internal/storage/managed_routes.go`、`internal/admin/`、管理 UI 资源和相应 tests。
- **需要新增的测试**：认证管理 API 创建/更新 route 后进入 proxyadapter/mediaproxy；非法 target、disabled/public、default line、事务失败和敏感字段边界。
- **修复前禁止进入的 phase**：Phase 3
- **状态**：OPEN
- **证据/链接**：`internal/storage/managed_routes.go`、`internal/proxyadapter/storage_resolver.go`；INSERT 仅见测试文件。
- **备注**：需先在 Phase 1 明确 schema migration、API contract 和 rollback，不能直接补 handler。

## GAP-RUNTIME-001 | Production failover runtime 尚未接线

- **Phase**：Phase 1 / Phase 2
- **严重程度**：High
- **描述**：主程序只在显式 loopback mock fixture 下创建 failover nodes；未从管理节点/配置构造 NOSLA primary 与 BWG fallback，未启动 `failover.Scheduler`，也未接 production health/traffic flow。
- **影响**：策略、API 和持久化虽有单元能力，但正常运行时没有 automatic failover control loop，不能视为完整 control-plane。
- **发现方式**：`cmd/embyproxy/main.go` wiring 与全仓生产调用搜索。
- **需要修改的文件**：Phase 1 设计后确定；预计 `cmd/embyproxy/main.go`、`internal/config/`、`internal/failover/`、storage/admin integration。
- **需要新增的测试**：真实 production wiring 的 mock-driven lifecycle、restart restore、scheduler cancellation/error、node mapping 和 fallback decision 不自动 apply 的边界。
- **修复前禁止进入的 phase**：Phase 4
- **状态**：OPEN
- **证据/链接**：`isolatedFailoverFixtureNodes`、`failover.NewMockDNSProvider()`、`internal/failover/scheduler.go` 无生产调用方。
- **备注**：Phase 3 可验证现有 mock 单元，但若目标是 runbook 的完整 mock E2E，本 gap 的测试 harness/入口需先明确。

## GAP-TRAFFIC-001 | 真实流量来源仍是 placeholder

- **Phase**：Phase 2
- **严重程度**：High
- **描述**：mock source 和 in-process proxy counter 已实现；provider API、SSH vnstat 和 persisted manual source 都只返回 unknown。主程序未选择或调度任何 traffic source。
- **影响**：无法依据 VPS 双向配额可靠自动切换或在 reset cycle 后自动切回。
- **发现方式**：`internal/failover/traffic.go` 与 production wiring 搜索。
- **需要修改的文件**：Phase 1 选择口径后确定；预计 `internal/failover/traffic.go`、`internal/config/`、`cmd/embyproxy/main.go`、storage/admin。
- **需要新增的测试**：inbound+outbound、初始已用量、持久化校准、stale/unknown、counter reset、provider failure、重启恢复和误差安全余量。
- **修复前禁止进入的 phase**：Phase 4
- **状态**：OPEN
- **证据/链接**：`ProviderAPISource`、`SSHVnstatSource`、`ManualTrafficSource` placeholder 注释和实现。
- **备注**：NOSLA reset day 仍未确认，真实流量口径不明时必须停止相关部署设计。

## GAP-DNS-001 | 真实 DNS provider 未实现或接线

- **Phase**：Phase 2 / Phase 5
- **严重程度**：High
- **描述**：mock provider 和严格 DNS guard 已实现，但主程序固定使用 mock provider；未发现真实 provider adapter。
- **影响**：不能执行真实 DNS failover；误把 mock apply 当真实 apply 会造成状态与公网记录不一致。
- **发现方式**：DNSProvider 实现和 `cmd/embyproxy/main.go` wiring 搜索。
- **需要修改的文件**：provider contract review 后确定；预计新增独立 provider adapter，并最小修改 config/wiring。
- **需要新增的测试**：provider dry-run、previous value、apply、propagation、failure、rollback metadata、credential redaction 和 active-state atomicity。
- **修复前禁止进入的 phase**：Phase 5
- **状态**：OPEN
- **证据/链接**：`internal/failover/dns.go`、`dns_guard.go`、`cmd/embyproxy/main.go`。
- **备注**：当前真实 provider/DNS 操作均未获授权。

## GAP-POLICY-001 | Failover policy 未完成运行时配置化和 minimum hold

- **Phase**：Phase 1 / Phase 2
- **严重程度**：Medium
- **描述**：`PolicyConfig` 包含失败、恢复、cooldown、流量阈值和切换窗口，但主程序直接使用固定 defaults；未发现独立 minimum-hold 配置。
- **影响**：无法从管理面安全调整全部防抖参数，也无法证明满足“最小保持时间”要求。
- **发现方式**：config、policy type 和 main wiring 搜索。
- **需要修改的文件**：Phase 1 设计后确定；预计 storage system config、admin config contract、failover policy 和 wiring。
- **需要新增的测试**：配置默认/校验/持久化、minimum hold、cooldown 区分、restart restore 和非法值 fail-closed。
- **修复前禁止进入的 phase**：Phase 4
- **状态**：OPEN
- **证据/链接**：`internal/failover/types.go`、`policy.go`、`cmd/embyproxy/main.go`。
- **备注**：现有默认策略测试仍是有效局部证据。

## GAP-REDIRECT-001 | Redirect fallback 仅有未接线 helper

- **Phase**：Phase 1 / Phase 2
- **严重程度**：Medium
- **描述**：`BuildRedirect` 固定 HTTPS 并验证 host allowlist，但没有生产调用方；测试未形成 path/query 安全与保真矩阵。
- **影响**：302 备选架构尚不能声明可用，且无法证明不会绕过 public route 或泄露不应传播的数据。
- **发现方式**：`BuildRedirect` production/test 调用搜索。
- **需要修改的文件**：是否接入及入口由 Phase 1 决定；预计 `internal/failover/redirect.go`、调用方和 tests。
- **需要新增的测试**：allowlisted host、固定 scheme、path 边界、encoded path、query 保真但日志不泄露、未知 route 和非公开 target 拒绝。
- **修复前禁止进入的 phase**：任何启用 302 fallback 的部署 phase
- **状态**：OPEN
- **证据/链接**：`internal/failover/redirect.go`、`redirect_test.go`；无非测试调用方。
- **备注**：DNS failover 仍是推荐方案，redirect 只作为备选。

## GAP-DOC-001 | Current-state 文档仍含 pre-tracking 旧状态

- **Phase**：Phase 0
- **严重程度**：Low
- **描述**：`00-current-state.md` 创建时的状态早于 `7d8ba77` tracking/push，不能作为当前 HEAD/origin 的最新事实。
- **影响**：未来 session 若只读 current-state 可能误判 runbook 尚未追踪或远端基线较旧。
- **发现方式**：本轮 fresh fetch/Git 基线与 runbook 内容对照。
- **需要修改的文件**：`docs/fusion-project-runbook/00-current-state.md`。
- **需要新增的测试**：更新后核对 branch/HEAD/origin/status 与本轮 evidence 一致。
- **修复前禁止进入的 phase**：Phase 1
- **状态**：OPEN
- **证据/链接**：当前 HEAD/origin `7d8ba77`；本轮写范围明确不含该文件。
- **备注**：为保持本次最小任务范围，不在本轮顺手扩展修改。
