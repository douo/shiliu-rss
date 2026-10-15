package store

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
)

// migrate runs only while the caller owns the application's process flock.
// Every upgrade of an existing version first produces a verified private copy;
// an unknown future schema is rejected before invoking goose.
func migrate(ctx context.Context, db *sql.DB, path string, files fs.FS) error {
	latest, e := latestVersion(files)
	if e != nil {
		return e
	}
	current, existing, e := schemaVersion(ctx, db)
	if e != nil {
		return e
	}
	if current > latest {
		return fmt.Errorf("database schema %d is newer than supported schema %d", current, latest)
	}
	if existing && current < latest {
		if e = preMigrationBackup(ctx, db, path, current); e != nil {
			return fmt.Errorf("pre-migration backup: %w", e)
		}
	}
	provider, e := goose.NewProvider(goose.DialectSQLite3, db, files)
	if e != nil {
		return e
	}
	_, e = provider.Up(ctx)
	return e
}
func latestVersion(files fs.FS) (int64, error) {
	names, e := fs.Glob(files, "*.sql")
	if e != nil {
		return 0, e
	}
	var latest int64
	for _, name := range names {
		prefix, _, ok := strings.Cut(filepath.Base(name), "_")
		if !ok {
			continue
		}
		n, e := strconv.ParseInt(prefix, 10, 64)
		if e != nil {
			return 0, e
		}
		if n > latest {
			latest = n
		}
	}
	if latest == 0 {
		return 0, fmt.Errorf("no versioned schema migrations")
	}
	return latest, nil
}
func schemaVersion(ctx context.Context, db *sql.DB) (int64, bool, error) {
	var exists bool
	if e := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='goose_db_version')`).Scan(&exists); e != nil {
		return 0, false, e
	}
	if !exists {
		var tables int
		if e := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&tables); e != nil {
			return 0, false, e
		}
		if tables > 0 {
			return 0, false, fmt.Errorf("refusing unversioned database with existing tables")
		}
		return 0, false, nil
	}
	rows, e := db.QueryContext(ctx, `SELECT version_id,is_applied FROM goose_db_version ORDER BY id DESC`)
	if e != nil {
		return 0, true, e
	}
	defer rows.Close()
	seen := map[int64]bool{}
	for rows.Next() {
		var version int64
		var applied bool
		if e = rows.Scan(&version, &applied); e != nil {
			return 0, true, e
		}
		if seen[version] {
			continue
		}
		seen[version] = true
		if applied {
			return version, version > 0, nil
		}
	}
	if e = rows.Err(); e != nil {
		return 0, true, e
	}
	return 0, false, fmt.Errorf("schema history has no applied baseline")
}
func preMigrationBackup(ctx context.Context, db *sql.DB, path string, version int64) error {
	dir := path + ".pre-migration"
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	if e := os.Chmod(dir, 0700); e != nil {
		return e
	}
	tmp, e := os.CreateTemp(dir, ".pending-*.db")
	if e != nil {
		return e
	}
	pending := tmp.Name()
	if e = tmp.Close(); e != nil {
		os.Remove(pending)
		return e
	}
	if e = os.Remove(pending); e != nil {
		return e
	}
	defer os.Remove(pending)
	if _, e = db.ExecContext(ctx, `VACUUM INTO ?`, pending); e != nil {
		return e
	}
	if e = os.Chmod(pending, 0600); e != nil {
		return e
	}
	if e = verifyBackup(ctx, pending); e != nil {
		return e
	}
	file, e := os.OpenFile(pending, os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	e = file.Sync()
	closeErr := file.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	destination := filepath.Join(dir, fmt.Sprintf("schema-%05d-%s-%s.db", version, time.Now().UTC().Format("20060102T150405.000000000Z"), strings.TrimSuffix(strings.TrimPrefix(filepath.Base(pending), ".pending-"), ".db")))
	if e = os.Rename(pending, destination); e != nil {
		return e
	}
	d, e := os.Open(dir)
	if e != nil {
		return e
	}
	e = d.Sync()
	closeErr = d.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	backups, e := filepath.Glob(filepath.Join(dir, "schema-*.db"))
	if e != nil {
		return e
	}
	sort.Strings(backups)
	for len(backups) > 7 {
		if e = os.Remove(backups[0]); e != nil {
			return e
		}
		backups = backups[1:]
	}
	return nil
}
func verifyBackup(ctx context.Context, path string) error {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=ro"}
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return e
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var result string
	if e = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result); e != nil {
		return e
	}
	if result != "ok" {
		return fmt.Errorf("backup integrity check failed")
	}
	return nil
}
