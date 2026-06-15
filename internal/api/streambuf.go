package api

import (
	"strings"
	"time"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// StreamBuffer — buffered SSE streaming with smart flush boundaries
// ---------------------------------------------------------------------------

// sentenceEnd is the set of characters that trigger a stream buffer flush.
var sentenceEnd = []rune{'.', '!', '?', '\n'}

func isSentenceEnd(ch rune) bool {
	for _, s := range sentenceEnd {
		if ch == s {
			return true
		}
	}
	return false
}

// StreamBuffer accumulates text and flushes at sentence boundaries or max batch size.
type StreamBuffer struct {
	buf             strings.Builder
	maxBatchSize    int
	maxFlushDelay   time.Duration
}

// NewStreamBuffer creates a stream buffer.
// maxBatchSize: max characters before forced flush.
// maxFlushDelay: max time before forced flush (checked externally via ShouldFlush).
func NewStreamBuffer(maxBatchSize int, maxFlushDelay time.Duration) *StreamBuffer {
	if maxBatchSize <= 0 {
		maxBatchSize = 50
	}
	if maxFlushDelay <= 0 {
		maxFlushDelay = 50 * time.Millisecond
	}
	return &StreamBuffer{
		maxBatchSize:  maxBatchSize,
		maxFlushDelay: maxFlushDelay,
	}
}

// Add appends a rune and returns flushed content if a flush boundary is hit.
// Returns empty string if no flush needed.
func (sb *StreamBuffer) Add(ch rune) string {
	sb.buf.WriteRune(ch)

	// Check for sentence-ending punctuation
	if isSentenceEnd(ch) {
		return sb.Finalize()
	}

	// Check batch size
	if utf8.RuneCountInString(sb.buf.String()) >= sb.maxBatchSize {
		return sb.Finalize()
	}

	return ""
}

// Finalize returns all accumulated content and resets the buffer.
func (sb *StreamBuffer) Finalize() string {
	if sb.buf.Len() == 0 {
		return ""
	}
	content := sb.buf.String()
	sb.buf.Reset()
	return content
}

// Len returns the number of buffered characters.
func (sb *StreamBuffer) Len() int {
	return utf8.RuneCountInString(sb.buf.String())
}

// ShouldFlush returns true if the buffer has content and the max delay has elapsed.
func (sb *StreamBuffer) ShouldFlush(lastFlush time.Time) bool {
	return sb.buf.Len() > 0 && time.Since(lastFlush) >= sb.maxFlushDelay
}
