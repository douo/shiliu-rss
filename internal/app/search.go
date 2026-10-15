package app

import (
	"context"
	"database/sql"
	"strings"
	"unicode"
)

// SearchText separates Han characters, preserving adjacent positions. English
// words remain words; no caller-controlled FTS grammar reaches MATCH.
func SearchText(v string) string {
	var b strings.Builder
	for _, r := range v {
		if unicode.Is(unicode.Han, r) {
			b.WriteByte(' ')
			b.WriteRune(r)
			b.WriteByte(' ')
		} else {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
func searchQuery(v string) (string, error) {
	if len([]rune(v)) > 128 {
		return "", ErrInvalid
	}
	terms := strings.Fields(v)
	if len(terms) > 8 {
		return "", ErrInvalid
	}
	out := []string{}
	for _, t := range terms {
		t = SearchText(t)
		if t != "" {
			out = append(out, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
		}
	}
	if len(out) == 0 {
		return "", ErrInvalid
	}
	return strings.Join(out, " AND "), nil
}
func (s *Service) indexArticle(ctx context.Context, tx *sql.Tx, articleID string) error {
	var title, url, note, tags string
	if e := tx.QueryRowContext(ctx, `SELECT title,url,note,tags FROM articles WHERE id=?`, articleID).Scan(&title, &url, &note, &tags); e != nil {
		return e
	}
	var feed, full string
	rows, e := tx.QueryContext(ctx, `SELECT kind,text FROM article_content WHERE article_id=? AND state='available'`, articleID)
	if e != nil {
		return e
	}
	for rows.Next() {
		var k, t string
		if e = rows.Scan(&k, &t); e != nil {
			rows.Close()
			return e
		}
		if k == "feed" {
			feed = t
		} else {
			full = t
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	rows, e = tx.QueryContext(ctx, `SELECT content FROM artifacts WHERE article_id=? ORDER BY id`, articleID)
	if e != nil {
		return e
	}
	var org strings.Builder
	org.WriteString(note)
	org.WriteByte(' ')
	org.WriteString(tags)
	for rows.Next() {
		var text string
		if e = rows.Scan(&text); e != nil {
			rows.Close()
			return e
		}
		org.WriteByte(' ')
		org.WriteString(text)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM search WHERE rowid=?`, articleID); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO search(rowid,title,url,feed_content,fulltext,organization)VALUES(?,?,?,?,?,?)`, articleID, SearchText(title), SearchText(url), SearchText(feed), SearchText(full), SearchText(org.String()))
	return e
}
