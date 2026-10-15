// Package importer transfers a versioned Miniflux snapshot through the same
// application rules used by the web UI. Snapshots contain private source data.
package importer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"shiliu-rss/internal/app"
)

const Format = "shiliu-miniflux-1"

type Header struct {
	Format         string    `json:"format"`
	SourceInstance string    `json:"source_instance"`
	SourceVersion  string    `json:"source_version"`
	SchemaVersion  string    `json:"schema_version"`
	ExportedAt     time.Time `json:"exported_at"`
	UserID         string    `json:"user_id"`
	Missing        []string  `json:"missing,omitempty"`
}
type Category struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	HideGlobally bool   `json:"hide_globally"`
}
type Feed struct {
	ID          int64    `json:"id"`
	CategoryID  int64    `json:"category_id"`
	Title       string   `json:"title"`
	URL         string   `json:"feed_url"`
	SiteURL     string   `json:"site_url"`
	Disabled    bool     `json:"disabled"`
	Unsupported []string `json:"unsupported,omitempty"`
}
type Entry struct {
	ID          int64      `json:"id"`
	FeedID      int64      `json:"feed_id"`
	Hash        string     `json:"hash"`
	Title       string     `json:"title"`
	URL         string     `json:"url"`
	Author      string     `json:"author"`
	Content     string     `json:"content"`
	Status      string     `json:"status"`
	Starred     bool       `json:"starred"`
	CreatedAt   *time.Time `json:"created_at"`
	PublishedAt *time.Time `json:"published_at"`
	Tags        []string   `json:"tags"`
}
type Counts struct {
	Categories         int `json:"categories"`
	Feeds              int `json:"feeds"`
	Articles           int `json:"articles"`
	Read               int `json:"read"`
	Favorite           int `json:"favorite"`
	Removed            int `json:"removed"`
	WithContent        int `json:"with_content"`
	EstimatedFirstSeen int `json:"estimated_first_seen"`
}
type Record struct {
	Kind     string    `json:"kind"`
	Header   *Header   `json:"header,omitempty"`
	Category *Category `json:"category,omitempty"`
	Feed     *Feed     `json:"feed,omitempty"`
	Entry    *Entry    `json:"entry,omitempty"`
	Counts   *Counts   `json:"counts,omitempty"`
}
type Issue struct {
	Entity   string   `json:"entity"`
	SourceID string   `json:"source_id,omitempty"`
	Code     string   `json:"code"`
	Fields   []string `json:"fields,omitempty"`
}
type Report struct {
	Header   Header  `json:"header"`
	Counts   Counts  `json:"counts"`
	Issues   []Issue `json:"issues"`
	Complete bool    `json:"complete"`
}

func sid(n int64) string { return strconv.FormatInt(n, 10) }

// Preview validates the entire stream, its order, references and final counts.
// Only IDs and counters are retained; article bodies are never accumulated.
func Preview(r io.Reader) (Report, error) { return walk(r, nil) }
func walk(r io.Reader, visit func(Record) error) (Report, error) {
	var out Report
	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 64*1024), 32*1024*1024)
	categories := map[int64]bool{}
	feeds := map[int64]bool{}
	entries := map[int64]bool{}
	phase := 0
	line := 0
	for scan.Scan() {
		line++
		var v Record
		if err := json.Unmarshal(scan.Bytes(), &v); err != nil {
			return out, fmt.Errorf("snapshot line %d: invalid JSON", line)
		}
		if out.Complete {
			return out, errors.New("records after snapshot trailer")
		}
		if line == 1 && v.Kind != "header" {
			return out, errors.New("snapshot header required")
		}
		switch v.Kind {
		case "header":
			if line != 1 || v.Header == nil {
				return out, errors.New("invalid snapshot header")
			}
			out.Header = *v.Header
			if out.Header.Format != Format || out.Header.SourceInstance == "" || out.Header.SourceVersion != "2.3.0" || out.Header.ExportedAt.IsZero() {
				return out, errors.New("unsupported or incomplete snapshot header")
			}
			for _, field := range out.Header.Missing {
				out.Issues = append(out.Issues, Issue{Entity: "schema", Code: "missing_optional_field", Fields: []string{field}})
			}
			out.Issues = append(out.Issues, Issue{Entity: "article", Code: "original_guid_unavailable"}, Issue{Entity: "article", Code: "read_at_unavailable"})
		case "category":
			if phase > 0 || v.Category == nil {
				return out, errors.New("category order invalid")
			}
			c := v.Category
			if c.ID <= 0 || categories[c.ID] {
				return out, errors.New("duplicate/invalid category ID")
			}
			categories[c.ID] = true
			out.Counts.Categories++
			if c.HideGlobally {
				out.Issues = append(out.Issues, Issue{Entity: "category", SourceID: sid(c.ID), Code: "unsupported_configuration", Fields: []string{"hide_globally"}})
			}
		case "feed":
			if phase > 1 || v.Feed == nil {
				return out, errors.New("feed order invalid")
			}
			phase = 1
			f := v.Feed
			if f.ID <= 0 || feeds[f.ID] || !categories[f.CategoryID] {
				return out, errors.New("duplicate feed ID or missing category")
			}
			feeds[f.ID] = true
			out.Counts.Feeds++
			if len(f.Unsupported) > 0 {
				out.Issues = append(out.Issues, Issue{Entity: "feed", SourceID: sid(f.ID), Code: "unsupported_configuration", Fields: f.Unsupported})
			}
		case "entry":
			if v.Entry == nil {
				return out, errors.New("missing entry")
			}
			phase = 2
			e := v.Entry
			if e.ID <= 0 || entries[e.ID] || !feeds[e.FeedID] {
				return out, errors.New("duplicate article ID or missing feed")
			}
			entries[e.ID] = true
			if e.Status != "read" && e.Status != "unread" && e.Status != "removed" {
				return out, errors.New("unsupported entry status")
			}
			out.Counts.Articles++
			if e.Status == "read" {
				out.Counts.Read++
			}
			if e.Status == "removed" {
				out.Counts.Removed++
			}
			if e.Starred {
				out.Counts.Favorite++
			}
			if e.Content != "" {
				out.Counts.WithContent++
			}
			if e.CreatedAt == nil {
				out.Counts.EstimatedFirstSeen++
			}
		case "trailer":
			if v.Counts == nil || *v.Counts != out.Counts {
				return out, errors.New("snapshot count reconciliation failed")
			}
			out.Complete = true
		default:
			return out, errors.New("unknown snapshot record")
		}
		if visit != nil {
			if err := visit(v); err != nil {
				return out, err
			}
		}
	}
	if err := scan.Err(); err != nil {
		return out, fmt.Errorf("read snapshot: %w", err)
	}
	if !out.Complete {
		return out, errors.New("snapshot truncated: final counts missing")
	}
	return out, nil
}

type ImportOptions struct{ AllowUnsupported bool }

// Import validates before any writes. Retrying an interrupted import is safe:
// application source mappings preserve the first import/protection timestamps.
func Import(ctx context.Context, r io.ReadSeeker, s *app.Service, opt ImportOptions) (Report, error) {
	report, err := Preview(r)
	if err != nil {
		return report, err
	}
	if !opt.AllowUnsupported {
		for _, issue := range report.Issues {
			if issue.Code == "unsupported_configuration" {
				return report, errors.New("unsupported source configuration; review preview and explicitly allow differences")
			}
		}
	}
	if _, err = r.Seek(0, io.SeekStart); err != nil {
		return report, err
	}
	cats := map[int64]string{}
	feeds := map[int64]string{}
	articleTargets := map[string]int64{}
	feedTargets := map[string]int64{}
	_, err = walk(r, func(v Record) error {
		switch v.Kind {
		case "category":
			c, e := s.ImportCategory(ctx, report.Header.SourceInstance, sid(v.Category.ID), v.Category.Title)
			if e != nil {
				return e
			}
			cats[v.Category.ID] = c.ID
		case "feed":
			f := v.Feed
			created, e := s.ImportFeed(ctx, report.Header.SourceInstance, sid(f.ID), app.Feed{URL: f.URL, Title: f.Title, SiteURL: f.SiteURL, CategoryID: cats[f.CategoryID], Paused: f.Disabled})
			if e != nil {
				return e
			}
			if previous, ok := feedTargets[created.ID]; ok && previous != f.ID {
				return fmt.Errorf("%w: feed source IDs %d and %d map to the same target", app.ErrConflict, previous, f.ID)
			}
			feedTargets[created.ID] = f.ID
			feeds[f.ID] = created.ID
		case "entry":
			e := v.Entry
			created, err := s.ImportArticle(ctx, app.ImportArticleInput{SourceInstance: report.Header.SourceInstance, SourceID: sid(e.ID), FeedID: feeds[e.FeedID], Entry: app.FeedEntry{URL: e.URL, Title: e.Title, Author: e.Author, PublishedAt: e.PublishedAt, HTML: e.Content, Aliases: map[string][]string{"miniflux_hash": {e.Hash}}}, FirstSeenAt: e.CreatedAt, Read: e.Status == "read", Favorite: e.Starred, Removed: e.Status == "removed", OldHash: e.Hash, Tags: e.Tags})
			if err != nil {
				return err
			}
			if previous, ok := articleTargets[created.ID]; ok && previous != e.ID {
				return fmt.Errorf("%w: article source IDs %d and %d map to the same target", app.ErrConflict, previous, e.ID)
			}
			articleTargets[created.ID] = e.ID
			return nil
		}
		return nil
	})
	return report, err
}
