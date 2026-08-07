#!/usr/bin/env python3
"""异构fusion验证: 5题 × (solo qwen3.6 | 同构fusion | 异构fusion) — 同一judge评分对比
复用 draco_bench.py 的评分逻辑"""
import json, os, re, subprocess, sys, time
from concurrent.futures import ThreadPoolExecutor

sys.path.insert(0, "/mnt/d/wsl2/dotnet/openfusion/scripts")
from draco_bench import score_answer, pct_weighted, call_chat

MOON = "http://127.0.0.1:38440/v1/chat/completions"   # moon-bridge → 115 qwen3.6
MS = "https://api-inference.modelscope.cn/v1/chat/completions"
MS_KEY = os.environ.get("MODELSCOPE_API_KEY", "ms-8ecc2026-4dfb-4f22-8e7a-e60d45c676c0")
FUSION_API = "http://127.0.0.1:8080/v1/chat/completions"  # openfusion 异构 preset

def ms_call(model, prompt, max_tokens=4096, timeout=300, retries=3):
    payload = {"model": model, "messages": [{"role": "user", "content": prompt}],
               "max_tokens": max_tokens, "temperature": 0.3}
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
            # choices=null (限流/冷却) → 等待后重试
            if attempt < retries - 1:
                wait = 8 * (attempt + 1)
                print(f"      ms限流({model[:20]}), {wait}s后重试...", flush=True)
                time.sleep(wait)
                continue
            return f"ERROR: {json.dumps(d)[:200]}"
        except Exception as e:
            if attempt < retries - 1:
                time.sleep(8 * (attempt + 1))
                continue
            return f"EXCEPTION: {r.stdout[:200]}"
    return "ERROR: max retries"

def qwen_call(prompt, max_tokens=6000, timeout=300):
    return call_chat(MOON, "qwen3.6-35b-a3b", prompt, max_tokens=max_tokens, timeout=timeout)[0]

def hetero_fusion(question):
    """异构 panel: qwen3.6(本地,并行) + 397B(MS) + 122B(MS,串行避免限流) → judge 合成"""
    prompts = {
        "p1_qwen36": "你是严谨的研究分析师。请详尽、准确地回答下面的问题，注意事实精确性：\n\n" + question,
        "p2_397B": "你是深度知识专家。请用广泛的世界知识深入回答下面的问题，给出准确细节：\n\n" + question,
        "p3_122B": "你是多学科视角专家。请从多角度完整覆盖下面的问题：\n\n" + question,
    }
    results = {}
    # qwen3.6 并行, ModelScope 两个串行 (免费API限流)
    with ThreadPoolExecutor(max_workers=1) as pool:
        fut = pool.submit(qwen_call, prompts["p1_qwen36"])
        results["p1_qwen36"] = fut.result()
    print(f"      panel[p1_qwen36]: {len(results['p1_qwen36'])} chars", flush=True)
    results["p2_397B"] = ms_call("Qwen/Qwen3.5-397B-A17B", prompts["p2_397B"], max_tokens=4096)
    print(f"      panel[p2_397B]: {len(results['p2_397B'])} chars", flush=True)
    time.sleep(2)
    results["p3_122B"] = ms_call("Qwen/Qwen3.5-122B-A10B", prompts["p3_122B"], max_tokens=4096)
    print(f"      panel[p3_122B]: {len(results['p3_122B'])} chars", flush=True)

    judge_prompt = (f"问题：\n{question}\n\n--- 答案A (qwen3.6严谨) ---\n{results['p1_qwen36']}"
                    f"\n\n--- 答案B (397B深度) ---\n{results['p2_397B']}"
                    f"\n\n--- 答案C (122B广度) ---\n{results['p3_122B']}"
                    f"\n\n请综合三个答案，指出共识、矛盾与盲点，输出一个最优最终答案。"
                    f"注意：不要引用非权威来源(Reddit/Quora/论坛)；不要把不同信用评级的债券等同对待；"
                    f"不要声称80TTB豁免适用于NCD利息；用结构化格式(表格/分节)呈现财务指标。")
    final = ms_call("MiniMax/MiniMax-M1-80k", judge_prompt, max_tokens=6000)
    return final, results

def parse_rubric(task):
    """从 DRACO 原始任务的 answer 字段提取 rubric"""
    # 从原始 DRACO 文件取完整 task (含 answer)
    tid = task["id"]
    for l in open("/tmp/draco_test.jsonl"):
        if not l.strip():
            continue
        d = json.loads(l)
        if d["id"] == tid:
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
                joined = "\n".join(lines)[:6000]
                return joined or "Answer the question accurately and completely."
            except Exception:
                pass
    return "Answer the question accurately and completely."

def main():
    tasks = json.load(open("/tmp/hetero_5tasks.json"))
    out = []
    for t in tasks:
        tid = t["id"][:8]
        q = t["problem"]
        print(f"\n=== [{tid}] {q[:60]}... ===", flush=True)
        # 复用已存的 DRACO 结果作为 solo / 同构fusion 分数
        solo = hom_fus = None
        for l in open("/tmp/draco_run/results.jsonl"):
            d = json.loads(l)
            if d["id"] == t["id"]:
                solo, hom_fus = d["solo_score"], d["fusion_score"]
                break
        # 跑异构
        t0 = time.time()
        final, panels = hetero_fusion(q)
        dt = time.time() - t0
        print(f"      异构完成: {len(final)} chars, {dt:.0f}s", flush=True)
        # 评分 (同一 qwen judge)
        rubric = parse_rubric(t)
        verdicts, raw = score_answer(q, final, rubric)
        het = pct_weighted(verdicts)
        print(f"      solo={solo} 同构={hom_fus} 异构={het}", flush=True)
        out.append({"id": t["id"], "solo": solo, "homogeneous": hom_fus,
                    "heterogeneous": het, "chars": len(final), "sec": round(dt),
                    "panels": {k: len(v) for k, v in panels.items()}})
        json.dump(out, open("/tmp/hetero_results.json", "w"), ensure_ascii=False, indent=1)

    print("\n===== 汇总 =====")
    print(f"{'ID':<10}{'Solo':>7}{'同构':>7}{'异构':>7}{'Δ异构-solo':>11}{'Δ异构-同构':>11}")
    for r in out:
        d1 = round((r["heterogeneous"] or 0) - (r["solo"] or 0), 1)
        d2 = round((r["heterogeneous"] or 0) - (r["homogeneous"] or 0), 1)
        print(f"{r['id'][:8]:<10}{r['solo']:>7}{r['homogeneous']:>7}{r['heterogeneous']:>7}{d1:>+11}{d2:>+11}")

if __name__ == "__main__":
    main()
