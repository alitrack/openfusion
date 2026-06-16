"""
对比测试：Solo vs Fusion
Solo 模型直连 Ollama/DeepSeek API，Fusion 走 OpenFusion
"""
import json, subprocess, time

OLLAMA = "http://10.10.10.8:11434/v1/chat/completions"
DEEPSEEK = "https://api.deepseek.com/v1/chat/completions"
FUSION = "http://127.0.0.1:8080/v1/chat/completions"
DS_KEY = "sk-8fc28136e409420c964e6134204f99d4"

TESTS = [
    {"id":"algo-ttl", "cat":"算法",
     "prompt":"用 Python 实现一个带 TTL 过期机制的缓存（类似 Redis）。要求：get(key) 返回 value 或 None；set(key, value, ttl_seconds) 过期后自动失效；O(1) 读写；多线程安全。"},
    {"id":"debug-fib", "cat":"Debug",
     "prompt":"分析这段代码是否有 bug，如果有请指出并修正：function fib(n) { const memo = {}; function f(x) { if (x in memo) return memo[x]; if (x <= 1) return x; memo[x] = f(x-1) + f(x-2); return memo[x]; } return f(n); }"},
    {"id":"design-queue", "cat":"系统设计",
     "prompt":"设计一个简单的任务队列系统，支持：添加任务、取消任务、设置最大并发数、任务完成回调。用 Python 实现。"},
    {"id":"project-notes", "cat":"多文件",
     "prompt":"写一个 Markdown 笔记 CLI 应用：note new 'title' 创建笔记；note list 列出所有笔记；note show <id> 显示笔记内容；note search <keyword> 搜索笔记。用 Python 实现，要求合理的文件和模块结构。"},
    {"id":"agent-deploy", "cat":"Agent",
     "prompt":"你是一个 DevOps agent。设计一个部署脚本：从 GitHub 拉取代码 → 构建 Docker 镜像 → 推送到私有仓库 → 在远程服务器上用 docker-compose 重启服务。给出完整的 bash 脚本。"},
]

MODES = [
    ("A-Qwen-solo",   OLLAMA,   {"model":"qwen3.6:35B",    "Authorization":"Bearer ollama"}),
    ("B-Gemma-solo",  OLLAMA,   {"model":"gemma4:26b",     "Authorization":"Bearer ollama"}),
    ("C-DS-solo",     DEEPSEEK, {"model":"deepseek-chat",  "Authorization":f"Bearer {DS_KEY}"}),
    ("D-QwenGemma",   FUSION,   {"model":"openfusion/qwen-gemma", "Authorization":"Bearer "}),
    ("E-QwenDS",      FUSION,   {"model":"openfusion/qwen-ds",    "Authorization":"Bearer "}),
    ("F-QwenGemmaDS", FUSION,   {"model":"openfusion/qwen-gemma-ds", "Authorization":"Bearer "}),
]

def call_model(endpoint, headers, prompt, timeout=300):
    payload = {"model": headers.pop("model"), "messages": [{"role":"user","content":prompt}], "max_tokens": 4096}
    auth = headers.pop("Authorization", "Bearer ")
    hs = ["-H", "Content-Type: application/json", "-H", f"Authorization: {auth}"]
    # Re-add model for curl since we popped it
    payload2 = {"model": payload["model"], "messages": payload["messages"], "max_tokens": payload["max_tokens"]}
    try:
        r = subprocess.run(["curl", "-s", endpoint, *hs, "-d", json.dumps(payload2)],
                          capture_output=True, text=True, timeout=timeout)
        data = json.loads(r.stdout)
        if data.get("choices"):
            return data["choices"][0]["message"]["content"], data.get("usage", {})
        return f"ERROR: {json.dumps(data)[:200]}", {}
    except subprocess.TimeoutExpired:
        return "TIMEOUT", {}
    except Exception as e:
        return f"EXCEPTION: {e}", {}

results = []
total = len(TESTS) * len(MODES)
count = 0

for test in TESTS:
    for label, endpoint, headers in MODES:
        count += 1
        print(f"[{count}/{total}] {label} → {test['id']}...", end=" ", flush=True)
        t0 = time.time()
        content, usage = call_model(endpoint, dict(headers), test["prompt"])
        elapsed = time.time() - t0
        print(f"⏱ {elapsed:.0f}s  📝 {len(content)} chars")
        results.append({
            "test": test["id"], "cat": test["cat"], "mode": label,
            "elapsed_s": round(elapsed, 1), "content_len": len(content),
        })

# 汇总表
print("\n\n" + "="*80)
print(f"{'测试':<12} {'A-Qwen':<14} {'B-Gemma':<14} {'C-DS':<14} {'D-QG':<14} {'E-QDS':<14} {'F-QGDS':<14}")
print("-"*80)
for test in TESTS:
    rs = [r for r in results if r["test"] == test["id"]]
    row = f"{test['id']:<12}"
    for r in rs:
        row += f"{r['content_len']:>6}/{r['elapsed_s']:>4.0f}s  "
    print(row)

# 保存
with open("/mnt/d/wsl2/dotnet/openfusion/bench_compare_results.json", "w") as f:
    json.dump(results, f, ensure_ascii=False, indent=2)
print(f"\n结果已保存到 bench_compare_results.json")
