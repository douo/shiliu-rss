package app

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestArticleListsUsePublicationOrderAcrossFiltersAndPages(t *testing.T) {
	ctx := context.Background()
	s, clock := testService(t)
	f := feedFixture(t, s)
	cat, err := s.CreateCategory(ctx, "合成分类")
	if err != nil {
		t.Fatal(err)
	}
	f.CategoryID = cat.ID
	if _, err = s.UpdateFeed(ctx, f.ID, f.Version, f); err != nil {
		t.Fatal(err)
	}
	pub := clock.Add(-time.Hour)
	older := pub.Add(-24 * time.Hour)
	tied := pub.Add(-time.Hour)
	var ids []string
	for i, at := range []*time.Time{&pub, &older, &tied, nil, &tied} {
		entry := entryFixture([]string{"合成新文", "合成旧文", "合成同日甲", "合成缺日期", "合成同日乙"}[i])
		entry.PublishedAt = at
		created, e := s.IngestFeed(ctx, f.ID, []FeedEntry{entry})
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, created[0])
		a, e := s.GetArticle(ctx, created[0])
		if e != nil {
			t.Fatal(e)
		}
		yes := true
		if _, e = s.UpdateArticle(ctx, a.ID, a.Version, ArticlePatch{Favorite: &yes}); e != nil {
			t.Fatal(e)
		}
		*clock = clock.Add(time.Minute)
	}
	want := []string{ids[3], ids[0], ids[4], ids[2], ids[1]}
	assertPages := func(q ArticleQuery) {
		t.Helper()
		q.Limit = 2
		var got []string
		for page := 0; page < 4; page++ {
			result, e := s.ListArticles(ctx, q)
			if e != nil {
				t.Fatal(e)
			}
			for _, a := range result.Items {
				got = append(got, a.ID)
			}
			if result.NextCursor == "" {
				break
			}
			q.Cursor = result.NextCursor
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("query %+v: got %v, want %v", q, got, want)
		}
	}
	yes := true
	for _, q := range []ArticleQuery{{Scope: "recent"}, {Scope: "all"}, {FeedID: f.ID}, {CategoryID: cat.ID}, {Favorite: &yes}, {Query: "合成"}} {
		assertPages(q)
	}
	for _, articleID := range ids {
		a, e := s.GetArticle(ctx, articleID)
		if e != nil {
			t.Fatal(e)
		}
		no := false
		if _, e = s.UpdateArticle(ctx, a.ID, a.Version, ArticlePatch{Favorite: &no}); e != nil {
			t.Fatal(e)
		}
	}
	*clock = clock.AddDate(0, 0, 31)
	assertPages(ArticleQuery{Scope: "archive"})
	legacy := base64.RawURLEncoding.EncodeToString([]byte(stamp(*clock) + "|" + ids[0]))
	if _, e := s.ListArticles(ctx, ArticleQuery{Cursor: legacy}); !errors.Is(e, ErrInvalid) {
		t.Fatalf("legacy cursor must restart: %v", e)
	}
}

func TestFeedUnreadCountIncludesArchiveAndExcludesReadOrRemoved(t *testing.T) {
	ctx := context.Background()
	s, clock := testService(t)
	f := feedFixture(t, s)
	empty, e := s.CreateFeed(ctx, Feed{URL: "https://empty.test/feed", Paused: true})
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 55; i++ {
		addFixture(t, s, f, string(rune('一'+i)))
	}
	old := clock.AddDate(0, 0, -60)
	archived, e := s.ImportArticle(ctx, ImportArticleInput{SourceInstance: "synthetic", SourceID: "archive", FeedID: f.ID, Entry: entryFixture("归档未读"), FirstSeenAt: &old})
	if e != nil {
		t.Fatal(e)
	}
	for _, imp := range []ImportArticleInput{
		{SourceInstance: "synthetic", SourceID: "removed", FeedID: f.ID, Entry: entryFixture("已移除"), Removed: true},
		{SourceInstance: "synthetic", SourceID: "read", FeedID: f.ID, Entry: entryFixture("已读"), Read: true},
	} {
		if _, e = s.ImportArticle(ctx, imp); e != nil {
			t.Fatal(e)
		}
	}
	assertCounts := func(want int64) {
		t.Helper()
		feeds, err := s.ListFeeds(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, feed := range feeds {
			expected := want
			if feed.ID == empty.ID {
				expected = 0
			}
			if feed.UnreadCount != expected {
				t.Fatalf("count %s: got %d want %d", feed.ID, feed.UnreadCount, expected)
			}
		}
		feed, err := s.GetFeed(ctx, f.ID)
		if err != nil || feed.UnreadCount != want {
			t.Fatal(feed.UnreadCount, err)
		}
	}
	assertCounts(56)
	yes := true
	archived, e = s.UpdateArticle(ctx, archived.ID, archived.Version, ArticlePatch{Read: &yes})
	if e != nil {
		t.Fatal(e)
	}
	assertCounts(55)
	no := false
	if _, e = s.UpdateArticle(ctx, archived.ID, archived.Version, ArticlePatch{Read: &no}); e != nil {
		t.Fatal(e)
	}
	assertCounts(56)
}
