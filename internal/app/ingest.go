package app

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

type identity struct{ kind, value string }

func entryIdentities(e FeedEntry) ([]identity, error) {
	out := []identity{}
	if e.GUID != "" {
		out = append(out, identity{"guid", e.GUID})
	}
	if e.URL != "" {
		out = append(out, identity{"url", normalizeURL(e.URL)})
	}
	keys := []string{}
	for k := range e.Aliases {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range e.Aliases[k] {
			if v != "" {
				out = append(out, identity{k, v})
			}
		}
	}
	if len(out) == 0 {
		if strings.TrimSpace(e.Title) == "" || (strings.TrimSpace(e.Author) == "" && (e.PublishedAt == nil || e.PublishedAt.IsZero())) {
			return nil, ErrInvalid
		}
		p := ""
		if e.PublishedAt != nil {
			p = stamp(*e.PublishedAt)
		}
		out = append(out, identity{"fallback", hash(e.Title + "|" + e.Author + "|" + p)})
	}
	return out, nil
}
func (s *Service) upsertEntry(ctx context.Context, tx *sql.Tx, feedID string, e FeedEntry, imp *ImportArticleInput) (string, error) {
	identities, identityErr := entryIdentities(e)
	if identityErr != nil {
		return "", identityErr
	}
	// Miniflux 2.3.0 persisted SHA256(GUID/JSON Feed ID), rather than
	// the raw identifier. Match existing import aliases so a changed URL does
	// not create a second article. These are lookup candidates only: native
	// articles never gain a fabricated legacy identity.
	candidates := identities
	if imp == nil && strings.TrimSpace(e.GUID) != "" {
		candidates = append(append([]identity(nil), identities...), identity{"miniflux_hash", hash(e.GUID)})
		if trimmed := strings.TrimSpace(e.GUID); trimmed != e.GUID {
			candidates = append(candidates, identity{"miniflux_hash", hash(trimmed)})
		}
	}
	articleID := ""
	for _, v := range candidates {
		var match string
		err := tx.QueryRowContext(ctx, `SELECT article_id FROM identities WHERE feed_id=? AND kind=? AND value=?`, feedID, v.kind, v.value).Scan(&match)
		if err == nil {
			if articleID != "" && articleID != match {
				return "", fmt.Errorf("%w: article identity collision", ErrConflict)
			}
			articleID = match
		} else if err != sql.ErrNoRows {
			return "", err
		}
	}
	now := s.Now()
	newArticle := articleID == ""
	if newArticle {
		seen := now
		var imported, protection, read any
		provenance := "feed"
		if len(identities) == 1 && identities[0].kind == "fallback" {
			provenance = "fallback"
		}
		favorite, removed, unknown := false, false, false
		tags := "[]"
		if imp != nil {
			imported = stamp(now)
			protection = stamp(now.AddDate(0, 0, 30))
			favorite = imp.Favorite
			removed = imp.Removed
			provenance = "miniflux"
			if imp.FirstSeenAt != nil {
				seen = *imp.FirstSeenAt
			} else {
				provenance = "estimated"
			}
			if imp.Read {
				if imp.ReadAt != nil {
					read = stamp(*imp.ReadAt)
				} else {
					read = stamp(now)
					unknown = true
				}
			}
			t, e := validateTags(imp.Tags)
			if e != nil {
				return "", e
			}
			tags = jsonTags(t)
		}
		r, err := tx.ExecContext(ctx, `INSERT INTO articles(feed_id,title,url,author,published_at,first_seen_at,read_at,favorite,removed,imported_at,protection_until,provenance,read_at_unknown,tags)VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, feedID, e.Title, e.URL, e.Author, nullable(e.PublishedAt), stamp(seen), read, favorite, removed, imported, protection, provenance, unknown, tags)
		if err != nil {
			return "", err
		}
		n, err := r.LastInsertId()
		if err != nil {
			return "", err
		}
		articleID = id(n)
		if e.HTML != "" || e.Text != "" {
			_, err = tx.ExecContext(ctx, `INSERT INTO article_content(article_id,kind,html,text,hash,obtained_at,expires_at)VALUES(?,'feed',?,?,?,?,?)`, articleID, e.HTML, e.Text, hash(e.HTML+"\x00"+e.Text), stamp(now), stamp(seen.AddDate(0, 0, 90)))
			if err != nil {
				return "", err
			}
		}
	} else if imp == nil {
		var title, url, author string
		var version int64
		if err := tx.QueryRowContext(ctx, `SELECT title,url,author,version FROM articles WHERE id=?`, articleID).Scan(&title, &url, &author, &version); err != nil {
			return "", err
		}
		changed := title != e.Title || url != e.URL || author != e.Author
		var oldHash, state, expiry string
		err := tx.QueryRowContext(ctx, `SELECT hash,state,expires_at FROM article_content WHERE article_id=? AND kind='feed'`, articleID).Scan(&oldHash, &state, &expiry)
		if err != nil && err != sql.ErrNoRows {
			return "", err
		}

		if err == sql.ErrNoRows && (e.HTML != "" || e.Text != "") {
			var seen string
			if err = tx.QueryRowContext(ctx, `SELECT first_seen_at FROM articles WHERE id=?`, articleID).Scan(&seen); err != nil {
				return "", err
			}
			r, insertErr := tx.ExecContext(ctx, `INSERT INTO article_content(article_id,kind,html,text,hash,obtained_at,expires_at) SELECT a.id,'feed',?,?,?,?,? FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE a.id=? AND NOT EXISTS(SELECT 1 FROM article_content c WHERE c.article_id=a.id AND c.state='purged') AND (a.favorite=1 OR f.archive=1 OR max(?,COALESCE(a.protection_until,''))>?)`, e.HTML, e.Text, hash(e.HTML+"\x00"+e.Text), stamp(now), stamp(parse(seen).AddDate(0, 0, 90)), articleID, stamp(parse(seen).AddDate(0, 0, 90)), stamp(now))
			if insertErr != nil {
				return "", insertErr
			}
			n, insertErr := r.RowsAffected()
			if insertErr != nil {
				return "", insertErr
			}
			if n > 0 {
				changed = true
			}
			// Mark the absent original state so the update path below does not
			// try to compare an unloaded prior content hash.
			err = sql.ErrNoRows
		}
		if err == nil && state == "available" {
			var protect sql.NullString
			var favorite, archive bool
			if err = tx.QueryRowContext(ctx, `SELECT a.favorite,f.archive,a.protection_until FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE a.id=?`, articleID).Scan(&favorite, &archive, &protect); err != nil {
				return "", err
			}
			effective := parse(expiry)
			if protect.Valid && parse(protect.String).After(effective) {
				effective = parse(protect.String)
			}
			newHash := hash(e.HTML + "\x00" + e.Text)
			if newHash != oldHash && (favorite || archive || now.Before(effective)) && (e.HTML != "" || e.Text != "") {
				_, err = tx.ExecContext(ctx, `UPDATE article_content SET html=?,text=?,hash=?,obtained_at=? WHERE article_id=? AND kind='feed' AND state='available'`, e.HTML, e.Text, newHash, stamp(now), articleID)
				if err != nil {
					return "", err
				}
				changed = true
			}
		}
		if changed {
			if _, err = tx.ExecContext(ctx, `UPDATE articles SET title=?,url=?,author=?,version=version+1 WHERE id=?`, e.Title, e.URL, e.Author, articleID); err != nil {
				return "", err
			}
			if err = s.change(ctx, tx, "article", articleID, "content_updated", version+1); err != nil {
				return "", err
			}
		}
	}
	for _, v := range identities {
		if _, err := tx.ExecContext(ctx, `INSERT INTO identities(feed_id,kind,value,article_id)VALUES(?,?,?,?) ON CONFLICT(feed_id,kind,value) DO NOTHING`, feedID, v.kind, v.value, articleID); err != nil {
			return "", err
		}
	}
	if err := s.indexArticle(ctx, tx, articleID); err != nil {
		return "", err
	}
	if newArticle {
		if err := s.change(ctx, tx, "article", articleID, "created", 1); err != nil {
			return "", err
		}
	}
	return articleID, nil
}
func (s *Service) ImportCategory(ctx context.Context, instance, sourceID, title string) (Category, error) {
	var out Category
	e := s.write(ctx, func(tx *sql.Tx) error {
		var target string
		e := tx.QueryRowContext(ctx, `SELECT target_id FROM imports WHERE source_instance=? AND entity_type='category' AND source_id=?`, instance, sourceID).Scan(&target)
		if e == nil {
			e = tx.QueryRowContext(ctx, `SELECT id,title,version FROM categories WHERE id=?`, target).Scan(&out.ID, &out.Title, &out.Version)
			// A retained import mapping records the original user's decision.
			// Repeating a migration must not resurrect a subsequently deleted
			// category or move feeds back into it.
			if e == sql.ErrNoRows {
				return fmt.Errorf("%w: imported category was deleted after import", ErrConflict)
			}
			return e
		}
		if e != sql.ErrNoRows {
			return e
		}
		e = tx.QueryRowContext(ctx, `SELECT id,title,version FROM categories WHERE title=?`, title).Scan(&out.ID, &out.Title, &out.Version)
		if e == sql.ErrNoRows {
			r, err := tx.ExecContext(ctx, `INSERT INTO categories(title)VALUES(?)`, title)
			if err != nil {
				return err
			}
			n, err := r.LastInsertId()
			if err != nil {
				return err
			}
			out = Category{ID: id(n), Title: title, Version: 1}
			if err = s.change(ctx, tx, "category", out.ID, "created", 1); err != nil {
				return err
			}
		} else if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO imports VALUES(?,'category',?,?,?)`, instance, sourceID, out.ID, stamp(s.Now()))
		return e
	})
	return out, e
}
func (s *Service) ImportFeed(ctx context.Context, instance, sourceID string, f Feed) (Feed, error) {
	var target string
	e := s.write(ctx, func(tx *sql.Tx) error {
		e := tx.QueryRowContext(ctx, `SELECT target_id FROM imports WHERE source_instance=? AND entity_type='feed' AND source_id=?`, instance, sourceID).Scan(&target)
		if e == nil {
			return nil
		}
		if e != sql.ErrNoRows {
			return e
		}
		e = tx.QueryRowContext(ctx, `SELECT id FROM feeds WHERE url=?`, normalizeURL(f.URL)).Scan(&target)
		if e == sql.ErrNoRows {
			created, err := s.createFeed(ctx, tx, f)
			if err != nil {
				return err
			}
			target = created.ID
		} else if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO imports VALUES(?,'feed',?,?,?)`, instance, sourceID, target, stamp(s.Now()))
		return e
	})
	if e != nil {
		return Feed{}, e
	}
	return s.GetFeed(ctx, target)
}
func (s *Service) ImportArticle(ctx context.Context, input ImportArticleInput) (Article, error) {
	prepared, err := prepareEntry(input.Entry)
	if err != nil {
		return Article{}, err
	}
	input.Entry = prepared
	var target string
	e := s.write(ctx, func(tx *sql.Tx) error {
		e := tx.QueryRowContext(ctx, `SELECT target_id FROM imports WHERE source_instance=? AND entity_type='article' AND source_id=?`, input.SourceInstance, input.SourceID).Scan(&target)
		if e == nil {
			return nil
		}
		if e != sql.ErrNoRows {
			return e
		}
		if input.OldHash != "" {
			if input.Entry.Aliases == nil {
				input.Entry.Aliases = map[string][]string{}
			}
			input.Entry.Aliases["miniflux_hash"] = append(input.Entry.Aliases["miniflux_hash"], input.OldHash)
		}
		target, e = s.upsertEntry(ctx, tx, input.FeedID, input.Entry, &input)
		if e != nil {
			return e
		}
		var priorSource string
		err := tx.QueryRowContext(ctx, `SELECT source_id FROM imports WHERE source_instance=? AND entity_type='article' AND target_id=? AND source_id<>? LIMIT 1`, input.SourceInstance, target, input.SourceID).Scan(&priorSource)
		if err == nil {
			return fmt.Errorf("%w: multiple imported source IDs resolve to one article", ErrConflict)
		}
		if err != sql.ErrNoRows {
			return err
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO imports VALUES(?,'article',?,?,?)`, input.SourceInstance, input.SourceID, target, stamp(s.Now()))
		return e
	})
	if e != nil {
		return Article{}, e
	}
	return s.GetArticle(ctx, target)
}

// IngestFeed is useful for controlled imports and integration fixtures; normal
// polling completes through CompleteFeedJob for atomic lease/result semantics.
func (s *Service) IngestFeed(ctx context.Context, feedID string, entries []FeedEntry) ([]string, error) {
	prepared, err := prepareEntries(entries)
	if err != nil {
		return nil, err
	}
	entries = prepared
	out := []string{}
	e := s.write(ctx, func(tx *sql.Tx) error {
		for _, entry := range entries {
			articleID, e := s.upsertEntry(ctx, tx, feedID, entry, nil)
			if e != nil {
				return e
			}
			out = append(out, articleID)
		}
		return nil
	})
	return out, e
}
