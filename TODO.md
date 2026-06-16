# OpenFusion TODO

> 开源多模型融合编排引擎 — 个人 IP，不涉及公司项目
> 状态更新：2026-06-16

---

## Short-term (1-2 周)

### MCP 知识服务集成

- [x] MCP 客户端模块 (`internal/mcp/`)
  - Stdio 传输（子进程通信）
  - HTTP/SSE 传输（远程 MCP Server）
  - 完整 MCP 生命周期（initialize → tools/list → tools/call）
  - 知识检索注入（panel dispatch 前调 MCP，结果注入 system prompt）
- [x] Skill 系统集成
  - `Strategy.MCPKnowledge` 配置字段
  - `executor.go` 中调用 `mcp.SearchAndInject`
- [ ] **ChatSQL MCP 连接测试** — 验证 `dotnet tool run chatsql-mcp` 作为知识源
- [ ] **OntoMind MCP 连接测试** — 验证本体知识查询
- [ ] **端到端示例** — 一个完整的 skill 配置 + 知识源连线文档

### 文档完善

- [ ] 添加 MCP 知识源配置文档到 README
- [ ] AGENTS.md 更新 MCP 模块说明
- [ ] 示例 skill 配置（domain-advisor.skill.yaml）完善注释
- [ ] MCP 知识服务器开发指南

### 测试

- [ ] MCP 客户端单元测试（mock transport）
- [ ] MCP 集成测试（用 Python 测试服务器）

---

## Mid-term (1-2 月)

### Fusion 引擎优化（另一个线程进行）

- [ ] 融合引擎性能优化（streaming、并发控制）
- [ ] 延迟优化：泛型复用、逃逸分析、零分配路径

### 功能扩展

- [ ] **Ontology-aware skill matching** — trigger 支持 `ontology_domains` 字段
- [ ] **缓存增强** — MCP 检索结果缓存，避免重复调用
- [ ] **Fallback 策略** — MCP 知识源不可用时 graceful degradation
- [ ] **MCP 多源并发** — 多个知识源并行检索，聚合结果

### 评估

- [ ] **盲审质量评测** — single model vs fusion vs fusion+MCP knowledge 对比
- [ ] **延迟基准** — 有/无 MCP 检索的 p50/p95 延迟
- [ ] **Benchmark 套件维护** — 测试集扩充

---

## Long-term (2-6 月)

### 论文准备

- [ ] 跑通端到端案例（电力预测场景）
- [ ] 收集评测数据
- [ ] 论文初稿
- [ ] 投稿（目标：ACL Demo / EMNLP Demo / AAAI Demo）

### 生态

- [ ] **SwanFlow MCP 集成** — 连接 SwanFlow 的 MCP Server
- [ ] **知识飞轮闭环** — Agent 反馈 → 知识库优化 → 效果提升
- [ ] **CONTRIBUTING.md** — 社区贡献指南
- [ ] **GitHub Actions CI** — 自动测试 + lint

### 商业化准备

- [ ] **行业模板** — 电力/制造/零售 predesigned skills + 知识源配置
- [ ] **定价模型示例** — skill 配置按行业打包
- [ ] **Case Study** — 第一个客户的完整技术方案

---

## 已知问题

- `internal/mcp/` 暂无单元测试（需要 mock transport）
- `internal/mcp/transport.go` 的 HTTP transport 未在生产环境验证
- `mcp.SearchAndInject` 在 `ModeDirect` 路径未覆盖（当前只在 fusion/self-ensemble 路径调）

---

> 此文件位于 repo 根目录，跟踪 OpenFusion 的开发路线。
> 关联 wiki: `wiki/raw/openfusion/` 目录
