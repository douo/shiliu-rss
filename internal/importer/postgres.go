package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"time"
)

type ExportOptions struct {
	SourceInstance string
	SourceVersion  string
	UserID         int64
	Now            func() time.Time
}
type schema map[string]map[string]bool

// SchemaError contains field names only, so it is safe to display without
// exposing PostgreSQL connection details or source configuration values.
type SchemaError struct{ Missing []string }

func (e *SchemaError) Error() string {
	return "incompatible Miniflux schema: missing " + strings.Join(e.Missing, ", ")
}

var required = map[string][]string{
	"categories":     {"id", "user_id", "title"},
	"feeds":          {"id", "user_id", "category_id", "title", "feed_url", "site_url", "disabled"},
	"entries":        {"id", "user_id", "feed_id", "hash", "published_at", "title", "url", "author", "content", "status", "starred"},
	"schema_version": {"version"},
}
var optional = map[string][]string{"categories": {"hide_globally"}, "entries": {"created_at", "tags"}}

func inspect(ctx context.Context, tx *sql.Tx) (schema, []string, error) {
	rows, e := tx.QueryContext(ctx, `SELECT table_name,column_name FROM information_schema.columns WHERE table_schema='public' AND table_name IN ('categories','feeds','entries','schema_version')`)
	if e != nil {
		return nil, nil, e
	}
	defer rows.Close()
	found := schema{}
	for rows.Next() {
		var t, c string
		if e = rows.Scan(&t, &c); e != nil {
			return nil, nil, e
		}
		if found[t] == nil {
			found[t] = map[string]bool{}
		}
		found[t][c] = true
	}
	if e = rows.Err(); e != nil {
		return nil, nil, e
	}
	missing, err := validateSchema(found)
	return found, missing, err
}
func validateSchema(found schema) ([]string, error) {
	var absent, missing []string
	for table, cols := range required {
		for _, col := range cols {
			if !found[table][col] {
				absent = append(absent, table+"."+col)
			}
		}
	}
	sort.Strings(absent)
	if len(absent) > 0 {
		return nil, &SchemaError{Missing: absent}
	}
	for table, cols := range optional {
		for _, col := range cols {
			if !found[table][col] {
				missing = append(missing, table+"."+col)
			}
		}
	}
	sort.Strings(missing)
	return missing, nil
}

// ExportPostgres performs all probes and reads in a single read-only repeatable
// read transaction. It never queries credentials or emits their values. The
// caller must protect both the database DSN and output as private material.
func ExportPostgres(ctx context.Context, db *sql.DB, w io.Writer, opt ExportOptions) error {
	if opt.SourceInstance == "" || opt.SourceVersion != "2.3.0" || opt.UserID <= 0 {
		return errors.New("source instance, supported version 2.3.0 and positive user ID required")
	}
	now := time.Now
	if opt.Now != nil {
		now = opt.Now
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SET LOCAL statement_timeout='60s'`); err != nil {
		return err
	}
	cols, missing, err := inspect(ctx, tx)
	if err != nil {
		return err
	}
	var version string
	if err = tx.QueryRowContext(ctx, `SELECT version FROM public.schema_version LIMIT 1`).Scan(&version); err != nil {
		return errors.New("cannot read Miniflux schema version")
	}
	enc := json.NewEncoder(w)
	if err = enc.Encode(Record{Kind: "header", Header: &Header{Format: Format, SourceInstance: opt.SourceInstance, SourceVersion: opt.SourceVersion, SchemaVersion: version, ExportedAt: now().UTC(), UserID: sid(opt.UserID), Missing: missing}}); err != nil {
		return err
	}
	counts := Counts{}
	// Explicit projection prevents accidental credential/content-cache export if
	// upstream adds new columns. Missing optional fields are declared in header.
	categories := `SELECT jsonb_build_object('id',id,'title',title,'hide_globally',` + column(cols, "categories", "hide_globally", "false") + `) FROM public.categories WHERE user_id=$1 ORDER BY id`
	if err = streamRows(ctx, tx, categories, opt.UserID, func(raw []byte) error {
		var c Category
		if e := json.Unmarshal(raw, &c); e != nil {
			return e
		}
		counts.Categories++
		return enc.Encode(Record{Kind: "category", Category: &c})
	}); err != nil {
		return err
	}
	// Values remain in PostgreSQL; only enabled/nonempty field names are returned.
	configFields := []string{"scraper_rules", "rewrite_rules", "crawler", "username", "password", "user_agent", "ignore_http_cache", "fetch_via_proxy", "blocklist_rules", "keeplist_rules", "allow_self_signed_certificates", "cookie", "hide_globally", "url_rewrite_rules", "disable_http2", "proxy_url", "block_filter_entry_rules", "keep_filter_entry_rules", "ignore_entry_updates"}
	var checks []string
	for _, field := range configFields {
		if cols["feeds"][field] {
			checks = append(checks, `CASE WHEN COALESCE(to_jsonb(f)->>'`+field+`','') NOT IN ('','false','0','[]','{}') THEN '`+field+`' END`)
		}
	}
	configSQL := `'[]'::jsonb`
	if len(checks) > 0 {
		configSQL = `to_jsonb(array_remove(ARRAY[` + strings.Join(checks, ",") + `],NULL))`
	}
	feeds := `SELECT jsonb_build_object('id',id,'category_id',category_id,'title',title,'feed_url',feed_url,'site_url',site_url,'disabled',` + column(cols, "feeds", "disabled", "false") + `,'unsupported',` + configSQL + `) FROM public.feeds f WHERE user_id=$1 ORDER BY id`
	if err = streamRows(ctx, tx, feeds, opt.UserID, func(raw []byte) error {
		var f Feed
		if e := json.Unmarshal(raw, &f); e != nil {
			return e
		}
		counts.Feeds++
		return enc.Encode(Record{Kind: "feed", Feed: &f})
	}); err != nil {
		return err
	}
	entries := `SELECT jsonb_build_object('id',id,'feed_id',feed_id,'hash',hash,'published_at',published_at,'title',title,'url',url,'author',COALESCE(author,''),'content',COALESCE(content,''),'status',status,'starred',COALESCE(starred,false),'created_at',` + column(cols, "entries", "created_at", "NULL") + `,'tags',` + column(cols, "entries", "tags", "'{}'::text[]") + `) FROM public.entries WHERE user_id=$1 ORDER BY id`
	if err = streamRows(ctx, tx, entries, opt.UserID, func(raw []byte) error {
		var e Entry
		if x := json.Unmarshal(raw, &e); x != nil {
			return x
		}
		counts.Articles++
		if e.Status == "read" {
			counts.Read++
		}
		if e.Status == "removed" {
			counts.Removed++
		}
		if e.Starred {
			counts.Favorite++
		}
		if e.Content != "" {
			counts.WithContent++
		}
		if e.CreatedAt == nil {
			counts.EstimatedFirstSeen++
		}
		return enc.Encode(Record{Kind: "entry", Entry: &e})
	}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return enc.Encode(Record{Kind: "trailer", Counts: &counts})
}
func column(cols schema, table, key, fallback string) string {
	if cols[table][key] {
		return key
	}
	return fallback
}
func streamRows(ctx context.Context, tx *sql.Tx, query string, userID int64, visit func([]byte) error) error {
	rows, err := tx.QueryContext(ctx, query, userID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		if err = visit(raw); err != nil {
			return err
		}
	}
	return rows.Err()
}
