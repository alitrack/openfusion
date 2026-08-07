# 题型路由配置示例 (Topic Routing)

config.yaml 因含 API key 被 .gitignore 排除。将以下配置 merge 到本地的 `config.yaml` 的 `fusion:` 段即可启用题型路由。

## 配置

```yaml
fusion:
  # ... 现有配置 ...

  # 题型路由: Gemma4 快速分类 (115:8012 /v1/completions)
  classifier_url: "http://10.10.10.115:8012/v1/completions"
  classifier_model: "gemma-4-26B-A4B-it"
  classifier_timeout: 12

  router:
    topic_classifier_enabled: true
    topic_confidence_threshold: 0.7

    # 开放/综述/方法论 → 异构fusion (qwen3.6 + 397B + 122B, 知识广度)
    open_panel:
      - provider: moon-bridge
        model: qwen3.6-35b-a3b
        system: "你是严谨的研究分析师。请详尽、准确地回答下面的问题，注意事实精确性："
      - provider: modelscope
        model: Qwen/Qwen3.5-397B-A17B
        system: "你是深度知识专家。请用广泛的世界知识深入回答下面的问题，给出准确细节："
      - provider: modelscope
        model: Qwen/Qwen3.5-122B-A10B
        system: "你是多学科视角专家。请从多角度完整覆盖下面的问题："
    open_judge:
      provider: cc-switch
      model: gpt-5.4
      system: "你是融合评测员。综合各panel回答，指出共识、矛盾与盲点，整合出超越任何单个回答的最优最终答案。注意：不要引用非权威来源；不要把不同信用评级的债券等同对待；用结构化格式呈现数据。"

    # 强事实/安全约束 → 同构fusion (qwen3.6×2, 敢给具体事实)
    fact_panel:
      - provider: moon-bridge
        model: qwen3.6-35b-a3b
        system: "你是严谨的研究分析师。请详尽、准确地回答下面的问题，注意事实精确性："
      - provider: moon-bridge
        model: qwen3.6-35b-a3b
        system: "你是全面的研究专家。请从多角度完整回答下面的问题，覆盖所有相关方面："
    fact_judge:
      provider: cc-switch
      model: gpt-5.4
      system: "你是融合评测员。综合各panel回答，指出共识、矛盾与盲点，整合出超越任何单个回答的最优最终答案。注意：不要引用非权威来源；不要把不同信用评级的债券等同对待；用结构化格式呈现数据。"

    # 简单题 → 单模型直答 (qwen3.6 快; Gemma4需completions端点支持, 后续再加)
    simple_panel:
      - provider: moon-bridge
        model: qwen3.6-35b-a3b
        system: "直接回答："
    simple_judge:
      provider: moon-bridge
      model: qwen3.6-35b-a3b
      system: "直接回答："

    # 复杂度路由 (回退用)
    simple_threshold: 0.3
    complex_threshold: 0.7
```

## 前置依赖 (providers 段)

```yaml
providers:
  moon-bridge:
    base_url: "http://127.0.0.1:38440"
    api_key: "noop"
  modelscope:
    base_url: "https://api-inference.modelscope.cn"
    api_key: "${MODELSCOPE_API_KEY}"
  cc-switch:
    base_url: "http://127.0.0.1:15722"
    api_key: "sk-noop"
```

## 把关机制 (四层)

1. **Gemma4 分类器**: 题目 → {topic: open|fact|simple, confidence} (~0.4s)
2. **置信度阈值**: confidence < 0.7 → 回退同构fusion (保守)
3. **用户覆盖**: 请求带 `panel:` / `judge:` 参数 → 最高优先级
4. **审计**: 每次路由决策写入 audit EventRouting (fusion.routing)

## 验证结果 (2026-08-07)

- 分类器 8/8 全对 (conf 0.80-0.95)
- 开放题 → 3成员异构panel; 强事实题 → 2成员同构panel
- judge 替换为 cc-switch/gpt-5.4 (modelscope 400 消除)
