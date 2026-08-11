# Future Session 固定开场 Prompt

以后每次启动新的 Codex session，先发送以下内容，再追加本次具体请求：

```text
这是 EmbyProxy + emby-reverse-proxy-go + BWG/NOSLA failover 融合项目。

开始任何操作前：
1. 完整阅读 docs/fusion-project-runbook/README.md。
2. 完整阅读 docs/fusion-project-runbook/00-current-state.md。
3. 完整阅读 docs/fusion-project-runbook/03-phase-map.md。
4. 完整阅读 docs/fusion-project-runbook/07-codex-operating-rules.md。
5. 只读确认当前 Git branch、HEAD、origin ref 和 worktree 状态。
6. 区分 VERIFIED、USER-REPORTED 和 UNKNOWN/TBD，不能凭记忆判断。
7. 判断项目当前处于哪个 phase，列出通过标准和阻塞 gap。
8. 输出建议下一步，然后停下等待需要的人工批准。

未经本轮明确批准，不得：
- 修改代码或现有文档；
- commit、push、merge、rebase、reset 或 force push；
- 构建、部署、替换 binary 或重启服务；
- SSH BWG/NOSLA；
- 修改或 reload/restart Nginx；
- 修改 systemd；
- 修改 DNS 或连接真实 provider；
- 写入真实 SQLite；
- 调用 admin 写接口；
- 切生产流量；
- 输出 token、cookie、secret、password、完整 URL query、完整 UUID 或订阅链接。

不能把 WebSocket、failover 或其他单个子任务完成误判为整个融合项目完成。
每完成一个获批步骤，更新 docs/fusion-project-runbook/08-progress-log.md；发现缺口时更新 09-gap-log.md。
```

