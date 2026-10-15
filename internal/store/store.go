package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
	"shiliu-rss/migrations"
)

type Store struct{ Read, Write *sql.DB }

var migrationMu sync.Mutex

// Open configures every connection through DSN pragmas. Callers hold the process
// lock before Open so schema upgrades cannot race a second running instance.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsnURL := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute), RawQuery: url.Values{"_pragma": {"foreign_keys(1)", "busy_timeout(5000)", "synchronous(FULL)", "journal_mode(WAL)", "cache_size(-8192)"}}.Encode()}
	dsn := dsnURL.String()
	w, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	w.SetMaxIdleConns(1)
	closeErr := func(e error) (*Store, error) { w.Close(); return nil, e }
	if err = w.PingContext(ctx); err != nil {
		return closeErr(err)
	}
	if err = os.Chmod(path, 0600); err != nil {
		return closeErr(err)
	}
	migrationMu.Lock()
	goose.SetBaseFS(migrations.Files)
	err = goose.SetDialect("sqlite3")
	if err == nil {
		err = goose.UpContext(ctx, w, ".")
	}
	migrationMu.Unlock()
	if err != nil {
		return closeErr(fmt.Errorf("migrate: %w", err))
	}
	r, err := sql.Open("sqlite", dsn)
	if err != nil {
		return closeErr(err)
	}
	r.SetMaxOpenConns(2)
	r.SetMaxIdleConns(2)
	if err = r.PingContext(ctx); err != nil {
		r.Close()
		return closeErr(err)
	}
	return &Store{Read: r, Write: w}, nil
}
func (s *Store) Close() error {
	a := s.Read.Close()
	b := s.Write.Close()
	if a != nil {
		return a
	}
	return b
}
