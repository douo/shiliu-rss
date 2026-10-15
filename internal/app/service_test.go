package app

import (
	"context"
	"database/sql"
	"errors"
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
		if len(page.Items) != q.want {
			t.Fatalf("search %s: got %d", q.term, len(page.Items))
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
