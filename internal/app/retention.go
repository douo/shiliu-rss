package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const expiredContent = `c.state='available' AND a.favorite=0 AND f.archive=0 AND max(c.expires_at,COALESCE(a.protection_until,''))<=?`

func (s *Service) RetentionPreview(ctx context.Context, overrides ...Feed) (RetentionPreview, error) {
	var out RetentionPreview
	var query string
	args := []any{}
	if len(overrides) > 1 {
		return out, ErrInvalid
	}
	if len(overrides) == 1 {
		f := overrides[0]
		if f.ID == "" {
			return out, ErrInvalid
		}
		query = `SELECT count(DISTINCT a.id),count(*),COALESCE(sum(length(c.html)+length(c.text)),0) FROM article_content c JOIN articles a ON a.id=c.article_id JOIN feeds f ON f.id=a.feed_id WHERE f.id=? AND c.state='available' AND a.favorite=0 AND ?=0 AND max(c.expires_at,COALESCE(a.protection_until,''))<=?`
		args = append(args, f.ID, f.Archive, stamp(s.Now()))
	} else {
		query = `SELECT count(DISTINCT a.id),count(*),COALESCE(sum(length(c.html)+length(c.text)),0) FROM article_content c JOIN articles a ON a.id=c.article_id JOIN feeds f ON f.id=a.feed_id WHERE ` + expiredContent
		args = append(args, stamp(s.Now()))
	}
	e := s.Store.Read.QueryRowContext(ctx, query, args...).Scan(&out.Articles, &out.Contents, &out.Bytes)
	return out, e
}

// Cleanup processes a bounded batch. Call until zero at restore/startup, then
// daily; read and search paths already enforce the same deadlines immediately.
func (s *Service) Cleanup(ctx context.Context, limit int) (int, error) {
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 1000 {
		return 0, ErrInvalid
	}
	count := 0
	e := s.write(ctx, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT DISTINCT a.id FROM article_content c JOIN articles a ON a.id=c.article_id JOIN feeds f ON f.id=a.feed_id WHERE `+expiredContent+` ORDER BY a.id LIMIT ?`, stamp(s.Now()), limit)
		if e != nil {
			return e
		}
		ids := []string{}
		for rows.Next() {
			var articleID string
			if e = rows.Scan(&articleID); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, articleID)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, articleID := range ids {
			if _, e = tx.ExecContext(ctx, `UPDATE article_content SET html='',text='',state='purged' WHERE article_id=? AND state='available' AND max(expires_at,COALESCE((SELECT protection_until FROM articles WHERE id=?),''))<=?`, articleID, articleID, stamp(s.Now())); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE articles SET version=version+1 WHERE id=?`, articleID); e != nil {
				return e
			}
			if e = s.indexArticle(ctx, tx, articleID); e != nil {
				return e
			}
			var version int64
			if e = tx.QueryRowContext(ctx, `SELECT version FROM articles WHERE id=?`, articleID).Scan(&version); e != nil {
				return e
			}
			if e = s.change(ctx, tx, "article", articleID, "content_purged", version); e != nil {
				return e
			}
			count++
		}
		return nil
	})
	return count, e
}
func (s *Service) BulkRead(ctx context.Context, ids []string, read bool) error {
	if len(ids) < 1 || len(ids) > 200 {
		return ErrInvalid
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		seen := map[string]bool{}
		for _, articleID := range ids {
			if seen[articleID] {
				continue
			}
			seen[articleID] = true
			var at any
			if read {
				at = stamp(s.Now())
			}
			r, e := tx.ExecContext(ctx, `UPDATE articles SET read_at=?,read_at_unknown=0,version=version+1 WHERE id=?`, at, articleID)
			if e = affected(r, e); e != nil {
				return e
			}
			var version int64
			if e = tx.QueryRowContext(ctx, `SELECT version FROM articles WHERE id=?`, articleID).Scan(&version); e != nil {
				return e
			}
			if e = s.change(ctx, tx, "article", articleID, "updated", version); e != nil {
				return e
			}
		}
		return nil
	})
}

// RebuildSearch removes expired source content first, then reconstructs only
// searchable authoritative data. It never recovers data from backups/caches.
func (s *Service) RebuildSearch(ctx context.Context) error {
	for {
		n, e := s.Cleanup(ctx, 100)
		if e != nil {
			return e
		}
		if n == 0 {
			break
		}
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT id FROM articles ORDER BY id`)
		if e != nil {
			return e
		}
		ids := []string{}
		for rows.Next() {
			var articleID string
			if e = rows.Scan(&articleID); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, articleID)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, articleID := range ids {
			if e = s.indexArticle(ctx, tx, articleID); e != nil {
				return e
			}
		}
		return nil
	})
}
func (s *Service) Stats(ctx context.Context) (map[string]int64, error) {
	out := map[string]int64{}
	for _, table := range []string{"feeds", "categories", "articles", "artifacts", "jobs"} {
		var n int64
		if e := s.Store.Read.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&n); e != nil {
			return nil, e
		}
		out[table] = n
	}
	for key, where := range map[string]string{"read": "read_at IS NOT NULL", "favorites": "favorite=1", "purged": "id IN (SELECT article_id FROM article_content WHERE state='purged')"} {
		var n int64
		if e := s.Store.Read.QueryRowContext(ctx, `SELECT count(*) FROM articles WHERE `+strings.TrimSpace(where)).Scan(&n); e != nil {
			return nil, e
		}
		out[key] = n
	}
	return out, nil
}
