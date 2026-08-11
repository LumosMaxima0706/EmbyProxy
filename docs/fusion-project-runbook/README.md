# Emby 融合项目总控执行手册

本目录是整个融合项目的唯一总控入口。它用于保存可跨 Codex session 复核的目标、事实、阶段、操作边界和进度，不以聊天记忆代替证据。

## 强制阅读顺序

每个新的 Codex session 开始工作前，必须先完整阅读：

1. `README.md`
2. `00-current-state.md`
3. `03-phase-map.md`
4. `07-codex-operating-rules.md`

然后只读确认 Git 状态，判断当前 phase，输出建议下一步，等待必要的人工批准。

## 项目主线

本项目不是单独实现 failover，也不是单独修复 WebSocket。最终目标是融合：

- EmbyProxy 的管理面板、节点管理、SQLite 配置、管理 API 和可视化运维能力；
- emby-reverse-proxy-go 更适合作为 data-plane 的自动代理和反向代理核心能力；
- BWG 与 NOSLA 之间基于健康、维护状态和流量周期的自动 failover。

WebSocket 4xx mapping/header hardening 是必要子任务，但其完成不能被解释为整个融合项目完成。

## 最终目标架构

- BWG 是唯一 management-plane 和 control-plane 部署位置，同时承担 fallback data-plane。
- NOSLA 只运行优先 data-plane，不运行管理 Web。
- 正常媒体流量优先进入 NOSLA。
- NOSLA 故障、关机、连续健康检查失败、维护或流量达到阈值时切换到 BWG。
- NOSLA 到 `reset_day` 后，仅在新流量周期已确认且健康稳定恢复时切回。
- 控制模式包括 `auto`、`force_nosla`、`force_bwg`、`maintenance_nosla`。
- 优先采用 DNS failover；302 redirect 仅作为备选。
- 不推荐 BWG 纯反代 NOSLA，因为用户流量仍经过 BWG，不能达到节省 BWG 用户侧流量的目标。

## 当前冻结边界

在 inventory、gap audit 和本地验证完成并获得对应 phase 的人工批准前，禁止：

- 部署或 SSH 到 BWG/NOSLA；
- 修改 DNS、Nginx 或 systemd；
- 写入真实 SQLite；
- 调用 admin 写接口；
- 切换生产流量；
- 把未审计分支直接视为可发布版本。

当前工作顺序必须是：事实 inventory -> gap audit -> 融合设计复核 -> clean worktree 本地验证 -> 经批准的旁路部署。

## 状态记录规则

- 命令现场验证过的内容标为 `VERIFIED`。
- 用户提供但本 session 未现场复核的内容标为 `USER-REPORTED`。
- 无法确认的内容写 `UNKNOWN` 或 `TBD`，禁止猜测。
- 每完成一步都要更新 `08-progress-log.md`；发现缺口要更新 `09-gap-log.md`。

