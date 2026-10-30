package agent

import (
	"strings"
	"time"
)

// flushInterval is the delta batching window (architecture/02: 30–50ms).
const flushInterval = 40 * time.Millisecond

// deltaBatcher coalesces streamed text deltas so the frontend receives one
// event per flush window instead of one per token. Chunks arriving faster
// than the window accumulate; a chunk arriving after the window flushes the
// buffer first. flush() is also called at tool boundaries and stream end, so
// no text is ever lost.
type deltaBatcher struct {
	emit func(joined string)
	buf  strings.Builder
	last time.Time
}

func newDeltaBatcher(emit func(string)) *deltaBatcher {
	return &deltaBatcher{emit: emit, last: time.Now()}
}

func (b *deltaBatcher) add(delta string) {
	if delta == "" {
		return
	}
	if b.buf.Len() > 0 && time.Since(b.last) >= flushInterval {
		b.flush()
	}
	b.buf.WriteString(delta)
}

// flush emits whatever is buffered (used at windows, boundaries and stream end).
func (b *deltaBatcher) flush() {
	if b.buf.Len() == 0 {
		return
	}
	b.emit(b.buf.String())
	b.buf.Reset()
	b.last = time.Now()
}
