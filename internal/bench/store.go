// Package bench — Phase 1: SQLite time-series store for cross-run trend tracking.
package bench

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Store persists benchmark runs for trend analysis.
type Store struct {
	db *sql.DB
}

// NewStore opens or creates the benchmark SQLite database.
func NewStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=DELETE&_synchronous=FULL")
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func migrate(db *sql.DB) error {
	ddl := `
	CREATE TABLE IF NOT EXISTS runs (
		id          TEXT PRIMARY KEY,
		timestamp   TEXT NOT NULL,
		fingerprint TEXT NOT NULL,  -- JSON
		variants    TEXT NOT NULL,  -- JSON array
		task_count  INTEGER NOT NULL,
		trials_per  INTEGER NOT NULL,
		duration_ms INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS trial_results (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		run_id     TEXT NOT NULL REFERENCES runs(id),
		task_id    TEXT NOT NULL,
		trial      INTEGER NOT NULL,
		variant    TEXT NOT NULL,
		preset     TEXT NOT NULL,
		score_json TEXT NOT NULL,   -- Score as JSON
		response   TEXT,
		judge_raw  TEXT,
		latency_ms INTEGER,
		panel_ok   INTEGER,
		panel_n    INTEGER,
		judge_ok   INTEGER,         -- 0/1
		timed_out  INTEGER,
		crashed    INTEGER,
		cost_usd   REAL,
		tokens     INTEGER
	);

	CREATE INDEX IF NOT EXISTS idx_trials_run ON trial_results(run_id);
	CREATE INDEX IF NOT EXISTS idx_trials_variant ON trial_results(variant);
	CREATE INDEX IF NOT EXISTS idx_trials_task ON trial_results(task_id);
	`
	_, err := db.Exec(ddl)
	return err
}

// SaveRun persists a complete run with all trial results.
func (s *Store) SaveRun(meta RunMetadata, variantScores []VariantScore, allTrials []TrialResult) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	fpJSON, _ := json.Marshal(meta.Fingerprint)
	variantsJSON, _ := json.Marshal(meta.Variants)

	_, err = tx.Exec(
		`INSERT INTO runs (id, timestamp, fingerprint, variants, task_count, trials_per, duration_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		meta.ID, meta.Timestamp.Format(time.RFC3339), string(fpJSON), string(variantsJSON),
		meta.TaskCount, meta.TrialsPerTask, meta.Duration.Milliseconds(),
	)
	if err != nil {
		return fmt.Errorf("insert run: %w", err)
	}

	stmt, err := tx.Prepare(
		`INSERT INTO trial_results
		 (run_id, task_id, trial, variant, preset, score_json, response, judge_raw,
		  latency_ms, panel_ok, panel_n, judge_ok, timed_out, crashed, cost_usd, tokens)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, t := range allTrials {
		scoreJSON, _ := json.Marshal(t.Score)
		judgeOk := 0
		if t.JudgeOk {
			judgeOk = 1
		}
		timedOut := 0
		if t.TimedOut {
			timedOut = 1
		}
		crashed := 0
		if t.Crashed {
			crashed = 1
		}

		_, err = stmt.Exec(meta.ID, t.TaskID, t.Trial, t.Variant, t.Preset,
			string(scoreJSON), t.Response, t.JudgeRaw,
			t.LatencyMs, t.PanelOk, t.PanelN, judgeOk, timedOut, crashed,
			t.CostUSD, t.TotalTokens)
		if err != nil {
			return fmt.Errorf("insert trial: %w", err)
		}
	}

	return tx.Commit()
}

// RecentRuns returns the last N run summaries.
func (s *Store) RecentRuns(n int) ([]RunMetadata, error) {
	rows, err := s.db.Query(
		`SELECT id, timestamp, fingerprint, variants, task_count, trials_per, duration_ms
		 FROM runs ORDER BY timestamp DESC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []RunMetadata
	for rows.Next() {
		var (
			id            string
			ts            string
			fpJSON        string
			variantsJSON  string
			taskCount     int
			trialsPer     int
			durationMs    int64
		)
		if err := rows.Scan(&id, &ts, &fpJSON, &variantsJSON, &taskCount, &trialsPer, &durationMs); err != nil {
			return nil, err
		}
		t, _ := time.Parse(time.RFC3339, ts)
		var fp HarnessFingerprint
		json.Unmarshal([]byte(fpJSON), &fp)
		var variants []string
		json.Unmarshal([]byte(variantsJSON), &variants)

		runs = append(runs, RunMetadata{
			ID:            id,
			Timestamp:     t,
			Fingerprint:   fp,
			Variants:      variants,
			TaskCount:     taskCount,
			TrialsPerTask: trialsPer,
			Duration:      time.Duration(durationMs) * time.Millisecond,
		})
	}
	return runs, nil
}

// TrendForVariant returns the score trend for a variant across recent runs.
func (s *Store) TrendForVariant(variant string, limit int) ([]TrendPoint, error) {
	rows, err := s.db.Query(
		`SELECT r.timestamp, AVG(
			CAST(json_extract(t.score_json, '$.accuracy') AS REAL) +
			CAST(json_extract(t.score_json, '$.completeness') AS REAL) +
			CAST(json_extract(t.score_json, '$.clarity') AS REAL) +
			CAST(json_extract(t.score_json, '$.citation_rating') AS REAL)
		) / 4.0 AS avg_score,
		 COUNT(*) AS n
		 FROM trial_results t JOIN runs r ON t.run_id = r.id
		 WHERE t.variant = ?
		 GROUP BY r.id
		 ORDER BY r.timestamp DESC LIMIT ?`, variant, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []TrendPoint
	for rows.Next() {
		var (
			ts  string
			avg float64
			n   int
		)
		if err := rows.Scan(&ts, &avg, &n); err != nil {
			return nil, err
		}
		t, _ := time.Parse(time.RFC3339, ts)
		points = append(points, TrendPoint{Timestamp: t, AvgScore: avg, Trials: n})
	}
	return points, nil
}

// TrendPoint is a single data point in a variant's score trend.
type TrendPoint struct {
	Timestamp time.Time `json:"timestamp"`
	AvgScore  float64   `json:"avg_score"`
	Trials    int       `json:"trials"`
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}
