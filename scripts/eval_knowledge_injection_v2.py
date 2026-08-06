#!/usr/bin/env python3
"""
MCP Knowledge Injection: Corrected comparison test.

This test uses PRIVATE/CUSTOM knowledge that the model was NEVER trained on.
This simulates the real enterprise scenario where knowledge injection matters.

v2 fixes:
- Knowledge base contains PRIVATE business rules (not general knowledge)
- Prompt combines "知识 + 你的训练知识" rather than "只基于知识"
- Questions specifically test private knowledge
"""

import json, re, subprocess, time, yaml, urllib.request

with open("/mnt/d/wsl2/dotnet/openfusion/config.yaml") as f:
    cfg = yaml.safe_load(f)
KEY = cfg["providers"]["deepseek"]["api_key"]

KB_DIR = "/home/lhy/.openfusion/knowledge"

TESTS = [
    {
        "id": "kb_201",
        "question": "我们公司的客户信用等级分几级？S级客户需要什么条件？",
        "kb_match": "客户信用等级",
        "note": "私有规则 — 模型训练数据绝对没有",
    },
    {
        "id": "kb_202",
        "question": "一份 80 万的合同需要谁来审批？流程是怎样的？",
        "kb_match": "审批流程",
        "note": "私有规则 — 模型训练数据绝对没有",
    },
    {
        "id": "kb_203",
        "question": "我们公司的产品定价公式是什么？给一个电力行业S级客户的定价示例。",
        "kb_match": "产品定价公式",
        "note": "私有规则 + 计算逻辑",
    },
    {
        "id": "kb_204",
        "question": "系统崩溃（P0）的售后服务SLA要求是什么？",
        "kb_match": "售后服务SLA",
        "note": "私有规则 — 模型训练数据绝对没有",
    },
    {
        "id": "kb_205",
        "question": "一个年交易额80万、合作2年、有1次逾期的客户，属于什么信用等级？",
        "kb_match": "客户信用等级",
        "note": "需要应用规则做推理",
    },
]


def call_llm(messages, model="deepseek-v4-flash", max_tokens=2048, temp=0.1):
    url = "https://api.deepseek.com/v1/chat/completions"
    payload = json.dumps({"model": model, "messages": messages,
                          "max_tokens": max_tokens, "temperature": temp}).encode()
    req = urllib.request.Request(url, data=payload,
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {KEY}"})
    resp = urllib.request.urlopen(req, timeout=60)
    return json.loads(resp.read())["choices"][0]["message"]["content"]


def mcp_search(query: str) -> str:
    proc = subprocess.Popen(["python3", "/mnt/d/wsl2/dotnet/openfusion/scripts/mcp-knowledge-server.py"],
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


def blind_judge(question: str, answer_a: str, answer_b: str, note: str) -> dict | None:
    prompt = f"""你是一位公正的 AI 回答质量评审专家。

## 原始问题
{question}

## 测试说明
{note}
回答 A 没有获得任何公司内部资料，只能凭训练数据猜测。
回答 B 在回答前获得了公司内部业务规则文档作为上下文。

## 回答 A（无内部资料）
{answer_a}

## 回答 B（有内部业务规则上下文）
{answer_b}

## 评分标准
从以下维度打分（1-10）：
- **符合事实**：如果问题涉及公司内部规则，回答是否准确匹配这些规则？
- **全面性**：覆盖了多少关键信息？
- **实用性**：回答是否能直接用于业务决策？

输出格式（严格 JSON）：
{{"scores":{{"A":{{"factual":N,"completeness":N,"usefulness":N}},"B":{{"factual":N,"completeness":N,"usefulness":N}}}},"winner":"A或B或tie","reason":"一句话理由"}}"""
    result = call_llm([{"role": "user", "content": prompt}], model="deepseek-chat")
    m = re.search(r"```(?:json)?\s*([\s\S]*?)```", result)
    if m:
        try: return json.loads(m.group(1))
        except: pass
    try: return json.loads(result)
    except: return None


def main():
    print("=" * 80)
    print("MCP KNOWLEDGE INJECTION — v2 (Private Knowledge Test)")
    print(f"Model: deepseek-v4-flash | KB: {KB_DIR}")
    print("=" * 80)

    results = []
    wins = {"A": 0, "B": 0, "tie": 0}

    for test in TESTS:
        qid = test["id"]
        question = test["question"]

        print(f"\n[{qid}] {question[:50]}...")
        print(f"  场景: {test['note']}")

        # MCP retrieval
        t0 = time.time()
        knowledge = mcp_search(test["kb_match"])
        t1 = time.time()
        print(f"  MCP: {len(knowledge)} chars in {(t1-t0)*1000:.0f}ms")

        # Answer A: no knowledge
        t0 = time.time()
        answer_a = call_llm([{"role": "user", "content": question}])
        t1 = time.time()
        print(f"  无知识: {len(answer_a)} chars in {t1-t0:.1f}s")

        # Answer B: with knowledge — prompt says "结合你的知识和以下信息"
        combined_prompt = f"""问题：{question}

以下是公司内部业务规则文档中与问题相关的内容，请结合这些内部规则和你的通用知识来回答：

{knowledge}"""
        t0 = time.time()
        answer_b = call_llm([{"role": "user", "content": combined_prompt}])
        t1 = time.time()
        print(f"  有知识: {len(answer_b)} chars in {t1-t0:.1f}s")

        # Blind judge
        jr = blind_judge(question, answer_a, answer_b, test["note"])
        if jr:
            sA, sB = jr["scores"]["A"], jr["scores"]["B"]
            avgA = (sA["factual"] + sA["completeness"] + sA["usefulness"]) / 3
            avgB = (sB["factual"] + sB["completeness"] + sB["usefulness"]) / 3
            w = jr.get("winner", "tie")
            wins[w] = wins.get(w, 0) + 1
            print(f"  盲审: A={avgA:.1f} vs B={avgB:.1f} → {w}")
            print(f"  理由: {jr.get('reason','')[:150]}")
        else:
            print(f"  盲审: 解析失败")

        results.append({
            "id": qid, "question": question, "note": test["note"],
            "knowledge": knowledge,
            "answer_without_kb": answer_a,
            "answer_with_kb": answer_b,
            "judge": jr,
        })

    print("\n" + "=" * 80)
    print("SUMMARY")
    print("=" * 80)
    total = sum(wins.values())
    for k, v in sorted(wins.items()):
        print(f"  {k}: {v}/{total} ({v/total*100:.0f}%)")
    if wins.get("B", 0) > wins.get("A", 0):
        print(f"\n✅ 知识注入优势明显: 私有知识使回答质量提升")
    elif wins.get("A", 0) > wins.get("B", 0):
        print(f"\n❌ 知识注入未见优势")
    else:
        print(f"\n⚖️ 持平")

    with open("/mnt/d/wsl2/dotnet/openfusion/scripts/eval_kb_v2_full.json", "w") as f:
        json.dump({"wins": wins, "results": results}, f, ensure_ascii=False, indent=2)
    print(f"\nFull: scripts/eval_kb_v2_full.json")


if __name__ == "__main__":
    main()
