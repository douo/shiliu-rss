package jobs

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"shiliu-rss/internal/app"
	"shiliu-rss/internal/fetch"
	"shiliu-rss/internal/store"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
func TestBackgroundRecoveryConditionalDedupAndExplicitFulltext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	now := func() time.Time { return time.Unix(0, clock.Load()).UTC() }
	db, e := store.Open(ctx, filepath.Join(t.TempDir(), "reader.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	a := app.New(db, now)
	var attempts, full, conditional atomic.Int64
	var source *httptest.Server
	source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/article" {
			full.Add(1)
			fmt.Fprint(w, `<html><head><title>文章</title></head><body><article><h1>文章</h1><p>这是一段用于测试的长正文，只有明确请求时才可以取得。阅读器需要提取并保留这段文字作为资料。</p><p>这是一段用于测试的长正文，只有明确请求时才可以取得。阅读器需要提取并保留这段文字作为资料。</p></article></body></html>`)
			return
		}
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(503)
			return
		}
		if r.Header.Get("If-None-Match") == `"stable"` {
			conditional.Add(1)
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", `"stable"`)
		fmt.Fprintf(w, `<rss version="2.0"><channel><title>自动更新</title><link>%s</link><item><guid>stable-id</guid><title>音乐测试</title><link>%s/article</link><description>仅有摘要</description></item></channel></rss>`, source.URL, source.URL)
	}))
	defer source.Close()
	f, e := a.CreateFeed(ctx, app.Feed{URL: source.URL, Title: "测试", AllowHost: "127.0.0.1"})
	if e != nil {
		t.Fatal(e)
	}
	r := New(a, fetch.New())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	waitFor(t, func() bool { current, _ := a.GetFeed(ctx, f.ID); return current.Error == "http_503" })
	failed, _ := a.GetFeed(ctx, f.ID)
	if failed.Paused || failed.LastSuccessAt != nil || failed.NextCheckAt.Before(now().Add(59*time.Minute)) {
		t.Fatalf("bad failure state %+v", failed)
	}
	clock.Add(int64(61 * time.Minute))
	r.Wake()
	waitFor(t, func() bool { page, _ := a.ListArticles(ctx, app.ArticleQuery{}); return len(page.Items) == 1 })
	page, _ := a.ListArticles(ctx, app.ArticleQuery{})
	articleID := page.Items[0].ID
	if full.Load() != 0 {
		t.Fatal("automatic fulltext fetch")
	}
	good, _ := a.GetFeed(ctx, f.ID)
	if good.Error != "" || good.LastSuccessAt == nil {
		t.Fatal("did not recover")
	}
	delay := good.NextCheckAt.Sub(now())
	if delay < 4*time.Hour || delay > 6*time.Hour {
		t.Fatal("default interval", delay)
	}
	_, e = a.QueueFeed(ctx, f.ID)
	if e != nil {
		t.Fatal(e)
	}
	r.Wake()
	waitFor(t, func() bool { return conditional.Load() == 1 })
	page, _ = a.ListArticles(ctx, app.ArticleQuery{})
	if len(page.Items) != 1 || page.Items[0].ID != articleID {
		t.Fatal("duplicate article")
	}
	job, e := a.QueueFulltext(ctx, articleID)
	if e != nil {
		t.Fatal(e)
	}
	r.Wake()
	waitFor(t, func() bool { j, _ := a.GetJob(ctx, job.ID); return j.State == "succeeded" })
	article, e := a.GetArticle(ctx, articleID)
	if e != nil || len(article.Content) != 2 || full.Load() != 1 {
		t.Fatal(article, e)
	}
}
func TestSiteGateDefersWithoutUsingAttempt(t *testing.T) {
	ctx := context.Background()
	db, e := store.Open(ctx, filepath.Join(t.TempDir(), "reader.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	a := app.New(db)
	f, e := a.CreateFeed(ctx, app.Feed{URL: "https://example.org/feed", Title: "gate"})
	if e != nil {
		t.Fatal(e)
	}
	_, _ = a.QueueFeed(ctx, f.ID)
	j, e := a.ClaimJob(ctx, "test", 2*time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	r := New(a, fetch.New())
	lock := new(sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	r.hosts["example.org"] = lock
	r.Execute(ctx, j)
	j, e = a.GetJob(ctx, j.ID)
	if e != nil || j.State != "queued" || j.Attempts != 0 {
		t.Fatal(j, e)
	}
}

func TestManualFulltextCannotBypassPersistedHostCooldown(t *testing.T) {
	ctx := context.Background()
	db, e := store.Open(ctx, filepath.Join(t.TempDir(), "reader.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	a := app.New(db)
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(429)
	}))
	defer srv.Close()
	f, e := a.CreateFeed(ctx, app.Feed{Title: "limited", URL: srv.URL, AllowHost: "127.0.0.1"})
	if e != nil {
		t.Fatal(e)
	}
	ids, e := a.IngestFeed(ctx, f.ID, []app.FeedEntry{{GUID: "a", Title: "a", URL: srv.URL + "/article", HTML: "<p>Existing summary</p>"}})
	if e != nil {
		t.Fatal(e)
	}
	_, _ = a.QueueFeed(ctx, f.ID)
	j, e := a.ClaimJob(ctx, "first", 2*time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	runner := New(a, fetch.New())
	runner.Execute(ctx, j)
	until, e := a.HostCooldown(ctx, "127.0.0.1")
	if e != nil || time.Until(until) < 59*time.Minute {
		t.Fatal(until, e)
	}
	full, e := a.QueueFulltext(ctx, ids[0])
	if e != nil {
		t.Fatal(e)
	}
	j, e = a.ClaimJob(ctx, "manual", 2*time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	runner = New(a, fetch.New())
	runner.Execute(ctx, j)
	got, e := a.GetJob(ctx, full.ID)
	if e != nil || got.State != "queued" || got.Attempts != 0 || got.DueAt.Before(until) || calls.Load() != 1 {
		t.Fatalf("cooldown bypass %+v calls=%d err=%v", got, calls.Load(), e)
	}
	article, e := a.GetArticle(ctx, ids[0])
	if e != nil || article.Content[0].State != "available" {
		t.Fatal("existing summary overwritten", e)
	}
}
