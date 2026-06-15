package codex

import (
	"testing"
)

func TestExtract_PythonCode(t *testing.T) {
	answer := `## Final Answer

Here's an LRU Cache implementation in Python:

` + "```python\n" +
`from collections import OrderedDict
from threading import Lock

class LRUCache:
    def __init__(self, capacity: int):
        self.cache = OrderedDict()
        self.lock = Lock()
        self.capacity = capacity

    def get(self, key: int) -> int:
        with self.lock:
            if key not in self.cache:
                return -1
            self.cache.move_to_end(key)
            return self.cache[key]

    def put(self, key: int, value: int) -> None:
        with self.lock:
            if key in self.cache:
                self.cache.move_to_end(key)
            self.cache[key] = value
            if len(self.cache) > self.capacity:
                self.cache.popitem(last=False)
` + "```\n\n" +
`And here's a test:

` + "```python\n" +
`def test_lru_cache():
    cache = LRUCache(2)
    cache.put(1, 1)
    cache.put(2, 2)
    assert cache.get(1) == 1
    cache.put(3, 3)
    assert cache.get(2) == -1
` + "```\n\n" +
`The implementation uses OrderedDict for O(1) operations and a Lock for thread safety.`

	cx := Extract(answer, 2)

	if cx.Language != "python" {
		t.Errorf("expected language 'python', got %s", cx.Language)
	}
	if len(cx.Files) != 2 {
		t.Errorf("expected 2 files (main + test), got %d", len(cx.Files))
	}
	if cx.Explanation == "" {
		t.Error("expected non-empty explanation")
	}

	hasMain := false
	hasTest := false
	for _, f := range cx.Files {
		if f.Path == "main.py" {
			hasMain = true
			if !contains(f.Content, "class LRUCache") {
				t.Error("main file missing LRUCache class")
			}
		}
		if f.Path == "main_test.py" {
			hasTest = true
			if !contains(f.Content, "test_lru_cache") {
				t.Error("test file missing test function")
			}
		}
	}
	if !hasMain {
		t.Error("no main.py found")
	}
	if !hasTest {
		t.Error("no main_test.py found")
	}
}

func TestExtract_GoCode(t *testing.T) {
	answer := `Here's a Go implementation:

` + "```go\n" +
`package main

import "fmt"

func main() {
    fmt.Println("Hello, World!")
}
` + "```\n\n" +
`Test:

` + "```go\n" +
`func TestMain(t *testing.T) {
}
` + "```"

	cx := Extract(answer, 2)

	if cx.Language != "go" {
		t.Errorf("expected language 'go', got %s", cx.Language)
	}
}

func TestExtract_NoCodeBlocks(t *testing.T) {
	answer := `This is just a text explanation with no code blocks.`

	cx := Extract(answer, 1)

	if cx.Language != "text" {
		t.Errorf("expected language 'text', got %s", cx.Language)
	}
	if len(cx.Files) != 0 {
		t.Errorf("expected 0 files, got %d", len(cx.Files))
	}
	if cx.Explanation != answer {
		t.Errorf("expected full answer as explanation")
	}
}

func TestExtract_MultipleLanguages(t *testing.T) {
	answer := `Here's the Python backend:

` + "```python\n" +
`def handler():
    return "hello"
` + "```\n\n" +
`And the SQL:

` + "```sql\n" +
`SELECT * FROM users;
` + "```\n\n" +
`The frontend:

` + "```javascript\n" +
`function hello() { return "hi"; }
` + "```"

	cx := Extract(answer, 3)

	if len(cx.Files) != 3 {
		t.Errorf("expected 3 files, got %d", len(cx.Files))
	}
}

func TestInferLanguage(t *testing.T) {
	tests := []struct {
		code     string
		expected string
	}{
		{`def hello(): pass`, "python"},
		{`import os`, "python"},
		{`func main() {}`, "go"},
		{`package main`, "go"},
		{`fn main() {}`, "rust"},
		{`SELECT * FROM users`, "sql"},
		{`function hello() {}`, "javascript"},
		{`console.log("hi")`, "javascript"},
		{`random text`, "text"},
	}

	for _, tt := range tests {
		got := inferLanguage(tt.code)
		if got != tt.expected {
			t.Errorf("inferLanguage(%q) = %s, want %s", tt.code, got, tt.expected)
		}
	}
}

func TestDetectLanguage(t *testing.T) {
	answer := "```python\nprint('hi')\n```"
	blocks := codeBlockRegex.FindAllStringSubmatch(answer, -1)

	lang := detectLanguage(answer, blocks)
	if lang != "python" {
		t.Errorf("expected 'python', got %s", lang)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
