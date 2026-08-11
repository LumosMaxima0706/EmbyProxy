# Inventory Checklist

本清单记录 2026-08-11 在 `feature/failover-phase2-local`、`7d8ba77` 上完成的 Phase 0 只读盘点。`[x]` 表示事实已核对，不代表整个功能已完成；部分实现和阻断项均链接到 `09-gap-log.md`。

## Git 与来源

- [x] 当前融合工作 canonical branch 为 `feature/failover-phase2-local`。
  - 证据：用户指定基线；`git branch --show-current`、`git rev-parse --short HEAD` 和 origin ref 分别为该分支、`7d8ba77`、`7d8ba77`。
- [x] `1514664` 存在并位于本地/远端 failover 与 mediaproxy 分支。
  - 证据：`git branch -a --contains 1514664`；commit message 为 `Integrate mediaproxy routes behind feature flag`。
- [x] `e1f5450` 存在并位于本地/远端 failover 分支。
  - 证据：`git branch -a --contains e1f5450`；commit 只修改四个 WebSocket 源码/测试文件。
- [x] 当前 origin failover 分支包含本地关键 commit。
  - 证据：fresh fetch 后本地 HEAD 与 origin 均为 `7d8ba77`；该历史包含 `1514664` 和 `e1f5450`。mediaproxy 远端 ref 为 `4074662`。
- [ ] 两个上游来源已定位，但 license、基线 provenance 和 attribution 未完成。
  - 证据：`a3c08dd` repo-only audit 确认 root LICENSE/COPYING/NOTICE 在当前 tree 和完整本地历史中均不存在；README 仅有 MIT 声明。既有 third-party review、另一个 README 致谢上游、逐文件 provenance 与 dependency licenses 仍不完整。见 `GAP-PROV-001`。
- [x] 工作区在盘点开始前 clean，无 untracked 项。
  - 证据：`git status --short --untracked-files=all` 和脱敏后的 ignored 状态检查均无输出。

### License/source provenance audit

- [x] 完成 repo-only 静态审计；未访问 GitHub、未安装依赖、未修改审计对象。
- [x] 确认没有 tracked vendor/、third_party/、node_modules/、build/dist、submodule、二进制归档或匹配的 minified/bundled/wasm/source-map/generated 文件。
- [x] 未发现 copied-from/snippet/source attribution 注释或明确 GPL/AGPL/LGPL/MPL 线索；该结论不等于完成权利确认。
- [x] 已建立 `10-provenance-evidence-matrix.md` skeleton，用于跟踪证据和人工确认；该结构化记录不是授权确认。
- [x] Owner 已授权为融合目标修改、重构、改写和集成源码；`GAP-PROV-001` 不再阻塞 Phase 3 implementation。
- [ ] `RELEASE HYGIENE PENDING`：确认 MIT 权利人、年份、copyright notice 和正式发布授权后，才可决定 root LICENSE 内容。
- [ ] `RELEASE HYGIENE PENDING`：为 README 致谢的 `chenhr454/emby---worker` 补齐 stable revision、license、复制/改写范围和 attribution。
- [ ] `RELEASE HYGIENE PENDING`：将 `internal/mediaproxy`、`internal/proxyadapter` skeleton 扩展为逐文件来源、作者确认、独立实现和测试 provenance 映射。
- [ ] `RELEASE HYGIENE PENDING`：完成 6 个 direct、12 个 indirect Go dependencies 的 license/notice inventory，并由人工决定 SBOM 要求。
- [ ] 上述事项由 `GAP-PROV-002` 跟踪，只影响正式 release/public distribution，不阻塞 Phase 3 coding。

## 模块与集成点

- [x] `internal/mediaproxy` 存在。
  - 职责：目标解析、安全校验、HTTP/Range、header、rewrite、transport 和 WebSocket data-plane executor。
  - 测试：9 个测试文件，覆盖 target、security、Range、rewrite、transport、header、日志脱敏和 WebSocket。
- [x] `internal/proxyadapter` 存在。
  - contract：把 managed slug 或既有 node 解析为 `mediaproxy.Target`；生产 router 支持 fallback。
  - 测试：`adapter_test.go`、`production_test.go`，覆盖 route/node、边界、fallback、WebSocket、安全目标和日志脱敏。
- [x] `internal/failover` 存在。
  - 职责：纯策略、controller、DNS guard、mock health/traffic/DNS、scheduler contract、redirect helper。
  - 持久化：`internal/storage/failover.go` 记录 state、event、health、traffic 与 DNS run，并提供事务提交和恢复。
  - 并发边界：controller、health tracker、mock/provider state 使用 mutex；已有并发 tracker 测试，但本轮未运行 race detector。
- [x] managed route schema 和只读 resolver 存在。
  - 证据：`internal/storage/managed_routes.go` 创建 `managed_routes` 与 `managed_route_lines`；`Store.InitSchema` 调用初始化；schema/query 测试存在。
  - 限制：只有 `CREATE IF NOT EXISTS` 和查询 API，未发现管理 API、兼容迁移版本或显式 rollback。见 `GAP-ROUTE-001`。
- [x] admin failover API 存在且走现有 admin auth/origin guard。
  - 范围：status、check-now、mode、manual switch、events、traffic status/manual sample、DNS status/dry-run/apply。
  - 证据：`internal/admin/failover_api.go`、`failover_api_test.go`、`auth_test.go`；写操作包含确认/guard，但仅对内存 controller/mock fixture 有完整测试。
- [x] traffic abstraction、mock 和 proxy counter 存在。
  - 证据：known/unknown/stale 模型、双向相加、cycle reset 与 unknown 测试存在。
  - 限制：provider API、SSH vnstat、persisted manual source 均为返回 unknown 的 placeholder，主程序未接线采集 scheduler。见 `GAP-TRAFFIC-001`、`GAP-RUNTIME-001`。
- [x] DNS mock provider 与 fail-closed guard 存在。
  - 证据：dry-run/apply/failure/propagation、one-time binding、allowlist、rollback metadata 和“失败不提交 active state”测试存在。
  - 限制：主程序固定构造 mock provider，未发现真实 provider adapter。见 `GAP-DNS-001`。
- [x] redirect fallback helper 存在但未接入运行时。
  - 证据：`BuildRedirect` 固定 HTTPS 并验证 host allowlist；测试覆盖非 allowlist 和畸形 host。
  - 限制：无生产调用方，path/query policy 测试不足。见 `GAP-REDIRECT-001`。
- [x] WebSocket 4xx mapping/header hardening 存在。
  - 证据：`e1f5450`；proxy/mediaproxy 测试覆盖 4xx passthrough、header 过滤、101、5xx 和 transport failure。
- [x] mediaproxy route feature flag 存在且默认关闭。
  - 证据：`Config.MediaProxyRoutes`、`MEDIAPROXY_ROUTES_ENABLED=false` 默认值、config tests 和 `proxyRouteHandler`；关闭或未匹配时进入旧 fallback。

## 行为核对

- [ ] EmbyProxy 管理 API 尚不能创建/更新 managed route。
  - 全仓搜索只发现测试直接 INSERT；生产 storage 仅提供 route 查询。见 `GAP-ROUTE-001`。
- [x] 已启用且 public 的 managed route 能进入 mediaproxy。
  - 证据：`proxyadapter.NewProductionRouter` wiring 与 `TestProductionSlugRouteUsesManagedTarget`。
- [x] 未知 route 和 flag-off 保持旧 fallback。
  - 证据：`proxyRouteHandler`、`Router.serveFallback`、`TestProductionNodeRouteAndFallbackBoundaries` 及 main route tests。
- [x] 400/401/403/404 WebSocket rejection 的源码/测试合同为透传且不 ban/fallback。
- [x] 101 tunnel、5xx 和 transport failure 的源码/测试合同保持原策略。
- [x] `auto`、`force_nosla`、`force_bwg`、`maintenance_nosla` 策略及单元测试存在。
- [ ] failure/recovery/cooldown/traffic threshold 有 `PolicyConfig`，但主程序使用固定 defaults，且没有独立 minimum-hold 配置。
  - 默认值为连续失败 3、恢复成功 3、冷却 30 分钟、流量 97%；见 `GAP-POLICY-001`。
- [x] reset cycle 要求 known 新周期；unknown/stale 默认不能触发切回。
  - 证据：`newKnownCycle` 及 recovery/cycle/unknown tests。
- [x] mock DNS apply/verify 失败不会提交成功 active state。
  - 证据：DNS failure、verify failure、writer/transaction failure tests。
- [ ] automatic failover 尚未完成 production wiring。
  - 主程序只在 loopback mock fixture 下构造 nodes，未启动 failover scheduler，未接真实 health/traffic/DNS。见 `GAP-RUNTIME-001`。

## 测试盘点

- [x] 模块测试文件已列出。
  - mediaproxy 9、proxyadapter 2、failover 6、admin 3、storage 4；另有 `cmd/embyproxy` wiring/restore tests 和 `internal/proxy/websocket_mapping_test.go`。
- [x] 跨模块测试已定位。
  - `cmd/embyproxy/main_test.go`、`failover_restore_test.go`、`proxyadapter/production_test.go` 覆盖 feature flag、storage resolver、fallback 和恢复 wiring。
  - 缺失：管理 API 写 managed route 后经生产 router 进入 mediaproxy 的端到端测试；真实 scheduler/provider wiring 测试。
- [x] concurrency/race 测试证据已定位。
  - `TestHealthTrackerIsRaceSafeForConcurrentRecords` 以及既有 proxy/storage 并发测试存在；本轮按 Phase 0 规则未运行 `go test -race`。
- [x] admin auth/origin 与写接口 guard 测试已定位。
  - failover API 全路由认证、cross-site POST、确认字段、DNS dry-run binding、unknown traffic view 均有测试；rate-limit 证据属于既有 admin auth 体系，尚未形成 failover 专用矩阵。
- [x] 日志与存储脱敏测试已定位。
  - mediaproxy URL/header/request logging、proxyadapter request/node log、failover reason/storage redaction 均有测试；本轮未执行测试。
- [ ] Phase 3 前的“需求 -> 测试 -> 证据”可执行矩阵尚未生成。
  - 应在 Phase 1 设计确认和 Phase 2 gap 修复后完成。

## 分类结论

- **已实现**：mediaproxy core、proxyadapter 读取/路由、managed schema/query、feature flag/fallback、failover policy/state persistence、admin failover endpoints、WebSocket hardening。
- **部分实现**：managed route management contract、运行时 node/control-plane wiring、可配置 policy、redirect fallback、failover UI/operational flow。
- **仅 mock**：DNS provider、health probe、scheduler scenario、自动采样 flow。
- **placeholder**：provider API、SSH vnstat、persisted manual traffic source。
- **未确认/未完成**：license/source provenance、真实 DNS provider、production automatic failover、Phase 3 全流程验证。

## Phase 0 Gate 结论

- [x] 所有确认的缺口已写入 `09-gap-log.md`。
- [x] 每个 gap 已标严重程度、阻塞 phase、预计文件和测试。
- [x] 已区分 implemented、partial、mock-only、placeholder 和 unverified。
- [x] Owner authorization 已解除 `GAP-PROV-001` 对 Phase 3 implementation 的阻塞；未完成的 release/docs hygiene 转入 `GAP-PROV-002`。
- [x] Phase 3 implementation 可开始；正式 release/public distribution 仍需单独完成 provenance/license hygiene review。
