package importer

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shiliu-rss/internal/app"
	"shiliu-rss/internal/store"
)

func syntheticSnapshot(t *testing.T, unsupported bool) []byte {
	t.Helper()
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -180)
	f := Feed{ID: 10, CategoryID: 1, Title: "合成源", URL: "https://example.invalid/feed", SiteURL: "https://example.invalid/", Disabled: true}
	if unsupported {
		f.Unsupported = []string{"crawler", "password"}
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	records := []Record{
		{Kind: "header", Header: &Header{Format: Format, SourceInstance: "synthetic-miniflux", SourceVersion: "2.3.0", SchemaVersion: "test", ExportedAt: now, UserID: "1"}},
		{Kind: "category", Category: &Category{ID: 1, Title: "测试"}},
		{Kind: "feed", Feed: &f},
		{Kind: "entry", Entry: &Entry{ID: 100, FeedID: 10, Hash: "old-a", URL: "https://example.invalid/a", Title: "古典音乐甲", Content: "<p>源内容独特词</p><script>alert(1)</script>", Status: "read", CreatedAt: &old, PublishedAt: &old, Tags: []string{"音乐"}}},
		{Kind: "entry", Entry: &Entry{ID: 101, FeedID: 10, Hash: "old-b", URL: "https://example.invalid/b", Title: "收藏乙", Content: "<p>收藏内容</p>", Status: "unread", Starred: true, CreatedAt: &old}},
		{Kind: "entry", Entry: &Entry{ID: 102, FeedID: 10, Hash: "old-c", URL: "https://example.invalid/c", Title: "移除丙", Content: "<p>归档</p>", Status: "removed"}},
		{Kind: "trailer", Counts: &Counts{Categories: 1, Feeds: 1, Articles: 3, Read: 1, Favorite: 1, Removed: 1, WithContent: 3, EstimatedFirstSeen: 1}},
	}
	for _, v := range records {
		if err := enc.Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	return b.Bytes()
}
func newService(t *testing.T, now *time.Time) (*app.Service, *store.Store) {
	t.Helper()
	s, e := store.Open(context.Background(), filepath.Join(t.TempDir(), "reader.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return app.New(s, func() time.Time { return *now }), s
}

func TestPreviewRejectsIncompleteAndInconsistentSnapshots(t *testing.T) {
	full := syntheticSnapshot(t, false)
	r, e := Preview(bytes.NewReader(full))
	if e != nil || !r.Complete || r.Counts.Articles != 3 {
		t.Fatalf("preview: %+v %v", r, e)
	}
	for _, bad := range [][]byte{full[:bytes.LastIndex(full, []byte(`{"kind":"trailer"`))], bytes.Replace(full, []byte(`"articles":3`), []byte(`"articles":4`), 1), bytes.Replace(full, []byte(`"feed_id":10`), []byte(`"feed_id":99`), 1), append(append([]byte{}, full...), []byte("{}\n")...)} {
		if _, e := Preview(bytes.NewReader(bad)); e == nil {
			t.Fatal("invalid snapshot accepted")
		}
	}
}
func TestImportReconcilesRetriesAndProtection(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	svc, db := newService(t, &now)
	data := syntheticSnapshot(t, false)
	report, e := Import(ctx, bytes.NewReader(data), svc, ImportOptions{})
	if e != nil {
		t.Fatal(e)
	}
	if report.Counts.Read != 1 || report.Counts.Favorite != 1 {
		t.Fatal(report)
	}
	for _, check := range []struct {
		query string
		want  int
	}{{`SELECT count(*) FROM categories`, 1}, {`SELECT count(*) FROM feeds`, 1}, {`SELECT count(*) FROM articles WHERE read_at IS NOT NULL`, 1}, {`SELECT count(*) FROM articles WHERE favorite=1`, 1}, {`SELECT count(*) FROM articles WHERE removed=1`, 1}, {`SELECT count(*) FROM imports`, 5}} {
		var actual int
		if e = db.Read.QueryRow(check.query).Scan(&actual); e != nil || actual != check.want {
			t.Fatalf("count reconciliation %q: %d want %d (%v)", check.query, actual, check.want, e)
		}
	}
	var originalID string
	if e = db.Read.QueryRow(`SELECT target_id FROM imports WHERE entity_type='article' AND source_id='100'`).Scan(&originalID); e != nil {
		t.Fatal(e)
	}
	original, e := svc.GetArticle(ctx, originalID)
	if e != nil {
		t.Fatal(e)
	}
	if original.BodyState != "available" || !original.ReadAtUnknown || len(original.Tags) != 1 || original.ProtectionUntil == nil || !original.Hidden && original.ReadAt == nil {
		t.Fatalf("import fields: %+v", original)
	}
	if strings.Contains(original.Content[0].HTML, "script") {
		t.Fatal("import content was not sanitized")
	}
	importedAt := *original.ImportedAt
	protection := *original.ProtectionUntil
	now = now.AddDate(0, 0, 29)
	if _, e = Import(ctx, bytes.NewReader(data), svc, ImportOptions{}); e != nil {
		t.Fatal(e)
	}
	var n int
	if e = db.Read.QueryRow(`SELECT count(*) FROM articles`).Scan(&n); e != nil || n != 3 {
		t.Fatalf("duplicate import: %d %v", n, e)
	}
	same, e := svc.GetArticle(ctx, originalID)
	if e != nil {
		t.Fatal(e)
	}
	if !same.ImportedAt.Equal(importedAt) || !same.ProtectionUntil.Equal(protection) {
		t.Fatal("retry extended protection")
	}
	if n, e = svc.Cleanup(ctx, 100); e != nil || n != 0 {
		t.Fatalf("protected cleanup: %d %v", n, e)
	}
	// A normal refetch matches the imported URL even if its GUID was unavailable.
	ids, e := svc.IngestFeed(ctx, original.FeedID, []app.FeedEntry{{GUID: "new-guid", URL: original.URL, Title: original.Title, HTML: "<p>更新正文</p>"}})
	if e != nil || len(ids) != 1 || ids[0] != original.ID {
		t.Fatalf("refetch dedup: %v %v", ids, e)
	}
	now = now.AddDate(0, 0, 2)
	if n, e = svc.Cleanup(ctx, 100); e != nil || n == 0 {
		t.Fatalf("expired cleanup: %d %v", n, e)
	}
	expired, e := svc.GetArticle(ctx, originalID)
	if e != nil {
		t.Fatal(e)
	}
	if expired.BodyState != "purged" {
		t.Fatalf("body state %s", expired.BodyState)
	}
	var starredID string
	db.Read.QueryRow(`SELECT target_id FROM imports WHERE entity_type='article' AND source_id='101'`).Scan(&starredID)
	starred, e := svc.GetArticle(ctx, starredID)
	if e != nil || starred.BodyState != "available" {
		t.Fatalf("favorite protection: %+v %v", starred, e)
	}
}
func TestPreviewBeforeWritesAndUnsupportedReview(t *testing.T) {
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	svc, db := newService(t, &now)
	full := syntheticSnapshot(t, true)
	if _, e := Import(context.Background(), bytes.NewReader(full), svc, ImportOptions{}); e == nil {
		t.Fatal("unsupported config silently ignored")
	}
	if _, e := Import(context.Background(), bytes.NewReader(full[:len(full)/2]), svc, ImportOptions{AllowUnsupported: true}); e == nil {
		t.Fatal("truncated import accepted")
	}
	var n int
	db.Read.QueryRow(`SELECT count(*) FROM categories`).Scan(&n)
	if n != 0 {
		t.Fatal("preflight refusal wrote data")
	}
	if _, e := Import(context.Background(), bytes.NewReader(full), svc, ImportOptions{AllowUnsupported: true}); e != nil {
		t.Fatal(e)
	}
	var paused bool
	db.Read.QueryRow(`SELECT paused FROM feeds`).Scan(&paused)
	if !paused {
		t.Fatal("explicit disabled state lost")
	}
}

func TestImportReportsDistinctSourceIDsCollapsingToOneArticle(t *testing.T) {
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	svc, _ := newService(t, &now)
	data := bytes.Replace(syntheticSnapshot(t, false), []byte(`https://example.invalid/c`), []byte(`https://example.invalid/a`), 1)
	if _, err := Import(context.Background(), bytes.NewReader(data), svc, ImportOptions{}); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("identity collapse was not reported: %v", err)
	}
}
func TestBackupRestoreCleansExpiredContentAndRotates(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	svc, _ := newService(t, &now)
	if _, e := Import(ctx, bytes.NewReader(syntheticSnapshot(t, false)), svc, ImportOptions{}); e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	var latest string
	for i := 0; i < 9; i++ {
		var e error
		latest, e = Backup(ctx, svc, dir, now.Add(time.Duration(i)*time.Second))
		if e != nil {
			t.Fatal(e)
		}
	}
	files, e := filepath.Glob(filepath.Join(dir, "reader-*.db"))
	if e != nil || len(files) != 7 {
		t.Fatalf("rotation %v %v", files, e)
	}
	for _, path := range files {
		info, e := os.Stat(path)
		if e != nil || info.Mode().Perm()&0077 != 0 {
			t.Fatalf("private backup permissions: %v %v", info, e)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = Backup(canceled, svc, dir, now.Add(time.Hour)); e == nil {
		t.Fatal("canceled backup succeeded")
	}
	after, _ := filepath.Glob(filepath.Join(dir, "reader-*.db"))
	if len(after) != 7 {
		t.Fatal("failed backup rotated successful copies")
	}
	destination := filepath.Join(t.TempDir(), "restored?#.db")
	future := now.AddDate(0, 0, 200)
	if e = Restore(ctx, latest, destination, future); e != nil {
		t.Fatal(e)
	}
	restored, e := store.Open(ctx, destination)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	restoredSvc := app.New(restored, func() time.Time { return future })
	var total, bodies int
	restored.Read.QueryRow(`SELECT count(*) FROM articles`).Scan(&total)
	restored.Read.QueryRow(`SELECT count(*) FROM article_content WHERE state='available'`).Scan(&bodies)
	if total != 3 || bodies != 1 {
		t.Fatalf("restore counts articles=%d available=%d", total, bodies)
	}
	if e = restoredSvc.Integrity(ctx); e != nil {
		t.Fatal(e)
	}
	if e = Restore(ctx, latest, destination, future); e == nil {
		t.Fatal("restore overwrote destination")
	}
	if e = os.WriteFile(filepath.Join(dir, "corrupt.db"), []byte("bad data"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = Restore(ctx, filepath.Join(dir, "corrupt.db"), filepath.Join(t.TempDir(), "bad.db"), future); e == nil {
		t.Fatal("corrupt backup accepted")
	}
	otherPath := filepath.Join(t.TempDir(), "other.db")
	other, e := sql.Open("sqlite", "file:"+otherPath)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = other.Exec(`CREATE TABLE unrelated(id INTEGER)`); e != nil {
		t.Fatal(e)
	}
	other.Close()
	if e = Restore(ctx, otherPath, filepath.Join(t.TempDir(), "unexpected.db"), future); e == nil {
		t.Fatal("unrelated SQLite file accepted as application backup")
	}
	futurePath := filepath.Join(t.TempDir(), "future.db")
	contents, e := os.ReadFile(latest)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(futurePath, contents, 0600); e != nil {
		t.Fatal(e)
	}
	other, e = sql.Open("sqlite", "file:"+futurePath)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = other.Exec(`UPDATE goose_db_version SET version_id=999 WHERE version_id>0`); e != nil {
		t.Fatal(e)
	}
	other.Close()
	if e = Restore(ctx, futurePath, filepath.Join(t.TempDir(), "future-restored.db"), future); e == nil {
		t.Fatal("future schema accepted")
	}
}

func TestBackupRestoreWithRelativePaths(t *testing.T) {
	t.Chdir(t.TempDir())
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	svc, _ := newService(t, &now)
	if _, err := Import(ctx, bytes.NewReader(syntheticSnapshot(t, false)), svc, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	backup, err := Backup(ctx, svc, filepath.Join("backups?#", "pre-sample"), now)
	if err != nil {
		t.Fatalf("relative backup: %v", err)
	}
	if filepath.IsAbs(backup) {
		t.Fatalf("backup changed the caller's relative output path: %s", backup)
	}
	destination := filepath.Join("restored?#", "reader.db")
	if err := Restore(ctx, backup, destination, now); err != nil {
		t.Fatalf("relative restore: %v", err)
	}
	restored, err := store.Open(ctx, destination)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var articles int
	if err := restored.Read.QueryRowContext(ctx, `SELECT count(*) FROM articles`).Scan(&articles); err != nil {
		t.Fatal(err)
	}
	if articles != 3 {
		t.Fatalf("restored article count = %d, want 3", articles)
	}
}

func TestSchemaProbeRejectsMissingRequiredColumns(t *testing.T) {
	cols := schema{}
	for table, fields := range required {
		cols[table] = map[string]bool{}
		for _, f := range fields {
			cols[table][f] = true
		}
	}
	missing, e := validateSchema(cols)
	if e != nil || len(missing) == 0 {
		t.Fatalf("optional fallback: %v %v", missing, e)
	}
	delete(cols["entries"], "hash")
	if _, e = validateSchema(cols); e == nil || !strings.Contains(e.Error(), "entries.hash") {
		t.Fatal("missing required hash accepted")
	}
}
