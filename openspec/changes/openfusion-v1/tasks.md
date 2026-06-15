# Tasks: OpenFusion v1

> Total: **21 tasks** | **Estimated: 4 sessions**

---

## Session 1: Project Scaffolding + Config + Types

- [ ] **1.1 Create `.gitignore`** — Go standard（binary / env / ide）
- [ ] **1.2 Define shared types** in `internal/types/`:
  - `ChatRequest` / `ChatResponse`（OpenAI 兼容格式）
  - `PanelMember`（model + provider + system）
  - `JudgeConfig`（model + provider + system）
  - `FusionResult`（panel 回答 + analysis + final answer）
  - `FusionAnalysis`（consensus / contradictions / partial / unique / blind_spots）
- [ ] **1.3 Config loader** in `internal/config/`:
  - `Config` struct（server / providers / presets / fusion）
  - YAML 文件加载 + env var 替换（`${VAR}` 模式）
  - 验证 provider 配置完整性
- [ ] **1.4 Preset loader** in `internal/preset/`:
  - `Preset` struct（name / description / panel[] / judge）
  - 从 `presets/` 目录加载所有 `.yaml` 文件
  - 内联 preset（写在 config.yaml presets.items 中）
  - Preset 注册表（`map[string]*Preset`）

## Session 2: Provider Abstraction + API Layer

- [ ] **2.1 Provider 接口定义** in `internal/provider/`:
  - `Provider` interface（`ChatCompletion(ctx, req) → resp, error`）
  - `Manager`（按名称注册/获取 provider）
- [ ] **2.2 OpenAI 适配器** in `internal/provider/openai.go`:
  - 标准 `POST /v1/chat/completions` 调用
  - API key 注入
  - 超时控制
- [ ] **2.3 OpenRouter 适配器** in `internal/provider/openrouter.go`:
  - 本质和 OpenAI 一致（OpenRouter 是 OpenAI 兼容）
  - 区别：base_url 不同
- [ ] **2.4 Anthropic 适配器** in `internal/provider/anthropic.go`:
  - 协议转换：Anthropic Messages API → 内部 ChatRequest → OpenAI 兼容
  - 处理 role 映射（assistant vs human vs user）
- [ ] **2.5 HTTP Server** in `internal/api/`:
  - `POST /v1/chat/completions` handler
  - `GET /v1/models` handler
  - Auth middleware（Bearer token 验证）
  - JSON 序列化/反序列化
  - Error handling（标准错误格式）

## Session 3: Fusion Core Engine

- [ ] **3.1 Panel dispatch** in `internal/panel/`:
  - `Dispatcher` 结构体（接收 Preset + 原始请求）
  - goroutine + errgroup 并行分发
  - 每个 panel member 独立超时控制
  - 收集 responses（含失败/超时标记）
- [ ] **3.2 Judge prompt builder** in `internal/judge/`:
  - 构造分析 prompt（含原始问题 + 各 panel 回答）
  - 支持系统 prompt 配置
  - 输出结构化 analysis + 最终答案
- [ ] **3.3 Judge executor** in `internal/judge/`:
  - 调用 Judge 模型（复用 provider 抽象）
  - 解析 Judge 输出
  - 提取 structured analysis + final answer
- [ ] **3.4 Fusion orchestrator** in `internal/api/handler.go`:
  - 串联整个流程：parse preset → panel dispatch → judge → format response
  - Graceful degradation（部分 panel 失败仍可 Judge）
  - 成本累加（逐模型记录 token）

## Session 4: Presets + Config + Integration

- [ ] **4.1 内置 preset 文件** in `presets/`:
  - `budget.yaml` — Gemini 3 Flash + Kimi K2.6 + DeepSeek V4 Pro → Opus 4.8
  - `quality.yaml` — Fable 5 + GPT-5.5 → Opus 4.8
  - `self-ensemble.yaml` — 同一模型 × 2 → 同模型
  - `frontier.yaml` — Opus 4.8 + GPT-5.5 + Gemini 3.1 Pro → Opus 4.8
- [ ] **4.2 Main entry** in `cmd/openfusion/main.go`:
  - 加载配置
  - 加载 preset
  - 初始化 provider manager
  - 启动 HTTP server
  - 优雅关闭（signal handling）
- [ ] **4.3 Example config** `config.example.yaml`:
  - 完整配置示例 + 注释
- [ ] **4.4 Integration test** `internal/e2e/e2e_test.go`:
  - 启动 server
  - 发送 `/v1/chat/completions` 请求
  - 验证响应格式
  - 验证成本字段
  - Mock provider（测试用 fake LLM）

---

## Summary

| Session | Tasks | Deliverable |
|---------|-------|-------------|
| 1 | 1.1~1.4 (4 tasks) | 项目骨架 + 类型定义 + 配置加载 + Preset 系统 |
| 2 | 2.1~2.5 (5 tasks) | Provider 层 + HTTP API 端点 |
| 3 | 3.1~3.4 (4 tasks) | Panel 并行 + Judge 综合 + 编排器 |
| 4 | 4.1~4.4 (4 tasks) | 内置 Preset + 入口 + E2E 测试 |

**总计: 17 开发任务 + 4 预设/配置任务 = 21 任务**

## Dependencies

- 1.1 → 1.2 → 1.3 → 1.4（顺序）
- 2.1 → 2.2 → 2.3 → 2.4（顺序, 2.1 依赖于 1.2）
- 2.5 依赖于 2.1~2.4（所有 provider 配齐后启动 server）
- 3.1 依赖于 2.5（需要 server + provider）
- 3.2, 3.3 依赖于 3.1（panel 返回后才能 Judge）
- 3.4 依赖于全部 3.1~3.3
- 4.1~4.4 可并行，依赖于 3.4
