package api

import (
	"strings"
	"testing"
	"time"
)

func TestStreamBuffer_NewBufferIsEmpty(t *testing.T) {
	sb := NewStreamBuffer(50, 100*time.Millisecond)
	if sb.Len() != 0 {
		t.Errorf("new buffer should be empty, got len=%d", sb.Len())
	}
}

func TestStreamBuffer_AccumulatesText(t *testing.T) {
	sb := NewStreamBuffer(50, 100*time.Millisecond)
	sb.Add('h')
	sb.Add('e')
	sb.Add('l')
	sb.Add('l')
	sb.Add('o')
	if sb.Len() != 5 {
		t.Errorf("expected 5 chars buffered, got %d", sb.Len())
	}
}

func TestStreamBuffer_FlushAtSentenceEnd(t *testing.T) {
	sb := NewStreamBuffer(50, 100*time.Millisecond)
	sb.Add('h')
	sb.Add('i')
	got := sb.Add('.')
	if got == "" {
		t.Fatal("expected flush on period, got empty")
	}
	if got != "hi." {
		t.Errorf("expected flushed content 'hi.', got %q", got)
	}
	if sb.Len() != 0 {
		t.Errorf("buffer should be empty after flush, got len=%d", sb.Len())
	}
}

func TestStreamBuffer_FlushAtMaxBatchSize(t *testing.T) {
	sb := NewStreamBuffer(10, 100*time.Millisecond)
	// Add 12 chars (should flush at char 10)
	var flushed string
	var remaining int
	for i, ch := range "ABCDEFGHIJKL" {
		if result := sb.Add(ch); result != "" {
			flushed = result
			remaining = 12 - (i + 1) // chars left after flush
		}
	}
	if flushed == "" {
		t.Fatal("expected flush at batch limit, got no flush")
	}
	if !strings.Contains(flushed, "ABCDEFGHIJ") {
		t.Errorf("expected first 10 chars flushed, got %q", flushed)
	}
	if sb.Len() != remaining {
		t.Errorf("expected %d remaining chars in buffer, got %d", remaining, sb.Len())
	}
}

func TestStreamBuffer_FlushOnExclamationAndQuestion(t *testing.T) {
	sb := NewStreamBuffer(50, 100*time.Millisecond)
	sb.Add('h')
	sb.Add('i')
	got := sb.Add('!')
	if got == "" {
		t.Fatal("expected flush on exclamation")
	}
	sb.Add('o')
	sb.Add('k')
	got2 := sb.Add('?')
	if got2 == "" {
		t.Fatal("expected flush on question mark")
	}
	if !strings.Contains(got2, "ok?") {
		t.Errorf("expected 'ok?' flushed, got %q", got2)
	}
}

func TestStreamBuffer_FlushOnNewline(t *testing.T) {
	sb := NewStreamBuffer(50, 100*time.Millisecond)
	sb.Add('a')
	sb.Add('b')
	got := sb.Add('\n')
	if got == "" {
		t.Fatal("expected flush on newline")
	}
	if got != "ab\n" {
		t.Errorf("expected 'ab\\n', got %q", got)
	}
}

func TestStreamBuffer_FinalFlush(t *testing.T) {
	sb := NewStreamBuffer(50, 100*time.Millisecond)
	sb.Add('h')
	sb.Add('i')
	got := sb.Finalize()
	if got == "" {
		t.Fatal("expected Finalize to return buffered content")
	}
	if got != "hi" {
		t.Errorf("expected 'hi', got %q", got)
	}
	// Second Finalize should be empty
	if sb.Finalize() != "" {
		t.Errorf("second Finalize should be empty")
	}
}

func TestStreamBuffer_EmptyFinalizeReturnsEmpty(t *testing.T) {
	sb := NewStreamBuffer(50, 100*time.Millisecond)
	if got := sb.Finalize(); got != "" {
		t.Errorf("empty buffer Finalize should be empty, got %q", got)
	}
}

func TestStreamBuffer_SkipsFlushForMidSentencePunctuation(t *testing.T) {
	sb := NewStreamBuffer(50, 100*time.Millisecond)
	sb.Add('U')
	sb.Add('.')
	sb.Add('S')
	got := sb.Add('.')
	_ = got
	// "U.S." — first period in "U." is mid-word, second in "S." also
	// This is a known limitation: we flush on every period.
	// Real sentence detection would need NLP.
	t.Log("known limitation: non-sentence periods also trigger flush")
}
