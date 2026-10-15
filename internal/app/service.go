package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"shiliu-rss/internal/store"
)

type authorizationKey struct{}

// WithAuthorization binds a transaction-time credential check to a request.
func WithAuthorization(ctx context.Context, check func(context.Context, *sql.Tx) error) context.Context {
	return context.WithValue(ctx, authorizationKey{}, check)
}

type Service struct {
	Store *store.Store
	Now   func() time.Time
}

func New(s *store.Store, clock ...func() time.Time) *Service {
	now := time.Now
	if len(clock) > 0 && clock[0] != nil {
		now = clock[0]
	}
	return &Service{Store: s, Now: now}
}
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }
func parse(s string) time.Time { t, _ := time.Parse(time.RFC3339Nano, s); return t }
func nullable(t *time.Time) any {
	if t == nil {
		return nil
	}
	return stamp(*t)
}
func ptrTime(n sql.NullString) *time.Time {
	if !n.Valid {
		return nil
	}
	t := parse(n.String)
	return &t
}
func id(n int64) string    { return strconv.FormatInt(n, 10) }
func hash(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func normalizeURL(raw string) string {
	u, e := url.Parse(raw)
	if e != nil {
		return raw
	}
	u.Fragment = ""
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String()
}
func validURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}
func (s *Service) write(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, e := s.Store.Write.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if check, ok := ctx.Value(authorizationKey{}).(func(context.Context, *sql.Tx) error); ok {
		if e = check(ctx, tx); e != nil {
			return e
		}
	}
	if e = fn(tx); e != nil {
		return normalizeDBError(e)
	}
	return normalizeDBError(tx.Commit())
}
func (s *Service) change(ctx context.Context, tx *sql.Tx, entity, entityID, kind string, version int64) error {
	_, e := tx.ExecContext(ctx, `INSERT INTO changes(entity,entity_id,kind,version,at) VALUES(?,?,?,?,?)`, entity, entityID, kind, version, stamp(s.Now()))
	return e
}
func affected(r sql.Result, e error) error {
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e == nil && n == 0 {
		return ErrVersion
	}
	return e
}
func jsonTags(v []string) string { b, _ := json.Marshal(v); return string(b) }
func validateTags(tags []string) ([]string, error) {
	if len(tags) > 50 {
		return nil, ErrInvalid
	}
	out := []string{}
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" || len([]rune(t)) > 64 {
			return nil, ErrInvalid
		}
		if !seen[t] {
			out = append(out, t)
			seen[t] = true
		}
	}
	return out, nil
}
func (s *Service) Ping(ctx context.Context) error { return s.Store.Read.PingContext(ctx) }
func (s *Service) Backup(ctx context.Context, path string) error {
	if strings.ContainsRune(path, 0) {
		return ErrInvalid
	}
	_, e := s.Store.Write.ExecContext(ctx, `VACUUM INTO ?`, path)
	return e
}
func (s *Service) Integrity(ctx context.Context) error {
	var result string
	if e := s.Store.Read.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result); e != nil {
		return e
	}
	if result != "ok" {
		return fmt.Errorf("integrity: %s", result)
	}
	return nil
}

func normalizeDBError(err error) error {
	if err == nil {
		return nil
	}
	var coded interface{ Code() int }
	if errors.As(err, &coded) {
		switch coded.Code() {
		case 1555, 2067:
			return ErrConflict
		}
		if coded.Code()&255 == 19 {
			return ErrInvalid
		}
	}
	return err
}
