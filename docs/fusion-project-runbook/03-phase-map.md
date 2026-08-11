# 项目阶段地图

任何 session 只能处于一个 phase。未达到通过标准或未获得下一 phase 明确批准时，不得前进。

## Phase 0：项目归档和事实核对

- **目标**：确认两个上游来源、license、当前 canonical branch/commit、已实现模块和真实缺口。
- **前置条件**：读完总控手册；确定可用的只读 repo；敏感 remote/config 已脱敏。
- **允许操作**：Git fetch/status/log/show/diff；只读搜索源码、测试、license 和配置样例；更新本 runbook。
- **禁止操作**：改源码、真实 provider、部署、SSH 服务器变更、Nginx/systemd/DNS/SQLite/admin 写接口。
- **通过标准**：`04-inventory-checklist.md` 有证据地完成；canonical branch 已确认；provenance 与 gap log 已建立。
- **失败时停止**：remote/commit 不一致、license 不明或 canonical branch 不明时标记 `BLOCKED/TBD`，不得猜测或合并。
- **Commit**：仅文档可在人工 review 后另行批准 commit。
- **Push**：否，除非单独明确批准。
- **Deploy**：否。
- **人工批准**：进入 Phase 1 前必须批准 inventory 结论。

## Phase 1：融合设计

- **目标**：确定管理面板如何接入代理核心，以及 SQLite schema、managed routes、feature flag 和 admin API 合约。
- **前置条件**：Phase 0 通过；license/provenance 风险有处置结论；gap 有 owner。
- **允许操作**：设计文档、接口草案、迁移方案、测试矩阵、mock contract；必要时只读代码验证。
- **禁止操作**：部署、真实数据库迁移、真实 DNS/provider、生产配置修改。
- **通过标准**：management/control/data-plane 边界、schema migration、route resolution、feature flag rollback、admin auth、错误模型均有 review 结论。
- **失败时停止**：schema 破坏兼容、管理 API 暴露敏感字段、代理核心边界或 license 无法说明时回到 Phase 0。
- **Commit**：设计文档 commit 可单独批准。
- **Push**：必须单独批准。
- **Deploy**：否。
- **人工批准**：进入 Phase 2 前必须批准设计和测试计划。

## Phase 2：本地实现

- **目标**：实现 mediaproxy core、proxy adapter、managed routes、failover policy、mock DNS/traffic、admin API、WebSocket 行为和测试。
- **前置条件**：Phase 1 通过；每个子任务有最小范围、回滚和测试定义；从 canonical branch 工作。
- **允许操作**：按获批子 gate 修改源码/测试；本地 gofmt/test/vet；仅使用临时 SQLite、mock provider 和假凭据。
- **禁止操作**：真实 provider、真实 SQLite、服务器部署、Nginx/systemd/DNS 修改、生产流量。
- **通过标准**：所有获批模块实现；单元/集成测试覆盖成功与失败路径；feature flag 默认安全；日志脱敏。
- **失败时停止**：测试失败、范围扩张、需要真实服务验证或发现高严重 gap 时停止当前子 gate。
- **Commit**：每个可审查子任务可在单独批准后 commit。
- **Push**：每个 commit 必须单独批准 push；禁止 force push。
- **Deploy**：否。
- **人工批准**：每个实现子 gate 和 Phase 3 均需批准。

## Phase 3：本地/mock 全流程验证

- **目标**：在从 GitHub 目标分支创建的 `/tmp` clean worktree 中验证融合主线，不修代码。
- **前置条件**：Phase 2 候选 commit 已在目标 remote；worktree 来源和 SHA 已确认；`05-phase3-verification-plan.md` 已 review。
- **允许操作**：`go test ./...`、`go vet ./...`、`git diff --check`、临时 SQLite/token、mock DNS/traffic、本机临时监听和只读结果采集。
- **禁止操作**：测试失败后自动修代码；真实 provider；服务器 SSH/部署；真实 SQLite；Nginx/systemd/DNS/admin 真实写入。
- **通过标准**：验证计划全部通过；clean worktree 保持 clean；无 secret 泄露；所有 gap 已关闭或明确阻塞后续 phase。
- **失败时停止**：记录失败命令、测试名、日志摘要和 gap；不得在该 worktree 临时修复后宣称通过。
- **Commit**：否。
- **Push**：否。
- **Deploy**：否。
- **人工批准**：进入 Phase 4 必须使用 Phase 4 专用批准短语。

## Phase 4：BWG 旁路部署

- **目标**：只在 BWG 部署旁路候选，验证真实主机环境但不替换现有入口和生产流量。
- **前置条件**：Phase 3 全部通过；已获得精确批准短语；完成端口、Nginx、证书、服务、备份和回滚 preflight。
- **允许操作**：仅按 `06-phase4-bwg-sidecar-plan.md` 在 BWG 创建独立服务/配置；监听 `127.0.0.1:18082`；增加 `/embyproxy-gsy-test/` 测试入口。
- **禁止操作**：NOSLA 管理面、替换 `/admin/` 或 `/s/`、stream 生产 DNS、生产切流、覆盖现有服务、restart Nginx。
- **通过标准**：旁路健康、admin 隔离、managed route、fallback、WebSocket 和日志验证通过；现有服务无变化；回滚可执行。
- **失败时停止**：恢复独立配置/旧 binary，先 `nginx -t` 再按已批准方式 reload；不得扩大修改范围。
- **Commit**：默认否；部署产生的环境文件不得进入源码 commit。
- **Push**：否。
- **Deploy**：是，仅限批准范围的 BWG 旁路。
- **人工批准**：必须逐字获得“我批准进入 Phase 4 BWG 旁路部署”。

## Phase 5：DNS dry-run / manual apply

- **目标**：验证 provider adapter 的 dry-run、人工确认、apply 和失败一致性。
- **前置条件**：Phase 4 旁路稳定；DNS 当前记录、TTL、provider 权限和回滚记录已只读确认；mock 流程通过。
- **允许操作**：先 dry-run；展示精确 diff；获得 apply 专用批准后仅修改指定测试/目标记录并验证。
- **禁止操作**：未批准 apply、批量 DNS 变更、修改 admin 固定域名、provider 失败时提交 active state。
- **通过标准**：dry-run 与预期一致；apply 后权威 DNS 和访问验证通过；失败注入不会改变 active state；回滚记录完整。
- **失败时停止**：provider/DNS 验证失败时停止状态提交，执行获批 DNS 回滚或保持原记录。
- **Commit**：默认否；如产生代码修复必须退回 Phase 2。
- **Push**：否。
- **Deploy**：dry-run 否；manual apply 仅在专用批准后允许 DNS 变更。
- **人工批准**：dry-run 和 apply 分开批准，apply 不得从 dry-run 授权推导。

## Phase 6：正式 automatic failover

- **目标**：启用 `auto`，以 NOSLA 为优先 data-plane、BWG 为 fallback，并在观察期内验证真实切换和恢复。
- **前置条件**：Phase 5 通过；流量口径/reset day 已确认；健康、防抖、冷却、回滚和监控齐备；旧服务保持可回滚。
- **允许操作**：按批准变更启用 auto；执行受控故障/阈值/reset 验收；持续观察和审计。
- **禁止操作**：立即下线旧 sidecar、跳过观察期、修改未知现有服务、无审计的强制切换。
- **通过标准**：NOSLA 优先；故障/维护/阈值切 BWG；新周期和稳定健康后切回；无抖动、无认证绕过、日志无敏感信息。
- **失败时停止**：切回明确 force 模式或旧入口，保留证据；不得在运行中临时改代码。
- **Commit**：运行期默认否；修复必须退回 Phase 2。
- **Push**：否。
- **Deploy**：是，仅限获批 production rollout。
- **人工批准**：启用 auto、故障演练、旧 sidecar 下线分别需要人工批准。

