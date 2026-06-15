#!/usr/bin/env python3
"""
OpenFusion Benchmark — 多模型融合效果 A/B 盲评

用法:
  # 先启动 OpenFusion server，然后:
  uv run scripts/benchmark.py --questions 5

原理:
  对每个问题，分别获取:
    A) 模型 1 单独回答
    B) 模型 2 单独回答  
    C) Fusion(模型1 + 模型2 → Judge) 综合回答
  然后由 EVALUATOR 盲评打分 (1-10)，比较平均分。
"""

import json
import os
import sys
import time
import urllib.request
import urllib.error

# ── 配置 ──────────────────────────────────────────────
OPENFUSION_URL = os.environ.get("OPENFUSION_URL", "http://127.0.0.1:8080/v1/chat/completions")
EVALUATOR_MODEL = os.environ.get("EVALUATOR_MODEL", "ZhipuAI/GLM-5.1")
EVALUATOR_PROVIDER = os.environ.get("EVALUATOR_PROVIDER", "modelscope")
EVALUATOR_KEY = os.environ.get("MODELSCOPE_KEY", "ms-8ecc2026-4dfb-4f22-8e7a-e60d45c676c0")
EVALUATOR_URL = os.environ.get("EVALUATOR_URL", "https://api-inference.modelscope.cn/v1/chat/completions")

# 默认测试问题（可换）
DEFAULT_QUESTIONS = [
    "Python 的 async/await 和 Go 的 goroutine 在并发模型上有什么本质区别？",
    "Rust 的所有权系统和 Java 的 GC 在内存管理哲学上谁更优？为什么？",
    "SQL 数据库和 NoSQL 数据库在什么场景下应该选择哪个？给出决策框架。",
    "解释微服务架构中 API Gateway 和 Service Mesh 的分工关系。",
    "Linux 的 cgroups 和 FreeBSD 的 jail 在容器化思路上的核心差异是什么？",
    "对比 Transformer 和 SSM（状态空间模型）在长序列建模中的优劣。",
    "为什么 Redis 快？它的单线程模型在当下多核时代还是优势吗？",
    "解释 TCP 拥塞控制中的 BBR 与传统基于丢包的算法有什么本质不同。",
    "Kubernetes 的调度器是如何做 Pod 调度的？影响调度的主要因素有哪些？",
    "函数式编程中的 Monad 是什么？用通俗的语言解释。",
]


def call_llm(url, key, model, messages, max_tokens=2000):
    """调用任意 OpenAI 兼容 API，返回回答文本"""
    data = json.dumps({
        "model": model,
        "messages": messages,
        "max_tokens": max_tokens,
        "temperature": 0.3,
    }).encode()

    req = urllib.request.Request(
        url, data=data,
        headers={
            "Authorization": f"Bearer {key}",
            "Content-Type": "application/json",
        },
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=180) as resp:
            result = json.loads(resp.read())
            return result["choices"][0]["message"]["content"]
    except Exception as e:
        return f"[ERROR: {e}]"


def call_openfusion(preset, question):
    """调用 OpenFusion API"""
    return call_llm(
        OPENFUSION_URL,
        "",  # OpenFusion 没设 auth
        preset,
        [{"role": "user", "content": question}],
    )


def evaluate_answers(question, answers, evaluator_model):
    """盲评：让 evaluator 给所有答案打分排序"""
    prompt = f"""你是严谨的 AI 回答评估专家。请对下面同一问题的多个回答进行盲评。

=== 问题 ===
{question}

=== 回答 A ===
{answers['A'][:1500]}

=== 回答 B ===
{answers['B'][:1500]}

=== 回答 C (Fusion) ===
{answers['C'][:1500]}

请按以下标准对每个回答评分 (1-10 分):
1. **事实准确度** — 是否有事实错误？结论是否可靠？
2. **覆盖广度** — 是否覆盖了问题的多个角度？
3. **洞察深度** — 是否有独到的见解和分析？
4. **结构清晰度** — 是否条理清晰，易于理解？

请输出:

## 评分
| 回答 | 准确度 | 广度 | 深度 | 清晰度 | 总分 |
|---|---|---|---|---|---|
| A | ? | ? | ? | ? | ? |
| B | ? | ? | ? | ? | ? |
| C | ? | ? | ? | ? | ? |

## 排名 (1st, 2nd, 3rd)
[你的排名]

## 简短理由
[为什么这个排名]
"""
    return call_llm(
        EVALUATOR_URL,
        EVALUATOR_KEY,
        evaluator_model,
        [{"role": "user", "content": prompt}],
        max_tokens=2000,
    )


def parse_scores(eval_text):
    """从 evaluator 输出中解析分数"""
    import re
    scores = {}
    for label in ['A', 'B', 'C']:
        # 匹配表格行: | A | 8 | 7 | 8 | 9 | 32 |
        pattern = rf'\|\s*{label}\s*\|(\s*\d+)\s*\|(\s*\d+)\s*\|(\s*\d+)\s*\|(\s*\d+)\s*\|(\s*\d+)'
        m = re.search(pattern, eval_text)
        if m:
            scores[label] = {
                'accuracy': int(m.group(1)),
                'breadth': int(m.group(2)),
                'depth': int(m.group(3)),
                'clarity': int(m.group(4)),
                'total': int(m.group(5)),
            }
    return scores


def main():
    import argparse
    parser = argparse.ArgumentParser(description="OpenFusion A/B Benchmark")
    parser.add_argument("--questions", type=int, default=3, help="测试问题数量 (default: 3)")
    parser.add_argument("--model-a", default="deepseek-ai/DeepSeek-V4-Pro", help="单模型 A")
    parser.add_argument("--model-b", default="Qwen/Qwen3.5-27B", help="单模型 B")
    parser.add_argument("--fusion-preset", default="openfusion/budget", help="Fusion preset")
    parser.add_argument("--list-questions", action="store_true", help="列出默认问题")
    args = parser.parse_args()

    if args.list_questions:
        for i, q in enumerate(DEFAULT_QUESTIONS, 1):
            print(f"{i:2d}. {q}")
        return

    questions = DEFAULT_QUESTIONS[:args.questions]
    n = len(questions)

    print(f"\n{'='*60}")
    print(f"OpenFusion A/B 盲评基准测试")
    print(f"{'='*60}")
    print(f"问题数: {n}")
    print(f"模型 A: {args.model_a} (单独)")
    print(f"模型 B: {args.model_b} (单独)")
    print(f"Fusion: {args.fusion_preset}")
    print(f"评估器: {EVALUATOR_MODEL}")
    print()

    all_scores = {'A': [], 'B': [], 'C': []}

    for i, question in enumerate(questions, 1):
        print(f"\n[{i}/{n}] 问题: {question[:60]}...")
        print(f"{'-'*50}")

        # Step 1: 获取三个回答
        print("  ⏳ 获取模型 A 单独回答...", end=" ", flush=True)
        ans_a = call_llm(
            EVALUATOR_URL, EVALUATOR_KEY, args.model_a,
            [{"role": "user", "content": question + " 200字以内。"}]
        )
        print(f"({len(ans_a)} chars)")

        print("  ⏳ 获取模型 B 单独回答...", end=" ", flush=True)
        ans_b = call_llm(
            EVALUATOR_URL, EVALUATOR_KEY, args.model_b,
            [{"role": "user", "content": question + " 200字以内。"}]
        )
        print(f"({len(ans_b)} chars)")

        print("  ⏳ 获取 Fusion 回答...", end=" ", flush=True)
        ans_c = call_openfusion(args.fusion_preset, question + " 200字以内。")
        print(f"({len(ans_c)} chars)")

        # Step 2: 盲评
        print("  ⏳ 盲评中...", end=" ", flush=True)
        eval_result = evaluate_answers(question, {'A': ans_a, 'B': ans_b, 'C': ans_c}, EVALUATOR_MODEL)
        print("done")

        # Step 3: 解析分数
        scores = parse_scores(eval_result)
        for label in ['A', 'B', 'C']:
            if label in scores:
                s = scores[label]
                total = s.get('total', s['accuracy'] + s['breadth'] + s['depth'] + s['clarity'])
                all_scores[label].append(total)
                print(f"    回答 {label}: 准确={s['accuracy']} 广度={s['breadth']} 深度={s['depth']} 清晰={s['clarity']} 总分={total}")
            else:
                print(f"    回答 {label}: 分数解析失败")
                print(f"    Raw: {eval_result[:200]}")

    # ── 汇总 ──
    print(f"\n{'='*60}")
    print(f"结果汇总 ({n} 题)")
    print(f"{'='*60}")
    for label, name in [('A', f'单模型 {args.model_a.split("/")[-1]}'),
                          ('B', f'单模型 {args.model_b.split("/")[-1]}'),
                          ('C', 'Fusion (A+B→Judge)')]:
        scores = all_scores[label]
        if scores:
            avg = sum(scores) / len(scores)
            print(f"  {name}: {avg:.1f} 分 (每题: {', '.join(map(str, scores))})")
        else:
            print(f"  {name}: 无数据")

    # 胜率统计
    wins = {'A': 0, 'B': 0, 'C': 0}
    for i in range(n):
        scores_i = {label: all_scores[label][i] for label in ['A', 'B', 'C'] if i < len(all_scores[label])}
        if scores_i:
            best = max(scores_i, key=scores_i.get)
            wins[best] = wins.get(best, 0) + 1

    print(f"\n胜率 (最高分题数):")
    for label, name in [('A', 'Model A'), ('B', 'Model B'), ('C', 'Fusion')]:
        print(f"  {name}: {wins.get(label, 0)}/{n}")


if __name__ == "__main__":
    main()
