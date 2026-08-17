# Phase 3 本地验证计划

状态：未来计划，当前不执行。

## 前置条件

- Phase 0/1/2 已通过并记录 canonical target branch/SHA。
- 目标 commit 已在 GitHub，且没有未 push 的关键 commit。
- 不使用 BWG/NOSLA、真实 DNS provider、真实 SQLite 或真实 admin 凭据。
- 失败只记录，不在验证 worktree 自动修代码。

## Clean worktree

1. fetch 目标 GitHub remote。
2. 从明确的远端目标分支创建唯一 `/tmp` clean worktree。
3. 记录 remote URL、branch、commit SHA 和初始 `git status`。
4. 禁止从当前 dirty worktree复制未提交文件到验证 worktree。

建议命令形态，实际路径和 branch 必须在执行 gate 中确认：

```bash
git fetch origin <target-branch>
git worktree add --detach /tmp/emby-fusion-phase3 origin/<target-branch>
cd /tmp/emby-fusion-phase3
git status --short --untracked-files=all
go test ./...
go vet ./...
git diff --check
```

## 隔离测试环境

- 创建 `/tmp` 下的临时 SQLite，结束后按单独 cleanup 许可处理。
- 使用本地临时 admin token；不得使用默认值或真实 token，不得输出。
- 所有服务只监听 `127.0.0.1` 的动态或明确测试端口。
- DNS 使用 mock provider；traffic 使用 mock source 和确定性周期时间。
- 禁止连接真实 DNS/VPS provider。

## 必验场景

### 管理与 route

- [ ] EmbyProxy 管理 API 能驱动 managed route。
- [ ] managed route 能进入 mediaproxy。
- [ ] 未知 route 走旧 fallback。
- [ ] feature flag 关闭时不接管旧 route。
- [ ] admin 未认证返回 401。
- [ ] admin 写操作要求正确认证和 origin guard。

### Failover policy

- [ ] NOSLA healthy -> NOSLA。
- [ ] NOSLA 连续失败 3 次 -> BWG。
- [ ] NOSLA 流量超阈值 -> BWG。
- [ ] `maintenance_nosla` -> BWG。
- [ ] `force_bwg` 始终选择 BWG。
- [ ] `force_nosla` 的行为和失败反馈符合设计。
- [ ] reset day 进入新周期并稳定恢复后切回 NOSLA。
- [ ] traffic unknown 不误判为 0，不触发错误切回。
- [ ] 冷却、最小保持时间和恢复连续成功防止抖动。

### DNS 与 redirect

- [ ] mock DNS dry-run 输出精确变更但不写状态。
- [ ] mock DNS apply 成功后才提交 active state。
- [ ] mock DNS failure 不改变 active state。
- [ ] 重复 apply 幂等。
- [ ] redirect scheme/host/path allowlist。
- [ ] 外部 host、userinfo、危险 scheme、CRLF 和敏感 query 被拒绝或脱敏。

### 代理协议

- [ ] WebSocket 4xx 不映射成 502，不 line-ban，不 fallback。
- [ ] WebSocket 101 tunnel 保持可用。
- [ ] WebSocket 5xx/transport/TLS/dial failure 保持 gateway 策略。
- [ ] Range、If-Range、流式响应、长连接和取消传播。
- [ ] 日志脱敏覆盖 Authorization、Cookie、token、secret、完整 query、UUID 和订阅链接。

## 通过标准

- `go test ./...`、`go vet ./...`、`git diff --check` 全部通过。
- 所有必验场景有命令和结果证据。
- worktree 结束时仍 clean。
- 没有连接真实 provider、没有部署、没有 secret 泄露。
- 任何失败进入 gap log；存在阻塞 gap 时禁止 Phase 4。

