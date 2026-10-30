package permission

import (
	"sync"
	"time"
)

// Quota is the DIM4 limit on an auto-allow row. Ask/deny decisions never
// consume quota: only verdicts that would execute without a new dialog count.
type Quota struct {
	Limit    int
	Window   time.Duration
	OnExceed string // ask (default) or deny
}

// quotaTracker keeps a per-session sliding window of auto-allows.
type quotaTracker struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newQuotaTracker() *quotaTracker {
	return &quotaTracker{hits: map[string][]time.Time{}}
}

// allow records one auto-allow and reports whether the limit was exceeded.
// Rejected calls are not recorded, so the window drains naturally.
func (q *quotaTracker) allow(key string, quota Quota, now time.Time) bool {
	if quota.Limit <= 0 || quota.Window <= 0 {
		return true
	}
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.Add(-quota.Window)
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := q.hits[key][:0]
	for _, hit := range q.hits[key] {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}
	if len(kept) >= quota.Limit {
		q.hits[key] = kept
		return false
	}
	q.hits[key] = append(kept, now)
	return true
}

func quotaKey(req Request) string {
	return req.SessionID + "|" + req.ToolRef + "|" + req.Action + "|" + req.MatchKey
}
