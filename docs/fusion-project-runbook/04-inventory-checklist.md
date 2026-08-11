# Inventory Checklist

本清单尚未执行完成。每项必须记录命令、commit SHA、文件路径或测试名作为证据；不能只写“看起来存在”。

## Git 与来源

- [ ] 确认 canonical branch 是哪个。
  - 证据：`TBD`
- [ ] 确认 `1514664` 存在及所在本地/远端 branch。
  - 当前参考证据：存在于 failover 与 mediaproxy 分支；canonical 复核待执行。
- [ ] 确认 `e1f5450` 存在及所在本地/远端 branch。
  - 当前参考证据：存在于 failover 分支；fresh fetch 待执行。
- [ ] 确认当前远端包含所有本地关键 commit。
- [ ] 确认两个上游项目 URL、基线 commit、license 和 attribution。
- [ ] 确认 clean worktree，列出所有 untracked/ignored 构建产物。

## 模块与集成点

- [ ] `internal/mediaproxy` 存在，列出职责、入口和测试。
- [ ] `internal/proxyadapter` 存在，列出 adapter contract 和验证逻辑。
- [ ] `internal/failover` 存在，列出 state machine、持久化和并发边界。
- [ ] managed route schema 存在，并确认 migration/兼容/rollback。
- [ ] admin failover API 存在，并确认认证、只读/写入边界和错误码。
- [ ] traffic source 存在，并确认 inbound+outbound、unknown、reset cycle 和校准。
- [ ] DNS mock provider 存在，并确认 dry-run/apply/failure/幂等测试。
- [ ] redirect fallback 存在，并确认 host/scheme/path allowlist。
- [ ] WebSocket 4xx mapping/header hardening 存在，并确认 `e1f5450` 内容。
- [ ] feature flags 存在，并确认默认值、route scope 和关闭后的旧 fallback。

## 行为核对

- [ ] 管理 API 可以创建/更新 managed route，但不能暴露 secret。
- [ ] managed route 可以进入 mediaproxy data-plane。
- [ ] 未知 route 保持旧 fallback，不被 feature flag 误接管。
- [ ] 400/401/403/404 WebSocket rejection 透传且不 ban/fallback。
- [ ] 101 tunnel、5xx 和 transport/TLS/dial failure 保持预期策略。
- [ ] `auto`、`force_nosla`、`force_bwg`、`maintenance_nosla` 完整实现。
- [ ] 连续失败 3 次、恢复阈值、冷却和最小保持时间已配置化。
- [ ] reset day 和新周期确认不会把 unknown traffic 当成 0。
- [ ] DNS apply 失败不会改变 active state。

## 测试盘点

- [ ] 列出每个模块的单元测试文件和场景。
- [ ] 列出跨模块集成测试和缺失场景。
- [ ] 列出 race/concurrency 测试。
- [ ] 列出 admin auth、origin/CSRF、rate limit 和 secret redaction 测试。
- [ ] 列出日志脱敏测试，包括 header、cookie、query 和错误字符串。
- [ ] 运行 Phase 3 前生成“需求 -> 测试 -> 证据”映射表。

## 缺口输出

- [ ] 把所有未实现需求写入 `09-gap-log.md`。
- [ ] 为每个 gap 标严重程度、阻塞 phase、修改文件和新增测试。
- [ ] 明确哪些需求已实现、部分实现、仅 mock、完全未实现。
- [ ] 明确是否存在阻止 Phase 3 或 Phase 4 的 gap。

