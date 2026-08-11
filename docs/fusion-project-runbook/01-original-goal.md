# 原始目标与来源边界

## 最初目标

项目目标是融合 EmbyProxy 与 emby-reverse-proxy-go，形成一个有统一管理能力、可靠代理 data-plane 和 BWG/NOSLA 自动 failover 的系统，而不是并排维护两个互不相干的应用。

## 从 EmbyProxy 需要的能力

- 管理面板和管理员认证；
- 节点增删改查、排序、维护状态和可视化运维；
- SQLite 配置持久化；
- 管理 API、审计和错误反馈；
- managed route 配置与 feature flag；
- 流量、健康、切换历史和运行状态展示；
- 敏感信息脱敏及管理面隔离。

## 从 emby-reverse-proxy-go 需要的能力

- 更适合作为 data-plane 的自动代理/反向代理核心；
- HTTP、HTTPS 和 WebSocket 转发；
- Range、长连接、流式响应和媒体播放路径；
- 目标拨号、TLS、代理环境和连接处理；
- 可嵌入 managed route 的代理执行接口；
- 在 NOSLA 与 BWG 上以无管理面的轻量服务运行。

## 复用与兼容实现原则

适合直接复用的部分，应满足：来源明确、license 允许、边界稳定、测试可移植、保留必要 attribution。候选包括协议处理、经过审计的代理核心算法和通用测试向量。

适合自研兼容实现的部分包括：

- EmbyProxy storage/admin 模型与 data-plane adapter 的接口层；
- managed route schema 和 feature flag；
- failover policy/state machine；
- DNS provider 抽象、mock provider 和 redirect allowlist；
- BWG/NOSLA 特定部署、流量周期和安全边界；
- 为避免耦合或 license/provenance 不清而重新实现的兼容行为。

自研实现必须记录“行为兼容但不是直接复制”的依据，包括设计说明、测试来源和作者。

## License 与 attribution

- 后续必须完成独立的 license/source provenance audit。
- 如果复制 MIT 源码，必须保留适用的 license 和 copyright notice。
- 如果自研实现，要记录没有直接复制以及参考了哪些公开行为或协议。
- 不得凭空声称 MIT 不允许融合；是否可融合应依据实际 license 文本、复制范围和 notice 要求判断。
- 不得仅凭仓库名称、README 或聊天记录推断 license。
- 每个引入文件应能回答：来源 commit、作者/项目、license、是否修改、对应测试。

## 待完成 provenance audit

| 项目 | 上游来源 | 当前基线 | License | 复制范围 | Notice 状态 |
|---|---|---|---|---|---|
| EmbyProxy | `TBD` | `TBD` | `TBD` | `TBD` | `TBD` |
| emby-reverse-proxy-go | `TBD` | `TBD` | `TBD` | `TBD` | `TBD` |

