package app

import (
	"context"
	"database/sql"
	"strings"
)

const feedCols = `id,url,title,site_url,COALESCE(category_id,''),paused,archive,interval_hours,read_days,unread_days,etag,last_modified,allow_host,last_attempt_at,last_success_at,next_check_at,error,version`

type scanner interface{ Scan(...any) error }

func scanFeed(row scanner) (Feed, error) {
	var f Feed
	var attempt, success sql.NullString
	var next string
	e := row.Scan(&f.ID, &f.URL, &f.Title, &f.SiteURL, &f.CategoryID, &f.Paused, &f.Archive, &f.IntervalHours, &f.ReadDays, &f.UnreadDays, &f.ETag, &f.LastModified, &f.AllowHost, &attempt, &success, &next, &f.Error, &f.Version)
	f.LastAttemptAt = ptrTime(attempt)
	f.LastSuccessAt = ptrTime(success)
	f.NextCheckAt = parse(next)
	if e == sql.ErrNoRows {
		e = ErrNotFound
	}
	return f, e
}
func (s *Service) ListFeeds(ctx context.Context) ([]Feed, error) {
	rows, e := s.Store.Read.QueryContext(ctx, `SELECT `+feedCols+` FROM feeds ORDER BY title,id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Feed{}
	for rows.Next() {
		f, e := scanFeed(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
func (s *Service) GetFeed(ctx context.Context, feedID string) (Feed, error) {
	return scanFeed(s.Store.Read.QueryRowContext(ctx, `SELECT `+feedCols+` FROM feeds WHERE id=?`, feedID))
}
func defaultFeed(f *Feed) error {
	f.Title = strings.TrimSpace(f.Title)
	if f.Title == "" {
		f.Title = "未命名订阅"
	}
	if !validURL(f.URL) {
		return ErrInvalid
	}
	if f.IntervalHours == 0 {
		f.IntervalHours = 5
	}
	if f.ReadDays == 0 {
		f.ReadDays = 7
	}
	if f.UnreadDays == 0 {
		f.UnreadDays = 30
	}
	if f.IntervalHours < 1 || f.IntervalHours > 168 || f.ReadDays < 1 || f.UnreadDays < 1 || f.ReadDays > 3650 || f.UnreadDays > 3650 {
		return ErrInvalid
	}
	return nil
}
func (s *Service) createFeed(ctx context.Context, tx *sql.Tx, f Feed) (Feed, error) {
	if e := defaultFeed(&f); e != nil {
		return f, e
	}
	f.URL = normalizeURL(f.URL)
	f.NextCheckAt = s.Now()
	r, e := tx.ExecContext(ctx, `INSERT INTO feeds(url,title,site_url,category_id,paused,archive,interval_hours,read_days,unread_days,next_check_at,allow_host) VALUES(?,?,?,NULLIF(?,''),?,?,?,?,?,?,?)`, f.URL, f.Title, f.SiteURL, f.CategoryID, f.Paused, f.Archive, f.IntervalHours, f.ReadDays, f.UnreadDays, stamp(f.NextCheckAt), f.AllowHost)
	if e != nil {
		return f, e
	}
	n, e := r.LastInsertId()
	f.ID = id(n)
	f.Version = 1
	if e != nil {
		return f, e
	}
	e = s.change(ctx, tx, "feed", f.ID, "created", 1)
	return f, e
}
func (s *Service) CreateFeed(ctx context.Context, f Feed) (Feed, error) {
	e := s.write(ctx, func(tx *sql.Tx) error { var e error; f, e = s.createFeed(ctx, tx, f); return e })
	return f, e
}
func (s *Service) UpdateFeed(ctx context.Context, feedID string, version int64, f Feed) (Feed, error) {
	if e := defaultFeed(&f); e != nil {
		return Feed{}, e
	}
	e := s.write(ctx, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, `UPDATE feeds SET url=?,title=?,site_url=?,category_id=NULLIF(?,''),paused=?,archive=?,interval_hours=?,read_days=?,unread_days=?,allow_host=?,version=version+1 WHERE id=? AND version=?`, normalizeURL(f.URL), f.Title, f.SiteURL, f.CategoryID, f.Paused, f.Archive, f.IntervalHours, f.ReadDays, f.UnreadDays, f.AllowHost, feedID, version)
		if e = affected(r, e); e != nil {
			return e
		}
		if f.Paused {
			if _, e = tx.ExecContext(ctx, `UPDATE jobs SET state='failed',error='paused',lease_owner='',lease_until=NULL,updated_at=? WHERE kind='feed' AND target_id=? AND state IN ('queued','running','waiting_retry')`, stamp(s.Now()), feedID); e != nil {
				return e
			}
		}
		return s.change(ctx, tx, "feed", feedID, "updated", version+1)
	})
	if e != nil {
		return Feed{}, e
	}
	return s.GetFeed(ctx, feedID)
}

// DeleteFeed pauses and removes an unused subscription; feeds with historical
// articles are retained to preserve provenance and may instead be paused.
func (s *Service) DeleteFeed(ctx context.Context, feedID string, version int64) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		var n int
		if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM articles WHERE feed_id=?`, feedID).Scan(&n); e != nil {
			return e
		}
		if n > 0 {
			return ErrConflict
		}
		if _, e := tx.ExecContext(ctx, `DELETE FROM jobs WHERE kind='feed' AND target_id=?`, feedID); e != nil {
			return e
		}
		r, e := tx.ExecContext(ctx, `DELETE FROM feeds WHERE id=? AND version=?`, feedID, version)
		if e = affected(r, e); e != nil {
			return e
		}
		return s.change(ctx, tx, "feed", feedID, "deleted", version+1)
	})
}
func (s *Service) ListCategories(ctx context.Context) ([]Category, error) {
	rows, e := s.Store.Read.QueryContext(ctx, `SELECT id,title,version FROM categories ORDER BY title,id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		var c Category
		if e = rows.Scan(&c.ID, &c.Title, &c.Version); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func categoryTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > 100 {
		return "", ErrInvalid
	}
	return title, nil
}
func (s *Service) CreateCategory(ctx context.Context, title string) (Category, error) {
	var c Category
	var err error
	title, err = categoryTitle(title)
	if err != nil {
		return c, err
	}
	e := s.write(ctx, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, `INSERT INTO categories(title)VALUES(?)`, title)
		if e != nil {
			return e
		}
		n, e := r.LastInsertId()
		c = Category{ID: id(n), Title: title, Version: 1}
		if e != nil {
			return e
		}
		return s.change(ctx, tx, "category", c.ID, "created", 1)
	})
	return c, e
}
func (s *Service) UpdateCategory(ctx context.Context, categoryID string, version int64, title string) (Category, error) {
	var err error
	title, err = categoryTitle(title)
	if err != nil {
		return Category{}, err
	}
	c := Category{ID: categoryID, Title: title, Version: version + 1}
	e := s.write(ctx, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, `UPDATE categories SET title=?,version=version+1 WHERE id=? AND version=?`, title, categoryID, version)
		if e = affected(r, e); e != nil {
			return e
		}
		return s.change(ctx, tx, "category", categoryID, "updated", version+1)
	})
	return c, e
}
func (s *Service) DeleteCategory(ctx context.Context, categoryID string, version int64) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		var current int64
		if e := tx.QueryRowContext(ctx, `SELECT version FROM categories WHERE id=?`, categoryID).Scan(&current); e == sql.ErrNoRows {
			return ErrNotFound
		} else if e != nil {
			return e
		}
		if current != version {
			return ErrVersion
		}
		rows, e := tx.QueryContext(ctx, `SELECT id,version FROM feeds WHERE category_id=?`, categoryID)
		if e != nil {
			return e
		}
		feeds := []Feed{}
		for rows.Next() {
			var f Feed
			if e = rows.Scan(&f.ID, &f.Version); e != nil {
				rows.Close()
				return e
			}
			feeds = append(feeds, f)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE feeds SET category_id=NULL,version=version+1 WHERE category_id=?`, categoryID); e != nil {
			return e
		}
		for _, f := range feeds {
			if e = s.change(ctx, tx, "feed", f.ID, "updated", f.Version+1); e != nil {
				return e
			}
		}
		r, e := tx.ExecContext(ctx, `DELETE FROM categories WHERE id=? AND version=?`, categoryID, version)
		if e = affected(r, e); e != nil {
			return e
		}
		return s.change(ctx, tx, "category", categoryID, "deleted", version+1)
	})
}
