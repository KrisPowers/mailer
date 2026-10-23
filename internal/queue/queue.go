// Package queue persists outbound messages and retries delivery.
package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mailer/internal/smtpclient"
	"mailer/internal/users"
)

const (
	StatusPending = "pending"
	StatusSent    = "sent"
	StatusFailed  = "failed"

	maxAge = 4 * 24 * time.Hour
)

var backoff = []time.Duration{
	5 * time.Minute, 10 * time.Minute, 30 * time.Minute, time.Hour,
	2 * time.Hour, 4 * time.Hour, 8 * time.Hour,
}

type Rcpt struct {
	Addr      string `json:"addr"`
	Status    string `json:"status"`
	LastError string `json:"last_error,omitempty"`
}

type Item struct {
	ID       string    `json:"id"`
	From     string    `json:"from"`
	Rcpts    []*Rcpt   `json:"rcpts"`
	Created  time.Time `json:"created"`
	NextTry  time.Time `json:"next_try"`
	Attempts int       `json:"attempts"`
}

// BounceFunc is called with recipients that permanently failed.
type BounceFunc func(item *Item, failed []*Rcpt, original []byte)

type Queue struct {
	dir    string
	client *smtpclient.Client
	bounce BounceFunc
	log    *log.Logger

	mu     sync.Mutex
	active map[string]bool
	wake   chan struct{}
}

func New(dir string, client *smtpclient.Client, bounce BounceFunc, logger *log.Logger) (*Queue, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Queue{dir: dir, client: client, bounce: bounce, log: logger,
		active: map[string]bool{}, wake: make(chan struct{}, 1)}, nil
}

// Enqueue stores a message for delivery and triggers an immediate attempt.
func (q *Queue) Enqueue(from string, rcpts []string, data []byte) error {
	id := make([]byte, 8)
	rand.Read(id)
	it := &Item{ID: time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(id),
		From: from, Created: time.Now(), NextTry: time.Now()}
	for _, r := range rcpts {
		it.Rcpts = append(it.Rcpts, &Rcpt{Addr: r, Status: StatusPending})
	}
	if err := users.WriteFileAtomic(q.path(it.ID, ".eml"), data, 0o600); err != nil {
		return err
	}
	if err := q.save(it); err != nil {
		os.Remove(q.path(it.ID, ".eml"))
		return err
	}
	q.Wake()
	return nil
}

func (q *Queue) Wake() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *Queue) path(id, ext string) string { return filepath.Join(q.dir, id+ext) }

func (q *Queue) save(it *Item) error {
	b, err := json.MarshalIndent(it, "", "  ")
	if err != nil {
		return err
	}
	return users.WriteFileAtomic(q.path(it.ID, ".json"), b, 0o600)
}

// List returns all queued items (for the CLI).
func (q *Queue) List() ([]*Item, error) {
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		return nil, err
	}
	var items []*Item
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(q.dir, e.Name()))
		if err != nil {
			continue
		}
		it := &Item{}
		if json.Unmarshal(b, it) == nil {
			items = append(items, it)
		}
	}
	return items, nil
}

// RetryAll makes every queued message due now and wakes the queue.
func (q *Queue) RetryAll() error {
	items, err := q.List()
	if err != nil {
		return err
	}
	for _, it := range items {
		it.NextTry = time.Now()
		if err := q.save(it); err != nil {
			return err
		}
	}
	q.Wake()
	return nil
}

// Run processes the queue until ctx is cancelled.
func (q *Queue) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		q.runDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-q.wake:
		}
	}
}

func (q *Queue) runDue(ctx context.Context) {
	items, err := q.List()
	if err != nil {
		q.log.Printf("queue: %v", err)
		return
	}
	now := time.Now()
	for _, it := range items {
		if it.NextTry.After(now) {
			continue
		}
		q.mu.Lock()
		busy := q.active[it.ID]
		q.active[it.ID] = true
		q.mu.Unlock()
		if busy {
			continue
		}
		go func(it *Item) {
			defer func() {
				q.mu.Lock()
				delete(q.active, it.ID)
				q.mu.Unlock()
			}()
			q.attempt(ctx, it)
		}(it)
	}
}

func (q *Queue) attempt(ctx context.Context, it *Item) {
	data, err := os.ReadFile(q.path(it.ID, ".eml"))
	if err != nil {
		q.log.Printf("queue: %s: %v; dropping", it.ID, err)
		os.Remove(q.path(it.ID, ".json"))
		return
	}

	byDomain := map[string][]*Rcpt{}
	for _, r := range it.Rcpts {
		if r.Status == StatusPending {
			d := strings.ToLower(r.Addr[strings.LastIndexByte(r.Addr, '@')+1:])
			byDomain[d] = append(byDomain[d], r)
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var newlyFailed []*Rcpt
	for domain, rs := range byDomain {
		wg.Add(1)
		go func(domain string, rs []*Rcpt) {
			defer wg.Done()
			addrs := make([]string, len(rs))
			for i, r := range rs {
				addrs[i] = r.Addr
			}
			dctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
			defer cancel()
			results := q.client.Deliver(dctx, domain, it.From, addrs, data)
			mu.Lock()
			defer mu.Unlock()
			for i, res := range results {
				r := rs[i]
				switch {
				case res.Err == nil:
					r.Status, r.LastError = StatusSent, ""
					q.log.Printf("queue: %s delivered to %s", it.ID, r.Addr)
				case res.Permanent:
					r.Status, r.LastError = StatusFailed, res.Err.Error()
					newlyFailed = append(newlyFailed, r)
					q.log.Printf("queue: %s permanently failed for %s: %v", it.ID, r.Addr, res.Err)
				default:
					r.LastError = res.Err.Error()
					q.log.Printf("queue: %s deferred for %s: %v", it.ID, r.Addr, res.Err)
				}
			}
		}(domain, rs)
	}
	wg.Wait()

	it.Attempts++
	pending := 0
	for _, r := range it.Rcpts {
		if r.Status == StatusPending {
			pending++
		}
	}
	if pending > 0 && time.Since(it.Created) > maxAge {
		for _, r := range it.Rcpts {
			if r.Status == StatusPending {
				r.Status = StatusFailed
				r.LastError = "gave up after " + maxAge.String() + ": " + r.LastError
				newlyFailed = append(newlyFailed, r)
			}
		}
		pending = 0
	}
	if len(newlyFailed) > 0 && q.bounce != nil {
		q.bounce(it, newlyFailed, data)
	}
	if pending == 0 {
		os.Remove(q.path(it.ID, ".json"))
		os.Remove(q.path(it.ID, ".eml"))
		return
	}
	it.NextTry = time.Now().Add(backoff[min(it.Attempts-1, len(backoff)-1)])
	if err := q.save(it); err != nil {
		q.log.Printf("queue: saving %s: %v", it.ID, err)
	}
}
