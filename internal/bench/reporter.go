package bench

import (
	"fmt"
	"sort"
	"strings"
)

// FormatReport returns a markdown comparison table.
func FormatReport(report *Report) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("# Benchmark: %s\n\n", report.TestSetDescription))
	b.WriteString(fmt.Sprintf("Variants: %s\n\n", strings.Join(report.Variants, ", ")))

	// Group results by task
	type taskScores struct {
		taskID  string
		scores  map[string]Score
	}
	grouped := map[string]map[string]Score{}
	for _, r := range report.Results {
		if grouped[r.TaskID] == nil {
			grouped[r.TaskID] = map[string]Score{}
		}
		grouped[r.TaskID][r.Variant] = r.Score
	}

	// Per-variant aggregate
	variantTotals := map[string]int{}
	variantCounts := map[string]int{}

	// Table header
	b.WriteString("| Task |")
	for _, v := range report.Variants {
		b.WriteString(fmt.Sprintf(" %s |", v))
	}
	b.WriteString(" Best |\n")

	b.WriteString("|------|")
	for range report.Variants {
		b.WriteString("------|")
	}
	b.WriteString("------|\n")

	// Sort task IDs for stable output
	taskIDs := make([]string, 0, len(grouped))
	for id := range grouped {
		taskIDs = append(taskIDs, id)
	}
	sort.Strings(taskIDs)

	for _, taskID := range taskIDs {
		scores := grouped[taskID]
		b.WriteString(fmt.Sprintf("| %s |", taskID))

		bestScore := 0
		bestVariant := ""
		for _, v := range report.Variants {
			s, ok := scores[v]
			total := s.Total()
			if ok {
				b.WriteString(fmt.Sprintf(" %d |", total))
				variantTotals[v] += total
				variantCounts[v]++
				if total > bestScore {
					bestScore = total
					bestVariant = v
				}
			} else {
				b.WriteString(" - |")
			}
		}
		b.WriteString(fmt.Sprintf(" **%s** |\n", bestVariant))
	}

	// Summary row
	b.WriteString("| **Average** |")
	for _, v := range report.Variants {
		if variantCounts[v] > 0 {
			avg := variantTotals[v] / variantCounts[v]
			b.WriteString(fmt.Sprintf(" **%d** |", avg))
		} else {
			b.WriteString(" - |")
		}
	}
	b.WriteString(" |\n")

	// Per-category breakdown
	b.WriteString("\n## Category Breakdown\n\n")
	byCategory := map[string]map[string]int{}
	byCategoryCount := map[string]map[string]int{}
	for _, r := range report.Results {
		if byCategory[r.Variant] == nil {
			byCategory[r.Variant] = map[string]int{}
			byCategoryCount[r.Variant] = map[string]int{}
		}
		// Find task category
		cat := taskCategory(r.TaskID)
		byCategory[r.Variant][cat] += r.Score.Total()
		byCategoryCount[r.Variant][cat]++
	}

	b.WriteString("| Variant |")
	cats := []string{"factual", "reasoning", "code", "synthesis", "controversial"}
	for _, c := range cats {
		b.WriteString(fmt.Sprintf(" %s |", c))
	}
	b.WriteString(" Avg |\n")
	b.WriteString("|---------|")
	for range cats {
		b.WriteString("------|")
	}
	b.WriteString("------|\n")

	for _, v := range report.Variants {
		b.WriteString(fmt.Sprintf("| %s |", v))
		total := 0
		count := 0
		for _, c := range cats {
			if s, ok := byCategory[v][c]; ok && byCategoryCount[v][c] > 0 {
				avg := s / byCategoryCount[v][c]
				b.WriteString(fmt.Sprintf(" %d |", avg))
				total += avg
				count++
			} else {
				b.WriteString(" - |")
			}
		}
		if count > 0 {
			b.WriteString(fmt.Sprintf(" **%d** |\n", total/count))
		} else {
			b.WriteString(" - |\n")
		}
	}

	return b.String()
}

func taskCategory(taskID string) string {
	parts := strings.SplitN(taskID, "-", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return "unknown"
}

// FormatReportSummary returns a short markdown summary.
func FormatReportSummary(report *Report, costMap map[string]float64) string {
	if len(report.Results) == 0 {
		return "No results."
	}

	var b strings.Builder
	b.WriteString("## Benchmark Summary\n\n")
	b.WriteString("| Variant | Avg Score | Total Cost | $\u00d7Score |\n")
	b.WriteString("|---------|-----------|------------|-----------|\n")

	type variantStats struct {
		name string
		avg  int
		cost float64
	}
	var stats []variantStats

	byVariant := map[string]int{}
	byVariantCount := map[string]int{}
	for _, r := range report.Results {
		byVariant[r.Variant] += r.Score.Total()
		byVariantCount[r.Variant]++
	}

	for _, v := range report.Variants {
		if byVariantCount[v] == 0 {
			continue
		}
		avg := byVariant[v] / byVariantCount[v]
		cost := costMap[v]
		costEff := "N/A"
		if cost > 0 {
			costEff = fmt.Sprintf("%.2f", float64(avg)/cost)
		}
		b.WriteString(fmt.Sprintf("| %s | %d | $%.4f | %s |\n", v, avg, cost, costEff))
		stats = append(stats, variantStats{v, avg, cost})
	}

	// Highlight best value
	if len(stats) >= 2 {
		b.WriteString("\n**Best score**: ")
		best := stats[0]
		for _, s := range stats[1:] {
			if s.avg > best.avg {
				best = s
			}
		}
		b.WriteString(best.name)

		if len(stats) >= 2 {
			ratioBest := stats[0]
			ratioBestRatio := 0.0
			for _, s := range stats {
				if s.cost > 0 {
					r := float64(s.avg) / s.cost
					if r > ratioBestRatio {
						ratioBestRatio = r
						ratioBest = s
					}
				}
			}
			b.WriteString(fmt.Sprintf("\n**Best value ($/score)**: %s (%.1f pts/$)\n", ratioBest.name, ratioBestRatio))
		}
	}

	return b.String()
}
