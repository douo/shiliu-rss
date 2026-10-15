package importer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"shiliu-rss/internal/app"
	"shiliu-rss/internal/store"
	"shiliu-rss/migrations"
)

// Backup creates and verifies a consistent VACUUM INTO copy, then atomically
// publishes it. Rotation only runs after a successful copy and keeps seven.
func Backup(ctx context.Context, s *app.Service, dir string, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(dir, ".reader-backup-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	tmp := filepath.Join(staging, "backup.db")
	if err = s.Backup(ctx, tmp); err != nil {
		return "", err
	}
	if err = os.Chmod(tmp, 0600); err != nil {
		return "", err
	}
	if err = verifyDatabase(ctx, tmp); err != nil {
		return "", err
	}
	f, err := os.OpenFile(tmp, os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	err = f.Sync()
	f.Close()
	if err != nil {
		return "", err
	}
	target := filepath.Join(dir, "reader-"+now.UTC().Format("20060102T150405.000000000Z")+".db")
	if _, err = os.Stat(target); err == nil {
		return "", errors.New("backup name already exists")
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	if err = os.Link(tmp, target); err != nil {
		return "", err
	}
	if err = syncDir(dir); err != nil {
		return target, err
	}
	candidates, err := filepath.Glob(filepath.Join(dir, "reader-*.db"))
	if err != nil {
		return target, err
	}
	files := []string{}
	for _, p := range candidates {
		if IsBackupName(filepath.Base(p)) {
			files = append(files, p)
		}
	}
	sort.Strings(files)
	for len(files) > 7 {
		old := files[0]
		files = files[1:]
		if err = os.Remove(old); err != nil {
			return target, err
		}
	}
	return target, syncDir(dir)
}
func verifyDatabase(ctx context.Context, path string) error {
	// Read-only checks do not silently migrate an old or corrupt backup.
	// A relative URL.Path is serialized as a file URI authority instead of a
	// local relative filename. Resolve it before encoding reserved characters.
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	dsn := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute), RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result); err != nil {
		return errors.New("backup database cannot be checked")
	}
	if result != "ok" {
		return errors.New("backup integrity check failed")
	}
	var violations int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil {
		return err
	}
	if violations > 0 {
		return errors.New("backup foreign key check failed")
	}
	var version int64
	if err = db.QueryRowContext(ctx, `SELECT version_id FROM goose_db_version WHERE is_applied=1 ORDER BY id DESC LIMIT 1`).Scan(&version); err != nil {
		return errors.New("backup is not a versioned Shiliu database")
	}
	files, err := migrations.Files.ReadDir(".")
	if err != nil {
		return err
	}
	var supported int64
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".sql") {
			v, e := strconv.ParseInt(strings.SplitN(f.Name(), "_", 2)[0], 10, 64)
			if e == nil && v > supported {
				supported = v
			}
		}
	}
	if version < 1 || version > supported {
		return errors.New("backup database version is unsupported by this binary")
	}
	for _, table := range []string{"articles", "feeds", "article_content", "imports", "changes"} {
		var exists int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
			return err
		}
		if exists != 1 {
			return errors.New("backup is missing required Shiliu tables")
		}
	}
	return nil
}
func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// Restore installs a backup only at a new destination. The application must be
// stopped (and its process lock held). Cleanup runs before publication, so an
// older backup cannot re-expose expired source content on first read.
func Restore(ctx context.Context, backup, destination string, now time.Time) error {
	if backup == destination {
		return errors.New("restore requires a new destination")
	}
	if _, err := os.Stat(destination); err == nil {
		return errors.New("restore destination already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := verifyDatabase(ctx, backup); err != nil {
		return err
	}
	dir := filepath.Dir(destination)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(dir, ".reader-restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	f, err := os.CreateTemp(staging, "reader-*.db")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		for _, p := range []string{tmp, tmp + "-wal", tmp + "-shm"} {
			os.Remove(p)
		}
	}()
	in, err := os.Open(backup)
	if err != nil {
		f.Close()
		return err
	}
	_, err = io.Copy(f, in)
	in.Close()
	if err == nil {
		err = f.Sync()
	}
	f.Close()
	if err != nil {
		return err
	}
	db, err := store.Open(ctx, tmp)
	if err != nil {
		return err
	}
	svc := app.New(db, func() time.Time { return now })
	for {
		n, e := svc.Cleanup(ctx, 100)
		if e != nil {
			db.Close()
			return e
		}
		if n == 0 {
			break
		}
	}
	if err = svc.Integrity(ctx); err != nil {
		db.Close()
		return err
	}
	var busy, pages, checkpointed int
	if err = db.Write.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &pages, &checkpointed); err != nil || busy != 0 {
		db.Close()
		if err != nil {
			return err
		}
		return errors.New("restored database checkpoint remained busy")
	}
	if err = db.Close(); err != nil {
		return err
	}
	if err = verifyDatabase(ctx, tmp); err != nil {
		return err
	}
	// Link is an atomic no-replace publication on the local SQLite filesystem.
	if err = os.Link(tmp, destination); err != nil {
		return fmt.Errorf("publish restored database: %w", err)
	}
	if err = os.Remove(tmp); err != nil {
		return err
	}
	return syncDir(dir)
}

// IsBackupName is deliberately strict when inspecting rotation candidates.
func IsBackupName(name string) bool {
	if !strings.HasPrefix(name, "reader-") || !strings.HasSuffix(name, ".db") {
		return false
	}
	_, err := time.Parse("20060102T150405.000000000Z", strings.TrimSuffix(strings.TrimPrefix(name, "reader-"), ".db"))
	return err == nil
}
