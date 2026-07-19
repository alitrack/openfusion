package fusion

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// PreCompressor compresses panel model responses before synthesis.
// Uses deterministic strategies — no LLM call, sub-millisecond overhead.
type PreCompressor struct {
	mu         sync.RWMutex
	dedupCache map[string]string
}

// NewPreCompressor creates a pre-compressor with an empty dedup cache.
func NewPreCompressor() *PreCompressor {
	return &PreCompressor{
		dedupCache: make(map[string]string),
	}
}

// Compress applies compression to a panel response. Errors pass through unmodified.
func (p *PreCompressor) Compress(content string) string {
	// Error/empty passthrough
	if content == "" || isErrorLike(content) {
		return content
	}

	// Content-addressed dedup
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	p.mu.RLock()
	_, ok := p.dedupCache[hash]
	p.mu.RUnlock()
	if ok {
		return fmt.Sprintf("[unchanged — same output as §%s§]", hash[:8])
	}

	// Structural collapse
	compressed := p.structuralCollapse(content)

	p.mu.Lock()
	p.dedupCache[hash] = compressed
	p.mu.Unlock()

	if compressed != content {
		return fmt.Sprintf("[of-compressed: %d→%d chars]\n%s",
			len(content), len(compressed), compressed)
	}
	return content
}

func (p *PreCompressor) structuralCollapse(text string) string {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "[") && len(trimmed) > 100 {
		return p.collapseJSON(text)
	}
	if strings.HasPrefix(trimmed, "{") && len(trimmed) > 200 {
		return p.collapseJSONObject(text)
	}
	return text
}

func (p *PreCompressor) collapseJSON(text string) string {
	var arr []interface{}
	if err := json.Unmarshal([]byte(text), &arr); err != nil || len(arr) <= 3 {
		return text
	}
	first2, _ := json.Marshal(arr[:2])
	return fmt.Sprintf("%s\n  …and %d more items", strings.TrimRight(string(first2), "]"), len(arr)-2)
}

func (p *PreCompressor) collapseJSONObject(text string) string {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(text), &obj); err != nil || len(obj) <= 10 {
		return text
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	shown := keys
	if len(keys) > 5 {
		shown = keys[:5]
	}
	return fmt.Sprintf("{\n  // …%d keys total, showing first 5\n  %s\n  …and %d more keys\n}",
		len(obj), strings.Join(shown, ", "), len(obj)-5)
}

func isErrorLike(text string) bool {
	lower := strings.ToLower(text)
	sigs := []string{"error:", "exception:", "panic:", "fatal:", "[error]", "timeout"}
	for _, sig := range sigs {
		if strings.HasPrefix(strings.TrimSpace(lower), sig) {
			return true
		}
	}
	return false
}
