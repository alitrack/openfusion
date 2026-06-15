# Design: OpenFusion v1 — Architecture Decisions

---

## ADR-1: OpenAI 兼容 API 作为唯一接口

**Decision**: 只暴露 `/v1/chat/completions`（OpenAI 兼容格式），不提供 MCP / gRPC / 自定义协议。

**Alternatives considered**:
- MCP Server — 仅限 MCP 客户端可用，不能 curl
- gRPC — 需要额外客户端生成
- 自定义协议 — 增加学习成本

**Rationale**: OpenAI 兼容是所有 LLM 工具链的通用接口。curl、Python SDK、LangChain、Codex、Claude Code 均原生支持。用户只需改 `model` 参数即可接入，零迁移成本。

---

## ADR-2: Go Std Library net/http，无第三方 HTTP 框架

**Decision**: 使用 Go 标准库 `net/http`（Go 1.25+ 增强路由），不引入 gin/chi/echo。

**Alternatives considered**:
- Gin — 社区流行但增加依赖
- Chi — 轻量但 1.25 原生路由已够用

**Rationale**: Go 1.22+ 的 `mux.HandleFunc("POST /v1/chat/completions", handler)` 即可满足需求。减少依赖 = 减少维护负担 + 安全风险。

---

## ADR-3: goroutine + errgroup 实现并行 panel 分发

**Decision**: 使用 `sync.WaitGroup` + `errgroup` 管理 panel 并发。

**Alternatives considered**:
- 串行调用 — 实现简单但延迟 = Σ 各模型延迟
- 消息队列 — 过度设计

**Rationale**: Panel 数量固定（2-8 个），Go goroutine 开销极低（~2KB/个），`errgroup` 天然支持超时 propagate 和 early cancel。

---

## ADR-4: Judge 通过 LLM Chat 调用实现，而非硬编码规则

**Decision**: Judge 是一个特殊的 LLM 调用（分析 prompt + 综合 prompt），而非规则/模板。

**Alternatives considered**:
- 规则引擎（consensus via exact match）— 不适用于自然语言
- 简单投票 — 精度不够，丢失独特发现
- 专用分类模型 — 需要训练和维护

**Rationale**: Fusion 论文证明 ~3/4 提升来自合成步骤。LLM 作为 Judge 能识别语义共识、矛盾、盲区，这些是规则无法做到的。Judge 模型可由用户自由选择。

---

## ADR-5: Preset 用 YAML 文件定义

**Decision**: Preset 以 `.yaml` 文件存储在 `presets/` 目录，支持运行时文件夹扫描。

**Alternatives considered**:
- 硬编码在 Go binary 中 — 改 Preset 需重编译
- JSON Schema — YAML 更易读
- 数据库存储 — 过度设计

**Rationale**: YAML 是最广泛使用的 LLM 配置文件格式（OpenRouter / Claude Code / Codex 都用）。文件夹扫描允许用户在运行时通过 `cp` 添加新 preset，无需重启。

---

## ADR-6: 单 binary 部署，无外部依赖

**Decision**: OpenFusion 编译为一个独立的 Go binary，无数据库、无 runtime 依赖。

**Alternatives considered**:
- Docker 容器 — 增加部署复杂度
- 依赖 SQLite — 对配置管理无必要
- 分多个微服务 — 过度设计

**Rationale**: MVP 阶段不需要持久化。配置 + preset 从文件加载，重启即重读。后期需要时再加存储层。

---

## ADR-7: Panel 模型统一使用 OpenAI Chat API 格式

**Decision**: 所有 provider 适配器将请求转换为 OpenAI Chat Completions 格式发送给模型。

**Alternatives considered**:
- 保留各 provider 原生格式 — 增加适配复杂度
- 统一用 Anthropic 格式 — 不是最通用

**Rationale**: OpenAI Chat API 是最普遍的标准。OpenRouter 原生支持此格式，Anthropic / DeepSeek 也提供兼容端点。仅需为 Anthropic 原生 SDK 写一个协议桥接。

---

## ADR-8: 错误处理策略 — Graceful Degradation

**Decision**: 单个 panel 模型超时或失败不影响其他 panel。Judge 在 panel 回答不完整时仍进行综合，并标注"部分模型未返回"。

**Alternatives considered**:
- 全部成功才返回 — 延迟爆炸
- 重试失败模型 — 可能无限等待
- 跳过 Judge 直接返回 — 丧失核心价值

**Rationale**: Graceful degradation 在 OpenRouter 的 Fusion 实现中也被采用。部分 panel 回答仍能提供有价值的综合。Judge 在 analysis 中标注哪些模型缺失。
