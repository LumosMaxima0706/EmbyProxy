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

