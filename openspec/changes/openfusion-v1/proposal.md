# Proposal: OpenFusion v1 — Multi-Model Fusion Engine

## Status

proposed

## Problem

OpenRouter 在 2026 年 6 月推出的 Fusion API 展示了多模型并行 + Judge 综合的显著效果：
- **Fusion 组合始终优于单模型**（已验证：所有 panel 组合 > 所有单模型）
- **平价模型 Fusion 可逼近前沿模型性能**（3 个平价模型 @ 50% 成本 = Fable 5 的 99%）
- **自融合效应**（同一模型跑 2 次 + Judge = +6.7 分）

但这些能力迄今为止只有 OpenRouter 的闭源商业 API 提供。团队无法：
- 自托管 Fusion 服务
- 自主选择 panel/judge 模型组合
- 在局域网/离线环境使用
- 按需定制 Fusion 逻辑

需要一个开源、可自托管的替代方案。

## Solution

**OpenFusion**：一个 Go 编写的多模型编排引擎，对外暴露 **OpenAI 兼容的 `/v1/chat/completions` 端点**。

用户发送标准 OpenAI 格式的请求（仅需改 `model` 参数），OpenFusion 内部：
1. 根据 preset 解析 panel 模型列表
2. 并行调用所有 panel 模型
3. Judge 模型分析共识/矛盾/盲区
4. 合成最终答案
5. 以标准 chat.completions 格式返回

### Scope

| Area | What | Why |
|------|------|-----|
| API 层 | `/v1/chat/completions` + `/v1/models` | OpenAI 兼容，零迁移成本 |
| Fusion 核心 | 并行 panel 分发 + Judge 综合 | 核心价值 |
| Provider 抽象 | OpenRouter / OpenAI / Anthropic / DeepSeek / Ollama | 覆盖主流模型来源 |
| Preset 系统 | YAML 定义 panel + judge 组合 | 可配置，可扩展 |
| 成本追踪 | 每请求记录 token + 费用 | 成本可见性 |

### Out of Scope

- 协议转换（moon-bridge 的领域）
- 流式响应（v1 后）
- Web Search 注入（v1 后）
- 请求级 panel 覆盖（v1 后）
- 健康检查 / 就绪探针（v1 后）

## Dependencies

- Go 1.25+
- 网络访问（调用上游 API）

## Risks

- **Risk**: Provider API 方言差异导致 panal 模型调用失败 → **Mitigation**: 统一使用 OpenAI 兼容格式，anthropic 适配器做协议桥接
- **Risk**: 单个 panel 超时阻塞整体 → **Mitigation**: goroutine 并发 + 独立超时 + graceful degradation
- **Risk**: 成本不可控 → **Mitigation**: 每请求记录 cost，LLM 返回中携带 usage 信息
- **Risk**: OpenRouter Fusion 变策略/kill 产品 → **Mitigation**: 我们开源开源，不受商业决策影响

## Timeline Estimate

~4 个开发 session（每个含 3-5 个子任务）
