package app

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const articleCols = `a.id,a.feed_id,f.title,a.title,a.url,a.author,a.published_at,a.first_seen_at,a.read_at,a.favorite,a.removed,a.note,a.tags,a.version,a.imported_at,a.protection_until,a.provenance,a.read_at_unknown,f.archive,f.read_days,f.unread_days`

func scanArticle(row scanner, now time.Time) (Article, bool, error) {
	var a Article
	var published, read, imported, protected sql.NullString
	var seen, tags string
	var archive bool
	var rd, ud int
	e := row.Scan(&a.ID, &a.FeedID, &a.FeedTitle, &a.Title, &a.URL, &a.Author, &published, &seen, &read, &a.Favorite, &a.Removed, &a.Note, &tags, &a.Version, &imported, &protected, &a.Provenance, &a.ReadAtUnknown, &archive, &rd, &ud)
	if e == sql.ErrNoRows {
		e = ErrNotFound
	}
	a.PublishedAt = ptrTime(published)
	a.FirstSeenAt = parse(seen)
	a.ReadAt = ptrTime(read)
	a.ImportedAt = ptrTime(imported)
	a.ProtectionUntil = ptrTime(protected)
	json.Unmarshal([]byte(tags), &a.Tags)
	if a.Tags == nil {
		a.Tags = []string{}
	}
	a.Hidden = a.Removed || (!a.Favorite && ((a.ReadAt == nil && !now.Before(a.FirstSeenAt.AddDate(0, 0, ud))) || (a.ReadAt != nil && !now.Before(a.ReadAt.AddDate(0, 0, rd)))))
	return a, archive, e
}
func (s *Service) loadContent(ctx context.Context, a *Article, archive, body bool) error {
	columns := `kind,html,text,hash,state,obtained_at,expires_at`
	if !body {
		columns = `kind,'','',hash,state,obtained_at,expires_at`
	}
	rows, e := s.Store.Read.QueryContext(ctx, `SELECT `+columns+` FROM article_content WHERE article_id=? ORDER BY kind`, a.ID)
	if e != nil {
		return e
	}
	defer rows.Close()
	a.BodyState = "not_fetched"
	for rows.Next() {
		var c Content
		var obtained, expiry string
		if e = rows.Scan(&c.Kind, &c.HTML, &c.Text, &c.Hash, &c.State, &obtained, &expiry); e != nil {
			return e
		}
		c.ObtainedAt = parse(obtained)
		exp := parse(expiry)
		if a.ProtectionUntil != nil && a.ProtectionUntil.After(exp) {
			exp = *a.ProtectionUntil
		}
		if !a.Favorite && !archive {
			c.ExpiresAt = &exp
		}
		if c.State == "available" && !a.Favorite && !archive && !s.Now().Before(exp) {
			c.State = "purged"
			c.HTML = ""
			c.Text = ""
		}
		if c.State != "available" {
			c.HTML = ""
			c.Text = ""
		}
		if c.State == "available" {
			a.BodyState = "available"
			a.ContentHash = c.Hash
			a.ContentExpiresAt = c.ExpiresAt
		} else if a.BodyState != "available" {
			a.BodyState = "purged"
			a.ContentHash = c.Hash
		}
		if body {
			a.Content = append(a.Content, c)
		}
	}
	return rows.Err()
}
func (s *Service) GetArticle(ctx context.Context, articleID string) (Article, error) {
	a, archive, e := scanArticle(s.Store.Read.QueryRowContext(ctx, `SELECT `+articleCols+` FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE a.id=?`, articleID), s.Now())
	if e != nil {
		return a, e
	}
	if e = s.loadContent(ctx, &a, archive, true); e != nil {
		return a, e
	}
	rows, e := s.Store.Read.QueryContext(ctx, `SELECT id,article_id,actor,external_id,kind,content,basis_hash,created_at,version FROM artifacts WHERE article_id=? ORDER BY id`, articleID)
	if e != nil {
		return a, e
	}
	defer rows.Close()
	a.Artifacts = []Artifact{}
	for rows.Next() {
		var x Artifact
		var at string
		if e = rows.Scan(&x.ID, &x.ArticleID, &x.Actor, &x.ExternalID, &x.Kind, &x.Content, &x.BasisHash, &at, &x.Version); e != nil {
			return a, e
		}
		x.CreatedAt = parse(at)
		a.Artifacts = append(a.Artifacts, x)
	}
	return a, rows.Err()
}
func (s *Service) ListArticles(ctx context.Context, q ArticleQuery) (ArticlePage, error) {
	out := ArticlePage{Items: []Article{}}
	if e := s.Store.Read.QueryRowContext(ctx, `SELECT CAST(COALESCE(max(seq),0) AS TEXT) FROM changes`).Scan(&out.HighWatermark); e != nil {
		return out, e
	}
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Limit < 1 || q.Limit > 200 {
		return out, ErrInvalid
	}
	if q.Scope == "" {
		q.Scope = "recent"
	}
	if q.Scope != "recent" && q.Scope != "archive" && q.Scope != "all" {
		return out, ErrInvalid
	}
	where := []string{"1=1"}
	args := []any{}
	now := stamp(s.Now())
	hidden := `(a.removed=1 OR (a.favorite=0 AND ((a.read_at IS NULL AND julianday(a.first_seen_at)+f.unread_days<=julianday(?)) OR (a.read_at IS NOT NULL AND julianday(a.read_at)+f.read_days<=julianday(?)))))`
	if q.Scope == "recent" {
		where = append(where, "NOT "+hidden)
		args = append(args, now, now)
	} else if q.Scope == "archive" {
		where = append(where, hidden)
		args = append(args, now, now)
	}
	if q.FeedID != "" {
		where = append(where, "a.feed_id=?")
		args = append(args, q.FeedID)
	}
	if q.CategoryID != "" {
		where = append(where, "f.category_id=?")
		args = append(args, q.CategoryID)
	}
	if q.Read != nil {
		if *q.Read {
			where = append(where, "a.read_at IS NOT NULL")
		} else {
			where = append(where, "a.read_at IS NULL AND a.removed=0")
		}
	}
	if q.Favorite != nil {
		where = append(where, "a.favorite=?")
		args = append(args, *q.Favorite)
	}
	if q.From != nil {
		where = append(where, "a.first_seen_at>=?")
		args = append(args, stamp(*q.From))
	}
	if q.Until != nil {
		where = append(where, "a.first_seen_at<?")
		args = append(args, stamp(*q.Until))
	}
	if q.Cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(q.Cursor)
		if e != nil {
			return out, ErrInvalid
		}
		p := strings.Split(string(raw), "|")
		if len(p) != 2 {
			return out, ErrInvalid
		}
		if _, e = strconv.ParseInt(p[1], 10, 64); e != nil {
			return out, ErrInvalid
		}
		if _, e = time.Parse(time.RFC3339Nano, p[0]); e != nil {
			return out, ErrInvalid
		}
		where = append(where, "(a.first_seen_at<? OR (a.first_seen_at=? AND a.id<?))")
		args = append(args, p[0], p[0], p[1])
	}
	if q.Query != "" {
		_, e := searchQuery(q.Query)
		if e != nil {
			return out, e
		}
		// A bounded probe avoids materializing every positional match for a
		// broad query when the page only needs 50 rows. Sparse terms still use
		// an FTS candidate set so they do not scan all article metadata.
		fullMatch, _ := searchQuery(q.Query)
		probe, err := s.Store.Read.QueryContext(ctx, `SELECT rowid FROM search WHERE search MATCH ? LIMIT 201`, fullMatch)
		if err != nil {
			return out, err
		}
		count := 0
		for probe.Next() {
			count++
		}
		err = probe.Err()
		probe.Close()
		if err != nil {
			return out, err
		}
		membership := `a.id IN (SELECT rowid FROM search WHERE search MATCH ?)`
		if count > 200 {
			membership = `EXISTS(SELECT 1 FROM search WHERE rowid=a.id AND search MATCH ?)`
		}
		for _, term := range strings.Fields(q.Query) {
			match, _ := searchQuery(term)
			where = append(where, `(`+membership+` OR (EXISTS(SELECT 1 FROM article_content c WHERE c.article_id=a.id AND c.kind='feed' AND c.state='available' AND (a.favorite=1 OR f.archive=1 OR max(c.expires_at,COALESCE(a.protection_until,''))>?)) AND `+membership+`) OR (EXISTS(SELECT 1 FROM article_content c WHERE c.article_id=a.id AND c.kind='fulltext' AND c.state='available' AND (a.favorite=1 OR f.archive=1 OR max(c.expires_at,COALESCE(a.protection_until,''))>?)) AND `+membership+`))`)
			args = append(args, "{title url organization} : ("+match+")", now, "feed_content : ("+match+")", now, "fulltext : ("+match+")")
		}
	}
	args = append(args, q.Limit+1)
	rows, e := s.Store.Read.QueryContext(ctx, `SELECT `+articleCols+` FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE `+strings.Join(where, " AND ")+` ORDER BY a.first_seen_at DESC,a.id DESC LIMIT ?`, args...)
	if e != nil {
		return out, e
	}
	archives := []bool{}
	for rows.Next() {
		a, archive, e := scanArticle(rows, s.Now())
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Items = append(out.Items, a)
		archives = append(archives, archive)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(out.Items) > q.Limit {
		out.Items = out.Items[:q.Limit]
		last := out.Items[q.Limit-1]
		out.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(stamp(last.FirstSeenAt) + "|" + last.ID))
	}
	for i := range out.Items {
		if e = s.loadContent(ctx, &out.Items[i], archives[i], false); e != nil {
			return out, e
		}
	}
	return out, nil
}
func (s *Service) UpdateArticle(ctx context.Context, articleID string, version int64, p ArticlePatch) (Article, error) {
	if p.Note != nil && len(*p.Note) > 100000 {
		return Article{}, ErrInvalid
	}
	if p.Tags != nil {
		tags, e := validateTags(*p.Tags)
		if e != nil {
			return Article{}, e
		}
		p.Tags = &tags
	}
	e := s.write(ctx, func(tx *sql.Tx) error {
		var current int64
		if e := tx.QueryRowContext(ctx, `SELECT version FROM articles WHERE id=?`, articleID).Scan(&current); e == sql.ErrNoRows {
			return ErrNotFound
		} else if e != nil {
			return e
		}
		if version != current {
			return ErrVersion
		}
		sets := []string{"version=version+1"}
		args := []any{}
		if p.Read != nil {
			sets = append(sets, "read_at=?", "read_at_unknown=0")
			var v any
			if *p.Read {
				v = stamp(s.Now())
			}
			args = append(args, v)
		}
		if p.Favorite != nil {
			sets = append(sets, "favorite=?")
			args = append(args, *p.Favorite)
		}
		if p.Note != nil {
			sets = append(sets, "note=?")
			args = append(args, *p.Note)
		}
		if p.Tags != nil {
			sets = append(sets, "tags=?")
			args = append(args, jsonTags(*p.Tags))
		}
		args = append(args, articleID, version)
		r, e := tx.ExecContext(ctx, `UPDATE articles SET `+strings.Join(sets, ",")+` WHERE id=? AND version=?`, args...)
		if e = affected(r, e); e != nil {
			return e
		}
		if e = s.indexArticle(ctx, tx, articleID); e != nil {
			return e
		}
		return s.change(ctx, tx, "article", articleID, "updated", version+1)
	})
	if e != nil {
		return Article{}, e
	}
	return s.GetArticle(ctx, articleID)
}
func (s *Service) Changes(ctx context.Context, after string, limit int) (ChangePage, error) {
	out := ChangePage{Items: []Change{}, NextCursor: after}
	if after == "" {
		after = "0"
	}
	if _, e := strconv.ParseUint(after, 10, 64); e != nil {
		return out, ErrInvalid
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return out, ErrInvalid
	}
	rows, e := s.Store.Read.QueryContext(ctx, `SELECT seq,entity,entity_id,kind,version,at FROM changes WHERE seq>? ORDER BY seq LIMIT ?`, after, limit)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var c Change
		var at string
		if e = rows.Scan(&c.Seq, &c.Entity, &c.EntityID, &c.Kind, &c.Version, &at); e != nil {
			rows.Close()
			return out, e
		}
		c.At = parse(at)
		out.Items = append(out.Items, c)
		out.NextCursor = c.Seq
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	e = s.Store.Read.QueryRowContext(ctx, `SELECT CAST(COALESCE(max(seq),0) AS TEXT) FROM changes`).Scan(&out.HighWatermark)
	return out, e
}
func (s *Service) PutArtifact(ctx context.Context, a Artifact) (Artifact, error) {
	if a.Actor == "" || a.ExternalID == "" || a.Content == "" || len(a.Content) > 100000 || len(a.ExternalID) > 200 || a.BasisHash == "" {
		return a, ErrInvalid
	}
	if a.Kind == "" {
		a.Kind = "summary"
	}
	e := s.write(ctx, func(tx *sql.Tx) (err error) {
		saved, e := s.idempotentResult(ctx, tx)
		if e != nil {
			return e
		}
		defer func() {
			if err == nil {
				err = s.saveIdempotency(ctx, tx, a.ID)
			}
		}()
		if saved != "" {
			var at string
			e = tx.QueryRowContext(ctx, `SELECT id,article_id,actor,external_id,kind,content,basis_hash,created_at,version FROM artifacts WHERE id=?`, saved).Scan(&a.ID, &a.ArticleID, &a.Actor, &a.ExternalID, &a.Kind, &a.Content, &a.BasisHash, &at, &a.Version)
			a.CreatedAt = parse(at)
			return e
		}
		var old Artifact
		var at string
		e = tx.QueryRowContext(ctx, `SELECT id,article_id,actor,external_id,kind,content,basis_hash,created_at,version FROM artifacts WHERE actor=? AND external_id=?`, a.Actor, a.ExternalID).Scan(&old.ID, &old.ArticleID, &old.Actor, &old.ExternalID, &old.Kind, &old.Content, &old.BasisHash, &at, &old.Version)
		if e == nil {
			if old.ArticleID != a.ArticleID || old.Kind != a.Kind || old.Content != a.Content || old.BasisHash != a.BasisHash {
				return ErrConflict
			}
			old.CreatedAt = parse(at)
			a = old
			return nil
		}
		if e != sql.ErrNoRows {
			return e
		}
		var valid int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM article_content c JOIN articles x ON x.id=c.article_id JOIN feeds f ON f.id=x.feed_id WHERE c.article_id=? AND c.hash=? AND c.state='available' AND (x.favorite=1 OR f.archive=1 OR max(c.expires_at,COALESCE(x.protection_until,''))>?)`, a.ArticleID, a.BasisHash, stamp(s.Now())).Scan(&valid); e != nil {
			return e
		}
		if valid == 0 {
			return fmt.Errorf("%w: content basis changed or expired", ErrConflict)
		}
		a.CreatedAt = s.Now().UTC()
		a.Version = 1
		r, e := tx.ExecContext(ctx, `INSERT INTO artifacts(article_id,actor,external_id,kind,content,basis_hash,created_at)VALUES(?,?,?,?,?,?,?)`, a.ArticleID, a.Actor, a.ExternalID, a.Kind, a.Content, a.BasisHash, stamp(a.CreatedAt))
		if e != nil {
			return e
		}
		n, e := r.LastInsertId()
		if e != nil {
			return e
		}
		a.ID = id(n)
		if _, e = tx.ExecContext(ctx, `UPDATE articles SET version=version+1 WHERE id=?`, a.ArticleID); e != nil {
			return e
		}
		if e = s.indexArticle(ctx, tx, a.ArticleID); e != nil {
			return e
		}
		var version int64
		if e = tx.QueryRowContext(ctx, `SELECT version FROM articles WHERE id=?`, a.ArticleID).Scan(&version); e != nil {
			return e
		}
		return s.change(ctx, tx, "article", a.ArticleID, "artifact", version)
	})
	return a, e
}
