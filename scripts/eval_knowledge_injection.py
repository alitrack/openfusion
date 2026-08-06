#!/usr/bin/env python3
"""
MCP Knowledge Injection: With vs Without comparison test.

Control variable: same model, same question.
Only difference: experimental group gets MCP knowledge context injected.

Usage:
  python3 scripts/eval_knowledge_injection.py
"""

import json
import re
import subprocess
import sys
import time
import yaml
import urllib.request

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------

with open("/mnt/d/wsl2/dotnet/openfusion/config.yaml") as f:
    cfg = yaml.safe_load(f)
KEY = cfg["providers"]["deepseek"]["api_key"]

MODEL = "deepseek-v4-flash"  # same model for both A and B

# ---------------------------------------------------------------------------
# Knowledge base path
# ---------------------------------------------------------------------------

KB_DIR = "/home/lhy/.openfusion/knowledge"

# ---------------------------------------------------------------------------
# Test set: questions that the knowledge base CAN answer
# ---------------------------------------------------------------------------

TESTS = [
    {
        "id": "kb_001",
        "question": "变电站负荷预测可以使用哪些模型？各有什么特点？",
        "kb_match": "变电站负荷预测",
    },
    {
        "id": "kb_002",
        "question": "SCADA 系统的告警分几级？每一级包含什么内容？",
        "kb_match": "SCADA 告警分类",
    },
    {
        "id": "kb_003",
        "question": "电力负荷数据的数据质量有哪些要求？异常值怎么检测？",
        "kb_match": "数据质量",
    },
    {
        "id": "kb_004",
        "question": "变电站负荷预测的典型准确率是多少？",
        "kb_match": "典型准确率",
    },
    {
        "id": "kb_005",
        "question": "什么是前向填充？在什么情况下使用？",
        "kb_match": "前向填充",
    },
]

# ---------------------------------------------------------------------------
# LLM API
# ---------------------------------------------------------------------------

def call_llm(messages, model=MODEL, max_tokens=1024, temp=0.1):
    """Call DeepSeek API."""
    url = "https://api.deepseek.com/v1/chat/completions"
    payload = json.dumps({
        "model": model,
        "messages": messages,
        "max_tokens": max_tokens,
        "temperature": temp,
    }).encode()
    req = urllib.request.Request(url, data=payload,
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {KEY}"})
    resp = urllib.request.urlopen(req, timeout=60)
    data = json.loads(resp.read())
    return data["choices"][0]["message"]["content"]

# ---------------------------------------------------------------------------
# MCP Knowledge retrieval
# ---------------------------------------------------------------------------

def mcp_search(query: str) -> str:
    """Query the MCP knowledge server via stdio."""
    server_script = "/mnt/d/wsl2/dotnet/openfusion/scripts/mcp-knowledge-server.py"
    proc = subprocess.Popen(
        ["python3", server_script],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        env={"MCP_KB_DIR": KB_DIR},
    )

    def send(body: str) -> dict:
        data = body.encode("utf-8")
        msg = f"Content-Length: {len(data)}\r\nContent-Type: application/json\r\n\r\n".encode()
        proc.stdin.write(msg + data)
        proc.stdin.flush()
        while True:
            line = proc.stdout.readline()
            if not line:
                break
            line = line.decode().strip()
            if line.startswith("Content-Length:"):
                length = int(line.split(":")[1].strip())
            elif line == "":
                return json.loads(proc.stdout.read(length))

    # Initialize
    send(json.dumps({
        "jsonrpc": "2.0", "id": 1, "method": "initialize",
        "params": {"protocolVersion": "2024-11-05", "capabilities": {},
                   "clientInfo": {"name": "eval", "version": "1.0"}}
    }))

    # Search
    result = send(json.dumps({
        "jsonrpc": "2.0", "id": 2, "method": "tools/call",
        "params": {"name": "search_knowledge", "arguments": {"query": query}}
    }))

    proc.terminate()
    content = result.get("result", {}).get("content", [])
    if content:
        return content[0].get("text", "")
    return ""

# ---------------------------------------------------------------------------
# Judge
# ---------------------------------------------------------------------------

def blind_judge(question: str, answer_a: str, answer_b: str, aspects: list[str]) -> dict | None:
    """Ask a blind judge which answer is better."""
    aspects_str = "\n".join(f"- {a}" for a in aspects)
    prompt = f"""你是一位公正的 AI 回答质量评审专家。

## 原始问题
{question}

## 期望的回答要点
{aspects_str}

## 回答 A（对照组 — 无知识库）
{answer_a}

## 回答 B（实验组 — 有知识库上下文）
{answer_b}

注意：回答 A 是普通回答，回答 B 在回答前获得了领域知识库的上下文。
请公平评审，不要因为"有知识库"就偏爱 B，看实际质量。

## 评审要求
从准确度(1-10)、全面性(1-10)、清晰度(1-10)打分。
输出格式（严格 JSON）：
{{"scores":{{"A":{{"accuracy":N,"completeness":N,"clarity":N}},"B":{{"accuracy":N,"completeness":N,"clarity":N}}}},"winner":"A或B或tie","reason":"一句话理由"}}"""

    result = call_llm([{"role": "user", "content": prompt}], model="deepseek-chat")

    # Parse JSON
    m = re.search(r"```(?:json)?\s*([\s\S]*?)```", result)
    if m:
        try: return json.loads(m.group(1))
        except: pass
    try: return json.loads(result)
    except: return None

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main():
    print("=" * 70)
    print("MCP KNOWLEDGE INJECTION EVALUATION")
    print(f"Model: {MODEL}")
    print(f"Knowledge base: {KB_DIR} ({len(TESTS)} test questions)")
    print("=" * 70)

    results = []
    wins = {"A": 0, "B": 0, "tie": 0}

    for test in TESTS:
        qid = test["id"]
        question = test["question"]
        kb_match = test["kb_match"]

        print(f"\n[{qid}] {question}")

        # Step 1: Retrieve knowledge from MCP
        t0 = time.time()
        knowledge = mcp_search(kb_match)
        t1 = time.time()
        kb_time = (t1 - t0) * 1000
        print(f"  MCP检索: {len(knowledge)} chars in {kb_time:.0f}ms")

        # Step 2: Generate answer WITHOUT knowledge (control)
        msg_no_kb = [{"role": "user", "content": question}]
        t0 = time.time()
        answer_a = call_llm(msg_no_kb)
        t1 = time.time()
        print(f"  无知识回答: {len(answer_a)} chars in {t1-t0:.1f}s")

        # Step 3: Generate answer WITH knowledge (experiment)
        context_msg = f"""以下是领域知识库中与你的问题相关的上下文信息，请基于这些知识回答：

{knowledge}

---
用户问题：{question}"""

        msg_with_kb = [{"role": "user", "content": context_msg}]
        t0 = time.time()
        answer_b = call_llm(msg_with_kb)
        t1 = time.time()
        print(f"  有知识回答: {len(answer_b)} chars in {t1-t0:.1f}s")

        # Step 4: Blind judge
        aspects = [f"回答应包含关于「{kb_match}」的准确信息"]
        judge_result = blind_judge(question, answer_a, answer_b, aspects)

        if judge_result:
            sA = judge_result["scores"]["A"]
            sB = judge_result["scores"]["B"]
            avgA = (sA["accuracy"] + sA["completeness"] + sA["clarity"]) / 3
            avgB = (sB["accuracy"] + sB["completeness"] + sB["clarity"]) / 3
            w = judge_result.get("winner", "tie")
            wins[w] = wins.get(w, 0) + 1
            reason = judge_result.get("reason", "")
            print(f"  Judge: A={avgA:.1f} vs B={avgB:.1f} → Winner: {w}")
            print(f"  理由: {reason[:120]}")
        else:
            print(f"  Judge: PARSE FAILED")
            print(f"  Raw: {judge_result}")

        results.append({
            "id": qid,
            "question": question,
            "knowledge_context": knowledge,
            "knowledge_retrieval_ms": kb_time,
            "answer_without_kb": answer_a,
            "answer_with_kb": answer_b,
            "judge_parsed": judge_result,
        })

        # Save intermediate
        with open(f"/mnt/d/wsl2/dotnet/openfusion/scripts/eval_kb_{qid}.json", "w") as f:
            json.dump(results[-1], f, ensure_ascii=False, indent=2)

    # Summary
    print("\n" + "=" * 70)
    print("SUMMARY")
    print("=" * 70)
    total = sum(wins.values())
    print(f"Total: {total} questions")
    print(f"  A (无知识) wins: {wins.get('A', 0)}")
    print(f"  B (有知识) wins: {wins.get('B', 0)}")
    print(f"  Tie:          {wins.get('tie', 0)}")
    if wins.get('B', 0) > wins.get('A', 0):
        pct = wins['B'] / total * 100
        print(f"\n✅ 知识注入优势: B 胜出 {pct:.0f}% 的测试")
    elif wins.get('A', 0) > wins.get('B', 0):
        pct = wins['A'] / total * 100
        print(f"\n❌ 知识注入未体现优势: A 胜出 {pct:.0f}%")
    else:
        print("\n⚖️ 持平，无显著差异")

    # Save full results
    with open("/mnt/d/wsl2/dotnet/openfusion/scripts/eval_kb_full.json", "w") as f:
        json.dump({
            "config": {"model": MODEL, "kb_dir": KB_DIR},
            "results": results,
            "wins": wins,
        }, f, ensure_ascii=False, indent=2)
    print(f"\nFull results: scripts/eval_kb_full.json")


if __name__ == "__main__":
    main()
