// This is a selection experiment, not application or production search code.
package searchproof

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	_ "modernc.org/sqlite"
)

// spaceHan leaves original text untouched: this form exists only for indexing.
// FTS5 unicode61 still normalizes case, diacritics and punctuation.
func spaceHan(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			b.WriteByte(' ')
			b.WriteRune(r)
			b.WriteByte(' ')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// The proof treats each whole input as a literal phrase, not FTS syntax.
func phrase(s string) string {
	return `"` + strings.ReplaceAll(spaceHan(s), `"`, `""`) + `"`
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	u := url.URL{Scheme: "file", Path: filepath.Join(t.TempDir(), "proof.db")}
	q := u.Query()
	for _, p := range []string{"journal_mode(WAL)", "foreign_keys(1)", "busy_timeout(5000)", "synchronous(FULL)"} {
		q.Add("_pragma", p)
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	var version, journal string
	var fk, synchronous int
	for query, dest := range map[string]any{
		"SELECT sqlite_version()": &version,
		"PRAGMA journal_mode":     &journal,
		"PRAGMA foreign_keys":     &fk,
		"PRAGMA synchronous":      &synchronous,
	} {
		if err := db.QueryRow(query).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	if journal != "wal" || fk != 1 || synchronous != 2 {
		t.Fatalf("unexpected settings: journal=%s foreign_keys=%d synchronous=%d", journal, fk, synchronous)
	}
	t.Logf("SQLite %s; WAL, foreign_keys=ON, synchronous=FULL", version)
	return db
}

func exec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, db *sql.DB, table, term string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(fmt.Sprintf("SELECT count(*) FROM %s WHERE %s MATCH ?", table, table), term).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSearchAndPurge(t *testing.T) {
	db := openDB(t)
	exec(t, db, `CREATE TABLE articles(id INTEGER PRIMARY KEY, title TEXT, body TEXT, curated TEXT)`)
	exec(t, db, `CREATE VIRTUAL TABLE search USING fts5(title, body, curated, content='', contentless_delete=1, tokenize='unicode61 remove_diacritics 2')`)
	title, body, curated := "五月天专辑归档", "喜欢音乐整理 Go SQLite CAFÉ", "个人笔记：保留总结"
	exec(t, db, `INSERT INTO articles VALUES(1, ?, ?, ?)`, title, body, curated)
	exec(t, db, `INSERT INTO search(rowid,title,body,curated) VALUES(1, ?, ?, ?)`, spaceHan(title), spaceHan(body), spaceHan(curated))
	for _, tc := range []struct {
		input string
		want  int
	}{
		{"音乐", 1}, {"乐整", 1}, {"音", 1}, {"五月天", 1},
		{"音乐整理 Go", 1}, {"sqlite", 1}, {"cafe", 1},
		{"乐音", 0}, {"音理", 0}, {"音 Go", 0}, {"SQL", 0},
		{`音乐" OR SQLite`, 0}, {"不存在", 0},
	} {
		t.Run(tc.input, func(t *testing.T) {
			if got := count(t, db, "search", phrase(tc.input)); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
	var indexedBody sql.NullString
	if err := db.QueryRow(`SELECT body FROM search WHERE rowid=1`).Scan(&indexedBody); err != nil || indexedBody.Valid {
		t.Fatalf("contentless index must not expose a stored body: %+v, %v", indexedBody, err)
	}
	// Updating every indexed column is required by contentless-delete tables.
	purge := func(commit bool) {
		t.Helper()
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`UPDATE articles SET body=NULL WHERE id=1`); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`UPDATE search SET title=?,body='',curated=? WHERE rowid=1`, spaceHan(title), spaceHan(curated)); err != nil {
			t.Fatal(err)
		}
		if commit {
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
		}
	}
	purge(false)
	if got := count(t, db, "search", phrase("音乐")); got != 1 {
		t.Fatal("rolled back purge lost source search result")
	}
	purge(true)
	if got := count(t, db, "search", phrase("音乐")); got != 0 {
		t.Fatal("purged body is still searchable")
	}
	for _, retained := range []string{"五月天", "个人笔记", "总结"} {
		if count(t, db, "search", phrase(retained)) != 1 {
			t.Fatalf("retained content no longer searchable: %s", retained)
		}
	}
	var remaining sql.NullString
	if err := db.QueryRow(`SELECT body FROM articles WHERE id=1`).Scan(&remaining); err != nil || remaining.Valid {
		t.Fatalf("source body still present: %+v, %v", remaining, err)
	}
}

func TestDefaultTokenizerLimitations(t *testing.T) {
	db := openDB(t)
	exec(t, db, `CREATE VIRTUAL TABLE words USING fts5(body, tokenize='unicode61')`)
	exec(t, db, `CREATE VIRTUAL TABLE triples USING fts5(body, tokenize='trigram')`)
	exec(t, db, `INSERT INTO words VALUES('喜欢音乐整理')`)
	exec(t, db, `INSERT INTO triples VALUES('喜欢音乐整理')`)
	if count(t, db, "words", `"音乐"`) != 0 || count(t, db, "triples", `"音乐"`) != 0 {
		t.Fatal("tokenizer assumptions changed; reassess selected search strategy")
	}
	if count(t, db, "triples", `"音乐整"`) != 1 {
		t.Fatal("trigram should match a three-character substring")
	}
}
