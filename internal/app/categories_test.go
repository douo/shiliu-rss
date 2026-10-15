package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDeleteNonemptyCategoryPreservesFeedsArticlesAndRecordsChanges(t *testing.T) {
	ctx := context.Background()
	s, _ := testService(t)
	category, e := s.CreateCategory(ctx, "合成分类")
	if e != nil {
		t.Fatal(e)
	}
	otherCategory, e := s.CreateCategory(ctx, "其它分类")
	if e != nil {
		t.Fatal(e)
	}
	feeds := []Feed{}
	articles := []Article{}
	for i, url := range []string{"https://one.invalid/feed", "https://two.invalid/feed", "https://other.invalid/feed"} {
		categoryID := category.ID
		if i == 2 {
			categoryID = otherCategory.ID
		}
		feed, e := s.CreateFeed(ctx, Feed{URL: url, Title: url, CategoryID: categoryID})
		if e != nil {
			t.Fatal(e)
		}
		feeds = append(feeds, feed)
		article := addFixture(t, s, feed, url)
		yes := true
		note := "保留个人笔记"
		tags := []string{"分类删除不清正文"}
		article, e = s.UpdateArticle(ctx, article.ID, article.Version, ArticlePatch{Read: &yes, Favorite: &yes, Note: &note, Tags: &tags})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.PutArtifact(ctx, Artifact{ArticleID: article.ID, Actor: "synthetic-agent", ExternalID: article.ID, Content: "保留摘要", BasisHash: article.ContentHash}); e != nil {
			t.Fatal(e)
		}
		article, e = s.GetArticle(ctx, article.ID)
		if e != nil {
			t.Fatal(e)
		}
		articles = append(articles, article)
	}
	before, e := s.Changes(ctx, "0", 200)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.DeleteCategory(ctx, category.ID, category.Version); e != nil {
		t.Fatal(e)
	}
	categories, e := s.ListCategories(ctx)
	if e != nil || len(categories) != 1 || categories[0].ID != otherCategory.ID {
		t.Fatal(categories, e)
	}
	for i, want := range feeds {
		got, e := s.GetFeed(ctx, want.ID)
		if e != nil {
			t.Fatal(e)
		}
		if i < 2 {
			want.CategoryID = ""
			want.Version++
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("feed state changed beyond category/version: got %+v want %+v", got, want)
		}
		gotArticle, e := s.GetArticle(ctx, articles[i].ID)
		if e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(gotArticle, articles[i]) {
			t.Fatalf("category removal changed article/content/organization: got %+v want %+v", gotArticle, articles[i])
		}
	}
	changes, e := s.Changes(ctx, before.HighWatermark, 200)
	if e != nil || len(changes.Items) != 3 {
		t.Fatal(changes, e)
	}
	seen := map[string]bool{}
	for _, change := range changes.Items {
		if change.Entity == "feed" {
			matched := false
			for _, feed := range feeds[:2] {
				if change.EntityID == feed.ID {
					matched = true
					if change.Kind != "updated" || change.Version != feed.Version+1 {
						t.Fatal(change)
					}
				}
			}
			if !matched {
				t.Fatal("unexpected feed event", change)
			}
		} else if change.Entity == "category" {
			if change.EntityID != category.ID || change.Kind != "deleted" || change.Version != category.Version+1 {
				t.Fatal(change)
			}
		} else {
			t.Fatal("unexpected article/content change", change)
		}
		key := change.Entity + ":" + change.EntityID
		if seen[key] {
			t.Fatal("duplicate change event", change)
		}
		seen[key] = true
	}
	last := changes.Items[len(changes.Items)-1]
	if last.Entity != "category" || last.Kind != "deleted" {
		t.Fatal("category deletion should follow feed updates", last)
	}
	if e = s.DeleteCategory(ctx, category.ID, category.Version); !errors.Is(e, ErrNotFound) {
		t.Fatal("missing category", e)
	}
}

func TestCategoryStaleVersionsLeaveAssociationsAndChangesUnmodified(t *testing.T) {
	ctx := context.Background()
	s, _ := testService(t)
	category, e := s.CreateCategory(ctx, "原始分类")
	if e != nil {
		t.Fatal(e)
	}
	feed, e := s.CreateFeed(ctx, Feed{URL: "https://synthetic.invalid/feed", Title: "合成源", CategoryID: category.ID})
	if e != nil {
		t.Fatal(e)
	}
	renamed, e := s.UpdateCategory(ctx, category.ID, category.Version, "用户已改名")
	if e != nil {
		t.Fatal(e)
	}
	before, e := s.Changes(ctx, "0", 200)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.DeleteCategory(ctx, category.ID, category.Version); !errors.Is(e, ErrVersion) {
		t.Fatal("stale delete", e)
	}
	if _, e = s.UpdateCategory(ctx, category.ID, category.Version, "迟到的旧修改"); !errors.Is(e, ErrVersion) {
		t.Fatal("stale rename", e)
	}
	gotFeed, e := s.GetFeed(ctx, feed.ID)
	if e != nil || !reflect.DeepEqual(gotFeed, feed) {
		t.Fatal("stale delete moved or changed feed", gotFeed, e)
	}
	categories, e := s.ListCategories(ctx)
	if e != nil || len(categories) != 1 || !reflect.DeepEqual(categories[0], renamed) {
		t.Fatal(categories, e)
	}
	after, e := s.Changes(ctx, before.HighWatermark, 200)
	if e != nil || len(after.Items) != 0 || after.HighWatermark != before.HighWatermark {
		t.Fatal("failed writes emitted changes", after, e)
	}
}

func TestCategoryCreateAndRenameUseSameUnicodeTitleValidation(t *testing.T) {
	ctx := context.Background()
	s, _ := testService(t)
	category, e := s.CreateCategory(ctx, "初始名称")
	if e != nil {
		t.Fatal(e)
	}
	for _, title := range []string{"", " \n\t ", strings.Repeat("中", 101)} {
		before, e := s.Changes(ctx, "0", 200)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.CreateCategory(ctx, title); !errors.Is(e, ErrInvalid) {
			t.Fatalf("create accepted invalid title %q: %v", title, e)
		}
		if _, e = s.UpdateCategory(ctx, category.ID, category.Version, title); !errors.Is(e, ErrInvalid) {
			t.Fatalf("rename accepted invalid title %q: %v", title, e)
		}
		after, e := s.Changes(ctx, before.HighWatermark, 200)
		if e != nil || len(after.Items) != 0 {
			t.Fatal("invalid title changed data", after, e)
		}
	}
	title := strings.Repeat("中", 100)
	renamed, e := s.UpdateCategory(ctx, category.ID, category.Version, " \t"+title+"\n ")
	if e != nil || renamed.Title != title || renamed.Version != category.Version+1 {
		t.Fatal(renamed, e)
	}
	duplicate, e := s.CreateCategory(ctx, "另一个名称")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateCategory(ctx, " "+title+" "); !errors.Is(e, ErrConflict) {
		t.Fatal("duplicate creation", e)
	}
	if _, e = s.UpdateCategory(ctx, duplicate.ID, duplicate.Version, " "+title+" "); !errors.Is(e, ErrConflict) {
		t.Fatal("duplicate rename", e)
	}
	created, e := s.CreateCategory(ctx, strings.Repeat("文", 100))
	if e != nil || len([]rune(created.Title)) != 100 {
		t.Fatal("create rejected 100 Unicode characters", e)
	}
}

func TestImportedCategoryRenamePreservedAndDeletionConflictsOnRepeatImport(t *testing.T) {
	ctx := context.Background()
	s, clock := testService(t)
	original, e := s.ImportCategory(ctx, "synthetic-instance", "source-category", "源系统分类")
	if e != nil {
		t.Fatal(e)
	}
	feed, e := s.ImportFeed(ctx, "synthetic-instance", "source-feed", Feed{URL: "https://import.invalid/feed", Title: "迁入源", CategoryID: original.ID})
	if e != nil {
		t.Fatal(e)
	}
	old := clock.AddDate(0, 0, -120)
	article, e := s.ImportArticle(ctx, ImportArticleInput{SourceInstance: "synthetic-instance", SourceID: "source-article", FeedID: feed.ID, Entry: entryFixture("保护期中的迁入正文"), FirstSeenAt: &old, Read: true, Favorite: true})
	if e != nil {
		t.Fatal(e)
	}
	renamed, e := s.UpdateCategory(ctx, original.ID, original.Version, "用户修改的分类名")
	if e != nil {
		t.Fatal(e)
	}
	before, e := s.Changes(ctx, "0", 200)
	if e != nil {
		t.Fatal(e)
	}
	repeated, e := s.ImportCategory(ctx, "synthetic-instance", "source-category", "源系统分类")
	if e != nil || !reflect.DeepEqual(repeated, renamed) {
		t.Fatal("repeat import overwrote user rename", repeated, e)
	}
	after, e := s.Changes(ctx, before.HighWatermark, 200)
	if e != nil || len(after.Items) != 0 {
		t.Fatal("repeat import emitted a change", after, e)
	}
	if e = s.DeleteCategory(ctx, renamed.ID, renamed.Version); e != nil {
		t.Fatal(e)
	}
	replacement, e := s.CreateCategory(ctx, "源系统分类")
	if e != nil {
		t.Fatal(e)
	}
	moved, e := s.GetFeed(ctx, feed.ID)
	if e != nil {
		t.Fatal(e)
	}
	moved.CategoryID = replacement.ID
	moved, e = s.UpdateFeed(ctx, moved.ID, moved.Version, moved)
	if e != nil {
		t.Fatal(e)
	}
	*clock = clock.AddDate(0, 0, 5)
	before, e = s.Changes(ctx, "0", 200)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ImportCategory(ctx, "synthetic-instance", "source-category", "源系统分类"); !errors.Is(e, ErrConflict) {
		t.Fatal("deleted imported category silently restored/remapped", e)
	}
	after, e = s.Changes(ctx, before.HighWatermark, 200)
	if e != nil || len(after.Items) != 0 {
		t.Fatal("conflicted import changed data", after, e)
	}
	got, e := s.GetFeed(ctx, feed.ID)
	if e != nil || !reflect.DeepEqual(got, moved) {
		t.Fatal("repeat import undid user feed assignment", got, e)
	}
	categories, e := s.ListCategories(ctx)
	if e != nil || len(categories) != 1 || categories[0].ID != replacement.ID {
		t.Fatal("deleted category recreated", categories, e)
	}
	current, e := s.GetArticle(ctx, article.ID)
	if e != nil || !reflect.DeepEqual(current, article) {
		t.Fatal("category operations changed imported article or protection clock", current, e)
	}
	var mapped string
	if e = s.Store.Read.QueryRowContext(ctx, `SELECT target_id FROM imports WHERE source_instance=? AND entity_type='category' AND source_id=?`, "synthetic-instance", "source-category").Scan(&mapped); e != nil || mapped != original.ID {
		t.Fatal("original import deletion evidence lost", mapped, e)
	}
}
