# Gap Log

本文件记录尚未解决的实现、测试、设计、license、部署和证据缺口。一个 gap 一条记录，不合并无关问题。

## 状态值

- `OPEN`：已确认，未开始处理。
- `INVESTIGATING`：只读核对中。
- `PLANNED`：方案已 review，等待实现批准。
- `IN_PROGRESS`：处于获批实现 gate。
- `BLOCKED`：缺少输入、授权或外部条件。
- `OWNER-AUTHORIZED / PHASE-3-UNBLOCKED`：owner 已明确授权实现，但证据 hygiene 仍待完成。
- `OPEN / NON-BLOCKING FOR PHASE 3`：仍需处理，但不阻止本地 Phase 3 coding。
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
- **描述**：在 `a3c08dd` 上完成的 repo tree 与完整本地 Git 历史只读审计仍不能建立完整 license/source provenance。根目录没有 LICENSE/COPYING/NOTICE；README 仅声明 MIT，缺少完整 license 文本与 copyright notice。`docs/third_party_notices.md` 记录了 EmbyProxy 的 MIT 意图、另一代理项目无可确认复制授权，以及 `internal/mediaproxy` 为独立实现的声明，但证据链仍不完整。
- **影响**：license/source provenance 细节仍需补齐，但 owner 已授权融合实现，Phase 3 coding 不再被本 gap 阻塞；正式 release/public distribution 仍需完成后续 evidence hygiene。
- **发现方式**：在无网络条件下检查当前 tree、完整本地 Git history、README、`docs/third_party_notices.md`、commit `97c0e55`、`go.mod`/`go.sum`、tracked 文件类型、source markers、submodule/vendor/generated/minified 资产和精确 license 关键词。
- **需要修改的文件**：当前 evidence tracking skeleton 为 `10-provenance-evidence-matrix.md`；未来修复范围由权利与来源确认结果决定，至少可能包括 root LICENSE/copyright notice、`docs/third_party_notices.md`、完整逐文件 provenance 清单和 dependency license inventory/SBOM。创建 skeleton 不代表授权问题已解决。
- **需要新增的测试**：不适用；需要可复核的 source commit/file/license/notice 映射和 dependency inventory 检查。
- **修复前禁止进入的 phase**：正式 release/public distribution（不阻塞 Phase 3 coding）
- **状态**：OWNER-AUTHORIZED / PHASE-3-UNBLOCKED
- **证据/链接**：HEAD `c7f475c`；`10-provenance-evidence-matrix.md`；`docs/third_party_notices.md`；`813118c`、`97c0e55`、`bbd9072`；root license/history 搜索为空；`go.mod` 有 6 个 direct 和 12 个 indirect dependencies。
- **备注**：
  - Project owner confirmed that code modification, refactoring, rewriting, and integration of the involved projects are authorized for the fusion objective. Codex does not need per-file provenance approval before implementation. Phase 3 development is unblocked.
  - Remaining license/provenance/SBOM/notice work is tracked as release/docs hygiene and must not block Phase 3 implementation.
  - README 致谢的另一上游尚无 revision、license、复制范围或 attribution 记录。
  - `internal/mediaproxy` 在 `97c0e55` 中整体引入并声明独立实现，但尚无逐文件 provenance 映射；`internal/proxyadapter` 也需要纳入映射。
  - repo 没有 dependency license inventory、SBOM 或对应 notices。
  - 未发现 tracked vendor/、third_party/、node_modules/、build/dist、submodule、二进制归档、copied-from/snippet/source attribution 注释，或明确 GPL/AGPL/LGPL/MPL 线索。
  - 未发现 minified、bundled、wasm、source map 或按文件名标记的 generated 文件；大型 admin HTML 是自包含资源，未发现外部脚本引用或内嵌 license marker。
  - 上述“未发现”仅限定于本次 repo-only 静态审计，不等于确认不存在版权或依赖义务。

### GAP-PROV-001 后续 release/docs hygiene checklist

- [ ] 由人工确认 MIT 授权权利人、适用年份、copyright notice，以及正式发布和再分发授权；确认前不得创建或补写 root LICENSE。
- [ ] 由人工确认 README 致谢上游 `chenhr454/emby---worker` 的 stable revision、license、复制/改写范围和 attribution 要求。
- [ ] 由作者或权利人确认 `internal/mediaproxy`、`internal/proxyadapter` 是否独立实现，以及是否引用、翻译、复制或改写外部项目；按 `10-provenance-evidence-matrix.md` 扩展逐文件记录。
- [ ] 完成 6 个 direct 和 12 个 indirect Go dependencies 的 license/notice inventory，由人工审核结果并决定 SBOM 要求。
- [ ] 人工 review 上述证据，确认 `docs/third_party_notices.md` 与实际复制/独立实现边界一致。
- [ ] 上述事项完成前，`GAP-PROV-001` 保持 owner-authorized/unblocked；它们只阻塞正式 release/public distribution，不阻塞 Phase 3 coding。

## GAP-PROV-002 | Release provenance and license hygiene

- **Phase**：Release / public distribution
- **严重程度**：High
- **描述**：root LICENSE/copyright notice、README upstream attribution、mediaproxy/proxyadapter 逐文件 provenance matrix、Go dependency license inventory、SBOM 和 notices 仍需补齐。
- **影响**：可能阻止正式 release/public distribution；不阻塞已获 owner 授权的 Phase 3 implementation。
- **发现方式**：`GAP-PROV-001` repo-only evidence audit 与 owner authorization review。
- **需要修改的文件**：root license/notice（待权利人确认）、`docs/third_party_notices.md`、`10-provenance-evidence-matrix.md` 及 release evidence 文件。
- **需要新增的测试**：release artifact license/notice presence、dependency inventory completeness 和 attribution review checks。
- **修复前禁止进入的 phase**：正式 release/public distribution
- **状态**：OPEN / NON-BLOCKING FOR PHASE 3
- **证据/链接**：`10-provenance-evidence-matrix.md`；owner authorization decision。
- **备注**：不得将本 gap 重新解释为 Phase 3 coding blocker；完成后再由人工 review release readiness。

## GAP-ROUTE-001 | Managed route 缺少管理 API 写入合同

- **Phase**：Phase 1 / Phase 2
- **严重程度**：High
- **描述**：managed route schema、query 和 production resolver 已存在，但没有发现 create/update/delete storage API 或 admin route API；现有测试通过直接 SQL 插入 fixture。
- **影响**：已由 storage CRUD、authenticated Admin API、embedded UI 和 runtime resolver 完成 management-plane 到 data-plane 的核心融合链路；历史 gap 描述保留用于审计上下文。
- **发现方式**：全仓搜索 managed route SQL、storage method 和 admin route。
- **需要修改的文件**：设计后确定；预计涉及 `internal/storage/managed_routes.go`、`internal/admin/`、管理 UI 资源和相应 tests。
- **需要新增的测试**：认证管理 API 创建/更新 route 后进入 proxyadapter/mediaproxy；非法 target、disabled/public、default line、事务失败和敏感字段边界。
- **修复前禁止进入的 phase**：无；实现已完成，后续回归由 I-001 管理。
- **状态**：CLOSED / VERIFIED
- **证据/链接**：`98229bd`、`0b7b590`、`8c00f1a`、`29118fb`、`4e60097`、`fc00c61`；storage/API/UI/runtime/integration tests。
- **备注**：managed-route contract 已按 runbook 阶段完成；不得将该历史 gap 重新解释为未授权的实现阻塞。

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
