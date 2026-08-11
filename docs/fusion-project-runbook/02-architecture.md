# 融合架构

## 平面划分

### Management-plane

仅部署在 BWG：

- EmbyProxy 管理面板；
- 管理员认证和管理 API；
- 节点、流量额度、reset day、阈值、模式和维护状态配置；
- 状态、健康、流量、切换历史和审计展示；
- SQLite 持久化。

NOSLA 不运行管理 Web，不开放管理 API。

### Control-plane

仅部署在 BWG：

- failover policy 和状态机；
- `auto`、`force_nosla`、`force_bwg`、`maintenance_nosla` 模式；
- NOSLA/BWG data-plane 健康检查；
- 流量周期、阈值和 reset day 判断；
- DNS dry-run/apply 编排；
- 302 redirect 备选控制；
- 状态持久化、冷却、防抖和切换审计。

DNS 更新失败不得先改变 active state。只有外部变更成功并通过验证后，才提交内部状态转换。

### Data-plane

- NOSLA：优先 data-plane，只运行代理服务，不运行管理面板。
- BWG：fallback data-plane，并与 management/control-plane 使用独立端口、配置和日志。
- data-plane 负责已授权请求的正常转发，不绕过上游认证。

## 推荐数据流

```text
用户 -> stream.149077530.xyz -> 当前 active data-plane
                                 |-- NOSLA（正常优先）
                                 `-- BWG（故障/维护/流量 fallback）

管理员 -> admin.149077530.xyz -> BWG management/control-plane
```

`admin.149077530.xyz` 永远指向 BWG。`stream.149077530.xyz` 根据已提交的 active node 指向 NOSLA 或 BWG。

## Auto policy

在 `auto` 模式下：

1. NOSLA 健康且流量可用时优先 NOSLA。
2. NOSLA 连续健康失败达到阈值、关机、维护或流量达到阈值时选择 BWG。
3. 切换需要最小保持时间和冷却，避免抖动。
4. 到 `reset_day` 后不能把 unknown traffic 当作 0；必须确认进入新周期。
5. NOSLA 在新周期中连续健康成功达到恢复阈值后才允许切回。

强制模式不得被 auto policy 覆盖；`maintenance_nosla` 明确使 NOSLA 不可选。

## DNS failover

推荐采用低 TTL、显式 provider adapter 和两阶段操作：

1. 生成 dry-run 变更和预期 active state；
2. 人工确认；
3. apply DNS；
4. 验证权威 DNS 和外部访问；
5. 最后提交 active state 与审计事件。

必须处理递归缓存、客户端缓存、TTL、失败重试和 provider 幂等性。不得在 provider 返回失败时伪造切换成功。

## 302 redirect 备选

302 模式只在 DNS 切换不满足需求时评估：

- 固定入口根据 active node 返回 allowlist 内目标；
- redirect host、scheme 和 path 必须严格 allowlist；
- 不携带或记录敏感 query、token、cookie；
- 必须评估客户端是否遵循 redirect、登录态、播放连接和长链接行为；
- 不能把 302 当作已经建立连接的无损迁移方案。

## 不推荐的架构

不推荐“用户 -> BWG -> NOSLA -> 上游”的纯级联反代。该方案使用户侧流量始终经过 BWG，同时增加一次转发和双向计费，不能达到优先消耗 NOSLA 并节省 BWG 流量的主要目标。只有经过独立流量测算和人工批准后才可作为特殊备选。

