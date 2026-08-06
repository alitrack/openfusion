#!/usr/bin/env python3
"""
Fact-Consistency Evaluation for Knowledge Injection (v2 - fixed).

Key fixes over v1:
1. Reads KB file directly (no MCP server truncation to 500 chars)
2. More robust LLM check_fact prompt with structured output (yes/no as JSON)
3. Uses LLM-based recall measurement (Chinese keyword splitting is broken)
4. Works in batch: process all check_fact calls in one LLM call when possible
"""

import json, subprocess, time, yaml, urllib.request

with open("/mnt/d/wsl2/dotnet/openfusion/config.yaml") as f:
    cfg = yaml.safe_load(f)
KEY = cfg["providers"]["deepseek"]["api_key"]
KB_PATH = "/home/lhy/.openfusion/knowledge/private-business-rules.md"

# ---------------------------------------------------------------------------
# Load KB
# ---------------------------------------------------------------------------

with open(KB_PATH) as f:
    FULL_KB = f.read()

# ---------------------------------------------------------------------------
# Test cases with ground truth facts
# ---------------------------------------------------------------------------

TESTS = [
    {
        "id": "fact_001",
        "question": "我们公司的客户信用等级分几级？各自的判定标准是什么？",
        "ground_truth_facts": [
            "信用等级分为S/A/B/C/D五级",
            "S级：年交易额≥500万且合作年限≥3年且无逾期",
            "A级：年交易额≥100万且合作年限≥1年且逾期≤2次",
            "B级：年交易额≥30万或合作年限≥6个月",
            "C级：有交易记录但未达B级标准",
            "D级：新注册未交易或存在欺诈记录",
        ],
    },
    {
        "id": "fact_002",
        "question": "一份150万的合同需要谁审批？流程是怎样的？",
        "ground_truth_facts": [
            "单笔合同≤10万：销售经理审批",
            "10万<单笔≤50万：销售总监审批",
            "50万<单笔≤200万：VP审批",
            "单笔>200万：CEO审批",
            "关联交易（同一客户30天内多笔）合并计算金额",
        ],
    },
    {
        "id": "fact_003",
        "question": "我们公司的产品定价公式是什么？电力行业S级客户怎么算？",
        "ground_truth_facts": [
            "标准品定价=成本×(1+行业系数+客户系数)",
            "电力行业系数为0.25",
            "S级客户系数为-0.05",
            "B级客户系数0.05",
        ],
    },
    {
        "id": "fact_004",
        "question": "售后服务SLA各等级的要求是什么？",
        "ground_truth_facts": [
            "P0（系统崩溃）：30分钟响应，2小时修复",
            "P1（功能不可用）：1小时响应，4小时修复",
            "P2（部分功能异常）：4小时响应，24小时修复",
            "P3（体验问题）：24小时响应，下一版本修复",
        ],
    },
    {
        "id": "fact_005",
        "question": "一个年交易额80万、合作2年、逾期1次的客户属于什么信用等级？定价系数是多少？",
        "ground_truth_facts": [
            "年交易额80万≥30万（符合B级交易额门槛）",
            "合作2年≥6个月（符合B级合作年限门槛）",
            "逾期1次≤2次（未超过A级上限）",
            "符合B级标准",
            "B级客户系数为0.05",
        ],
    },
]

# ---------------------------------------------------------------------------
# LLM API
# ---------------------------------------------------------------------------

def call_llm(messages, model="deepseek-v4-flash", max_tokens=4096, temp=0):
    url = "https://api.deepseek.com/v1/chat/completions"
    payload = json.dumps({"model": model, "messages": messages,
                          "max_tokens": max_tokens, "temperature": temp}).encode()
    req = urllib.request.Request(url, data=payload,
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {KEY}"})
    for attempt in range(3):
        try:
            resp = urllib.request.urlopen(req, timeout=120)
            return json.loads(resp.read())["choices"][0]["message"]["content"]
        except Exception as e:
            if attempt < 2:
                print(f"    ⚠ LLM call failed (attempt {attempt+1}): {e}, retrying...")
                time.sleep(3)
            else:
                print(f"    ✗ LLM call failed after 3 attempts: {e}")
                return ""

# ---------------------------------------------------------------------------
# Batch fact verification
# ---------------------------------------------------------------------------

def batch_check_facts(claim_groups, evidence):
    """
    Check multiple groups of claims against evidence in one LLM call per group.
    claim_groups: [{"group": "A", "claims": ["fact1", "fact2", ...]}, ...]
    Returns: {"A": {"fact1": True, "fact2": False, ...}, ...}
    """
    result = {}
    for group in claim_groups:
        gname = group["group"]
        claims = group["claims"]
        if not claims:
            result[gname] = {}
            continue

        claims_text = "\n".join(f"{i+1}. {c}" for i, c in enumerate(claims))
        prompt = f"""你是一个事实核查员。请判断以下每个断言是否被「参考知识库」明确支持。

「参考知识库」：
---
{evidence}

「待核查断言」：
{claims_text}

对于每个断言，判断它是否被知识库明确支持（即知识库中明确写明了该规则或数值）。

以JSON数组格式输出，不要加任何其他文字：
[
  {{"index": 1, "matched": true}},
  {{"index": 2, "matched": false}},
  ...
]

注意：知识库是自己的私有规则文档，不是公开知识。不要因为你没见过这些规则就判false。只要知识库中明确写了该规则，就判true。"""
        output = call_llm([{"role": "user", "content": prompt}], max_tokens=2048, temp=0)
        
        # Initialize group results
        result[gname] = {}
        
        # Parse JSON from output
        try:
            # Find JSON array in output
            start = output.find("[")
            end = output.rfind("]")
            if start >= 0 and end > start:
                parsed = json.loads(output[start:end+1])
                for item in parsed:
                    idx = item.get("index", 0) - 1
                    if 0 <= idx < len(claims):
                        result[gname][claims[idx]] = item.get("matched", False)
        except (json.JSONDecodeError, KeyError, TypeError) as e:
            print(f"    ⚠ Failed to parse batch result for group {gname}: {e}")
            print(f"    Raw output: {output[:200]}")
            # Fallback: mark all as unknown
            for c in claims:
                result[gname][c] = False
    
    return result

# ---------------------------------------------------------------------------
# Recall: check if answer mentions each ground truth fact
# ---------------------------------------------------------------------------

def batch_check_recall(ground_truth_facts, answer_a, answer_b):
    """Check which ground truth facts are mentioned in each answer."""
    facts_text = "\n".join(f"{i+1}. {f}" for i, f in enumerate(ground_truth_facts))
    
    prompt = f"""以下是一组「事实断言」和两个「回答」。请判断每个事实断言是否在回答A或回答B中得到体现。

「事实断言列表」：
{facts_text}

「回答A」：
---
{answer_a[:2000]}

「回答B」：
---
{answer_b[:2000]}

对于每个断言（1到{len(ground_truth_facts)}），判断：
- 回答A中是否明确提到了这个事实？（不仅仅是暗示，要明确的数值、规则或结论）
- 回答B中是否明确提到了这个事实？

如果一个回答给出了通用答案（如"取决于公司政策"），而没有提到具体的断言内容，则视为未体现。

以JSON数组格式输出：
[
  {{"index": 1, "in_a": true, "in_b": false}},
  {{"index": 2, "in_a": false, "in_b": true}},
  ...
]"""
    
    output = call_llm([{"role": "user", "content": prompt}], max_tokens=2048, temp=0)
    
    in_a = set()
    in_b = set()
    try:
        start = output.find("[")
        end = output.rfind("]")
        if start >= 0 and end > start:
            parsed = json.loads(output[start:end+1])
            for item in parsed:
                idx = item.get("index", 0) - 1
                if 0 <= idx < len(ground_truth_facts):
                    if item.get("in_a", False):
                        in_a.add(idx)
                    if item.get("in_b", False):
                        in_b.add(idx)
    except (json.JSONDecodeError, KeyError, TypeError) as e:
        print(f"    ⚠ Failed to parse recall result: {e}")
    
    return in_a, in_b


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main():
    print("=" * 80)
    print("FACT-CONSISTENCY EVALUATION v2")
    print("KB文件直接读取（无MCP截断）")
    print("=" * 80)
    print(f"\n知识库: {KB_PATH} ({len(FULL_KB)} chars)")

    all_results = []
    agg_a = {"f1": [], "precision": [], "recall": []}
    agg_b = {"f1": [], "precision": [], "recall": []}

    for test in TESTS:
        qid = test["id"]
        question = test["question"]
        ground_truth = test["ground_truth_facts"]

        print(f"\n[{qid}] {question[:60]}...")

        # --- Get answers ---
        # Answer A: no knowledge
        answer_a = call_llm([{"role": "user", "content": question}])
        print(f"  回答A (无知识): {len(answer_a)} chars")
        
        # Answer B: with knowledge
        prompt_b = f"请回答以下问题。除你的通用知识外，以下内部资料可供参考：\n\n{FULL_KB}\n\n---\n问题：{question}"
        answer_b = call_llm([{"role": "user", "content": prompt_b}])
        print(f"  回答B (有知识): {len(answer_b)} chars")

        print("  评估中...")

        # --- Extract claims from both answers ---
        def extract_claims(answer, label):
            if not answer:
                return []
            prompt = f"从以下回答中提取所有具体的、可验证的事实断言。只提取明确的数值、规则、定义、分类、关系。不提取观点、建议或通用知识。每行一个断言，不要编号。\n\n回答：\n{answer}"
            out = call_llm([{"role": "user", "content": prompt}], max_tokens=1024, temp=0)
            claims = [l.strip() for l in out.strip().split("\n") if l.strip() and len(l.strip()) > 5]
            # LLM might mix numbered list, so strip leading numbers
            cleaned = []
            for c in claims:
                c = c.lstrip("0123456789. )-—")
                if len(c) > 5:
                    cleaned.append(c)
            return cleaned

        claims_a = extract_claims(answer_a, "A")
        claims_b = extract_claims(answer_b, "B")
        print(f"  提取断言: A={len(claims_a)}条, B={len(claims_b)}条")

        # --- Verify claims against KB ---
        # Only check claims that could match KB rules
        # Filter to KB-relevant claims (contain numbers, thresholds, rules)
        def is_kb_relevant(claim):
            kb_keywords = ["级", "万", "年", "审批", "定价", "系数", "SLA", "响应", "修复",
                          "P0", "P1", "P2", "P3", "VIP", "CEO", "VP", "经理", "总监",
                          "≥", "≤", "=", "+", "×", "逾期", "交易额", "成本"]
            return any(k in claim for k in kb_keywords)

        relevant_a = [c for c in claims_a if is_kb_relevant(c)]
        relevant_b = [c for c in claims_b if is_kb_relevant(c)]
        
        # Build batch verification requests
        check_groups = []
        if relevant_a:
            check_groups.append({"group": "A", "claims": relevant_a})
        if relevant_b:
            check_groups.append({"group": "B", "claims": relevant_b})
        
        check_results = {"A": {}, "B": {}}
        if check_groups:
            check_results = batch_check_facts(check_groups, FULL_KB)
        
        # Compute precision
        supported_a = sum(1 for c in relevant_a if c in check_results.get("A", {}) and check_results["A"][c])
        supported_b = sum(1 for c in relevant_b if c in check_results.get("B", {}) and check_results["B"][c])
        
        # Debug: show what was matched
        matched_claims_a = [c for c in relevant_a if c in check_results.get("A", {}) and check_results["A"][c]]
        matched_claims_b = [c for c in relevant_b if c in check_results.get("B", {}) and check_results["B"][c]]
        if matched_claims_a:
            print(f"    A匹配: {matched_claims_a[:3]}")
        if matched_claims_b:
            print(f"    B匹配: {matched_claims_b[:3]}")
        if not matched_claims_a and relevant_a:
            print(f"    A未匹配示例: {relevant_a[:3]}")
        if not matched_claims_b and relevant_b:
            print(f"    B未匹配示例: {relevant_b[:3]}")

        precision_a = supported_a / len(relevant_a) if relevant_a else 0
        precision_b = supported_b / len(relevant_b) if relevant_b else 0

        # --- Compute recall (which ground truth facts appear in each answer) ---
        in_a, in_b = batch_check_recall(ground_truth, answer_a, answer_b)
        recall_a = len(in_a) / len(ground_truth) if ground_truth else 0
        recall_b = len(in_b) / len(ground_truth) if ground_truth else 0

        print(f"  Recall: A掩盖={len(in_a)}/{len(ground_truth)}, B掩盖={len(in_b)}/{len(ground_truth)}")

        # --- F1 ---
        f1_a = 2 * precision_a * recall_a / (precision_a + recall_a) if (precision_a + recall_a) > 0 else 0
        f1_b = 2 * precision_b * recall_b / (precision_b + recall_b) if (precision_b + recall_b) > 0 else 0

        for label, p, r, f in [("A(无知识)", precision_a, recall_a, f1_a),
                                ("B(有知识)", precision_b, recall_b, f1_b)]:
            print(f"  {label}: P={p:.3f} R={r:.3f} F1={f:.3f}")

        agg_a["f1"].append(f1_a)
        agg_a["precision"].append(precision_a)
        agg_a["recall"].append(recall_a)
        agg_b["f1"].append(f1_b)
        agg_b["precision"].append(precision_b)
        agg_b["recall"].append(recall_b)

        all_results.append({
            "id": qid, "question": question,
            "ground_truth": ground_truth,
            "answer_a": answer_a,
            "answer_b": answer_b,
            "metrics_a": {"precision": round(precision_a, 3), "recall": round(recall_a, 3), "f1": round(f1_a, 3)},
            "metrics_b": {"precision": round(precision_b, 3), "recall": round(recall_b, 3), "f1": round(f1_b, 3)},
        })

    # Final summary
    print("\n" + "=" * 80)
    print("FINAL SUMMARY")
    print("=" * 80)

    def avg(vals):
        return round(sum(vals) / len(vals), 3) if vals else 0

    print(f"\n{'Metric':<20} {'无知识(A)':<20} {'有知识(B)':<20} {'改善':<20}")
    print("-" * 80)
    ap = avg(agg_a["precision"])
    bp = avg(agg_b["precision"])
    ar = avg(agg_a["recall"])
    br = avg(agg_b["recall"])
    af = avg(agg_a["f1"])
    bf = avg(agg_b["f1"])
    
    def delta_str(new, old):
        d = new - old
        return f"{'+' if d >= 0 else ''}{d:.3f}"

    print(f"{'Precision':<20} {ap:<20.3f} {bp:<20.3f} {delta_str(bp, ap):<20}")
    print(f"{'Recall':<20} {ar:<20.3f} {br:<20.3f} {delta_str(br, ar):<20}")
    print(f"{'F1':<20} {af:<20.3f} {bf:<20.3f} {delta_str(bf, af):<20}")

    summary = {
        "avg_a": {"precision": ap, "recall": ar, "f1": af},
        "avg_b": {"precision": bp, "recall": br, "f1": bf},
    }
    with open("/mnt/d/wsl2/dotnet/openfusion/scripts/eval_fact_results.json", "w") as f:
        json.dump({"summary": summary, "results": all_results}, f, ensure_ascii=False, indent=2)
    print(f"\nSaved: scripts/eval_fact_results.json")

    # Also fix the MCP server to not truncate
    fix_mcp_server()


def fix_mcp_server():
    """Fix the MCP server to return full content instead of 500-char snippets."""
    server_path = "/mnt/d/wsl2/dotnet/openfusion/scripts/mcp-knowledge-server.py"
    with open(server_path) as f:
        content = f.read()
    
    old = '"snippet": content[:500]'
    new = '"snippet": content[:4000]'
    if old in content:
        content = content.replace(old, new)
        with open(server_path, "w") as f:
            f.write(content)
        print(f"\nFixed MCP server: snippet size 500→4000 chars")
    else:
        print(f"\nMCP server snippet already fixed (or different format)")


if __name__ == "__main__":
    main()
