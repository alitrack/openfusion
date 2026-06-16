"""
精简对比测试：1 题 × 6 模式，带 flush 输出
"""
import json, subprocess, sys, time

OLLAMA = "http://10.10.10.8:11434/v1/chat/completions"
DEEPSEEK = "https://api.deepseek.com/v1/chat/completions"
FUSION = "http://127.0.0.1:8080/v1/chat/completions"
DS_KEY = "sk-8fc28136e409420c964e6134204f99d4"

PROMPT = "用 Python 实现一个带 TTL 过期机制的缓存（类似 Redis）。要求：get(key) 返回 value 或 None；set(key, value, ttl_seconds) 过期后自动失效；O(1) 读写；多线程安全。"

MODES = [
    ("A-Qwen-solo",   OLLAMA,   {"model":"qwen3.6:35B", "max_tokens":2048}),
    ("B-Gemma-solo",  OLLAMA,   {"model":"gemma4:26b",  "max_tokens":2048}),
    ("C-DS-solo",     DEEPSEEK, {"model":"deepseek-chat","max_tokens":2048}),
    ("D-QwenGemma",   FUSION,   {"model":"openfusion/qwen-gemma", "max_tokens":2048}),
    ("E-QwenDS",      FUSION,   {"model":"openfusion/qwen-ds", "max_tokens":2048}),
    ("F-QwenGemmaDS", FUSION,   {"model":"openfusion/qwen-gemma-ds", "max_tokens":2048}),
]

results = []
for label, endpoint, params in MODES:
    headers = ["-H", "Content-Type: application/json"]
    if "deepseek.com" in endpoint:
        headers += ["-H", f"Authorization: Bearer {DS_KEY}"]

    print(f"\n▶ {label}...", flush=True)
    t0 = time.time()

    payload = {"model": params["model"], "messages": [{"role":"user","content":PROMPT}], "max_tokens": params["max_tokens"]}

    r = subprocess.run(["curl", "-s", "--max-time", "300", endpoint] + headers +
                       ["-d", json.dumps(payload)], capture_output=True, text=True, timeout=310)

    elapsed = time.time() - t0

    if r.returncode != 0:
        print(f"  ❌ curl 失败 ({r.returncode})", flush=True)
        continue

    try:
        data = json.loads(r.stdout)
    except:
        print(f"  ❌ JSON 解析失败: {r.stdout[:200]}", flush=True)
        continue

    if data.get("choices"):
        content = data["choices"][0]["message"]["content"]
        usage = data.get("usage", {})
        print(f"  ✅ {elapsed:.0f}s | {len(content)} chars | tokens: {usage.get('total_tokens','?')}", flush=True)
        results.append({"mode": label, "elapsed": round(elapsed, 1), "chars": len(content), "usage": usage, "content": content[:300]})
    elif data.get("error"):
        print(f"  ❌ API 错误: {data['error']}", flush=True)
    else:
        print(f"  ❌ 未知: {json.dumps(data)[:200]}", flush=True)

# 结果表
print("\n\n" + "="*60)
print(f"{'模式':<18} {'耗时(s)':<10} {'长度(字符)':<12} {'预览'}")
print("="*60)
for r in results:
    prev = r["content"][:40].replace("\n"," ")
    print(f"{r['mode']:<18} {r['elapsed']:<10} {r['chars']:<12} {prev}")

with open("/mnt/d/wsl2/dotnet/openfusion/bench_results.json", "w") as f:
    json.dump(results, f, ensure_ascii=False, indent=2)
print(f"\n已保存到 bench_results.json（{len(results)} 条）")
