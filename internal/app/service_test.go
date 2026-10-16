package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"shiliu-rss/internal/store"
)

func testService(t *testing.T) (*Service, *time.Time) {
	t.Helper()
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	st, e := store.Open(context.Background(), filepath.Join(t.TempDir(), "reader.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, func() time.Time { return clock }), &clock
}
func feedFixture(t *testing.T, s *Service) Feed {
	t.Helper()
	f, e := s.CreateFeed(context.Background(), Feed{URL: "https://example.test/feed", Title: "合成源"})
	if e != nil {
		t.Fatal(e)
	}
	return f
}
func entryFixture(title string) FeedEntry {
	return FeedEntry{GUID: "guid-" + title, URL: "https://example.test/article/" + title, Title: title, HTML: "<p>古典音乐 SQLite</p><script>alert(1)</script>"}
}
func addFixture(t *testing.T, s *Service, f Feed, title string) Article {
	t.Helper()
	ids, e := s.IngestFeed(context.Background(), f.ID, []FeedEntry{entryFixture(title)})
	if e != nil {
		t.Fatal(e)
	}
	a, e := s.GetArticle(context.Background(), ids[0])
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func TestRetentionSearchAndExplicitRefetch(t *testing.T) {
	ctx := context.Background()
	s, clock := testService(t)
	f := feedFixture(t, s)
	a := addFixture(t, s, f, "测试标题")
	if strings.Contains(a.Content[0].HTML, "script") {
		t.Fatal("unsafe HTML")
	}
	note := "保留笔记"
	tags := []string{"标签"}
	a, e := s.UpdateArticle(ctx, a.ID, a.Version, ArticlePatch{Note: &note, Tags: &tags})
	if e != nil {
		t.Fatal(e)
	}
	art, e := s.PutArtifact(ctx, Artifact{ArticleID: a.ID, Actor: "agent", ExternalID: "one", Content: "摘要成果", BasisHash: a.ContentHash})
	if e != nil {
		t.Fatal(e)
	}
	*clock = clock.AddDate(0, 0, 91)
	for _, q := range []struct {
		term string
		want int
	}{{"音乐", 0}, {"测试", 1}, {"笔记", 1}, {"摘要", 1}, {"标签", 1}} {
		page, e := s.ListArticles(ctx, ArticleQuery{Scope: "all", Query: q.term})
		if e != nil {
			t.Fatal(e)
		}
		if len(page.Items) != q.want || page.UnreadCount != int64(q.want) {
			t.Fatalf("search %s: got %d items, %d unread", q.term, len(page.Items), page.UnreadCount)
		}
	}
	a, e = s.GetArticle(ctx, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	if !a.Hidden || a.ReadAt != nil || a.BodyState != "purged" || a.Content[0].HTML != "" {
		t.Fatalf("deadline not enforced: %+v", a)
	}
	if n, e := s.Cleanup(ctx, 100); e != nil || n != 1 {
		t.Fatalf("cleanup %d %v", n, e)
	}
	a = addFixture(t, s, f, "测试标题")
	if a.BodyState != "purged" || a.Note != note || len(a.Artifacts) != 1 || a.Artifacts[0].ID != art.ID {
		t.Fatal("poll refilled or erased metadata")
	}
	job, e := s.QueueFulltext(ctx, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	claimed, e := s.ClaimJob(ctx, "worker", time.Minute)
	if e != nil || job.ID != claimed.ID {
		t.Fatalf("claim %v", e)
	}
	if e = s.CompleteFulltextJob(ctx, job.ID, "worker", FulltextResult{HTML: "<p>重新获取全文</p>"}); e != nil {
		t.Fatal(e)
	}
	a, e = s.GetArticle(ctx, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	if a.BodyState != "available" || a.ContentExpiresAt == nil || !a.ContentExpiresAt.Equal(clock.AddDate(0, 0, 90)) {
		t.Fatalf("refetch expiry %+v", a.ContentExpiresAt)
	}
}
func TestFavoritesArchiveAndImportProtection(t *testing.T) {
	ctx := context.Background()
	s, clock := testService(t)
	f := feedFixture(t, s)
	fav := addFixture(t, s, f, "收藏")
	yes := true
	if _, e := s.UpdateArticle(ctx, fav.ID, fav.Version, ArticlePatch{Favorite: &yes}); e != nil {
		t.Fatal(e)
	}
	arch, e := s.CreateFeed(ctx, Feed{URL: "https://archive.test/feed", Archive: true})
	if e != nil {
		t.Fatal(e)
	}
	aa := addFixture(t, s, arch, "长期")
	old := clock.AddDate(-1, 0, 0)
	im, e := s.ImportArticle(ctx, ImportArticleInput{SourceInstance: "synthetic", SourceID: "1", FeedID: f.ID, Entry: entryFixture("迁入"), FirstSeenAt: &old, Read: true})
	if e != nil {
		t.Fatal(e)
	}
	protection := *im.ProtectionUntil
	*clock = clock.AddDate(0, 0, 29)
	im2, e := s.ImportArticle(ctx, ImportArticleInput{SourceInstance: "synthetic", SourceID: "1", FeedID: f.ID, Entry: entryFixture("迁入"), FirstSeenAt: &old})
	if e != nil {
		t.Fatal(e)
	}
	if !im2.ProtectionUntil.Equal(protection) || im2.ID != im.ID || im2.ReadAt == nil || !im2.ReadAtUnknown {
		t.Fatal("import reset")
	}
	if n, e := s.Cleanup(ctx, 100); e != nil || n != 0 {
		t.Fatalf("protection %d %v", n, e)
	}
	*clock = clock.AddDate(0, 0, 100)
	if n, e := s.Cleanup(ctx, 100); e != nil || n != 1 {
		t.Fatalf("protected source %d %v", n, e)
	}
	for _, articleID := range []string{fav.ID, aa.ID} {
		a, e := s.GetArticle(ctx, articleID)
		if e != nil || a.BodyState != "available" {
			t.Fatalf("lost protected content: %s %v", a.BodyState, e)
		}
	}
}
func TestDedupLeasesRestartAndConflict(t *testing.T) {
	ctx := context.Background()
	s, clock := testService(t)
	f := feedFixture(t, s)
	j, e := s.QueueFeed(ctx, f.ID)
	if e != nil {
		t.Fatal(e)
	}
	j2, e := s.QueueFeed(ctx, f.ID)
	if e != nil || j.ID != j2.ID {
		t.Fatal("not coalesced")
	}
	claimed, e := s.ClaimJob(ctx, "old", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	*clock = clock.Add(2 * time.Minute)
	claimed2, e := s.ClaimJob(ctx, "new", time.Minute)
	if e != nil || claimed.ID != claimed2.ID || claimed2.Attempts != 2 {
		t.Fatalf("recovery %v", e)
	}
	result := FeedResult{Entries: []FeedEntry{entryFixture("去重")}}
	if e = s.CompleteFeedJob(ctx, claimed.ID, "old", result); !errors.Is(e, ErrLease) {
		t.Fatal(e)
	}
	if e = s.CompleteFeedJob(ctx, claimed.ID, "new", result); e != nil {
		t.Fatal(e)
	}
	a := addFixture(t, s, f, "去重")
	page, e := s.ListArticles(ctx, ArticleQuery{Scope: "all"})
	if e != nil || len(page.Items) != 1 {
		t.Fatal("duplicate", e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	yes := true
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.UpdateArticle(ctx, a.ID, a.Version, ArticlePatch{Read: &yes})
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for e := range errs {
		if e == nil {
			success++
		} else if errors.Is(e, ErrVersion) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success %d conflict %d", success, conflict)
	}
}
func TestStablePaginationAndArtifactIdempotency(t *testing.T) {
	ctx := context.Background()
	s, clock := testService(t)
	f := feedFixture(t, s)
	for _, title := range []string{"一", "二", "三"} {
		addFixture(t, s, f, title)
	}
	page, e := s.ListArticles(ctx, ArticleQuery{Scope: "all", Limit: 2})
	if e != nil || len(page.Items) != 2 || page.NextCursor == "" {
		t.Fatal(e)
	}
	*clock = clock.Add(time.Hour)
	addFixture(t, s, f, "新文章")
	next, e := s.ListArticles(ctx, ArticleQuery{Scope: "all", Limit: 2, Cursor: page.NextCursor})
	if e != nil || len(next.Items) != 1 {
		t.Fatalf("page %+v %v", next, e)
	}
	a := addFixture(t, s, f, "摘要文章")
	req := Artifact{ArticleID: a.ID, Actor: "one", ExternalID: "stable", Content: "分析", BasisHash: a.ContentHash}
	first, e := s.PutArtifact(ctx, req)
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.PutArtifact(ctx, req)
	if e != nil || again.ID != first.ID {
		t.Fatal("not idempotent", e)
	}
	req.Content = "different"
	if _, e = s.PutArtifact(ctx, req); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	req.ExternalID = "different"
	req.BasisHash = "stale"
	if _, e = s.PutArtifact(ctx, req); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
}

func TestTransactionalIdempotencyAndAuthorization(t *testing.T) {
	ctx := context.Background()
	s, _ := testService(t)
	f := feedFixture(t, s)
	req := WithIdempotency(ctx, "token-1", "feed-refresh", "key-1", "payload-1")
	j, e := s.QueueFeed(req, f.ID)
	if e != nil {
		t.Fatal(e)
	}
	claimed, e := s.ClaimJob(ctx, "worker", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteFeedJob(ctx, claimed.ID, "worker", FeedResult{}); e != nil {
		t.Fatal(e)
	}
	again, e := s.QueueFeed(req, f.ID)
	if e != nil || again.ID != j.ID || again.State != "succeeded" {
		t.Fatalf("terminal retry %+v %v", again, e)
	}
	changed := WithIdempotency(ctx, "token-1", "feed-refresh", "key-1", "different")
	if _, e = s.QueueFeed(changed, f.ID); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	blocked := WithAuthorization(ctx, func(context.Context, *sql.Tx) error { return errors.New("revoked") })
	if _, e = s.QueueFeed(blocked, f.ID); e == nil || e.Error() != "revoked" {
		t.Fatal(e)
	}
	a := addFixture(t, s, f, "原子幂等")
	req = WithIdempotency(ctx, "token-1", "artifact", "key-2", "payload-2")
	artifact := Artifact{ArticleID: a.ID, Actor: "token-1", ExternalID: "summary-1", Content: "摘要", BasisHash: a.ContentHash}
	first, e := s.PutArtifact(req, artifact)
	if e != nil {
		t.Fatal(e)
	}
	againArtifact, e := s.PutArtifact(req, artifact)
	if e != nil || againArtifact.ID != first.ID {
		t.Fatal(e)
	}
	if _, e = s.PutArtifact(WithIdempotency(ctx, "token-1", "artifact", "key-2", "different"), artifact); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
}
func TestVisibilityDeadlinesAndSearchTermsAcrossFields(t *testing.T) {
	ctx := context.Background()
	s, clock := testService(t)
	f := feedFixture(t, s)
	unread := addFixture(t, s, f, "标题甲")
	read := addFixture(t, s, f, "标题乙")
	yes := true
	if _, e := s.UpdateArticle(ctx, read.ID, read.Version, ArticlePatch{Read: &yes}); e != nil {
		t.Fatal(e)
	}
	page, e := s.ListArticles(ctx, ArticleQuery{Scope: "all", Query: "标题甲 音乐"})
	if e != nil || len(page.Items) != 1 {
		t.Fatal("cross field search", len(page.Items), e)
	}
	*clock = clock.AddDate(0, 0, 7)
	r, e := s.GetArticle(ctx, read.ID)
	if e != nil || !r.Hidden {
		t.Fatal("read deadline", e)
	}
	u, e := s.GetArticle(ctx, unread.ID)
	if e != nil || u.Hidden {
		t.Fatal("unread early", e)
	}
	*clock = clock.AddDate(0, 0, 23)
	u, e = s.GetArticle(ctx, unread.ID)
	if e != nil || !u.Hidden || u.ReadAt != nil {
		t.Fatal("unread deadline", e)
	}
	if _, e = s.ListArticles(ctx, ArticleQuery{Query: strings.Repeat("中", 129)}); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
}
func TestFailedCompletionRollsBackArticleIndexAndJob(t *testing.T) {
	ctx := context.Background()
	s, _ := testService(t)
	f := feedFixture(t, s)
	first := addFixture(t, s, f, "既有一")
	second := addFixture(t, s, f, "既有二")
	j, e := s.QueueFeed(ctx, f.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ClaimJob(ctx, "worker", time.Minute); e != nil {
		t.Fatal(e)
	}
	collision := entryFixture("既有一")
	collision.URL = second.URL
	e = s.CompleteFeedJob(ctx, j.ID, "worker", FeedResult{Entries: []FeedEntry{entryFixture("应回滚"), collision}})
	if !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	page, e := s.ListArticles(ctx, ArticleQuery{Scope: "all", Query: "应回滚"})
	if e != nil || len(page.Items) != 0 {
		t.Fatal("partial content/index", e)
	}
	job, e := s.GetJob(ctx, j.ID)
	if e != nil || job.State != "running" {
		t.Fatal("partial job state", e)
	}
	a, e := s.GetArticle(ctx, first.ID)
	if e != nil || a.Version != first.Version {
		t.Fatal("partial article", e)
	}
}

func TestFeedCanSupplyInitiallyAbsentContentWithinOriginalClock(t *testing.T) {
	ctx := context.Background()
	s, clock := testService(t)
	f := feedFixture(t, s)
	entry := entryFixture("后来有正文")
	entry.HTML = ""
	ids, e := s.IngestFeed(ctx, f.ID, []FeedEntry{entry})
	if e != nil {
		t.Fatal(e)
	}
	*clock = clock.AddDate(0, 0, 10)
	entry.HTML = "<p>新提供的正文</p>"
	if _, e = s.IngestFeed(ctx, f.ID, []FeedEntry{entry}); e != nil {
		t.Fatal(e)
	}
	a, e := s.GetArticle(ctx, ids[0])
	if e != nil || a.BodyState != "available" || a.ContentExpiresAt == nil || !a.ContentExpiresAt.Equal(a.FirstSeenAt.AddDate(0, 0, 90)) {
		t.Fatalf("%+v %v", a, e)
	}
}

func TestPauseRevokesRunningLeaseAndResumeSchedules(t *testing.T) {
	ctx := context.Background()
	s, _ := testService(t)
	f := feedFixture(t, s)
	j, e := s.QueueFeed(ctx, f.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ClaimJob(ctx, "worker", time.Minute); e != nil {
		t.Fatal(e)
	}
	f.Paused = true
	f, e = s.UpdateFeed(ctx, f.ID, f.Version, f)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteFeedJob(ctx, j.ID, "worker", FeedResult{Entries: []FeedEntry{entryFixture("禁止迟到")}}); !errors.Is(e, ErrLease) {
		t.Fatal(e)
	}
	if _, e = s.QueueFeed(ctx, f.ID); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	job, e := s.GetJob(ctx, j.ID)
	if e != nil || job.State != "failed" || job.Error != "paused" {
		t.Fatal(job, e)
	}
	f.Paused = false
	if _, e = s.UpdateFeed(ctx, f.ID, f.Version, f); e != nil {
		t.Fatal(e)
	}
	if e = s.ScheduleDue(ctx); e != nil {
		t.Fatal(e)
	}
	claim, e := s.ClaimJob(ctx, "new", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.DeferJob(ctx, claim.ID, "new", s.Now().Add(time.Minute)); e != nil {
		t.Fatal(e)
	}
	job, e = s.GetJob(ctx, claim.ID)
	if e != nil || job.Attempts != 0 || job.State != "queued" {
		t.Fatal(job, e)
	}
}

func TestBroadSearchStrategyHonorsContentDeadlineAndLiteralInput(t *testing.T) {
	ctx := context.Background()
	s, clock := testService(t)
	f := feedFixture(t, s)
	entries := make([]FeedEntry, 250)
	for i := range entries {
		entries[i] = entryFixture(fmt.Sprintf("广泛标题%d", i))
	}
	if _, e := s.IngestFeed(ctx, f.ID, entries); e != nil {
		t.Fatal(e)
	}
	p, e := s.ListArticles(ctx, ArticleQuery{Scope: "all", Query: "音乐", Limit: 50})
	if e != nil || len(p.Items) != 50 || p.NextCursor == "" {
		t.Fatal(len(p.Items), e)
	}
	*clock = clock.AddDate(0, 0, 91)
	p, e = s.ListArticles(ctx, ArticleQuery{Scope: "all", Query: "音乐"})
	if e != nil || len(p.Items) != 0 {
		t.Fatal("expired broad source match", len(p.Items), e)
	}
	p, e = s.ListArticles(ctx, ArticleQuery{Scope: "all", Query: "广泛"})
	if e != nil || len(p.Items) != 50 {
		t.Fatal("retained broad title", len(p.Items), e)
	}
	for _, query := range []string{`"`, `OR`, `-`, `title:音乐`, `音乐 OR 标题`} {
		if _, e = s.ListArticles(ctx, ArticleQuery{Scope: "all", Query: query}); e != nil {
			t.Fatalf("literal %q: %v", query, e)
		}
	}
}

func TestRetentionPreviewOverrideOnlyCountsRequestedFeed(t *testing.T) {
	ctx := context.Background()
	s, clock := testService(t)
	one := feedFixture(t, s)
	two, e := s.CreateFeed(ctx, Feed{URL: "https://other.test/feed", Title: "另一个源"})
	if e != nil {
		t.Fatal(e)
	}
	addFixture(t, s, one, "源一")
	addFixture(t, s, two, "源二")
	*clock = clock.AddDate(0, 0, 91)
	all, e := s.RetentionPreview(ctx)
	if e != nil || all.Articles != 2 {
		t.Fatal(all, e)
	}
	preview, e := s.RetentionPreview(ctx, one)
	if e != nil || preview.Articles != 1 || preview.Contents != 1 {
		t.Fatal(preview, e)
	}
	one.Archive = true
	preview, e = s.RetentionPreview(ctx, one)
	if e != nil || preview.Articles != 0 || preview.Contents != 0 || preview.Bytes != 0 {
		t.Fatal("other feed leaked into preview", preview, e)
	}
	all, e = s.RetentionPreview(ctx)
	if e != nil || all.Articles != 2 {
		t.Fatal("preview changed real policy", all, e)
	}
}
func TestUntrustedContentPreparedBeforeTransactionForEveryEntryPoint(t *testing.T) {
	ctx := context.Background()
	s, _ := testService(t)
	f := feedFixture(t, s)
	huge := entryFixture("超限")
	huge.HTML = strings.Repeat("x", 8<<20+1)
	checked := false
	guard := WithAuthorization(ctx, func(context.Context, *sql.Tx) error { checked = true; return nil })
	if _, e := s.IngestFeed(guard, f.ID, []FeedEntry{huge}); !errors.Is(e, ErrInvalid) || checked {
		t.Fatal("ingest opened transaction before content validation", e)
	}
	if _, e := s.ImportArticle(guard, ImportArticleInput{SourceInstance: "synthetic", SourceID: "oversized", FeedID: f.ID, Entry: huge}); !errors.Is(e, ErrInvalid) || checked {
		t.Fatal("import opened transaction before content validation", e)
	}
	j, e := s.QueueFeed(ctx, f.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ClaimJob(ctx, "worker", time.Minute); e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteFeedJob(guard, j.ID, "worker", FeedResult{Entries: []FeedEntry{huge}}); !errors.Is(e, ErrInvalid) || checked {
		t.Fatal("completion opened transaction before content validation", e)
	}
	dirty := entryFixture("净化")
	dirty.HTML = `<p>safe 音乐</p><script>alert(1)</script><img src=x onerror=alert(1)>`
	if e = s.CompleteFeedJob(ctx, j.ID, "worker", FeedResult{Entries: []FeedEntry{dirty}}); e != nil {
		t.Fatal(e)
	}
	im, e := s.ImportArticle(ctx, ImportArticleInput{SourceInstance: "synthetic", SourceID: "safe", FeedID: f.ID, Entry: FeedEntry{GUID: "import-safe", Title: "迁入净化", HTML: dirty.HTML}})
	if e != nil {
		t.Fatal(e)
	}
	page, e := s.ListArticles(ctx, ArticleQuery{Scope: "all"})
	if e != nil {
		t.Fatal(e)
	}
	for _, article := range append(page.Items, im) {
		detail, e := s.GetArticle(ctx, article.ID)
		if e != nil {
			t.Fatal(e)
		}
		if len(detail.Content) == 0 || strings.Contains(detail.Content[0].HTML, "script") || strings.Contains(detail.Content[0].HTML, "onerror") || strings.Contains(detail.Content[0].Text, "alert") {
			t.Fatal("unsafe content reached storage", detail.Content)
		}
	}
}
func TestHostCooldownPersistsAndOnlyExtendsAtomically(t *testing.T) {
	ctx := context.Background()
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "reader.db")
	st, e := store.Open(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	s := New(st, func() time.Time { return clock })
	f := feedFixture(t, s)
	j, e := s.QueueFeed(ctx, f.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ClaimJob(ctx, "worker", time.Minute); e != nil {
		t.Fatal(e)
	}
	until := clock.Add(2 * time.Hour)
	if e = s.FailJob(WithHostCooldown(ctx, " EXAMPLE.TEST. ", until), j.ID, "worker", "http_429", &until); e != nil {
		t.Fatal(e)
	}
	got, e := s.HostCooldown(ctx, "example.test")
	if e != nil || !got.Equal(until) {
		t.Fatal(got, e)
	}
	if e = s.FailJob(WithHostCooldown(ctx, "example.test", until.Add(time.Hour)), j.ID, "worker", "http_429", nil); !errors.Is(e, ErrLease) {
		t.Fatal(e)
	}
	got, e = s.HostCooldown(ctx, "example.test")
	if e != nil || !got.Equal(until) {
		t.Fatal("lost lease extended cooldown", got, e)
	}
	if _, e = s.HostCooldown(ctx, "missing.test"); e != nil {
		t.Fatal(e)
	}
	if e = st.Close(); e != nil {
		t.Fatal(e)
	}
	st, e = store.Open(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	s = New(st, func() time.Time { return clock })
	got, e = s.HostCooldown(ctx, "Example.Test")
	if e != nil || !got.Equal(until) {
		t.Fatal("restart lost host cooldown", got, e)
	}
	clock = until.Add(time.Second)
	j, e = s.ClaimJob(ctx, "next", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	shorter := until.Add(-time.Minute)
	if e = s.FailJob(WithHostCooldown(ctx, "example.test", shorter), j.ID, "next", "http_429", nil); e != nil {
		t.Fatal(e)
	}
	got, e = s.HostCooldown(ctx, "example.test")
	if e != nil || !got.Equal(until) {
		t.Fatal("shortened host cooldown", got, e)
	}
}
func TestChangesDefaultPageMatchesContract(t *testing.T) {
	ctx := context.Background()
	s, _ := testService(t)
	f := feedFixture(t, s)
	entries := make([]FeedEntry, 55)
	for i := range entries {
		entries[i] = entryFixture(fmt.Sprintf("增量%d", i))
	}
	if _, e := s.IngestFeed(ctx, f.ID, entries); e != nil {
		t.Fatal(e)
	}
	first, e := s.Changes(ctx, "0", 0)
	if e != nil || len(first.Items) != 50 {
		t.Fatal(len(first.Items), e)
	}
	second, e := s.Changes(ctx, first.NextCursor, 0)
	if e != nil || len(second.Items) != 6 || second.Items[0].Seq == first.Items[49].Seq {
		t.Fatal(len(second.Items), e)
	}
}

func TestImportedRemovedIsArchivedWithoutBeingOrdinaryUnread(t *testing.T) {
	ctx := context.Background()
	s, _ := testService(t)
	f := feedFixture(t, s)
	article, e := s.ImportArticle(ctx, ImportArticleInput{SourceInstance: "synthetic", SourceID: "removed", FeedID: f.ID, Entry: entryFixture("旧系统removed"), Removed: true})
	if e != nil {
		t.Fatal(e)
	}
	if !article.Hidden || !article.Removed || article.ReadAt != nil {
		t.Fatal(article)
	}
	no := false
	unread, e := s.ListArticles(ctx, ArticleQuery{Scope: "all", Read: &no})
	if e != nil || len(unread.Items) != 0 {
		t.Fatal("removed became unread", len(unread.Items), e)
	}
	archive, e := s.ListArticles(ctx, ArticleQuery{Scope: "archive"})
	if e != nil || len(archive.Items) != 1 || archive.Items[0].ID != article.ID {
		t.Fatal("removed lost from archive", len(archive.Items), e)
	}
}

func TestConsecutiveNotModifiedResponsesRetainValidators(t *testing.T) {
	ctx := context.Background()
	s, _ := testService(t)
	f := feedFixture(t, s)
	for _, result := range []FeedResult{{ETag: `"synthetic-etag"`, LastModified: "Wed, 01 Jan 2025 00:00:00 GMT"}, {NotModified: true}, {NotModified: true}} {
		j, e := s.QueueFeed(ctx, f.ID)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.ClaimJob(ctx, "worker", time.Minute); e != nil {
			t.Fatal(e)
		}
		if e = s.CompleteFeedJob(ctx, j.ID, "worker", result); e != nil {
			t.Fatal(e)
		}
		got, e := s.GetFeed(ctx, f.ID)
		if e != nil || got.ETag != `"synthetic-etag"` || got.LastModified != "Wed, 01 Jan 2025 00:00:00 GMT" {
			t.Fatal("304 lost validators", got.ETag, got.LastModified, e)
		}
	}
}
func TestFallbackIdentityRejectsTitleAloneAndSurvivesBodyChange(t *testing.T) {
	ctx := context.Background()
	s, _ := testService(t)
	f := feedFixture(t, s)
	for _, entry := range []FeedEntry{{Title: "仅相同标题", HTML: "<p>甲</p>"}, {Title: "仅相同标题", HTML: "<p>乙</p>"}, {Author: "只有作者"}} {
		if _, e := s.IngestFeed(ctx, f.ID, []FeedEntry{entry}); !errors.Is(e, ErrInvalid) {
			t.Fatalf("insufficient identity accepted: %+v %v", entry, e)
		}
	}
	entry := FeedEntry{Title: "后备身份", Author: "合成作者", HTML: "<p>原始正文</p>"}
	first, e := s.IngestFeed(ctx, f.ID, []FeedEntry{entry})
	if e != nil {
		t.Fatal(e)
	}
	entry.HTML = "<p>变化后的正文</p>"
	again, e := s.IngestFeed(ctx, f.ID, []FeedEntry{entry})
	if e != nil || first[0] != again[0] {
		t.Fatal("body changed fallback identity", first, again, e)
	}
	article, e := s.GetArticle(ctx, first[0])
	if e != nil || article.Provenance != "fallback" || article.Content[0].Text != "变化后的正文" {
		t.Fatal(article, e)
	}
	var aliases int
	if e = s.Store.Read.QueryRowContext(ctx, `SELECT count(*) FROM identities WHERE article_id=? AND kind='fallback'`, article.ID).Scan(&aliases); e != nil || aliases != 1 {
		t.Fatal(aliases, e)
	}
}

func TestMinifluxGUIDHashMatchesAfterURLChangeWithoutResettingState(t *testing.T) {
	ctx := context.Background()
	s, clock := testService(t)
	f := feedFixture(t, s)
	firstSeen := clock.AddDate(0, 0, -120)
	readAt := firstSeen.AddDate(0, 0, 2)
	guid := "stable-synthetic-guid"
	original, e := s.ImportArticle(ctx, ImportArticleInput{SourceInstance: "synthetic-miniflux", SourceID: "legacy-1", FeedID: f.ID, OldHash: hash(guid), FirstSeenAt: &firstSeen, Read: true, ReadAt: &readAt, Favorite: true, Entry: FeedEntry{URL: "https://example.test/old-link", Title: "迁入文章", HTML: "<p>迁入正文</p>"}})
	if e != nil {
		t.Fatal(e)
	}
	importedAt, protection := *original.ImportedAt, *original.ProtectionUntil
	*clock = clock.AddDate(0, 0, 4)
	entry := FeedEntry{GUID: guid, URL: "https://example.test/changed-link", Title: "迁入文章更新", HTML: "<p>更新正文</p>"}
	ids, e := s.IngestFeed(ctx, f.ID, []FeedEntry{entry})
	if e != nil || len(ids) != 1 || ids[0] != original.ID {
		t.Fatal("URL change duplicated legacy article", ids, e)
	}
	updated, e := s.GetArticle(ctx, original.ID)
	if e != nil || updated.URL != entry.URL || updated.ReadAt == nil || !updated.ReadAt.Equal(readAt) || !updated.Favorite || !updated.FirstSeenAt.Equal(firstSeen) || !updated.ImportedAt.Equal(importedAt) || !updated.ProtectionUntil.Equal(protection) {
		t.Fatalf("legacy state changed: %+v %v", updated, e)
	}
	page, e := s.ListArticles(ctx, ArticleQuery{Scope: "all"})
	if e != nil || len(page.Items) != 1 {
		t.Fatal("duplicate legacy article", len(page.Items), e)
	}
	var aliases int
	if e = s.Store.Read.QueryRowContext(ctx, `SELECT count(*) FROM identities WHERE article_id=? AND ((kind='guid' AND value=?) OR (kind='url' AND value=?))`, original.ID, guid, normalizeURL(entry.URL)).Scan(&aliases); e != nil || aliases != 2 {
		t.Fatal("new local aliases missing", aliases, e)
	}
	native := addFixture(t, s, f, "原生文章")
	if e = s.Store.Read.QueryRowContext(ctx, `SELECT count(*) FROM identities WHERE article_id=? AND kind='miniflux_hash'`, native.ID).Scan(&aliases); e != nil || aliases != 0 {
		t.Fatal("fabricated legacy alias on native article", aliases, e)
	}
}
func TestMinifluxGUIDHashLookupIsFeedScopedAndConflictsAtomically(t *testing.T) {
	ctx := context.Background()
	s, _ := testService(t)
	f := feedFixture(t, s)
	guid := "synthetic-conflict-guid"
	legacy, e := s.ImportArticle(ctx, ImportArticleInput{SourceInstance: "synthetic", SourceID: "one", FeedID: f.ID, OldHash: hash(guid), Entry: FeedEntry{URL: "https://example.test/legacy", Title: "旧记录", HTML: "<p>旧正文</p>"}})
	if e != nil {
		t.Fatal(e)
	}
	other := addFixture(t, s, f, "URL另有所属")
	entry := FeedEntry{GUID: guid, URL: other.URL, Title: "冲突更新", HTML: "<p>不可写入</p>"}
	if _, e = s.IngestFeed(ctx, f.ID, []FeedEntry{entry}); !errors.Is(e, ErrConflict) {
		t.Fatal("legacy/local identity conflict accepted", e)
	}
	unchanged, e := s.GetArticle(ctx, legacy.ID)
	if e != nil || unchanged.Title != legacy.Title || unchanged.Version != legacy.Version {
		t.Fatal("conflict partially updated legacy record", unchanged, e)
	}
	var count int
	if e = s.Store.Read.QueryRowContext(ctx, `SELECT count(*) FROM identities WHERE kind='guid' AND value=?`, guid).Scan(&count); e != nil || count != 0 {
		t.Fatal("conflict left a local alias", count, e)
	}
	another, e := s.CreateFeed(ctx, Feed{URL: "https://another.test/feed", Title: "另一个源"})
	if e != nil {
		t.Fatal(e)
	}
	entry.URL = "https://another.test/article"
	ids, e := s.IngestFeed(ctx, another.ID, []FeedEntry{entry})
	if e != nil || ids[0] == legacy.ID {
		t.Fatal("cross-feed legacy merge", ids, e)
	}
}
