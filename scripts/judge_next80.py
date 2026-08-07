#!/usr/bin/env python3
"""完全独立裁判实验: 融合judge=Next-80B + 评分judge=Next-80B
对比: solo(qwen3.6) / 同构fusion / 异构fusion — 全部由 Next-80B 独立评分
"""
import json, os, re, subprocess, sys, time
from concurrent.futures import ThreadPoolExecutor

sys.path.insert(0, "/mnt/d/wsl2/dotnet/openfusion/scripts")

MS = "https://api-inference.modelscope.cn/v1/chat/completions"
MS_KEY = os.environ.get("MODELSCOPE_API_KEY", "ms-8ecc2026-4dfb-4f22-8e7a-e60d45c676c0")
JUDGE_URL = "http://127.0.0.1:15722/v1/chat/completions"
JUDGE_MODEL = "gpt-5.4"
MOON = "http://127.0.0.1:38440/v1/chat/completions"

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

def ms_call(model, prompt, max_tokens=4000, timeout=300, retries=4, temp=0.3, url=None):
    payload = {"model": model, "messages": [{"role": "user", "content": prompt}],
               "max_tokens": max_tokens, "temperature": temp}
    base = url or MS
    for attempt in range(retries):
        r = subprocess.run(["curl", "-s", "--max-time", str(timeout), base,
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

def qwen_call(prompt, max_tokens=6000, timeout=300):
    payload = {"model": "qwen3.6-35b-a3b", "messages": [{"role": "user", "content": prompt}],
               "max_tokens": max_tokens}
    r = subprocess.run(["curl", "-s", "--max-time", str(timeout), MOON,
                        "-H", "Content-Type: application/json",
                        "-d", json.dumps(payload)],
                       capture_output=True, text=True, timeout=timeout + 10)
    try:
        d = json.loads(r.stdout)
        c = d["choices"][0]["message"].get("content") or ""
        return c if c else f"ERROR: {json.dumps(d)[:150]}"
    except Exception as e:
        return f"EXCEPTION: {r.stdout[:150]}"

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
                return "\n".join(lines)[:8000] or "Answer accurately."
            except Exception:
                pass
    return "Answer accurately."

def pct_weighted(sc):
    if not sc:
        return None
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

def score(problem, answer, rubric):
    prompt = RUBRIC_PROMPT.format(criteria=rubric[:8000], answer=answer[:12000])
    raw = ms_call(JUDGE_MODEL, prompt, max_tokens=4000, temp=0.1, url=JUDGE_URL)
    # 直接解析; 失败则提取 [] 部分
    try:
        return json.loads(raw), raw
    except Exception:
        m = re.search(r"\[.*\]", raw, re.S)
        if m:
            try:
                return json.loads(m.group(0)), raw
            except Exception:
                pass
    return None, raw

def solo_answer(q):
    return qwen_call("请准确、完整地回答下面的问题：\n\n" + q, max_tokens=6000)

def hom_fusion(q):
    """同构: qwen3.6 ×2 panel → qwen3.6 judge"""
    prompts = {
        "p1": "你是严谨的研究分析师。请详尽、准确地回答下面的问题，注意事实精确性：\n\n" + q,
        "p2": "你是全面的研究专家。请从多角度完整回答下面的问题，覆盖所有相关方面：\n\n" + q,
    }
    with ThreadPoolExecutor(max_workers=2) as pool:
        f1 = pool.submit(qwen_call, prompts["p1"])
        f2 = pool.submit(qwen_call, prompts["p2"])
        r1, r2 = f1.result(), f2.result()
    jp = (f"问题：\n{q}\n\n--- 答案A (严谨分析) ---\n{r1}\n\n--- 答案B (全面综述) ---\n{r2}\n\n请综合两个答案，输出一个最优最终答案。")
    return qwen_call(jp, max_tokens=6000)

def het_fusion(q):
    """异构: qwen3.6 + 397B + 122B → Next-80B judge (独立)"""
    prompts = {
        "p1": "你是严谨的研究分析师。请详尽、准确地回答下面的问题，注意事实精确性：\n\n" + q,
        "p2": "你是深度知识专家。请用广泛的世界知识深入回答下面的问题，给出准确细节：\n\n" + q,
        "p3": "你是多学科视角专家。请从多角度完整覆盖下面的问题：\n\n" + q,
    }
    r1 = qwen_call(prompts["p1"])
    r2 = ms_call("Qwen/Qwen3.5-397B-A17B", prompts["p2"], max_tokens=4096)
    time.sleep(2)
    r3 = ms_call("Qwen/Qwen3.5-122B-A10B", prompts["p3"], max_tokens=4096)
    jp = (f"问题：\n{q}\n\n--- 答案A (qwen3.6严谨) ---\n{r1}"
          f"\n\n--- 答案B (397B深度) ---\n{r2}"
          f"\n\n--- 答案C (122B广度) ---\n{r3}"
          f"\n\n请综合三个答案，指出共识、矛盾与盲点，输出一个最优最终答案。"
          f"注意：不要引用非权威来源；不要把不同信用评级的债券等同对待；用结构化格式呈现数据。")
    return ms_call(JUDGE_MODEL, jp, max_tokens=6000, url=JUDGE_URL)

def main():
    tasks = json.load(open("/tmp/hetero_5tasks.json"))
    out = []
    for t in tasks:
        tid = t["id"]
        q = t["problem"]
        print(f"\n=== [{tid[:8]}] {q[:55]}... ===", flush=True)
        rubric = parse_rubric(tid)
        print(f"  rubric: {len(rubric)} chars", flush=True)
        # 三方案
        for name, fn in [("solo", solo_answer), ("同构", hom_fusion), ("异构", het_fusion)]:
            t0 = time.time()
            ans = fn(q)
            dt = time.time() - t0
            if ans.startswith("ERROR") or ans.startswith("EXCEPTION"):
                print(f"  {name}: 失败 ({ans[:80]})", flush=True)
                continue
            v, raw = score(q, ans, rubric)
            s = pct_weighted(v)
            print(f"  {name}: {len(ans)} chars, {dt:.0f}s, 得分 {s}", flush=True)
            out.append({"id": tid, "scheme": name, "score": s, "chars": len(ans), "sec": round(dt)})
            json.dump(out, open("/tmp/judge_next80_results.json", "w"), ensure_ascii=False, indent=1)
        # 每题之间休息避免限流
        time.sleep(5)

    print("\n===== Next-80B 独立裁判汇总 =====")
    by_id = {}
    for r in out:
        by_id.setdefault(r["id"][:8], {})[r["scheme"]] = r["score"]
    print(f"{'ID':<10}{'Solo':>7}{'同构':>7}{'异构':>7}{'Δ异构-solo':>11}{'Δ异构-同构':>11}")
    for tid, d in by_id.items():
        s, h, e = d.get("solo"), d.get("同构"), d.get("异构")
        d1 = round((e or 0) - (s or 0), 1) if s is not None and e is not None else None
        d2 = round((e or 0) - (h or 0), 1) if h is not None and e is not None else None
        print(f"{tid:<10}{s!s:>7}{h!s:>7}{e!s:>7}{str(d1):>11}{str(d2):>11}")

if __name__ == "__main__":
    main()
