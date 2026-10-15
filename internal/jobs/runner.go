package jobs

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"sync"
	"time"

	"shiliu-rss/internal/app"
	"shiliu-rss/internal/fetch"
	"shiliu-rss/internal/importer"
)

type Runner struct {
	App         *app.Service
	Client      *fetch.Client
	BackupDir   string
	wake        chan struct{}
	mu          sync.Mutex
	last        time.Time
	lastCleanup *time.Time
	nextCleanup time.Time
	healthy     bool
	hosts       map[string]*sync.Mutex
	extract     sync.Mutex
}

func New(a *app.Service, c *fetch.Client) *Runner {
	return &Runner{App: a, Client: c, wake: make(chan struct{}, 1), hosts: map[string]*sync.Mutex{}, nextCleanup: a.Now()}
}
func (r *Runner) Wake() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}
func (r *Runner) Status() (time.Time, *time.Time, time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last, r.lastCleanup, r.nextCleanup, r.healthy && !r.last.IsZero() && r.App.Now().Sub(r.last) < 6*time.Minute
}
func (r *Runner) Run(ctx context.Context) {
	var wg sync.WaitGroup
	defer wg.Wait()
	slots := make(chan struct{}, 2)
	for ctx.Err() == nil {
		r.mu.Lock()
		r.last = r.App.Now()
		cleanup := !r.last.Before(r.nextCleanup)
		r.mu.Unlock()
		e := r.App.ScheduleDue(ctx)
		if e == nil && cleanup {
			for {
				var n int
				n, e = r.App.Cleanup(ctx, 100)
				if e != nil || n < 100 {
					break
				}
				if ctx.Err() != nil {
					break
				}
			}
			if e == nil {
				now := r.App.Now()
				r.mu.Lock()
				r.lastCleanup = &now
				r.nextCleanup = now.Add(24 * time.Hour)
				r.mu.Unlock()
				if r.BackupDir != "" {
					if _, err := importer.Backup(ctx, r.App, r.BackupDir, now); err != nil {
						slog.Error("backup_failed")
					}
				}
			}
		}
		r.mu.Lock()
		r.healthy = e == nil
		r.mu.Unlock()
		if e != nil && ctx.Err() == nil {
			slog.Error("scheduler_failed")
		}
		if e == nil {
			for len(slots) < cap(slots) {
				job, err := r.App.ClaimJob(ctx, rand.Text(), 2*time.Minute)
				if errors.Is(err, app.ErrNotFound) {
					break
				}
				if err != nil {
					slog.Error("claim_failed")
					break
				}
				slots <- struct{}{}
				wg.Add(1)
				go func(j app.Job) { defer wg.Done(); defer func() { <-slots; r.Wake() }(); r.Execute(ctx, j) }(job)
			}
		}
		due, err := r.App.NextJobAt(ctx)
		wait := 5 * time.Minute
		if err != nil {
			wait = 30 * time.Second
		} else if !due.IsZero() {
			wait = min(wait, max(time.Until(due), 100*time.Millisecond))
		}
		if len(slots) == cap(slots) {
			wait = 5 * time.Minute
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-r.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
	r.mu.Lock()
	r.healthy = false
	r.mu.Unlock()
}
func (r *Runner) Execute(ctx context.Context, j app.Job) {
	var f app.Feed
	var a app.Article
	var e error
	if j.Kind == "feed" {
		f, e = r.App.GetFeed(ctx, j.TargetID)
	} else {
		a, e = r.App.GetArticle(ctx, j.TargetID)
		if e == nil {
			f, e = r.App.GetFeed(ctx, a.FeedID)
		}
	}
	if e != nil {
		_ = r.App.FailJob(ctx, j.ID, j.LeaseOwner, "target_unavailable", nil)
		return
	}
	target := f.URL
	if j.Kind != "feed" {
		target = a.URL
	}
	host := fetch.HostKey(target)
	r.mu.Lock()
	lock := r.hosts[host]
	if lock == nil {
		lock = &sync.Mutex{}
		r.hosts[host] = lock
	}
	r.mu.Unlock()
	if !lock.TryLock() {
		_ = r.App.DeferJob(ctx, j.ID, j.LeaseOwner, r.App.Now().Add(30*time.Second))
		return
	}
	defer lock.Unlock()
	if j.Kind == "feed" {
		var result app.FeedResult
		result, e = r.Client.Feed(ctx, f)
		if e == nil {
			e = r.App.CompleteFeedJob(ctx, j.ID, j.LeaseOwner, result)
		}
	} else {
		if !r.extract.TryLock() {
			_ = r.App.DeferJob(ctx, j.ID, j.LeaseOwner, r.App.Now().Add(30*time.Second))
			return
		}
		var result app.FulltextResult
		result, e = r.Client.Fulltext(ctx, a, f.AllowHost)
		r.extract.Unlock()
		if e == nil {
			e = r.App.CompleteFulltextJob(ctx, j.ID, j.LeaseOwner, result)
		}
	}
	if e != nil && ctx.Err() == nil {
		code := "processing_failed"
		var retry *time.Time
		var fe *fetch.Error
		if errors.As(e, &fe) {
			code = fe.Code
			retry = fe.RetryAfter
		}
		if errors.Is(e, app.ErrLease) {
			return
		}
		if err := r.App.FailJob(ctx, j.ID, j.LeaseOwner, code, retry); err != nil {
			slog.Error("job_commit_failed", "job_id", j.ID)
		}
	}
}
