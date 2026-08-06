#!/usr/bin/env python3
"""
OpenFusion Quality Evaluation Suite

Compares answer quality across configurations:
- Single model (no fusion, no knowledge) - baseline
- Fusion only (no knowledge injection)
- Fusion + MCP knowledge injection

Uses blind LLM-as-Judge methodology.
"""

import json
import os
import subprocess
import sys
import time
from typing import Any

# ---------------------------------------------------------------------------
# Test set: enterprise knowledge questions
# ---------------------------------------------------------------------------

TEST_SET = [
    {
        "id": "pwr_001",
        "category": "power",
        "question": "变电站负荷预测通常使用哪些模型？ARIMA 和 Prophet 各适合什么场景？",
        "expected_aspects": ["ARIMA短期", "Prophet中期", "气象因素", "节假日"]
    },
    {
        "id": "pwr_002",
        "category": "power",
        "question": "SCADA 系统的一级告警包括哪些类型？",
        "expected_aspects": ["设备跳闸", "过流保护", "差动保护", "紧急"]
    },
    {
        "id": "pwr_003",
        "category": "power",
        "question": "电力负荷数据缺失率超过多少需要处理？通常用什么方法填充？",
        "expected_aspects": ["5%", "前向填充", "数据质量"]
    },
    {
        "id": "gen_001",
        "category": "general",
        "question": "什么是检索增强生成（RAG）？它的主要优势是什么？",
        "expected_aspects": ["检索", "生成", "知识库", "减少幻觉"]
    },
    {
        "id": "gen_002",
        "category": "general",
        "question": "解释一下向量数据库在大语言模型应用中的作用。",
        "expected_aspects": ["嵌入", "相似度搜索", "语义检索", "知识库"]
    },
    {
        "id": "gen_003",
        "category": "general",
        "question": "大模型微调和 RAG 有什么区别？各适用于什么场景？",
        "expected_aspects": ["微调改参数", "RAG不改参数", "知识更新频率", "成本差异"]
    },
]

# ---------------------------------------------------------------------------
# Judge prompt
# ---------------------------------------------------------------------------

JUDGE_PROMPT_TEMPLATE = """你是一位公正的 AI 回答质量评审专家。

## 原始问题
{question}

## 期望的回答要点
{expected_aspects}

## 回答 A
{answer_a}

## 回答 B
{answer_b}

## 评审要求
请从以下维度对两个回答分别打分（1-10 分），并给出最终结论：

1. **事实准确度**：回答中的信息是否正确、无幻觉
2. **全面性**：覆盖了多少期望要点
3. **清晰度**：结构是否清晰，是否易于理解
4. **实用性**：是否给出可操作的信息

输出格式（严格 JSON）：
```json
{{
    "scores": {{
        "A": {{ "accuracy": N, "completeness": N, "clarity": N, "practicality": N }},
        "B": {{ "accuracy": N, "completeness": N, "clarity": N, "practicality": N }}
    }},
    "reasoning": "你的判断理由",
    "winner": "A" 或 "B" 或 "tie"
}}
```"""


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def call_openfusion(prompt: str, model: str = "openfusion/budget",
                    knowledge: bool = False) -> str | None:
    """Call OpenFusion API and return the response content."""
    # This is a placeholder — replace with actual API call
    # curl http://localhost:8080/v1/chat/completions
    raise NotImplementedError("Replace with actual OpenFusion API call")


def call_llm(prompt: str, model: str = "deepseek-v4-pro") -> str:
    """Call any LLM directly via cc-switch."""
    import urllib.request
    url = "http://127.0.0.1:15721/v1/chat/completions"
    payload = json.dumps({
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": 2048,
        "temperature": 0.1,
    }).encode()
    req = urllib.request.Request(url, data=payload,
                                 headers={"Content-Type": "application/json"})
    try:
        resp = urllib.request.urlopen(req, timeout=60)
        data = json.loads(resp.read())
        return data["choices"][0]["message"]["content"]
    except Exception as e:
        return f"[ERROR: {e}]"


def parse_json_from_response(text: str) -> dict | None:
    """Extract JSON from LLM response (handles markdown fences)."""
    import re
    # Try ```json ... ```
    m = re.search(r'```(?:json)?\s*([\s\S]*?)```', text)
    if m:
        try:
            return json.loads(m.group(1))
        except json.JSONDecodeError:
            pass
    # Try bare JSON
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        return None


# ---------------------------------------------------------------------------
# Evaluation
# ---------------------------------------------------------------------------

def evaluate():
    """Run the full evaluation."""
    results = []

    for test in TEST_SET:
        qid = test["id"]
        question = test["question"]
        aspects = test["expected_aspects"]

        print(f"\n[{qid}] {question[:50]}...")

        # Get two answers from different configurations
        # Placeholder: both use same LLM for now
        answer_a = call_llm(question, "deepseek-v4-pro")
        answer_b = call_llm(question, "deepseek-v4-flash")

        # Blind judge
        judge_input = JUDGE_PROMPT_TEMPLATE.format(
            question=question,
            expected_aspects="\n".join(f"- {a}" for a in aspects),
            answer_a=answer_a,
            answer_b=answer_b,
        )
        judge_result = call_llm(judge_input, "deepseek-v4-pro")
        parsed = parse_json_from_response(judge_result)

        entry = {
            "id": qid,
            "category": test["category"],
            "question": question,
            "answer_a": answer_a,
            "answer_b": answer_b,
            "judge_raw": judge_result,
            "judge_parsed": parsed,
        }
        results.append(entry)

        # Save intermediate result
        with open(f"eval_results_{qid}.json", "w") as f:
            json.dump(entry, f, ensure_ascii=False, indent=2)

    # Summary
    print("\n" + "=" * 60)
    print("EVALUATION SUMMARY")
    print("=" * 60)
    wins = {"A": 0, "B": 0, "tie": 0}
    for r in results:
        if r["judge_parsed"]:
            w = r["judge_parsed"].get("winner", "tie")
            wins[w] = wins.get(w, 0) + 1
            print(f"  [{r['id']}] Winner: {w}")
        else:
            print(f"  [{r['id']}] Judge parse FAILED")
    print(f"\nTotal: A={wins['A']}, B={wins['B']}, tie={wins['tie']}")

    # Save full results
    with open("eval_results_full.json", "w") as f:
        json.dump({"results": results, "summary": wins}, f,
                  ensure_ascii=False, indent=2)
    print("\nFull results saved to eval_results_full.json")


# ---------------------------------------------------------------------------
# MCP Knowledge Server for evaluation
# ---------------------------------------------------------------------------

def start_knowledge_server():
    """Start the MCP knowledge server for evaluation."""
    import subprocess
    env = {**os.environ, "MCP_KB_DIR": "/home/lhy/.openfusion/knowledge"}
    proc = subprocess.Popen(
        ["python3", "scripts/mcp-knowledge-server.py"],
        cwd="/mnt/d/wsl2/dotnet/openfusion",
        env=env,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    return proc


# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------

if __name__ == "__main__":
    import argparse
    parser = argparse.ArgumentParser(description="OpenFusion Quality Evaluation")
    parser.add_argument("--mode", choices=["check", "run", "summary"],
                       default="check", help="Mode: check env, run eval, or summarize")
    args = parser.parse_args()

    if args.mode == "check":
        # Check environment
        print("Checking environment...")
        # Test cc-switch
        try:
            resp = call_llm("Say hello in one word", "deepseek-v4-flash")
            print(f"  cc-switch (flash): ✅ {resp[:50]}...")
        except Exception as e:
            print(f"  cc-switch: ❌ {e}")

        # Check knowledge base
        kb_dir = "/home/lhy/.openfusion/knowledge"
        if os.path.isdir(kb_dir):
            files = os.listdir(kb_dir)
            print(f"  Knowledge base: ✅ {len(files)} files")
        else:
            print(f"  Knowledge base: ❌ not found at {kb_dir}")

        print("\nReady to run: python3 scripts/eval_quality.py --mode run")

    elif args.mode == "run":
        evaluate()

    elif args.mode == "summary":
        try:
            with open("eval_results_full.json") as f:
                data = json.load(f)
            print(f"Loaded {len(data['results'])} results")
            print(f"Summary: {data['summary']}")
        except FileNotFoundError:
            print("No evaluation results found. Run --mode run first.")
