# 当前状态

记录时间：`2026-08-11T15:25:57+08:00`

## 证据范围

本次 session 禁止 SSH。指定的 canonical 工作区 `/root/staging/embyproxy-staging` 不存在于当前执行环境，当前工作目录也不是 Git 仓库，因此没有对 BWG 工作区执行 `git fetch`。

本机存在参考 clone `.research/EmbyProxy`，其 remote 指向目标 GitHub 仓库。本节的 Git 数据来自该参考 clone 的只读命令；是否与 BWG canonical 工作区逐字一致仍需下一次获批 inventory 复核。

## Git 状态快照

| 项目 | 状态 | 证据级别 |
|---|---|---|
| 参考 clone branch | `feature/failover-phase2-local` | VERIFIED |
| 参考 clone HEAD | `e1f5450` | VERIFIED |
| `origin/feature/failover-phase2-local` | `e1f5450` | VERIFIED，但本 session 未 fetch |
| `origin/feature/mediaproxy-core-poc` | `4074662` | VERIFIED，但本 session 未 fetch |
| 参考 clone worktree | clean | VERIFIED |
| 本 runbook 的 Git 可见性 | 被 `.gitignore` 中 `/docs/` 规则忽略 | VERIFIED；普通 `git status` 不显示 |
| BWG canonical branch/HEAD/worktree | `UNKNOWN` | SSH 被禁止，未现场读取 |
| 用户报告的 post-push 状态 | HEAD/origin 均为 `e1f5450`，worktree cleanup 后干净 | USER-REPORTED |

## 最近 20 个 commit

以下来自 `.research/EmbyProxy`：

```text
e1f5450 fix: pass through websocket client rejections
0671788 Fix isolated failover regression guards
74c7c8e Harden admin failover and DNS apply guards
f0e12dc deployment: allow localhost-only staging listeners
6d19746 ci: run Go CI on failover branch
b0599de Make failover evaluation read-only until transition commit
f73af9f Fix failover recovery and transition consistency
6d3adce Fix failover controller state consistency findings
b26a5a2 Implement local failover phase2 controller
4074662 Run Go CI tests uncached
7d346bc Fix mediaproxy route adapter validation findings
1514664 Integrate mediaproxy routes behind feature flag
e80b9fc Add Go CI workflow
ceb4204 Harden mock adapter target base paths
d191d15 Fix mock adapter raw route validation
bbd9072 Add mediaproxy mock route adapter
97c0e55 Add license-safe mediaproxy core
34a2a74 phase2b document license blocker and add race container target
6957727 phase2a failover core mock and playback stats stabilization
0629ca3 优化同源验证
```

## Commit containment

- `e1f5450` 存在于：
  - `feature/failover-phase2-local`
  - `remotes/origin/feature/failover-phase2-local`
- `1514664` 存在于：
  - `feature/failover-phase2-local`
  - `feature/mediaproxy-core-poc`
  - `remotes/origin/feature/failover-phase2-local`
  - `remotes/origin/feature/mediaproxy-core-poc`

## 当前已知已完成事项

- Phase 2C route commit `1514664` 存在，message 为 `Integrate mediaproxy routes behind feature flag`。VERIFIED。
- WebSocket 4xx mapping/header hardening commit `e1f5450` 存在于本地与 origin failover 分支引用。VERIFIED，但本 session 未 fetch。
- 用户报告 WebSocket staging deploy/smoke、post-push verification 和 cleanup 已通过。USER-REPORTED。
- 用户报告 `staging-v1-test` 已清理、staging service 仍为 active/disabled。USER-REPORTED。

## 当前未确认事项

- 哪个分支被正式指定为融合项目 canonical branch：`TBD`。
- GitHub 当前远端状态是否在本记录时间后发生变化：`UNKNOWN`。
- 两个上游项目的精确 source provenance、license version 和 attribution 覆盖：`TBD`。
- `internal/mediaproxy`、`internal/proxyadapter`、`internal/failover` 的完整性和集成边界：`TBD`。
- managed route schema、feature flags、admin failover API、traffic source、DNS mock provider、redirect fallback 的实现与测试完成度：`TBD`。
- Phase 3 clean worktree 全流程验证是否完整通过：`UNKNOWN`。
- Phase 4 所需端口、旁路路径与现有 BWG 配置是否仍无冲突：`UNKNOWN`。
- 本 runbook 应通过修改 ignore 规则还是显式 force-add 纳入版本控制：`TBD`；本 session 不修改 `.gitignore`、不 stage。

## 下次 inventory 建议命令

仅在允许访问 canonical repo 的 session 中执行：

```bash
cd /root/staging/embyproxy-staging
git fetch --all --prune
git branch --show-current
git rev-parse --short HEAD
git status --short --untracked-files=all
git log -20 --oneline --decorate --graph
git branch -a --contains e1f5450
git branch -a --contains 1514664
git rev-parse --short origin/feature/failover-phase2-local
git rev-parse --short origin/feature/mediaproxy-core-poc
```

当前不能直接进入部署阶段。下一步只能是 Phase 0 inventory/gap audit，或经明确批准后执行 Phase 3 本地验证。
