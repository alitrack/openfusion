# Spec: No-Judge Mode

## Behaviour

When `"judge": false` in the request, skip the judge synthesis step. Return all panel responses directly.

## Request Change

```json
{
  "model": "openfusion/budget",
  "messages": [...],
  "judge": false
}
```

## Response Change (non-streaming)

```json
{
  "id": "fusion-...",
  "object": "chat.completion",
  "model": "openfusion/budget",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "=== modelscope / deepseek-ai/DeepSeek-V4-Pro ===\n...\n\n=== modelscope / Qwen/Qwen3.5-27B ===\n..."
    }
  }],
  "panel_responses": [
    {
      "model": "deepseek-ai/DeepSeek-V4-Pro",
      "content": "...",
      "duration_ms": 2100,
      "tokens": { "prompt": 120, "completion": 80 }
    },
    {
      "model": "Qwen/Qwen3.5-27B",
      "content": "...",
      "duration_ms": 3400,
      "tokens": { "prompt": 130, "completion": 90 }
    }
  ],
  "usage": { "total_tokens": 420, "cost_usd": 0.0015 }
}
```

## Implementation

1. Add `Judge bool` field to `types.ChatRequest` (default `true`)
2. In `fusion.Engine.Execute()`: if `!req.Judge`, skip judge step, construct response with panel responses directly
3. Add `PanelResponses []PanelResponseSummary` field to `types.ChatResponse`
4. `content` in `choices[0].message` = concatenation of all panel responses with model labels

## Test Scenarios

- S1: judge=false returns all panel responses (not just first)
- S2: judge=false does not call judge model (0 cost from judge)
- S3: judge=false still respects panel timeout/failure
- S4: judge=true (default) works as before, no panel_responses in output
