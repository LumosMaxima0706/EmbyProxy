# Codex 操作守则

## Session 启动规则

每次开始必须依次完整阅读：

1. `README.md`
2. `00-current-state.md`
3. `03-phase-map.md`
4. `07-codex-operating-rules.md`

随后必须：

1. 只读确认当前 Git branch、HEAD、origin ref 和 worktree。
2. 区分现场验证、用户报告和 UNKNOWN/TBD。
3. 判断当前 phase 及其通过标准。
4. 输出当前事实、阻塞项和建议下一步。
5. 在需要写入或外部变更前等待明确人工批准。

## 状态判断

- 不能凭聊天记忆判断项目状态。
- 不能把一个子任务完成当成整个项目完成。
- WebSocket、failover、managed route 或某次 staging smoke 通过都只是对应范围的证据。
- commit 存在不等于功能完整；测试通过不等于生产可部署；staging 通过不等于 production 验收。
- 不确定时只能执行必要的只读检查，并将结论写为 `UNKNOWN/TBD`。
- 不能擅自进入下一 phase。

## Git 规则

- 禁止 reset 已 push commit。
- 禁止 force push。
- 已 push commit 如需回滚只能用新的 revert commit，并需人工批准。
- commit、push、merge、rebase、cherry-pick、tag 和 branch 删除都需要当前 gate 明确授权。
- 不得 stage `bin/`、临时数据库、凭据、日志或 provider 响应。
- dirty worktree 中不得清理、覆盖或还原不属于当前任务的用户改动。

### BWG publish bridge rule

以下规则是所有 approved commit 发布到 GitHub 的默认流程；每次 bundle 传输、BWG SSH 和 push 仍需当前 gate 分别明确授权，不能把本规则视为长期授权。

- Codex 当前执行环境默认不直接 push GitHub，也不得尝试修复或修改 Git remote、DNS、credential、token 或认证配置。
- approved commit 必须先在本地确认 branch、HEAD、clean status 和 commit path whitelist，再创建只包含已批准范围的 Git bundle。
- 未明确批准 BWG SSH/SCP 时，只能生成并验证 bundle，然后输出 bundle 路径等待人工处理。
- BWG 不可用或 SSH/SCP 失败时立即停止；只输出已验证的 bundle 路径，不猜测连接目标，不改用其他 publisher，不修 remote/auth。
- 获得明确授权后，只能把 bundle 传到 BWG；禁止 SSH NOSLA。
- BWG 是发布桥，只能在 `/root/staging/embyproxy-staging` 内执行该发布流程。
- BWG 必须依次检查当前 branch、预期 base HEAD、clean status、bundle verify/list-heads、临时 ref HEAD 和变更路径白名单；任何不一致立即停止。
- bundle 只能 fetch 到本次专用临时 ref；不得直接覆盖 branch，不得 merge 未验证 ref。
- 当前发布分支只能通过 `git merge --ff-only` 前进；禁止 merge commit、rebase、reset 和新增 commit。
- 只能 push `HEAD:refs/heads/feature/failover-phase2-local`；禁止 force push，禁止 push `main`、`master` 或其他分支。
- push 后必须只读核对 GitHub 目标 ref 等于 approved commit，并确认 BWG branch/HEAD/status 正常。
- 成功后清理本次专用 BWG 临时 ref 和远端 bundle；不得顺手清理其他 ref、bundle 或文件。
- publish bridge 只负责发布 Git commit；禁止借此部署、替换 binary、reload/restart 服务、修改 Nginx/systemd/DNS/SQLite 或切换流量。
- 输出必须脱敏，不得显示 private key、credential、token、cookie、password、完整 URL query、完整 UUID 或完整订阅链接。
- 任一步失败即停止并保留可复核 evidence；不得猜测修复、扩大 scope 或绕过 gate。

已验证参考案例：`feature/failover-phase2-local` 从 base `7d8ba77` 以 ff-only 前进到 approved commit `871cfc2`，再由 BWG 成功 push 同名 GitHub feature branch。该案例仅证明流程可行，不构成未来 SSH、merge 或 push 授权。

## 外部与运行环境规则

以下操作必须有本轮明确、具体的人工批准，不能从旧批准或 phase 名推导：

- SSH 到 BWG/NOSLA；
- 部署、替换 binary、容器或服务；
- Nginx 配置和 reload/restart；
- systemd unit、enable、restart；
- DNS/provider dry-run 以外的真实 apply；
- 真实 SQLite 写入、迁移或删除；
- admin 写接口；
- 生产流量切换；
- 删除文件、node、备份或数据。

操作必须限制到精确路径、服务、端口、域名和 action。保护未知服务优先于完成任务。

## 凭据与日志

- 遇到 token、Authorization、Cookie、secret、password、API key、私钥、完整 URL query、完整 UUID 或订阅链接必须脱敏。
- 凭据只能从安全环境读取并直接用于请求，不能 echo、写入仓库或出现在命令输出。
- 结构化响应优先使用结构化解析，只输出任务需要的非敏感字段。
- 日志检查必须脱敏；不能用真实账号、密码或媒体 token 做 smoke。

## 变更与验证

- 写入前说明精确文件和行为变化。
- 每次只执行一个获批阶段或子 gate。
- 测试失败必须如实记录，不得自动扩大范围修复。
- 部署必须先有 build/test/hash/backup/rollback 证据。
- Nginx 变更必须独立文件、先备份、先 `nginx -t`，reload 另行批准，默认禁止 restart。
- DNS apply 失败不得提交 active state。

## 记录义务

每完成一步必须更新 `08-progress-log.md`，至少记录：时间、phase、操作、验证、变更、外部影响、secret 状态和下一步。

每发现一个未解决缺口必须更新 `09-gap-log.md`。高严重 gap 未关闭前，禁止进入其标记的后续 phase。

