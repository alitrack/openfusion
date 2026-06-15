# Proposal: OpenFusion v2 — Production-Ready Features

## Status

proposed

## Problem

OpenFusion v1 实现了核心 Fusion 流水线（panel → judge → synthesis），但在生产环境中缺少基础设施能力：

1. **不可观测** — 无法知道每个 preset 花了多少钱、平均延迟、调用次数
2. **功能单一** — 只做 Fusion，不支持 "只跑 panel 不做 judge" 的场景
3. **无保护** — 没有限流机制，一个失控客户端可以打满所有 provider quota
4. **脆弱** — provider 故障只有运行时暴露，没有主动健康检测
5. **浪费** — 相同问题重复调用，没有缓存
6. **难调试** — 链路耗时无法追踪

## Solution

在 OpenFusion v1 的基础上增加 6 项功能，按优先级排序：

| # | 功能 | 价值 | 复杂度 |
|---|---|---|---|
| A | 用量统计 + Cost Dashboard | 成本可见性，告诉用户每个 preset 花了多少钱 | 中 |
| B | No-Judge 模式 | 适用场景扩展：让用户自己当 judge | 低 |
| C | Rate Limiting | 生产安全：防止单个客户端打爆 quota | 中 |
| D | Provider Health Check | 可靠性：主动检测 provider 健康状态 | 中 |
| E | 响应缓存 | 成本优化：相同问题命中缓存省 100% 调用 | 中 |
| F | OpenTelemetry Tracing | 可观测性：全链路追踪，定位性能瓶颈 | 高 |

## Non-Goals

- Web UI（v3 再考虑）
- 用户管理 / 多租户
- 模型路由 / fallback 链（不同于 Fusion）
- Provider 自动切换

---

## Appendix: v1 → v2 diff overview

```
v1: 7 preset, SSE streaming, basic error handling, OpenAI API
v2: +metrics endpoint, +no-judge mode, +rate-limiter, +health checker,
    +response cache, +OTel tracing, +config fields for all above
```
