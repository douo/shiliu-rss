package app

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestArticleUnreadCountIndependentOfTabsAndPagination(t *testing.T) {
	ctx := context.Background()
	s, clock := testService(t)
	f := feedFixture(t, s)
	for i := 0; i < 60; i++ {
		entry := entryFixture(fmt.Sprintf("synthetic-%02d", i))
		at := clock.Add(-time.Duration(i) * time.Minute)
		entry.PublishedAt = &at
		if _, err := s.ImportArticle(ctx, ImportArticleInput{SourceInstance: "synthetic", SourceID: entry.GUID, FeedID: f.ID, Entry: entry, Read: i < 55}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ListArticles(ctx, ArticleQuery{Scope: "all"})
	if err != nil || len(page.Items) != 50 || page.NextCursor == "" || page.UnreadCount != 5 {
		t.Fatalf("first page: items=%d unread=%d cursor=%q err=%v", len(page.Items), page.UnreadCount, page.NextCursor, err)
	}
	for _, a := range page.Items {
		if a.ReadAt == nil {
			t.Fatal("regression fixture must have no unread on the first page")
		}
	}
	yes, no := true, false
	for _, read := range []*bool{nil, &yes, &no} {
		q := ArticleQuery{Scope: "all", Read: read, Limit: 7}
		seen := 0
		for {
			page, err := s.ListArticles(ctx, q)
			if err != nil || page.UnreadCount != 5 {
				t.Fatalf("tab/page %+v: unread=%d err=%v", q, page.UnreadCount, err)
			}
			seen += len(page.Items)
			if page.NextCursor == "" {
				break
			}
			q.Cursor = page.NextCursor
		}
		want := 60
		if read != nil {
			want = 5
			if *read {
				want = 55
			}
		}
		if seen != want {
			t.Fatalf("tab read=%v: got %d articles want %d", read, seen, want)
		}
	}
	unread, err := s.ListArticles(ctx, ArticleQuery{Scope: "all", Read: &no})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.UpdateArticle(ctx, unread.Items[0].ID, unread.Items[0].Version, ArticlePatch{Read: &yes})
	if err != nil {
		t.Fatal(err)
	}
	page, err = s.ListArticles(ctx, ArticleQuery{Scope: "all", Read: &yes, Limit: 1})
	if err != nil || page.UnreadCount != 4 {
		t.Fatalf("after read: %d %v", page.UnreadCount, err)
	}
	if _, err = s.UpdateArticle(ctx, a.ID, a.Version, ArticlePatch{Read: &no}); err != nil {
		t.Fatal(err)
	}
	page, err = s.ListArticles(ctx, ArticleQuery{Scope: "all"})
	if err != nil || page.UnreadCount != 5 {
		t.Fatalf("after unread: %d %v", page.UnreadCount, err)
	}
}

func TestArticleUnreadCountMatchesContextFilters(t *testing.T) {
	ctx := context.Background()
	s, clock := testService(t)
	cat, err := s.CreateCategory(ctx, "合成分类")
	if err != nil {
		t.Fatal(err)
	}
	feeds := make([]Feed, 3)
	for i := range feeds {
		category := ""
		if i < 2 {
			category = cat.ID
		}
		feeds[i], err = s.CreateFeed(ctx, Feed{URL: fmt.Sprintf("https://synthetic-%d.test/feed", i), CategoryID: category})
		if err != nil {
			t.Fatal(err)
		}
	}
	old, fresh := clock.AddDate(0, 0, -40), clock.AddDate(0, 0, -2)
	for i, row := range []struct {
		feed                    int
		at                      time.Time
		favorite, read, removed bool
		title                   string
	}{
		{0, fresh, false, false, false, "Match one"},
		{0, fresh, true, false, false, "Match two"},
		{0, old, false, false, false, "Match archive"},
		{0, old, true, false, false, "Match favorite"},
		{1, fresh, false, false, false, "Match other source"},
		{2, fresh, false, false, false, "Different category"},
		{0, fresh, false, true, false, "Match read"},
		{0, fresh, false, false, true, "Match removed"},
	} {
		if _, err := s.ImportArticle(ctx, ImportArticleInput{SourceInstance: "synthetic", SourceID: fmt.Sprint(i), FeedID: feeds[row.feed].ID, Entry: entryFixture(row.title), FirstSeenAt: &row.at, Favorite: row.favorite, Read: row.read, Removed: row.removed}); err != nil {
			t.Fatal(err)
		}
	}
	yes, no := true, false
	from, until := clock.AddDate(0, 0, -3), clock.AddDate(0, 0, -1)
	for _, tc := range []struct {
		name string
		q    ArticleQuery
		want int64
	}{
		{"all", ArticleQuery{Scope: "all"}, 6},
		{"recent", ArticleQuery{Scope: "recent"}, 5},
		{"archive", ArticleQuery{Scope: "archive"}, 1},
		{"feed", ArticleQuery{Scope: "all", FeedID: feeds[0].ID}, 4},
		{"category", ArticleQuery{Scope: "all", CategoryID: cat.ID}, 5},
		{"favorites", ArticleQuery{Scope: "all", Favorite: &yes}, 2},
		{"nonfavorites", ArticleQuery{Scope: "all", Favorite: &no}, 4},
		{"search", ArticleQuery{Scope: "all", Query: "Match"}, 5},
		{"date", ArticleQuery{Scope: "all", From: &from, Until: &until}, 4},
		{"exclusive until", ArticleQuery{Scope: "all", Until: &fresh}, 2},
		{"intersection", ArticleQuery{Scope: "all", FeedID: feeds[0].ID, CategoryID: cat.ID, Query: "Match", Favorite: &yes, From: &from, Until: &until}, 1},
		{"empty", ArticleQuery{Scope: "all", FeedID: "999999"}, 0},
		{"empty search", ArticleQuery{Scope: "all", Query: "nomatch"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, read := range []*bool{nil, &yes, &no} {
				q := tc.q
				q.Read, q.Limit = read, 1
				page, err := s.ListArticles(ctx, q)
				if err != nil || page.UnreadCount != tc.want {
					t.Fatalf("query %+v: unread=%d want=%d err=%v", q, page.UnreadCount, tc.want, err)
				}
			}
		})
	}
}
