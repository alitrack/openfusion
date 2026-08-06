package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/lhy/openfusion/internal/bench"
	"github.com/lhy/openfusion/internal/config"
	"github.com/lhy/openfusion/internal/fusion"
	"github.com/lhy/openfusion/internal/judge"
	"github.com/lhy/openfusion/internal/logger"
	"github.com/lhy/openfusion/internal/metrics"
	"github.com/lhy/openfusion/internal/openrouter"
	"github.com/lhy/openfusion/internal/panel"
	"github.com/lhy/openfusion/internal/plugin"
	"github.com/lhy/openfusion/internal/preset"
	"github.com/lhy/openfusion/internal/provider"
	"github.com/lhy/openfusion/internal/skill"
	"github.com/lhy/openfusion/internal/tracing"
	"github.com/lhy/openfusion/internal/types"
)

func runBench(args []string) {
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	cfgPath := fs.String("config", "config.yaml", "Config file path")
	dryRun := fs.Bool("dry-run", false, "Validate infra without API calls")
	fs.Parse(args)

	testSet, err := bench.LoadTestSet()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load test set: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Loaded %d benchmark tasks (%d categories)\n", len(testSet.Tasks), countCategories(testSet.Tasks))

	if *dryRun {
		fmt.Println("\n--- Dry run: validating infrastructure ---")
		for _, task := range testSet.Tasks {
			prompt := bench.BuildJudgePrompt(task, "[mock response]")
			fmt.Printf("  [%s] %s ✓ (%d chars)\n", task.ID, truncate(task.Prompt, 50), len(prompt))
		}
		fmt.Println("\nDry run complete. All prompts built correctly.")
		fmt.Println("\nRun with real API calls:")
		fmt.Println("  openfusion bench -config config.yaml")
		return
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load config: %v\n", err)
		os.Exit(1)
	}

	_ = logger.New(nil) // log instance

	provider.NewManager()
	pm := provider.NewManager()

	plugin.Register(&plugin.DeepSeekPlugin{})
	plugin.Register(&plugin.GenericPlugin{})
	plugin.Register(&openrouter.GatewayPlugin{})

	for name, def := range cfg.Providers {
		adapter := provider.NewOpenAIAdapter(name, def.BaseURL, def.APIKey)
		pluginName := def.Plugin
		if pluginName == "" {
			pluginName = "generic"
		}
		if p := plugin.Get(pluginName); p != nil {
			adapter.SetPlugin(p)
		} else {
			adapter.SetPlugin(plugin.Get("generic"))
		}
		pm.Register(name, adapter)
	}

	pr := preset.NewRegistry()
	if cfg.Presets.Dir != "" {
		pr.LoadDir(cfg.Presets.Dir)
	}
	if len(cfg.Presets.Items) > 0 {
		pr.LoadInline(cfg.Presets.Items)
	}

	// Build engine and collect variants
	panelTimeout := parseDuration(fmt.Sprintf("%ds", cfg.Fusion.PanelTimeoutPerModel), 60)
	judgeTimeout := parseDuration("120s", 120)
	defaultTimeout := parseDuration(fmt.Sprintf("%ds", cfg.Fusion.DefaultTimeout), 120)

	dispatcher := panel.NewDispatcher(pm, panelTimeout, nil, cfg.Fusion.MaxConcurrent, 0)
	synth := judge.NewSynthesizer(pm, judgeTimeout)
	metricsCollector := metrics.NewCollector()
	tracer := tracing.NewTracer()
	sm := skill.NewMatcher(nil, "")
	se := skill.NewExecutor(pm, dispatcher, synth, defaultTimeout)

	engine := fusion.NewEngine(pr, pm, panelTimeout, judgeTimeout, defaultTimeout,
		metricsCollector, nil, nil, tracer, sm, se, fusion.NewModelRouter(fusion.DefaultRouterConfig()))

	variants := collectVariants(pr)
	fmt.Printf("Testing %d variants across %d tasks...\n", len(variants), len(testSet.Tasks))

	var results []bench.TaskResult
	start := time.Now()

	for _, task := range testSet.Tasks {
		for _, variant := range variants {
			req := &types.ChatRequest{
				Model:    variant,
				Messages: []types.ChatMessage{{Role: "user", Content: task.Prompt}},
			}

			resp, execErr := engine.Execute(variant, req)
			if execErr != nil {
				fmt.Fprintf(os.Stderr, "  [%s][%s] ERROR: %v\n", task.ID, variant, execErr)
				continue
			}

			response := ""
			if len(resp.Choices) > 0 {
				response = resp.Choices[0].Message.Content
			}

			judgeName := findJudgePreset(pr)
			if judgeName == "" {
				fmt.Fprintf(os.Stderr, "  [%s][%s] no judge preset\n", task.ID, variant)
				continue
			}

			judgePrompt := bench.BuildJudgePrompt(task, response)
			scoreResp, scoreErr := engine.Execute(judgeName, &types.ChatRequest{
				Model:    judgeName,
				Messages: []types.ChatMessage{{Role: "user", Content: judgePrompt}},
			})

			var score bench.Score
			judgeRaw := ""
			if scoreErr == nil && len(scoreResp.Choices) > 0 {
				judgeRaw = scoreResp.Choices[0].Message.Content
				score, scoreErr = bench.ExtractScore(judgeRaw)
				if scoreErr != nil {
					fmt.Fprintf(os.Stderr, "  [%s][%s] score parse: %v\n", task.ID, variant, scoreErr)
				}
			}

			results = append(results, bench.TaskResult{
				TaskID:   task.ID,
				Variant:  variant,
				Response: response,
				Score:    score,
				JudgeRaw: judgeRaw,
			})

			fmt.Printf("  [%s][%s] score=%d/100\n", task.ID, variant, score.Total())
		}
	}

	elapsed := time.Since(start)

	report := &bench.Report{
		TestSetDescription: testSet.Description,
		Variants:           variants,
		Results:            results,
	}

	fmt.Println("\n" + bench.FormatReport(report))
	fmt.Println(bench.FormatReportSummary(report, costMap(results)))
	fmt.Printf("\nTotal time: %v\n", elapsed)
}

func countCategories(tasks []bench.Task) int {
	seen := map[string]bool{}
	for _, t := range tasks {
		seen[t.Category] = true
	}
	return len(seen)
}

func collectVariants(pr *preset.Registry) []string {
	var variants []string
	for _, p := range pr.List() {
		if len(p.Panel) == 1 {
			variants = append(variants, p.Name)
		}
	}
	for _, p := range pr.List() {
		if len(p.Panel) > 1 {
			variants = append(variants, p.Name)
		}
	}
	if len(variants) == 0 {
		variants = []string{"default"}
	}
	return variants
}

func findJudgePreset(pr *preset.Registry) string {
	for _, p := range pr.List() {
		if p.Judge.Model != "" && len(p.Panel) <= 1 {
			return p.Name
		}
	}
	for _, p := range pr.List() {
		if p.Judge.Model != "" {
			return p.Name
		}
	}
	return ""
}

func costMap(results []bench.TaskResult) map[string]float64 {
	m := map[string]float64{}
	for _, r := range results {
		m[r.Variant] = 0
	}
	return m
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
