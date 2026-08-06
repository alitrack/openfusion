#!/usr/bin/env python3
"""Quick evaluation runner for OpenFusion quality comparison."""
import json, re, yaml, urllib.request

with open("/mnt/d/wsl2/dotnet/openfusion/config.yaml") as f:
    cfg = yaml.safe_load(f)
KEY = cfg["providers"]["deepseek"]["api_key"]

def call_llm(prompt, model="deepseek-chat"):
    url = "https://api.deepseek.com/v1/chat/completions"
    payload = json.dumps({
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": 1024,
        "temperature": 0.1,
    }).encode()
    req = urllib.request.Request(url, data=payload,
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {KEY}"})
    resp = urllib.request.urlopen(req, timeout=60)
    data = json.loads(resp.read())
    return data["choices"][0]["message"]["content"], data["usage"]

def parse_json(text):
    m = re.search(r"```(?:json)?\s*([\s\S]*?)```", text)
    if m:
        try: return json.loads(m.group(1))
        except: pass
    try: return json.loads(text)
    except: return None

tests = [
    ("pwr_001", "变电站负荷预测通常使用哪些模型？ARIMA 和 Prophet 各适合什么场景？",
     ["ARIMA短期", "Prophet中期", "气象因素", "节假日标记"]),
    ("pwr_002", "SCADA 系统的一级告警包括哪些类型？二级和三级呢？",
     ["设备跳闸", "过流保护", "差动保护", "油温过高", "SF6气压低"]),
    ("pwr_003", "电力负荷数据缺失率超过多少需要处理？通常用什么方法填充？",
     ["5%", "前向填充", "数据质量", "3σ", "IQR"]),
    ("gen_001", "什么是检索增强生成（RAG）？它的主要优势是什么？",
     ["检索", "生成", "知识库", "减少幻觉", "外部知识"]),
    ("gen_002", "解释向量数据库在大语言模型应用中的作用。",
     ["嵌入", "相似度搜索", "语义检索", "Top-K"]),
]

results = []
wins = {"A": 0, "B": 0, "tie": 0}

fmt = "{:<10} {:<38} {:>8} {:>8} {:>8} {:>10} {:>10}"
print(fmt.format("ID", "Question", "A_avg", "B_avg", "Winner", "CostA", "CostB"))
print("-" * 92)

for qid, question, aspects in tests:
    a, ua = call_llm(question, "deepseek-chat")
    b, ub = call_llm(question, "deepseek-v4-flash")

    aspects_str = "\n".join(f"- {x}" for x in aspects)
    jp = f"""你是一位公正的 AI 回答质量评审专家。

## 原始问题
{question}

## 期望的回答要点
{aspects_str}

## 回答 A
{a}

## 回答 B
{b}

## 评审要求
从准确度(1-10)、全面性(1-10)、清晰度(1-10)打分。
格式: {{"scores":{{"A":{{"accuracy":N,"completeness":N,"clarity":N}},"B":{{"accuracy":N,"completeness":N,"clarity":N}}}},"winner":"A或B或tie"}}"""

    j, _ = call_llm(jp, "deepseek-chat")
    parsed = parse_json(j)

    if parsed:
        sA = parsed["scores"]["A"]
        sB = parsed["scores"]["B"]
        avgA = (sA["accuracy"] + sA["completeness"] + sA["clarity"]) / 3
        avgB = (sB["accuracy"] + sB["completeness"] + sB["clarity"]) / 3
        w = parsed.get("winner", "tie")
        wins[w] = wins.get(w, 0) + 1
        costA = f"${ua['total_tokens']*0.000002:.4f}"
        costB = f"${ub['total_tokens']*0.0000005:.4f}"
        print(fmt.format(qid, question[:36], f"{avgA:.1f}", f"{avgB:.1f}", w, costA, costB))
    else:
        print(fmt.format(qid, question[:36], "FAIL", "", "", "", ""))

    results.append({
        "id": qid, "question": question, "aspects": aspects,
        "answer_a": a, "answer_b": b,
        "judge_raw": j, "judge_parsed": parsed,
        "usage_a": ua, "usage_b": ub,
    })

print()
print(f"Final: A={wins.get('A',0)}, B={wins.get('B',0)}, tie={wins.get('tie',0)}")
with open("/mnt/d/wsl2/dotnet/openfusion/scripts/eval_results.json", "w") as f:
    json.dump({"results": results, "wins": wins}, f, ensure_ascii=False, indent=2)
print("Saved to scripts/eval_results.json")
