package auth

import (
	"sync"
	"time"
)

// Window counts events per key over a sliding period, for rate limits and
// failed sign-in lockouts.
type Window struct {
	Max int
	Per time.Duration

	mu   sync.Mutex
	hits map[string][]time.Time
}

// Full reports whether key has reached Max within the period.
func (w *Window) Full(key string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.prune(key, time.Now())) >= w.Max
}

// Add records an event for key.
func (w *Window) Add(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	w.hits[key] = append(w.prune(key, now), now)
}

// Allow records an event for key unless it's already full.
func (w *Window) Allow(key string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	ts := w.prune(key, now)
	if len(ts) >= w.Max {
		return false
	}
	w.hits[key] = append(ts, now)
	return true
}

// Reset forgets key's events.
func (w *Window) Reset(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.hits, key)
}

// Until returns when key drops below Max, or the zero time if it isn't full.
func (w *Window) Until(key string) time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	ts := w.prune(key, time.Now())
	if len(ts) < w.Max {
		return time.Time{}
	}
	return ts[len(ts)-w.Max].Add(w.Per)
}

func (w *Window) prune(key string, now time.Time) []time.Time {
	if w.hits == nil {
		w.hits = map[string][]time.Time{}
	}
	// Keys nobody comes back for are dropped as the map grows.
	if len(w.hits) > 10000 {
		for k, ts := range w.hits {
			if len(ts) == 0 || now.Sub(ts[len(ts)-1]) > w.Per {
				delete(w.hits, k)
			}
		}
	}
	ts := w.hits[key]
	i := 0
	for i < len(ts) && now.Sub(ts[i]) > w.Per {
		i++
	}
	ts = ts[i:]
	if len(ts) == 0 {
		delete(w.hits, key)
		return nil
	}
	w.hits[key] = ts
	return ts
}
