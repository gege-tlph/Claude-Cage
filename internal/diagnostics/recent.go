package diagnostics

import (
	"sync"
	"time"
)

// The default store's latest events stay in memory for the 操作日志 window's
// advanced view: this run's last hour, bounded by count and size. They are
// the same redacted records the store writes.
const (
	recentAge   = time.Hour
	recentCount = 5000
	recentBytes = 4 << 20
)

type recentEvent struct {
	Event
	at   time.Time
	size int
}

type recentLog struct {
	mu     sync.Mutex
	events []recentEvent
	bytes  int
}

var recent recentLog

var levelRank = map[string]int{"debug": 0, "info": 1, "warn": 2, "error": 3}

func (r *recentLog) add(events []Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, event := range events {
		at, err := time.Parse(time.RFC3339Nano, event.Time)
		if err != nil {
			at = time.Now()
		}
		size := len(event.Message) + len(event.Source) + 64
		r.events = append(r.events, recentEvent{Event: event, at: at, size: size})
		r.bytes += size
	}
	r.trim(time.Now())
}

func (r *recentLog) trim(now time.Time) {
	first := 0
	for first < len(r.events) && (len(r.events)-first > recentCount || r.bytes > recentBytes || now.Sub(r.events[first].at) > recentAge) {
		r.bytes -= r.events[first].size
		first++
	}
	if first > 0 {
		r.events = append([]recentEvent(nil), r.events[first:]...)
	}
}

// Recent returns this run's events of the last hour at minLevel or above,
// oldest first, at most max of the newest. An unknown level means info.
func Recent(minLevel string, max int) []Event {
	rank, ok := levelRank[minLevel]
	if !ok {
		rank = levelRank["info"]
	}
	recent.mu.Lock()
	defer recent.mu.Unlock()
	recent.trim(time.Now())
	var out []Event
	for i := len(recent.events) - 1; i >= 0 && len(out) < max; i-- {
		if levelRank[recent.events[i].Level] >= rank {
			out = append(out, recent.events[i].Event)
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
