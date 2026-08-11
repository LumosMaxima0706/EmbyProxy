# Phase 4 BWG 旁路部署计划

状态：未来计划，当前不执行。

## 必要人工批准

必须逐字获得：

> 我批准进入 Phase 4 BWG 旁路部署

没有这句话：

- 不允许 SSH BWG 或 NOSLA；
- 不允许部署；
- 不允许修改 Nginx；
- 不允许创建/修改 systemd unit；
- 不允许替换 binary 或重启服务。

## 部署边界

- 只能部署到 BWG。
- 服务只能监听 `127.0.0.1:18082`。
- 使用独立目录、配置、日志、service name 和 binary backup。
- Nginx 只能增加测试入口 `/embyproxy-gsy-test/`。
- 不替换现有 `/admin/`。
- 不替换现有 `/s/`。
- 不修改 `stream.149077530.xyz` 生产 DNS。
- 不切生产流量。
- 不在 NOSLA 部署管理面或管理 API。
- 不修改未知现有服务、rathole、3x-ui 或已有 server block。

## 执行前 preflight

1. 确认 Phase 3 证据和候选 SHA。
2. 只读检查 BWG 端口、Nginx include、证书、systemd 名称、磁盘和现有服务。
3. 确认 `127.0.0.1:18082` 未占用。
4. 展示将新增的精确文件、Nginx diff、service unit、日志路径和 SHA。
5. 为每个将变更的文件创建时间戳备份。
6. `nginx -t` 必须在 reload gate 前通过。
7. 将“创建配置”和“reload Nginx”拆成两个人工批准 gate。

## 旁路验收

- 本机直连 `127.0.0.1:18082` 有应用响应。
- `/embyproxy-gsy-test/` 只进入新 sidecar。
- 现有 `/admin/`、`/s/` 和生产域名行为不变。
- admin 未认证被拒绝，管理面不经测试入口公开。
- managed route、旧 fallback、WebSocket 4xx/101、Range 和日志脱敏通过。
- 没有 502/503/504 回归。
- 不存在公网 `18082` 监听。

## Rollback

发生失败时只回滚本次新增旁路资产：

1. 停止后续 smoke/切换，不扩大修改范围。
2. 恢复或停用独立 `/embyproxy-gsy-test/` 配置，不修改原有 server block。
3. 运行 `nginx -t`；只有通过后才按已批准 gate reload。
4. 恢复旧 sidecar binary/config，必要时只重启新增 sidecar service。
5. 验证原有 `/admin/`、`/s/`、Nginx、rathole 和其他现有服务。
6. 保留日志、备份和临时数据库用于分析，不删除未知数据。

Phase 4 不授权 DNS apply、NOSLA 部署或生产切流。

