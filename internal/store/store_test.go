package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestEveryConnectionPragmasAndMigrationReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reader.db")
	s, e := Open(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	for _, pool := range []struct {
		name string
		n    int
	}{{"read", 2}, {"write", 1}} {
		db := s.Read
		if pool.name == "write" {
			db = s.Write
		}
		connections := []func(){}
		for i := 0; i < pool.n; i++ {
			c, e := db.Conn(ctx)
			if e != nil {
				t.Fatal(e)
			}
			connections = append(connections, func() { c.Close() })
			for pragma, want := range map[string]int{"foreign_keys": 1, "busy_timeout": 5000, "synchronous": 2} {
				var got int
				if e = c.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); e != nil || got != want {
					t.Fatalf("%s conn %d %s=%d wanted %d: %v", pool.name, i, pragma, got, want, e)
				}
			}
			var journal string
			if e = c.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journal); e != nil || journal != "wal" {
				t.Fatal(journal, e)
			}
		}
		for _, close := range connections {
			close()
		}
	}
	if _, e = s.Write.ExecContext(ctx, `INSERT INTO categories(title)VALUES('synthetic')`); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var n int
	if e = s.Read.QueryRowContext(ctx, `SELECT count(*) FROM categories`).Scan(&n); e != nil || n != 1 {
		t.Fatal(n, e)
	}
}
