package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"
	"shiliu-rss/migrations"
)

func versionOneFixture(t *testing.T, path string) *sql.DB {
	t.Helper()
	body, e := migrations.Files.ReadFile("00001_initial.sql")
	if e != nil {
		t.Fatal(e)
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	db.SetMaxOpenConns(1)
	p, e := goose.NewProvider(goose.DialectSQLite3, db, fstest.MapFS{"00001_initial.sql": &fstest.MapFile{Data: body}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Up(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO categories(title)VALUES('synthetic before upgrade')`); e != nil {
		t.Fatal(e)
	}
	return db
}
func TestUpgradeBacksUpPriorVersionAndKeepsData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reader.db")
	old := versionOneFixture(t, path)
	old.Close()
	s, e := Open(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	version, _, e := schemaVersion(ctx, s.Read)
	if e != nil || version != 4 {
		t.Fatal(version, e)
	}
	var title string
	if e = s.Read.QueryRow(`SELECT title FROM categories`).Scan(&title); e != nil || title != "synthetic before upgrade" {
		t.Fatal(title, e)
	}
	backups, e := filepath.Glob(filepath.Join(path+".pre-migration", "schema-*.db"))
	if e != nil || len(backups) != 1 {
		t.Fatal(backups, e)
	}
	copyDB, e := sql.Open("sqlite", backups[0])
	if e != nil {
		t.Fatal(e)
	}
	defer copyDB.Close()
	version, _, e = schemaVersion(ctx, copyDB)
	if e != nil || version != 1 {
		t.Fatal("backup version", version, e)
	}
	if e = copyDB.QueryRow(`SELECT title FROM categories`).Scan(&title); e != nil || title != "synthetic before upgrade" {
		t.Fatal("backup data", title, e)
	}
	for _, name := range []string{path + ".pre-migration", backups[0]} {
		info, e := os.Stat(name)
		if e != nil {
			t.Fatal(e)
		}
		want := os.FileMode(0600)
		if info.IsDir() {
			want = 0700
		}
		if info.Mode().Perm() != want {
			t.Fatalf("permissions %s %v", name, info.Mode())
		}
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	again, e := Open(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	defer again.Close()
	backups, e = filepath.Glob(filepath.Join(path+".pre-migration", "schema-*.db"))
	if e != nil || len(backups) != 1 {
		t.Fatal("repeat current open made a backup", backups, e)
	}
}
func TestFutureVersionAndBackupFailureRefuseOpen(t *testing.T) {
	ctx := context.Background()
	t.Run("future", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "reader.db")
		db := versionOneFixture(t, path)
		if _, e := db.Exec(`INSERT INTO goose_db_version(version_id,is_applied)VALUES(99,1)`); e != nil {
			t.Fatal(e)
		}
		db.Close()
		if s, e := Open(ctx, path); e == nil {
			s.Close()
			t.Fatal("opened future schema")
		} else if !strings.Contains(e.Error(), "newer than supported") {
			t.Fatal(e)
		}
		if _, e := os.Stat(path + ".pre-migration"); !os.IsNotExist(e) {
			t.Fatal("future schema created upgrade backup", e)
		}
	})
	t.Run("backup fails", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "reader.db")
		db := versionOneFixture(t, path)
		db.Close()
		if e := os.WriteFile(path+".pre-migration", []byte("blocked"), 0600); e != nil {
			t.Fatal(e)
		}
		if s, e := Open(ctx, path); e == nil {
			s.Close()
			t.Fatal("opened without pre-upgrade backup")
		}
		db, e := sql.Open("sqlite", path)
		if e != nil {
			t.Fatal(e)
		}
		defer db.Close()
		version, _, e := schemaVersion(ctx, db)
		if e != nil || version != 1 {
			t.Fatal("migrated despite backup failure", version, e)
		}
	})
	t.Run("fresh", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "reader.db")
		s, e := Open(ctx, path)
		if e != nil {
			t.Fatal(e)
		}
		s.Close()
		if _, e = os.Stat(path + ".pre-migration"); !os.IsNotExist(e) {
			t.Fatal("empty first database was backed up", e)
		}
	})
}
func TestFailedMigrationRollsBackAndLeavesVerifiedBackup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reader.db")
	db := versionOneFixture(t, path)
	defer db.Close()
	body, e := migrations.Files.ReadFile("00001_initial.sql")
	if e != nil {
		t.Fatal(e)
	}
	files := fstest.MapFS{"00001_initial.sql": &fstest.MapFile{Data: body}, "00002_bad.sql": &fstest.MapFile{Data: []byte("-- +goose Up\nCREATE TABLE partial_upgrade(id INTEGER);\nINSERT INTO nonexistent_table VALUES(1);\n-- +goose Down\nDROP TABLE partial_upgrade;\n")}}
	if e = migrate(ctx, db, path, files); e == nil {
		t.Fatal("bad migration succeeded")
	}
	version, _, e := schemaVersion(ctx, db)
	if e != nil || version != 1 {
		t.Fatal(version, e)
	}
	var exists bool
	if e = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE name='partial_upgrade')`).Scan(&exists); e != nil || exists {
		t.Fatal("partial schema committed", exists, e)
	}
	backups, e := filepath.Glob(filepath.Join(path+".pre-migration", "schema-*.db"))
	if e != nil || len(backups) != 1 {
		t.Fatal(backups, e)
	}
	if e = verifyBackup(ctx, backups[0]); e != nil {
		t.Fatal(e)
	}
}
func TestUpgradeBackupRotationKeepsSevenVerifiedCopies(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reader.db")
	db := versionOneFixture(t, path)
	defer db.Close()
	for i := 0; i < 9; i++ {
		if e := preMigrationBackup(ctx, db, path, 1); e != nil {
			t.Fatal(e)
		}
	}
	backups, e := filepath.Glob(filepath.Join(path+".pre-migration", "schema-*.db"))
	if e != nil || len(backups) != 7 {
		t.Fatal(len(backups), e)
	}
	for _, path := range backups {
		if e = verifyBackup(ctx, path); e != nil {
			t.Fatal(e)
		}
	}
}

func TestVersionTwoAgentUpgradePreservesReadingAndDefaultsDisabled(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reader.db")
	db := versionOneFixture(t, path)
	files := fstest.MapFS{}
	for _, name := range []string{"00001_initial.sql", "00002_site_cooldowns.sql"} {
		b, e := migrations.Files.ReadFile(name)
		if e != nil {
			t.Fatal(e)
		}
		files[name] = &fstest.MapFile{Data: b}
	}
	if e := migrate(ctx, db, path, files); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`INSERT INTO feeds(id,url,title,paused,next_check_at)VALUES(1,'https://example.invalid/feed','合成源',1,'2026-01-01T00:00:00Z');INSERT INTO articles(id,feed_id,title,url,first_seen_at,read_at,favorite,note)VALUES(1,1,'合成文章','https://example.invalid/article','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z',1,'合成笔记')`); e != nil {
		t.Fatal(e)
	}
	db.Close()
	st, e := Open(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	var importance, note, read string
	var favorite, configs int
	if e = st.Read.QueryRow(`SELECT f.importance,a.note,a.read_at,a.favorite,(SELECT count(*) FROM agent_settings) FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE a.id=1`).Scan(&importance, &note, &read, &favorite, &configs); e != nil {
		t.Fatal(e)
	}
	if importance != "normal" || note != "合成笔记" || read != "2026-01-02T00:00:00Z" || favorite != 1 || configs != 0 {
		t.Fatal("upgrade changed data", importance, note, read, favorite, configs)
	}
}
