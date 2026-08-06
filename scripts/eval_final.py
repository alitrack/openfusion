#!/usr/bin/env python3
"""
Final combined evaluation: private business rules + pumpkin book.

This uses TWO knowledge files to demonstrate the real value:
1. Private business rules (model has NEVER seen) → should show HUGE gain
2. Pumpkin book (model partially knows) → should show small/mixed gain

This is the data we need for the paper.
"""

import json, re, subprocess, time, yaml, urllib.request

with open("/mnt/d/wsl2/dotnet/openfusion/config.yaml") as f:
    cfg = yaml.safe_load(f)
KEY = cfg["providers"]["deepseek"]["api_key"]
KB_DIR = "/home/lhy/.openfusion/knowledge"

TESTS = [
    # --- Private knowledge (model has NEVER seen) ---
    {
        "id": "priv_001", "type": "private",
        "question": "我们公司的客户信用等级分几级？年交易额100万、合作2年、逾期1次的客户属于什么等级？",
        "kb_match": "客户信用等级",
    },
    {
        "id": "priv_002", "type": "private",
        "question": "一份150万的合同，走什么审批流程？需要谁签字？",
        "kb_match": "审批流程规则",
    },
    {
        "id": "priv_003", "type": "private",
        "question": "一个电力行业C级客户的产品定价怎么算？请给出公式和计算过程。",
        "kb_match": "产品定价公式",
    },
    {
        "id": "priv_004", "type": "private",
        "question": "P0级别的售后服务SLA要求是什么？",
        "kb_match": "售后服务SLA",
    },
    # --- Public ML knowledge (model partially knows) ---
    {
        "id": "ml_002", "type": "public_ml",
        "question": "决策树中信息增益的计算公式是什么？请举例说明。",
        "kb_match": "决策树 信息增益",
    },
    {
        "id": "ml_005", "type": "public_ml",
        "question": "Adaboost 的权重更新公式是什么？",
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
        proc.stdin.write(msg + data); proc.stdin.flush()
        while True:
            line = proc.stdout.readline()
            if not line: break
            line = line.decode().strip()
            if line.startswith("Content-Length:"):
                length = int(line.split(":")[1].strip())
            elif line == "":
                return json.loads(proc.stdout.read(length))
    send(json.dumps({"jsonrpc":"2.0","id":1,"method":"initialize",
        "params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"eval","version":"1.0"}}}))
    result = send(json.dumps({"jsonrpc":"2.0","id":2,"method":"tools/call",
        "params":{"name":"search_knowledge","arguments":{"query":query}}}))
    proc.terminate()
    content = result.get("result",{}).get("content",[])
    return content[0].get("text","") if content else ""


def judge(question, answer_a, answer_b, qtype):
    note = "回答A无公司内部资料，B有公司内部业务规则" if qtype == "private" else "回答A无参考书，B有《南瓜书》公式推导"
    prompt = f"""你是一位公正的评审专家。

## 问题
{question}

## {note}

## 回答 A
{answer_a}

## 回答 B
{answer_b}

## 评分（1-10）：事实准确度、完整性、实用性
JSON: {{"scores":{{"A":{{"accuracy":N,"completeness":N,"usefulness":N}},"B":{{"accuracy":N,"completeness":N,"usefulness":N}}}},"winner":"A/B/tie","reason":"一句话"}}"""
    result = call_llm([{"role":"user","content":prompt}], model="deepseek-chat")
    m = re.search(r"```(?:json)?\s*([\s\S]*?)```", result)
    if m:
        try: return json.loads(m.group(1))
        except: pass
    try: return json.loads(result)
    except: return None


def main():
    print("=" * 80)
    print("FINAL COMBINED EVALUATION")
    print("=" * 80)
    results = []
    wins = {"A": 0, "B": 0, "tie": 0}
    type_wins = {"private": {"A": 0, "B": 0, "tie": 0},
                 "public_ml": {"A": 0, "B": 0, "tie": 0}}

    for test in TESTS:
        qid, qtype, question = test["id"], test["type"], test["question"]
        print(f"\n[{qid}] ({qtype}) {question[:50]}...")

        kb = mcp_search(test["kb_match"])
        a = call_llm([{"role":"user","content":question}])

        prompt_b = f"""请回答以下问题。除了你的知识，以下有相关参考信息：

{kb}

---
问题：{question}"""
        b = call_llm([{"role":"user","content":prompt_b}])

        jr = judge(question, a, b, qtype)
        if jr:
            sA, sB = jr["scores"]["A"], jr["scores"]["B"]
            avgA = (sA["accuracy"]+sA["completeness"]+sA["usefulness"])/3
            avgB = (sB["accuracy"]+sB["completeness"]+sB["usefulness"])/3
            w = jr.get("winner","tie")
            wins[w] = wins.get(w,0)+1
            type_wins[qtype][w] = type_wins[qtype].get(w,0)+1
            print(f"  A={avgA:.1f} vs B={avgB:.1f} → {w}")
            print(f"  理由: {jr.get('reason','')[:150]}")
        else:
            print(f"  Judge: 解析失败")

        results.append({"id":qid,"type":qtype,"question":question,"answer_a":a,"answer_b":b,"judge":jr})

    print("\n" + "=" * 80)
    print("OVERALL")
    for k in ["A","B","tie"]:
        print(f"  {k}: {wins.get(k,0)}/{len(TESTS)} ({wins.get(k,0)/len(TESTS)*100:.0f}%)")
    print()
    for qt in ["private","public_ml"]:
        tw = type_wins[qt]
        total = sum(tw.values())
        print(f"  [{qt}] A={tw.get('A',0)}/{total} B={tw.get('B',0)}/{total} tie={tw.get('tie',0)}/{total}")

    with open("/mnt/d/wsl2/dotnet/openfusion/scripts/eval_final_results.json","w") as f:
        json.dump({"wins":wins,"type_wins":type_wins,"results":results}, f, ensure_ascii=False, indent=2)
    print(f"\nSaved: scripts/eval_final_results.json")


if __name__ == "__main__":
    main()
