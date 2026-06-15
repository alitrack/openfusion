// Package cache provides LRU response caching for fusion results.
package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/lhy/openfusion/internal/types"
)

// Entry holds a cached response with expiry.
type Entry struct {
	Response  *types.ChatResponse
	ExpiresAt time.Time
}

// Cache provides TTL-based LRU response caching.
type Cache struct {
	lru   *lru.Cache[string, *Entry]
	ttl   time.Duration
	enabled bool
}

// Config holds cache configuration.
type Config struct {
	Enabled  bool
	MaxSize  int
	TTL      time.Duration
	Presets  map[string]time.Duration // per-preset TTL overrides
}

// New creates a new response cache.
func New(cfg Config) (*Cache, error) {
	maxSize := cfg.MaxSize
	if maxSize <= 0 {
		maxSize = 1000
	}

	l, err := lru.New[string, *Entry](maxSize)
	if err != nil {
		return nil, fmt.Errorf("create lru cache: %w", err)
	}

	return &Cache{
		lru:     l,
		ttl:     cfg.TTL,
		enabled: cfg.Enabled,
	}, nil
}

// Key generates a cache key from preset name and messages.
func Key(preset string, messages []types.ChatMessage) string {
	data, _ := json.Marshal(struct {
		Preset   string
		Messages []types.ChatMessage
	}{Preset: preset, Messages: messages})
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%s:%x", preset, hash[:16])
}

// Get retrieves a cached response. Returns nil on miss or expiry.
func (c *Cache) Get(key string) *types.ChatResponse {
	if !c.enabled {
		return nil
	}

	entry, ok := c.lru.Get(key)
	if !ok {
		return nil
	}

	if time.Now().After(entry.ExpiresAt) {
		c.lru.Remove(key)
		return nil
	}

	return entry.Response
}

// Set stores a response in the cache.
func (c *Cache) Set(key string, resp *types.ChatResponse, presetTTL ...time.Duration) {
	if !c.enabled {
		return
	}

	ttl := c.ttl
	if len(presetTTL) > 0 && presetTTL[0] > 0 {
		ttl = presetTTL[0]
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	c.lru.Add(key, &Entry{
		Response:  resp,
		ExpiresAt: time.Now().Add(ttl),
	})
}

// Enabled returns whether caching is active.
func (c *Cache) Enabled() bool {
	return c.enabled
}

// Len returns the number of entries currently cached.
func (c *Cache) Len() int {
	return c.lru.Len()
}
