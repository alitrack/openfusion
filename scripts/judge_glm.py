#!/usr/bin/env python3
"""独立裁判验证: 用 GLM-5.1 (非panel成员) 重新评分异构 vs 同构 vs solo 输出
核心: 裁判必须独立于panel, 否则自己给自己当裁判"""
import json, os, subprocess, sys, time

MS = "https://api-inference.modelscope.cn/v1/chat/completions"
MS_KEY = os.environ.get("MODELSCOPE_API_KEY", "ms-8ecc2026-4dfb-4f22-8e7a-e60d45c676c0")

RUBRIC_PROMPT = """你是严格的基准评测员。根据给定的评估标准(rubric)，对下面的回答逐条评分。
每条 criterion 格式为: [领域] (weight N) 要求描述。
权重表示该要求的重要性，PASS 得满分，PARTIAL 得一半，FAIL 不得分。

评估标准:
{criteria}

回答:
{answer}

请对 rubric 的每条要求给出:
- requirement: 该要求的原文
- weight: 权重数值
- verdict: PASS / PARTIAL / FAIL
- 简短理由

输出 JSON 数组: [{{"requirement": "...", "weight": N, "verdict": "PASS|PARTIAL|FAIL", "reason": "..."}}]"""

def ms_call(model, prompt, max_tokens=4000, timeout=300, retries=4):
    payload = {"model": model, "messages": [{"role": "user", "content": prompt}],
               "max_tokens": max_tokens, "temperature": 0.1}
    for attempt in range(retries):
        r = subprocess.run(["curl", "-s", "--max-time", str(timeout), MS,
                            "-H", f"Authorization: Bearer {MS_KEY}",
                            "-H", "Content-Type: application/json",
                            "-d", json.dumps(payload)],
                           capture_output=True, text=True, timeout=timeout + 10)
        try:
            d = json.loads(r.stdout)
            ch = d.get("choices")
            if ch:
                msg = ch[0].get("message") or ch[0].get("delta") or {}
                c = msg.get("content") or ""
                if c:
                    return c
            if attempt < retries - 1:
                time.sleep(10 * (attempt + 1))
                continue
            return f"ERROR: {json.dumps(d)[:200]}"
        except Exception:
            if attempt < retries - 1:
                time.sleep(10 * (attempt + 1))
                continue
            return "EXCEPTION"
    return "ERROR: max retries"

def parse_rubric(task_id):
    for l in open("/tmp/draco_test.jsonl"):
        if not l.strip():
            continue
        d = json.loads(l)
        if d["id"] == task_id:
            a = d.get("answer", "")
            try:
                obj = json.loads(a) if isinstance(a, str) else a
                lines = []
                for s in obj.get("sections", []):
                    for c in s.get("criteria", []):
                        if isinstance(c, dict):
                            req = c.get("requirement", c.get("description", ""))
                            lines.append(f"[{s.get('title','')}] (weight {c.get('weight',1)}) {req}")
                        else:
                            lines.append(f"[{s.get('title','')}] {c}")
                return "\n".join(lines)[:6000] or "Answer accurately."
            except Exception:
                pass
    return "Answer accurately."

def pct_weighted(sc):
    if not sc:
        return None
    import re
    pts = {"PASS": 1.0, "PARTIAL": 0.5, "FAIL": 0.0}
    total_w = earned = 0.0
    for x in sc:
        if not isinstance(x, dict):
            continue
        w = x.get("weight", x.get("w", 1))
        if isinstance(w, str):
            m = re.search(r"weight\s*(-?\d+)", w)
            w = float(m.group(1)) if m else 1.0
        else:
            try:
                w = float(w) if w else 1.0
            except (TypeError, ValueError):
                w = 1.0
        v = pts.get(x.get("verdict", x.get("v", "FAIL")), 0.0)
        total_w += w
        earned += w * v
    return round(100 * earned / total_w, 1) if total_w else None

def main():
    results = json.load(open("/tmp/hetero_results.json"))
    out = []
    for r in results:
        tid = r["id"]
        rubric = parse_rubric(tid)
        # 从 draco_run 取同构输出, 从异构保存取异构输出
        # 需要答案文本! 找同构fusion的答案
        hom_ans = het_ans = None
        for l in open("/tmp/draco_run/results.jsonl"):
            d = json.loads(l)
            if d["id"] == tid:
                # results.jsonl 没存答案文本, 只有分数
                pass
        print(f"\n=== [{tid[:8]}] GLM-5.1 独立裁判 ===", flush=True)
        print("注意: 需要答案文本, 检查数据可用性", flush=True)
        out.append({"id": tid, "note": "need answers"})
    json.dump(out, open("/tmp/judge_glm.json", "w"), ensure_ascii=False, indent=1)
    print("\n完成 (此版本只检查数据; 答案文本需从验证脚本重新生成)")

if __name__ == "__main__":
    main()
