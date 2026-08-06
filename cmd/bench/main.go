// Command bench runs A/B comparison of OpenFusion presets against the
// embedded benchmark test set with multi-trial, latency tracking,
// mechanical reliability signals, and SQLite trend storage.
//
// Usage:
//
//	go run ./cmd/bench/                         # mock judge, 2 trials (CI-safe)
//	go run ./cmd/bench/ -real-judge             # real LLM judge via cc-switch
//	go run ./cmd/bench/ -real-judge -trials 3   # 3 trials per task
//	go run ./cmd/bench/ -real-panel -real-judge   # real models + real judge (full benchmark)
//	go run ./cmd/bench/ -real-panel -real-judge -trials 3
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lhy/openfusion/internal/bench"
	"github.com/lhy/openfusion/internal/fusion"
	"github.com/lhy/openfusion/internal/preset"
	"github.com/lhy/openfusion/internal/provider"
	"github.com/lhy/openfusion/internal/types"
)

func main() {
	realPanel := flag.Bool("real-panel", false, "Use real LLM providers (cc-switch) instead of mock panel")
	realJudge := flag.Bool("real-judge", false, "Use real LLM judge (cc-switch) instead of mock")
	presetList := flag.String("presets", "quality,budget", "Comma-separated preset names to compare")
	trials := flag.Int("trials", 2, "Trials per task (>=1)")
	storePath := flag.String("store", "", "SQLite store path (default: bench_results.db in project root)")
	flag.Parse()

	presets := strings.Split(*presetList, ",")
	if len(presets) < 2 {
		fmt.Fprintln(os.Stderr, "Need at least 2 presets to compare")
		os.Exit(1)
	}
	if *trials < 1 {
		*trials = 1
	}

	// Setup
	ts, err := bench.LoadTestSet()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load testset: %v\n", err)
		os.Exit(1)
	}

	pm := provider.NewManager()
	if *realPanel {
		// Real providers via cc-switch
		pm.Register("cc-switch", provider.NewOpenAIAdapter("cc-switch", "http://127.0.0.1:15722", "sk-noop"))
		pm.Register("modelscope", provider.NewOpenAIAdapter("modelscope", "http://127.0.0.1:15722", "sk-noop"))
	} else {
		// Mock providers for CI/quick testing
		pm.Register("cc-switch", newModelAwareMock("cc-switch"))
		pm.Register("modelscope", newModelAwareMock("modelscope"))
	}
	pm.Register("mock-judge", newModelAwareMock("mock-judge"))

	var judgeProv provider.Provider
	if *realJudge {
		judgeProv = provider.NewOpenAIAdapter("cc-switch", "http://127.0.0.1:15722", "sk-noop")
	} else {
		p, err := pm.Get("mock-judge")
		if err != nil {
			fmt.Fprintf(os.Stderr, "mock-judge: %v\n", err)
			os.Exit(1)
		}
		judgeProv = p
	}

	pr := preset.NewRegistry()
	if err := pr.LoadDir("presets"); err != nil {
		fmt.Fprintf(os.Stderr, "load presets: %v\n", err)
		os.Exit(1)
	}

	cfg := bench.RunnerConfig{
		TrialsPerTask: *trials,
		PanelTimeout:  30 * time.Second,
		JudgeTimeout:  60 * time.Second,
	}

	runID := bench.NewRunID()
	var allTrials []bench.TrialResult
	allModelIDs := make(map[string]bool)
	start := time.Now()

	for _, presetName := range presets {
		_, ok := pr.Get(presetName)
		if !ok {
			fmt.Fprintf(os.Stderr, "preset %q not found\n", presetName)
			os.Exit(1)
		}

		engine := fusion.NewEngine(pr, pm,
			cfg.PanelTimeout,
			cfg.JudgeTimeout,
			120*time.Second,
			nil, nil, nil, nil, nil, nil,
			fusion.NewModelRouter(fusion.DefaultRouterConfig()),
		)

		fmt.Printf("\n── %s (%d trials) ──\n", presetName, cfg.TrialsPerTask)

		trials, mIDs := bench.RunPreset(engine, judgeProv, presetName, ts, cfg)
		allTrials = append(allTrials, trials...)
		for _, id := range mIDs {
			allModelIDs[id] = true
		}

		vs := bench.ComputeVariantScore(presetName, trials)
		fmt.Printf("  Avg=%.1f ±%.1f | Capability=%.0f Reliability=%.0f Efficiency=%.0f | Gated=%.0f | P50=%dms\n",
			vs.AvgScore, vs.StdDev,
			vs.CapabilityScore, vs.ReliabilityScore, vs.EfficiencyScore,
			vs.GatedScore, vs.LatencyP50)
	}

	duration := time.Since(start)

	// Phase 1 report
	fmt.Println()
	fmt.Println(bench.FormatComparisonReport(ts.Description, presets, allTrials))

	// Save to SQLite store
	dbPath := *storePath
	if dbPath == "" {
		dbPath = filepath.Join("bench_results.db")
	}
	store, err := bench.NewStore(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: store open failed: %v — results not persisted\n", err)
	} else {
		defer store.Close()

		mIDs := make([]string, 0, len(allModelIDs))
		for id := range allModelIDs {
			mIDs = append(mIDs, id)
		}

		meta := bench.RunMetadata{
			ID:        runID,
			Timestamp: time.Now().UTC(),
			Fingerprint: bench.FingerprintPreset(
				strings.Join(presets, "+"),
				mIDs,
			),
			Variants:      presets,
			TaskCount:     len(ts.Tasks),
			TrialsPerTask: cfg.TrialsPerTask,
			Duration:      duration,
		}

		var variantScores []bench.VariantScore
		for _, v := range presets {
			byVariant := filterTrials(allTrials, v)
			variantScores = append(variantScores, bench.ComputeVariantScore(v, byVariant))
		}

		if err := store.SaveRun(meta, variantScores, allTrials); err != nil {
			fmt.Fprintf(os.Stderr, "warn: store save failed: %v\n", err)
		} else {
			fmt.Printf("\n💾 Saved run %s to %s\n", runID, dbPath)

			// Show baseline comparison if we have history
			runs, _ := store.RecentRuns(5)
			if len(runs) > 1 {
				fmt.Println("\n## Recent Runs")
				for _, r := range runs {
					fmt.Printf("  %s  variants=%s  tasks=%d  trials=%d\n",
						r.ID, strings.Join(r.Variants, ","), r.TaskCount, r.TrialsPerTask)
				}
				fmt.Print(bench.CompareToBaseline(variantScores, store))
			}
		}
	}

	fmt.Printf("\n⏱️  Total: %v\n", duration.Round(time.Second))
}

func filterTrials(all []bench.TrialResult, variant string) []bench.TrialResult {
	var out []bench.TrialResult
	for _, t := range all {
		if t.Variant == variant {
			out = append(out, t)
		}
	}
	return out
}

// ── Model-aware mock provider ──

type modelAwareMock struct {
	name string
}

func newModelAwareMock(name string) *modelAwareMock {
	return &modelAwareMock{name: name}
}

func (m *modelAwareMock) Name() string { return m.name }

func (m *modelAwareMock) ChatCompletion(ctx context.Context, req *types.ChatRequest) (*types.ChatResponse, error) {
	content := req.Messages[len(req.Messages)-1].Content

	if strings.Contains(content, "You are an expert evaluator") {
		return mockJudgeResponse(), nil
	}

	model := strings.ToLower(req.Model)
	var response string
	switch {
	case strings.Contains(model, "flash") || strings.Contains(model, "step"):
		response = mockResearcher(content)
	case strings.Contains(model, "glm") || strings.Contains(model, "zhipu"):
		response = mockJudgeStyle(content)
	default:
		response = mockAnalyst(content)
	}

	return &types.ChatResponse{
		ID:      "mock_" + m.name + "_" + model,
		Object:  "chat.completion",
		Choices: []types.Choice{{Index: 0, Message: types.ChatMessage{Role: "assistant", Content: response}}},
		Usage:   types.Usage{PromptTokens: 10, CompletionTokens: max(1, len(response)/3), TotalTokens: 10 + max(1, len(response)/3)},
	}, nil
}

func mockJudgeResponse() *types.ChatResponse {
	s := bench.Score{Accuracy: 85, Completeness: 80, Clarity: 90, CitationRating: 75, Notes: "mock judge"}
	raw := fmt.Sprintf(`{"accuracy":%d,"completeness":%d,"clarity":%d,"citation_rating":%d,"notes":"%s"}`,
		s.Accuracy, s.Completeness, s.Clarity, s.CitationRating, s.Notes)
	return &types.ChatResponse{
		ID: "mock_judge", Object: "chat.completion",
		Choices: []types.Choice{{Index: 0, Message: types.ChatMessage{Role: "assistant", Content: raw}}},
	}
}

// ── Response strategies (same as before) ──

func mockAnalyst(prompt string) string {
	switch {
	case strings.Contains(prompt, "Burkina Faso"):
		return "The capital of Burkina Faso is Ouagadougou (~2.8M metro, 2024 est). Official language: French. Widely spoken: Mossi (Mooré). Landlocked West African nation, formerly Upper Volta, renamed 1984."
	case strings.Contains(prompt, "IPv4 and IPv6"):
		return "IPv4: 32-bit (dotted decimal, ~4.3B addresses). IPv6: 128-bit (colon hex, 3.4×10^38 addresses). IPv6 created to solve address exhaustion. NAT/CIDR were interim solutions. IPv6 adds built-in IPsec, simplified header, SLAAC autoconfig."
	case strings.Contains(prompt, "3 cats catch 3 mice"):
		return "Step by step: 3 cats catch 3 mice in 3 min → each cat catches 1 mouse in 3 min. 100 cats working independently → 100 mice in 3 min. Answer: 3 minutes. (Common trap: linear scaling → 100 min, which is wrong because cats work in parallel.)"
	case strings.Contains(prompt, "3-gallon jug"):
		return "Fill 5-gal, pour into 3-gal → 2 left in 5. Empty 3, pour 2 into 3. Fill 5, top off 3 (takes 1) → 4 gal in 5. Done."
	case strings.Contains(prompt, "reverse a singly linked list") && strings.Contains(prompt, "Go"):
		return "```go\ntype ListNode struct { Val int; Next *ListNode }\nfunc reverseList(head *ListNode) *ListNode {\n    var prev *ListNode\n    for cur := head; cur != nil; {\n        next := cur.Next\n        cur.Next = prev\n        prev = cur\n        cur = next\n    }\n    return prev\n}\n```\nO(n) time, O(1) space."
	case strings.Contains(prompt, "asyncio"):
		return "```python\nimport asyncio, aiohttp\n\nasync def fetch(session, url):\n    try:\n        async with session.get(url, timeout=aiohttp.ClientTimeout(5)) as r:\n            return await r.text()\n    except: return None\n\nasync def main():\n    urls = [f'https://ex.com/{i}' for i in range(10)]\n    async with aiohttp.ClientSession() as s:\n        return await asyncio.gather(*[fetch(s,u) for u in urls], return_exceptions=True)\n\nasyncio.run(main())\n```"
	case strings.Contains(prompt, "Transformer architecture with Mamba"):
		return "Transformer: O(n²) self-attention, KV cache, excellent recall. Mamba (SSM): O(n) selective scan, constant inference memory, fast. Trade-off: Mamba excels at long sequences but may lag on exact recall. Hybrids (Jamba, Samba) emerging."
	case strings.Contains(prompt, "SFT, RLHF, and DPO"):
		return "SFT: supervised, stable, ceiling = data quality. RLHF: reward model + PPO, can exceed demonstrations but unstable. DPO: direct preference optimization, no reward model, simpler/stable, dominant in 2024-25. Online DPO emerging."
	case strings.Contains(prompt, "RAG") && strings.Contains(prompt, "long-context"):
		return "RAG: modular, cheap per-query, updatable, interpretable. Cons: retrieval latency/quality risk. Long-context: simple end-to-end, full attention. Cons: O(n²) cost, middle dilution. Use RAG for large/evolving corpora, long-context when corpus fits in window."
	case strings.Contains(prompt, "open-source AI models be regulated"):
		return "Pro-regulation: open weights can't be recalled, can be fine-tuned for harm. Anti: openness drives transparency + innovation, concentration of power is worse. Middle: tiered regulation by capability (not openness), compute thresholds, mandatory eval. No global consensus as of 2025."
	default:
		return fmt.Sprintf("Analyst: %.100s — detailed, factual, with numbers.", prompt)
	}
}

func mockResearcher(prompt string) string {
	switch {
	case strings.Contains(prompt, "Burkina Faso"):
		return "Ouagadougou, Burkina Faso. Metro: ~3M. Languages: French (official), Mossi, Dyula, Fula. Named 'Land of Honest People' by Sankara in 1984. Gold and cotton exports."
	case strings.Contains(prompt, "IPv4 and IPv6"):
		return "IPv4: 32-bit, 4 decimal octets, ~4.3B addresses, NAT extends life. IPv6: 128-bit, 8 hex groups, massive space, no broadcast, IPsec built-in. Created because internet grew beyond IPv4 limits. Migration slow because NAT works well enough."
	case strings.Contains(prompt, "3 cats catch 3 mice"):
		return "Per-cat rate = 1 mouse / 3 min. 100 cats × (1 mouse / 3 min) = 100 mice in 3 min. Answer: 3 min."
	case strings.Contains(prompt, "3-gallon jug"):
		return "Alternative (3→5): Fill 3, pour to 5. Fill 3, pour to 5 (5 now full, 3 has 1). Empty 5, pour 1 into 5. Fill 3, pour to 5 → 4 gal."
	case strings.Contains(prompt, "reverse a singly linked list") && strings.Contains(prompt, "Go"):
		return "Recursive: base case nil/single → head. Recursively reverse rest, then head.next.next=head, head.next=nil. O(n) time, O(n) stack. Iterative preferred for large lists."
	case strings.Contains(prompt, "asyncio"):
		return "httpx alternative: `async with httpx.AsyncClient() as c: tasks = [c.get(u, timeout=5) for u in urls]; results = await asyncio.gather(*tasks, return_exceptions=True)`."
	case strings.Contains(prompt, "Transformer architecture with Mamba"):
		return "Attention vs SSMs: transformers enable direct token-to-token interaction (O(n²)). Mamba processes sequentially with fixed hidden state (O(n)). On long sequences Mamba wins speed; on exact recall tasks, attention still superior. The best of both worlds: hybrid architectures."
	case strings.Contains(prompt, "SFT, RLHF, and DPO"):
		return "Three eras: 1) SFT — supervised imitation, simple but capped. 2) RLHF — preference-based RL, powerful but fragile. 3) DPO — reformulates RLHF as direct loss, no reward model needed. Current best practice: iterative DPO with online preference data."
	case strings.Contains(prompt, "RAG") && strings.Contains(prompt, "long-context"):
		return "The retrieval-generation boundary is blurring. RAG wins on cost and freshness; long-context wins on simplicity and avoiding retrieval failures. Hybrid: retrieve first, then long-context model processes results."
	case strings.Contains(prompt, "open-source AI models be regulated"):
		return "Three camps: (1) regulate open weights strictly — recall impossible. (2) open source IS safety — inspectable, democratized. (3) tiered approach: capability thresholds, not openness, should trigger regulation. EU AI Act and US exec order take different approaches."
	default:
		return fmt.Sprintf("Researcher view: %.100s — broader context, alternative angles.", prompt)
	}
}

func mockJudgeStyle(prompt string) string {
	switch {
	case strings.Contains(prompt, "Burkina Faso"):
		return "Ouagadougou is the capital of Burkina Faso, with a metro population of ~2.8–3 million. Official language: French; indigenous languages (Mossi, Dyula, Fula) more widely spoken. Landlocked West African nation, renamed from Upper Volta in 1984 by Thomas Sankara."
	case strings.Contains(prompt, "IPv4 and IPv6"):
		return "IPv4: 32-bit addresses (dotted decimal, ~4.3B). IPv6: 128-bit (colon hex, 3.4×10^38). IPv6 addresses IPv4 exhaustion; NAT/CIDR were interim solutions. IPv6 adds IPsec, simplified header, SLAAC auto-configuration. Migration is ongoing but slow due to NAT sufficiency."
	case strings.Contains(prompt, "3 cats catch 3 mice"):
		return "Step-by-step: (1) 3 cats → 3 mice in 3 min → per-cat rate = 1 mouse/3 min. (2) 100 cats working independently each catch 1 mouse in 3 min. (3) Total: 100 mice in 3 minutes. Common trap: confusing serial vs parallel work."
	case strings.Contains(prompt, "3-gallon jug"):
		return "Two solutions: (5→3 method) Fill 5, pour to 3 → 2 left. Empty 3, pour 2 into 3. Fill 5, top off 3 → 4 in 5. OR (3→5 method) Fill 3, pour to 5. Fill 3, pour to 5 until full → 1 left in 3. Empty 5, pour 1, fill 3, pour → 4. Both valid."
	case strings.Contains(prompt, "reverse a singly linked list"):
		return "Iterative (O(n), O(1)): track prev/curr/next, reverse links in place. Recursive (O(n), O(n)): base case nil/single, recursively reverse rest. Iterative preferred for Go — avoids stack overflow on large lists."
	case strings.Contains(prompt, "asyncio"):
		return "Both aiohttp and httpx work. Key patterns: (1) per-request timeout, (2) asyncio.gather with return_exceptions=True for graceful failure, (3) shared ClientSession for connection reuse. Critical: timeout per request, not global."
	case strings.Contains(prompt, "Transformer architecture with Mamba"):
		return "Transformers (attention-based): O(n²) complexity, excellent long-range recall, KV-cache. Mamba (SSM-based): O(n) selective scan, constant inference memory, fast on long sequences. Hybrid architectures (Jamba, Samba) combine both benefits."
	case strings.Contains(prompt, "SFT, RLHF, and DPO"):
		return "Three alignment approaches: SFT (supervised, stable, data-bound), RLHF (reward model + PPO, can exceed demonstrations but unstable), DPO (direct preference optimization, simpler, currently dominant). Trend: iterative DPO with online data."
	case strings.Contains(prompt, "RAG") && strings.Contains(prompt, "long-context"):
		return "RAG advantages: modular, cheap, updatable, interpretable. Long-context advantages: simpler, no retrieval failures, full attention. Practical: use RAG for large/evolving corpora; long-context when corpus fits in window. Hybrid: retrieve then long-context process."
	case strings.Contains(prompt, "open-source AI models be regulated"):
		return "The open-source AI regulation debate has valid arguments on all sides. Pro-regulation: open weights enable irreversible harm. Pro-openness: transparency enables safety research, democratization prevents concentration. Middle: tiered regulation by capability thresholds. No global consensus."
	default:
		return fmt.Sprintf("Synthesized answer combining multiple perspectives on: %.80s", prompt)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
