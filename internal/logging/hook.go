package logging

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// HookConfig controls the fusion logging hook.
type HookConfig struct {
	Enabled       bool   `yaml:"enabled"`
	OutputDir     string `yaml:"output_dir"`
	AutoSplit     string `yaml:"auto_split"`     // "monthly" or "daily"
	KnowledgeBase bool   `yaml:"knowledge_base"` // also write knowledge_base.csv
	MaxWorkers    int    `yaml:"max_workers"`
	BufferSize    int    `yaml:"buffer_size"`
}

// DefaultHookConfig returns sensible defaults.
func DefaultHookConfig() HookConfig {
	return HookConfig{
		Enabled:    false,
		OutputDir:  "fusion_log",
		AutoSplit:  "monthly",
		MaxWorkers: 2,
		BufferSize: 100,
	}
}

// Hook non-blockingly writes fusion logs to CSV with monthly rotation + zstd compression.
type Hook struct {
	cfg    HookConfig
	logCh  chan *FusionLog
	kbCh   chan *KnowledgeEntry
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu         sync.Mutex
	currFile   *os.File
	currCSV    *csv.Writer
	currPeriod string // "2006-01" for monthly, "2006-01-02" for daily
	kbFile     *os.File
	kbCSV      *csv.Writer
	kbPeriod   string
	log        interface{ Info(string, ...any); Warn(string, ...any) }
}

// NewHook creates and starts the logging hook.
func NewHook(cfg HookConfig) *Hook {
	if !cfg.Enabled {
		return &Hook{cfg: cfg}
	}
	if cfg.MaxWorkers < 1 {
		cfg.MaxWorkers = 1
	}
	if cfg.BufferSize < 1 {
		cfg.BufferSize = 100
	}

	ctx, cancel := context.WithCancel(context.Background())
	h := &Hook{
		cfg:    cfg,
		logCh:  make(chan *FusionLog, cfg.BufferSize),
		kbCh:   make(chan *KnowledgeEntry, cfg.BufferSize),
		cancel: cancel,
	}

	// Ensure output dir exists
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "fusion_log: mkdir %s: %v\n", cfg.OutputDir, err)
	}

	// Start worker goroutines
	for i := 0; i < cfg.MaxWorkers; i++ {
		h.wg.Add(1)
		go h.runWorker(ctx)
	}

	return h
}

// Log queues a fusion log entry for async writing. Non-blocking.
func (h *Hook) Log(entry *FusionLog) {
	if !h.cfg.Enabled {
		return
	}
	select {
	case h.logCh <- entry:
	default:
		// buffer full — drop the entry silently
	}
}

// LogKnowledge queues a knowledge-base entry. Non-blocking.
func (h *Hook) LogKnowledge(entry *KnowledgeEntry) {
	if !h.cfg.Enabled || !h.cfg.KnowledgeBase {
		return
	}
	select {
	case h.kbCh <- entry:
	default:
	}
}

// Close flushes all buffered entries and compresses the current file.
func (h *Hook) Close() {
	if !h.cfg.Enabled {
		return
	}
	h.cancel()
	h.wg.Wait()

	h.mu.Lock()
	defer h.mu.Unlock()

	h.closeCurrent()
	h.compressOld(h.currPeriod)
}

// ---------------------------------------------------------------------------
// worker
// ---------------------------------------------------------------------------

func (h *Hook) runWorker(ctx context.Context) {
	defer h.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case entry := <-h.logCh:
			h.writeLog(entry)
		case entry := <-h.kbCh:
			h.writeKB(entry)
		}
	}
}

// ---------------------------------------------------------------------------
// CSV writing
// ---------------------------------------------------------------------------

func (h *Hook) writeLog(entry *FusionLog) {
	h.mu.Lock()
	defer h.mu.Unlock()

	period := periodKey(h.cfg.AutoSplit, entry.Timestamp)
	if err := h.ensureFile(&h.currFile, &h.currCSV, &h.currPeriod, period,
		"fusion", entry.CSVHeaders()); err != nil {
		return
	}
	h.currCSV.Write(entry.CSVRecord())
	h.currCSV.Flush()
}

func (h *Hook) writeKB(entry *KnowledgeEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()

	period := periodKey(h.cfg.AutoSplit, entry.Timestamp)
	if err := h.ensureFile(&h.kbFile, &h.kbCSV, &h.kbPeriod, period,
		"knowledge", entry.CSVHeaders()); err != nil {
		return
	}
	h.kbCSV.Write(entry.CSVRecord())
	h.kbCSV.Flush()
}

// ensureFile opens a new CSV file when the period changes, closing the old one.
func (h *Hook) ensureFile(f **os.File, w **csv.Writer, curPeriod *string,
	period, prefix string, headers []string) error {

	if *f != nil && *curPeriod == period {
		return nil // same period, file open
	}

	// Close old file and compress it
	if *f != nil {
		(*w).Flush()
		(*f).Close()
		h.compressOld(*curPeriod)
	}

	// Open new file
	name := filepath.Join(h.cfg.OutputDir, fmt.Sprintf("%s_%s.csv", prefix, period))
	fh, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	// Write header if file is new (empty)
	info, _ := fh.Stat()
	if info.Size() == 0 {
		cw := csv.NewWriter(fh)
		cw.Write(headers)
		cw.Flush()
	}

	*f = fh
	*w = csv.NewWriter(fh)
	*curPeriod = period
	return nil
}

// closeCurrent closes the open CSV file(s).
func (h *Hook) closeCurrent() {
	if h.currFile != nil {
		h.currCSV.Flush()
		h.currFile.Close()
		h.currFile = nil
	}
	if h.kbFile != nil {
		h.kbCSV.Flush()
		h.kbFile.Close()
		h.kbFile = nil
	}
}

// compressOld runs zstd on the CSV for the given period, then removes the raw file.
func (h *Hook) compressOld(period string) {
	if period == "" {
		return
	}
	csvPath := filepath.Join(h.cfg.OutputDir, fmt.Sprintf("fusion_%s.csv", period))
	zstPath := csvPath + ".zst"

	// Only compress if the raw file exists and .zst doesn't (idempotent)
	if _, err := os.Stat(zstPath); err == nil {
		os.Remove(csvPath)
		return
	}
	if _, err := os.Stat(csvPath); os.IsNotExist(err) {
		return
	}

	// Try external zstd first
	if err := exec.Command("zstd", "-q", "--rm", csvPath).Run(); err == nil {
		return
	}

	// Fallback: compress with Go's gzip (stdlib, zero deps)
	// The user can recompress with zstd later for better ratio
	if err := exec.Command("gzip", "-f", csvPath).Run(); err != nil {
		// Warn but don't fail — file stays uncompressed
		fmt.Fprintf(os.Stderr, "fusion_log: compress %s: no zstd or gzip found\n", csvPath)
	}
}

// periodKey returns the current period string for the given split mode and timestamp.
func periodKey(mode, ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t = time.Now()
	}
	switch mode {
	case "daily":
		return t.Format("2006-01-02")
	default: // "monthly"
		return t.Format("2006-01")
	}
}

// ---------------------------------------------------------------------------
// Build a FusionLog from a completed fusion response
// ---------------------------------------------------------------------------

// IsGoodForKnowledge returns true if the judge found meaningful analysis.
// Used to filter entries for the knowledge base.
func IsGoodForKnowledge(a *FusionAnalysis) bool {
	if a == nil {
		return false
	}
	return a.SignalCount >= 2
}

// FusionAnalysis is a lightweight summary of judge output for knowledge-base filtering.
type FusionAnalysis struct {
	Consensus      []string
	Contradictions int
	UniqueInsights int
	BlindSpots     []string
	SignalCount    int
}

// SanitizeCSVField removes characters that would break CSV parsing.
func SanitizeCSVField(s string) string {
	// Replace newlines and commas with spaces
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, ",", " ")
	return s
}
