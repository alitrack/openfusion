#!/usr/bin/env python3
"""
Final MCP Knowledge Injection Test — with 南瓜书 (Pumpkin Book) ML knowledge.

This tests knowledge injection with DEEP, TECHNICAL content 
that even strong models get wrong or incomplete.
Model can guess ML concepts, but 南瓜书 has precise formulas.
"""

import json, re, subprocess, time, yaml, urllib.request

with open("/mnt/d/wsl2/dotnet/openfusion/config.yaml") as f:
    cfg = yaml.safe_load(f)
KEY = cfg["providers"]["deepseek"]["api_key"]

KB_DIR = "/home/lhy/.openfusion/knowledge"

# Questions that test PRECISE technical knowledge (formulas, derivations)
TESTS = [
    {
        "id": "ml_001",
        "question": "线性判别分析（LDA）的优化目标是什么？请给出数学表达式。",
        "kb_match": "线性判别分析 LDA 优化目标",
    },
    {
        "id": "ml_002",
        "question": "决策树中信息增益的计算公式是什么？请举例说明。",
        "kb_match": "决策树 信息增益",
    },
    {
        "id": "ml_003",
        "question": "SVM 的对偶问题是如何推导出来的？KKT 条件是什么？",
        "kb_match": "SVM 对偶问题 KKT",
    },
    {
        "id": "ml_004",
        "question": "BP 神经网络中误差逆传播的权重更新公式是什么？",
        "kb_match": "BP 神经网络 误差逆传播",
    },
    {
        "id": "ml_005",
        "question": "Adaboost 的权重更新公式是什么？请详细说明。",
        "kb_match": "Adaboost 权重更新",
    },
]


def call_llm(messages, model="deepseek-v4-flash", max_tokens=2048, temp=0.1):
    url = "https://api.deepseek.com/v1/chat/completions"
    payload = json.dumps({"model": model, "messages": messages,
                          "max_tokens": max_tokens, "temperature": temp}).encode()
    req = urllib.request.Request(url, data=payload,
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {KEY}"})
    resp = urllib.request.urlopen(req, timeout=120)
    return json.loads(resp.read())["choices"][0]["message"]["content"]


def mcp_search(query: str) -> str:
    proc = subprocess.Popen(
        ["python3", "/mnt/d/wsl2/dotnet/openfusion/scripts/mcp-knowledge-server.py"],
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        env={"MCP_KB_DIR": KB_DIR})

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

    send(json.dumps({"jsonrpc": "2.0", "id": 1, "method": "initialize",
        "params": {"protocolVersion": "2024-11-05", "capabilities": {},
                   "clientInfo": {"name": "eval", "version": "1.0"}}}))
    result = send(json.dumps({"jsonrpc": "2.0", "id": 2, "method": "tools/call",
        "params": {"name": "search_knowledge", "arguments": {"query": query}}}))
    proc.terminate()
    content = result.get("result", {}).get("content", [])
    return content[0].get("text", "") if content else ""


def judge(question, answer_a, answer_b):
    prompt = f"""你是一位严格的 AI 回答质量评审专家，专长是机器学习。

## 问题
{question}

## 回答 A（无知识库上下文）
{answer_a}

## 回答 B（有《南瓜书》机器学习公式知识库上下文）
{answer_b}

## 评分标准（1-10）
- **公式准确性**：数学表达式和推导是否正确
- **完整性**：是否覆盖关键步骤
- **清晰度**：解释是否易于理解

输出严格 JSON：
{{"scores":{{"A":{{"accuracy":N,"completeness":N,"clarity":N}},"B":{{"accuracy":N,"completeness":N,"clarity":N}}}},"winner":"A/B/tie","reason":"一句话理由"}}"""
    result = call_llm([{"role": "user", "content": prompt}], model="deepseek-chat")
    m = re.search(r"```(?:json)?\s*([\s\S]*?)```", result)
    if m:
        try: return json.loads(m.group(1))
        except: pass
    try: return json.loads(result)
    except: return None


def main():
    print("=" * 80)
    print("FINAL TEST: MCP Knowledge Injection with 南瓜书 (Pumpkin Book)")
    print(f"KB: {KB_DIR} (1 file, 12506 lines)")
    print("=" * 80)

    results = []
    wins = {"A": 0, "B": 0, "tie": 0}

    for test in TESTS:
        qid = test["id"]
        question = test["question"]
        print(f"\n[{qid}] {question[:50]}...")

        # MCP retrieval
        t0 = time.time()
        knowledge = mcp_search(test["kb_match"])
        t1 = time.time()
        print(f"  检索: {len(knowledge)} chars in {(t1-t0)*1000:.0f}ms")

        # A: no knowledge
        t0 = time.time()
        answer_a = call_llm([{"role": "user", "content": question}])
        t1 = time.time()
        print(f"  无知识: {len(answer_a)} chars in {t1-t0:.1f}s")

        # B: with knowledge
        prompt_b = f"""请回答以下机器学习问题。除了你的训练知识，以下是《南瓜书》中相关的公式和推导，请参考它们确保回答的准确性：

{knowledge}

---
问题：{question}"""
        t0 = time.time()
        answer_b = call_llm([{"role": "user", "content": prompt_b}])
        t1 = time.time()
        print(f"  有知识: {len(answer_b)} chars in {t1-t0:.1f}s")

        # Judge
        jr = judge(question, answer_a, answer_b)
        if jr:
            sA, sB = jr["scores"]["A"], jr["scores"]["B"]
            avgA = (sA["accuracy"] + sA["completeness"] + sA["clarity"]) / 3
            avgB = (sB["accuracy"] + sB["completeness"] + sB["clarity"]) / 3
            w = jr.get("winner", "tie")
            wins[w] = wins.get(w, 0) + 1
            print(f"  盲审: A={avgA:.1f} vs B={avgB:.1f} → {w}")
            print(f"  理由: {jr.get('reason','')[:200]}")
        else:
            print(f"  盲审: 解析失败")

        results.append({
            "id": qid, "question": question,
            "knowledge": knowledge[:500],
            "answer_without_kb": answer_a,
            "answer_with_kb": answer_b,
            "judge": jr,
        })

    print("\n" + "=" * 80)
    print("RESULTS")
    print("=" * 80)
    total = sum(wins.values())
    for k in ["A", "B", "tie"]:
        v = wins.get(k, 0)
        print(f"  {k}: {v}/{total} ({v/total*100:.0f}%)")
    if wins.get("B", 0) > wins.get("A", 0):
        print(f"\n✅ 知识注入提升明显！")
    elif wins.get("A", 0) > wins.get("B", 0):
        print(f"\n❌ 知识注入未见提升")
    else:
        print(f"\n⚖️ 持平")

    with open("/mnt/d/wsl2/dotnet/openfusion/scripts/eval_pumpkin_results.json", "w") as f:
        json.dump({"wins": wins, "results": results}, f, ensure_ascii=False, indent=2)
    print(f"\nResult: scripts/eval_pumpkin_results.json")


if __name__ == "__main__":
    main()
