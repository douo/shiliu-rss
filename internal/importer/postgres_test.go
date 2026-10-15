package importer

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// This test starts only an isolated local PostgreSQL cluster over a Unix socket.
// Set SHILIU_TEST_POSTGRES_BIN to a directory containing initdb and pg_ctl.
func temporaryPostgres(t *testing.T) *sql.DB {
	t.Helper()
	bin := os.Getenv("SHILIU_TEST_POSTGRES_BIN")
	if bin == "" {
		if p, e := exec.LookPath("initdb"); e == nil {
			bin = filepath.Dir(p)
		} else if _, e = os.Stat("/opt/homebrew/opt/postgresql@18/bin/initdb"); e == nil {
			bin = "/opt/homebrew/opt/postgresql@18/bin"
		} else {
			t.Skip("local PostgreSQL tools unavailable; set SHILIU_TEST_POSTGRES_BIN")
		}
	}
	root, e := os.MkdirTemp("", "shiliu-pg-")
	if e != nil {
		t.Fatal(e)
	}
	// A short /tmp path also avoids macOS's 104-byte Unix socket path limit.
	if len(root) > 55 {
		os.RemoveAll(root)
		root, e = os.MkdirTemp("/tmp", "shiliu-pg-")
		if e != nil {
			t.Fatal(e)
		}
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	data := filepath.Join(root, "data")
	socket := filepath.Join(root, "socket")
	if e = os.Mkdir(socket, 0700); e != nil {
		t.Fatal(e)
	}
	run := func(args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		out, e := exec.CommandContext(ctx, filepath.Join(bin, args[0]), args[1:]...).CombinedOutput()
		if e != nil {
			t.Fatalf("local postgres %s failed: %v\n%s", args[0], e, out)
		}
	}
	run("initdb", "-D", data, "-A", "trust", "-U", "synthetic", "--no-locale", "-E", "UTF8", "-N")
	run("pg_ctl", "-D", data, "-l", filepath.Join(root, "postgres.log"), "-o", "-k "+socket+" -h '' -p 5432 -c max_connections=8 -c shared_buffers=16MB", "-w", "-t", "20", "start")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if out, e := exec.CommandContext(ctx, filepath.Join(bin, "pg_ctl"), "-D", data, "-m", "fast", "-w", "-t", "15", "stop").CombinedOutput(); e != nil {
			t.Errorf("stop local PostgreSQL: %v %s", e, out)
		}
	})
	db, e := sql.Open("pgx", "host="+socket+" port=5432 user=synthetic dbname=postgres sslmode=disable")
	if e != nil {
		t.Fatal(e)
	}
	db.SetMaxOpenConns(2)
	t.Cleanup(func() { db.Close() })
	return db
}

type onFirstWrite struct {
	bytes.Buffer
	once sync.Once
	fn   func()
}

func (w *onFirstWrite) Write(p []byte) (int, error) { w.once.Do(w.fn); return w.Buffer.Write(p) }

func TestPostgresSnapshotSchemaConsistencyAndPrivacy(t *testing.T) {
	db := temporaryPostgres(t)
	ctx := context.Background()
	fixture := `CREATE TABLE schema_version(version text not null); INSERT INTO schema_version VALUES('synthetic-2.3.0-shape');
 CREATE TABLE categories(id bigint,user_id bigint,title text,hide_globally boolean DEFAULT false);
 CREATE TABLE feeds(id bigint,user_id bigint,category_id bigint,title text,feed_url text,site_url text,disabled boolean,crawler boolean,password text,parsing_error_count int);
 CREATE TABLE entries(id bigint,user_id bigint,feed_id bigint,hash text,published_at timestamptz,title text,url text,author text,content text,status text,starred boolean,created_at timestamptz,tags text[]);
 INSERT INTO categories VALUES(1,1,'合成分类',false),(2,2,'他人分类',false);
 INSERT INTO feeds VALUES(10,1,1,'合成源','https://example.invalid/rss','https://example.invalid',false,true,'SECRET-NOT-EXPORTED',100);
 INSERT INTO entries VALUES(100,1,10,'old-hash',now(),'音乐文章','https://example.invalid/a','作者','<p>合成正文</p>','read',true,now()-interval '100 days',ARRAY['音乐']);`
	if _, e := db.ExecContext(ctx, fixture); e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	out := &onFirstWrite{fn: func() {
		_, e := db.ExecContext(ctx, `INSERT INTO entries VALUES(101,1,10,'late-hash',now(),'并发新增','https://example.invalid/b','','late body','unread',false,now(),'{}')`)
		if e != nil {
			t.Error(e)
		}
	}}
	if e := ExportPostgres(ctx, db, out, ExportOptions{SourceInstance: "synthetic", SourceVersion: "2.3.0", UserID: 1, Now: func() time.Time { return now }}); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out.String(), "SECRET-NOT-EXPORTED") || strings.Contains(out.String(), "late-hash") || strings.Contains(out.String(), "他人分类") {
		t.Fatal("snapshot leaked secret, concurrent insert or another user's data")
	}
	report, e := Preview(bytes.NewReader(out.Bytes()))
	if e != nil {
		t.Fatal(e)
	}
	if report.Counts.Articles != 1 || report.Counts.Read != 1 || report.Counts.Favorite != 1 || report.Counts.Categories != 1 {
		t.Fatalf("snapshot counts %+v", report.Counts)
	}
	found := false
	for _, issue := range report.Issues {
		if issue.Code == "unsupported_configuration" && strings.Contains(strings.Join(issue.Fields, ","), "password") {
			found = true
		}
	}
	if !found {
		t.Fatal("credential incompatibility not reported")
	}
	svc, _ := newService(t, &now)
	if _, e = Import(ctx, bytes.NewReader(out.Bytes()), svc, ImportOptions{AllowUnsupported: true}); e != nil {
		t.Fatal(e)
	}
	// An older/incomplete schema is probed rather than assumed. Missing created_at
	// uses the fixed first import timestamp and is declared in the preview.
	if _, e = db.ExecContext(ctx, `ALTER TABLE entries DROP COLUMN created_at; ALTER TABLE entries DROP COLUMN tags`); e != nil {
		t.Fatal(e)
	}
	var fallback bytes.Buffer
	if e = ExportPostgres(ctx, db, &fallback, ExportOptions{SourceInstance: "synthetic", SourceVersion: "2.3.0", UserID: 1}); e != nil {
		t.Fatal(e)
	}
	report, e = Preview(bytes.NewReader(fallback.Bytes()))
	if e != nil || len(report.Header.Missing) != 2 || report.Counts.EstimatedFirstSeen != 2 {
		t.Fatalf("fallback %+v %v", report, e)
	}
	if _, e = db.ExecContext(ctx, `ALTER TABLE entries DROP COLUMN hash`); e != nil {
		t.Fatal(e)
	}
	fallback.Reset()
	if e = ExportPostgres(ctx, db, &fallback, ExportOptions{SourceInstance: "synthetic", SourceVersion: "2.3.0", UserID: 1}); e == nil || !strings.Contains(e.Error(), "entries.hash") || fallback.Len() != 0 {
		t.Fatalf("bad schema accepted: %v", e)
	}
}
