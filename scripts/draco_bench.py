#!/usr/bin/env python3
"""
DRACO 风格基准战 runner — OpenFusion vs Solo (第一版, 无 web search)

核心对比:
  A) Solo   : qwen3.6-35b-a3b (moon-bridge) 直接回答 DRACO 问题
  B) Fusion : 两个 panel 模型 (qwen3.6-35b + qwen3.6:27b) → judge (qwen3.6-35b) 合成

评分: Gemini 2.5 Flash 按 DRACO rubric 逐条判分 (rubric 库, PerCriterionGrader 替代)
说明: DRACO 官方用 gemini-3-pro-preview judge; 本跑用 gemini-2.5-flash, 分数仅供
      内部相对对比 (solo vs fusion), 不与官方榜单直接比较.
"""
import json, subprocess, time, sys, os, re, asyncio
from concurrent.futures import ThreadPoolExecutor

# ---------- 配置 ----------
MOON = "http://127.0.0.1:38440/v1/chat/completions"
OLLAMA = "http://10.10.10.122:11434/v1/chat/completions"
GEMINI_BASE = "https://gemini.pdfcpu.com/v1beta"
GEMINI_KEY = os.environ.get("GEMINI_API_KEY", "")
GEMINI_MODEL = "gemini-2.5-flash"
DRACO_FILE = "/tmp/draco_test.jsonl"
OUT_DIR = "/tmp/draco_run"
N_TASKS = int(os.environ.get("N_TASKS", "20"))   # 默认 20 题 (10 领域 x 2)
MAX_TOKENS = 6000
TIMEOUT = 600

os.makedirs(OUT_DIR, exist_ok=True)

def call_chat(url, model, prompt, max_tokens=MAX_TOKENS, timeout=TIMEOUT, extra=None):
    """curl 调用 OpenAI 兼容端点 (带真实超时, 不挂起)"""
    payload = {"model": model, "messages": [{"role": "user", "content": prompt}],
               "max_tokens": max_tokens}
    if extra:
        payload.update(extra)
    body = json.dumps(payload).encode()
    r = subprocess.run(["curl", "-s", "--max-time", str(timeout), url,
                        "-H", "Content-Type: application/json",
                        "-d", body], capture_output=True, text=True, timeout=timeout + 10)
    try:
        d = json.loads(r.stdout)
        if d.get("choices") and d["choices"][0]["message"].get("content"):
            return d["choices"][0]["message"]["content"], d.get("usage", {})
        return f"ERROR: {json.dumps(d)[:200]}", {}
    except Exception:
        return f"EXCEPTION: {r.stdout[:200] if r.stdout else r.stderr[:200]}", {}

def call_gemini(system, user, retries=3):
    """rubric 评分 (默认 qwen3.6-35b via moon-bridge; 失败回退 Gemini pdfcpu 代理)
    说明: DRACO 官方用 gemini-3-pro; 本项目本地评分与生成同源, 但 solo/fusion 用
    同一 judge, 相对对比有效. GEMINI_API_KEY 可用时仍优先 Gemini."""
    if not GEMINI_KEY:
        return call_chat(MOON, "qwen3.6-35b-a3b", user, max_tokens=4000,
                         extra={"system": system})[0]
    url = f"{GEMINI_BASE}/models/{GEMINI_MODEL}:generateContent?key={GEMINI_KEY}"
    payload = {"contents": [{"parts": [{"text": f"{system}\n\nUSER:\n{user}"}]}],
               "generationConfig": {"maxOutputTokens": 4000, "temperature": 0.2}}
    for attempt in range(retries):
        r = subprocess.run(["curl", "-s", "--max-time", "180", url,
                            "-H", "Content-Type: application/json",
                            "-d", json.dumps(payload)],
                           capture_output=True, text=True, timeout=190)
        try:
            d = json.loads(r.stdout)
            if d.get("error"):
                code = d["error"].get("code", 0)
                # 429 quota 耗尽: 直接回退本地 qwen, 不浪费重试
                if code in (429, 500, 503):
                    print(f"      gemini {code}, 回退 qwen 评分...", flush=True)
                    return call_chat(MOON, "qwen3.6-35b-a3b", user, max_tokens=4000,
                                     extra={"system": system})[0]
                return f"ERR: {json.dumps(d)[:200]}"
            parts = d.get("candidates", [{}])[0].get("content", {}).get("parts", [])
            return parts[0].get("text", "") if parts else f"ERR: {json.dumps(d)[:200]}"
        except Exception as e:
            if attempt < retries - 1:
                time.sleep(5 * (attempt + 1))
                continue
            return f"EXCEPTION: {e}"
    return "ERR: max retries"

# ---------- Fusion: panel 并行 → judge 合成 ----------
FUSION_SYSTEM = """You are a synthesis judge. Given a question and multiple model answers, produce the best final answer. Combine the strongest parts, resolve contradictions, and cover what others missed. Output only the final answer."""

def fusion_answer(question):
    """panel: qwen3.6-35b(本地) + qwen3.6:27b(Mac) → judge 合成"""
    panel_prompts = {
        "p1_precise": (MOON, "qwen3.6-35b-a3b",
            "你是严谨的研究分析师。请详尽、准确地回答下面的问题，注意事实精确性：\n\n" + question),
        "p2_comprehensive": (OLLAMA, "qwen3.6:27b",
            "你是全面的研究专家。请从多角度完整回答下面的问题，覆盖所有相关方面：\n\n" + question),
    }
    results = {}
    with ThreadPoolExecutor(max_workers=2) as pool:
        futures = {pool.submit(call_chat, url, model, prompt): name
                   for name, (url, model, prompt) in panel_prompts.items()}
        for fut in futures:
            content, usage = fut.result()
            results[futures[fut]] = content
            print(f"      panel[{futures[fut]}]: {len(content)} chars", flush=True)

    judge_prompt = f"问题：\n{question}\n\n--- 答案A (严谨分析) ---\n{results['p1_precise']}\n\n--- 答案B (全面综述) ---\n{results['p2_comprehensive']}\n\n请综合两个答案，输出一个最优最终答案。"
    final, usage = call_chat(MOON, "qwen3.6-35b-a3b", judge_prompt, max_tokens=MAX_TOKENS,
                             extra={"system": FUSION_SYSTEM})
    return final, results

# ---------- Rubric 评分 (简化版, 直接让 Gemini 按 rubric 打分) ----------
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

def score_answer(problem, answer, rubric):
    prompt = RUBRIC_PROMPT.format(criteria=rubric[:6000], answer=answer[:10000])
    sys_msg = "你是 DRACO 基准的专家评分员。只输出 JSON。"
    raw = call_gemini(sys_msg, prompt)
    # 提取 JSON
    m = re.search(r'\[.*\]', raw, re.S)
    if not m:
        return None, raw
    try:
        return json.loads(m.group(0)), raw
    except Exception:
        return None, raw

def pct_weighted(sc):
    """按 DRACO 官方口径：每条 criterion 的 weight 加权计算百分比"""
    if not sc:
        return None
    pts = {"PASS": 1.0, "PARTIAL": 0.5, "FAIL": 0.0}
    total_w = 0.0
    earned = 0.0
    for x in sc:
        w = x.get("weight", 1)
        if isinstance(w, str):
            m = re.search(r"weight\s*(\d+)", w)
            w = float(m.group(1)) if m else 1.0
        else:
            try:
                w = float(w) if w else 1.0
            except (TypeError, ValueError):
                w = 1.0
        v = pts.get(x.get("verdict", "FAIL"), 0.0)
        total_w += w
        earned += w * v
    return round(100 * earned / total_w, 1) if total_w else None

def parse_rubric(task):
    """从 DRACO answer 字段提取 rubric (sections with weighted criteria)"""
    ans = task.get("answer", "")
    try:
        a = json.loads(ans) if isinstance(ans, str) else ans
        secs = a.get("sections", [])
        lines = []
        for s in secs:
            title = s.get("title", "")
            crits = s.get("criteria", [])
            for c in crits:
                if isinstance(c, dict):
                    req = c.get("requirement", c.get("description", ""))
                    w = c.get("weight", 1)
                    lines.append(f"[{title}] (weight {w}) {req}")
                else:
                    lines.append(f"[{title}] {c}")
        joined = "\n".join(lines)[:6000]
        return joined or "Answer the question accurately and completely."
    except Exception:
        return "Answer the question accurately and completely."

# ---------- Main ----------
def main():
    tasks = [json.loads(l) for l in open(DRACO_FILE) if l.strip()]
    print(f"DRACO 数据集: {len(tasks)} 题, 本次跑 {N_TASKS} 题", flush=True)
    tasks = tasks[:N_TASKS]

    results = []
    t_start = time.time()
    for i, task in enumerate(tasks):
        qid = task["id"][:8]
        problem = task["problem"]
        rubric = parse_rubric(task)
        print(f"\n[{i+1}/{len(tasks)}] {qid}: {problem[:80]}...", flush=True)

        # A) Solo
        t0 = time.time()
        solo, _ = call_chat(MOON, "qwen3.6-35b-a3b", problem)
        t_solo = time.time() - t0
        print(f"  solo: {len(solo)} chars, {t_solo:.0f}s", flush=True)

        # B) Fusion
        t0 = time.time()
        fusion, panel = fusion_answer(problem)
        t_fusion = time.time() - t0
        print(f"  fusion: {len(fusion)} chars, {t_fusion:.0f}s", flush=True)

        # 评分 (同题同 judge)
        sc_solo, raw1 = score_answer(problem, solo, rubric)
        sc_fusion, raw2 = score_answer(problem, fusion, rubric)

        row = {
            "id": task["id"], "problem": problem[:200],
            "solo_score": pct_weighted(sc_solo), "fusion_score": pct_weighted(sc_fusion),
            "solo_chars": len(solo), "fusion_chars": len(fusion),
            "solo_s": round(t_solo,1), "fusion_s": round(t_fusion,1),
            "solo_verdicts": [{"r": x.get("requirement","")[:80], "w": x.get("weight",1), "v": x.get("verdict","")} for x in sc_solo] if sc_solo else None,
            "fusion_verdicts": [{"r": x.get("requirement","")[:80], "w": x.get("weight",1), "v": x.get("verdict","")} for x in sc_fusion] if sc_fusion else None,
        }
        results.append(row)
        print(f"  → solo {row['solo_score']}% vs fusion {row['fusion_score']}%", flush=True)

        # 中途保存
        with open(f"{OUT_DIR}/results.jsonl", "w") as f:
            for r in results:
                f.write(json.dumps(r, ensure_ascii=False) + "\n")

    # 汇总
    print("\n" + "="*70)
    print(f"{'ID':<10} {'Solo%':>7} {'Fusion%':>8} {'Δ':>6}  {'solo_s':>7} {'fusion_s':>9}")
    wins = 0; ties = 0; losses = 0; deltas = []
    for r in results:
        a, b = r["solo_score"], r["fusion_score"]
        if a is None or b is None:
            print(f"{r['id'][:8]:<10}  ERR")
            continue
        d = round(b - a, 1); deltas.append(d)
        if d > 0.5: wins += 1
        elif d < -0.5: losses += 1
        else: ties += 1
        print(f"{r['id'][:8]:<10} {a:>7} {b:>8} {d:>+6}  {r['solo_s']:>7.0f} {r['fusion_s']:>9.0f}")
    if deltas:
        print(f"\nFusion 胜 {wins}, 平 {ties}, 负 {losses}; 平均Δ = {sum(deltas)/len(deltas):+.1f} 分")
    print(f"总耗时: {(time.time()-t_start)/60:.1f} min")

if __name__ == "__main__":
    main()
