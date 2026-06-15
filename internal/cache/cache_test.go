package cache

import (
	"testing"
	"time"

	"github.com/lhy/openfusion/internal/types"
)

func TestDisabled(t *testing.T) {
	c, err := New(Config{Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	key := Key("budget", []types.ChatMessage{{Role: "user", Content: "hello"}})
	if got := c.Get(key); got != nil {
		t.Fatal("disabled cache should return nil")
	}
	c.Set(key, &types.ChatResponse{ID: "test"})
	if got := c.Get(key); got != nil {
		t.Fatal("disabled cache should not store")
	}
}

func TestSetAndGet(t *testing.T) {
	c, err := New(Config{Enabled: true, MaxSize: 100, TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}

	key := Key("budget", []types.ChatMessage{{Role: "user", Content: "hello"}})
	resp := &types.ChatResponse{ID: "test123", Model: "budget"}

	c.Set(key, resp)
	got := c.Get(key)
	if got == nil {
		t.Fatal("expected cached response")
	}
	if got.ID != "test123" {
		t.Fatalf("got ID %q, want %q", got.ID, "test123")
	}
}

func TestCacheKeyUnique(t *testing.T) {
	k1 := Key("budget", []types.ChatMessage{{Role: "user", Content: "hello"}})
	k2 := Key("budget", []types.ChatMessage{{Role: "user", Content: "world"}})
	k3 := Key("frontier", []types.ChatMessage{{Role: "user", Content: "hello"}})

	if k1 == k2 {
		t.Fatal("different messages should produce different keys")
	}
	if k1 == k3 {
		t.Fatal("different presets should produce different keys")
	}
}

func TestTTLExpiry(t *testing.T) {
	c, err := New(Config{Enabled: true, MaxSize: 100, TTL: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	key := Key("budget", []types.ChatMessage{{Role: "user", Content: "hello"}})
	c.Set(key, &types.ChatResponse{ID: "test"})

	if got := c.Get(key); got == nil {
		t.Fatal("expected cache hit before TTL")
	}

	time.Sleep(150 * time.Millisecond)
	if got := c.Get(key); got != nil {
		t.Fatal("expected cache miss after TTL")
	}
}

func TestLRUEviction(t *testing.T) {
	c, err := New(Config{Enabled: true, MaxSize: 2, TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}

	c.Set("k1", &types.ChatResponse{ID: "1"})
	c.Set("k2", &types.ChatResponse{ID: "2"})
	c.Set("k3", &types.ChatResponse{ID: "3"}) // k1 should be evicted

	if got := c.Get("k1"); got != nil {
		t.Fatal("expected k1 evicted (LRU)")
	}
	if got := c.Get("k2"); got == nil {
		t.Fatal("expected k2 still in cache")
	}
}

func TestEnabled(t *testing.T) {
	c, err := New(Config{Enabled: true, MaxSize: 10, TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if !c.Enabled() {
		t.Fatal("expected enabled")
	}

	c2, _ := New(Config{Enabled: false})
	if c2.Enabled() {
		t.Fatal("expected disabled")
	}
}
