// bench-reader generates only synthetic data through the application service.
// It measures this host, not deployment hardware; no port or private feed is used.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"shiliu-rss/internal/app"
	"shiliu-rss/internal/importer"
	"shiliu-rss/internal/store"
)

type latency struct {
	Samples int     `json:"samples"`
	P50MS   float64 `json:"p50_ms"`
	P95MS   float64 `json:"p95_ms"`
	MaxMS   float64 `json:"max_ms"`
}
type report struct {
	At              time.Time          `json:"at"`
	Host            string             `json:"host"`
	Go              string             `json:"go"`
	Feeds           int                `json:"feeds"`
	Articles        int                `json:"articles"`
	Bodies          int                `json:"bodies"`
	SourceHTMLBytes int64              `json:"source_html_bytes"`
	DBBytes         int64              `json:"db_bytes"`
	IndexBytes      *int64             `json:"index_bytes"`
	SeedSeconds     float64            `json:"seed_seconds"`
	RSSBytes        int64              `json:"rss_bytes"`
	HeapBytes       uint64             `json:"heap_bytes"`
	Queries         map[string]latency `json:"queries"`
	Notes           []string           `json:"notes"`
	Maintenance     *maintenanceReport `json:"maintenance,omitempty"`
}
type maintenanceReport struct {
	BackupSeconds    float64 `json:"backup_seconds"`
	RestoreSeconds   float64 `json:"restore_seconds"`
	BackupBytes      int64   `json:"backup_bytes"`
	RestoredDBBytes  int64   `json:"restored_db_bytes"`
	SourceFeeds      int     `json:"source_feeds"`
	SourceArticles   int     `json:"source_articles"`
	SourceBodies     int     `json:"source_bodies"`
	RestoredFeeds    int     `json:"restored_feeds"`
	RestoredArticles int     `json:"restored_articles"`
	RestoredBodies   int     `json:"restored_bodies"`
	IntegrityPassed  bool    `json:"integrity_passed"`
}

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	feeds := flag.Int("feeds", 200, "synthetic feeds")
	articles := flag.Int("articles", 100000, "metadata records")
	bodies := flag.Int("bodies", 20000, "records containing source HTML")
	bodyBytes := flag.Int("body-bytes", 26214, "approximate HTML bytes per source body")
	samples := flag.Int("samples", 30, "query repetitions")
	output := flag.String("output", "", "JSON report path (stdout always prints report)")
	maintenance := flag.Bool("maintenance", false, "also verify a consistent backup and cleaned restore before deleting synthetic fixtures")
	flag.Parse()
	if *feeds < 1 || *articles < 1 || *bodies < 0 || *bodies > *articles || *bodyBytes < 100 || *samples < 1 {
		return fmt.Errorf("invalid scale")
	}
	ctx := context.Background()
	dir, e := os.MkdirTemp("", "shiliu-scale-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	dbPath := filepath.Join(dir, "reader.db")
	st, e := store.Open(ctx, dbPath)
	if e != nil {
		return e
	}
	defer st.Close()
	s := app.New(st)
	r := report{At: time.Now().UTC(), Host: runtime.GOOS + "/" + runtime.GOARCH, Go: runtime.Version(), Feeds: *feeds, Articles: *articles, Bodies: *bodies, Queries: map[string]latency{}, Notes: []string{"Synthetic local workload; does not establish NAS/J1900 latency, power, or memory budgets.", "Reported RSS is a post-seed process snapshot; it is not mixed-workload peak memory.", "HTML is sanitized and indexed through the application service; source HTML and derived text both occupy authoritative content storage."}}
	fs := make([]app.Feed, *feeds)
	for i := range fs {
		fs[i], e = s.CreateFeed(ctx, app.Feed{URL: fmt.Sprintf("https://synthetic-%03d.invalid/feed", i), Title: fmt.Sprintf("合成订阅 %03d", i)})
		if e != nil {
			return e
		}
	}
	unit := "古典音乐与科学阅读 classical music SQLite database synthetic article. "
	body := "<p>" + strings.Repeat(unit, *bodyBytes/len(unit)+1) + "</p>"
	start := time.Now()
	for feedIndex, f := range fs {
		entries := []app.FeedEntry{}
		for n := feedIndex; n < *articles; n += *feeds {
			entry := app.FeedEntry{GUID: fmt.Sprintf("synthetic-%d", n), Title: fmt.Sprintf("合成文章 阅读记录 %06d", n), URL: fmt.Sprintf("https://synthetic-%03d.invalid/articles/%d", feedIndex, n)}
			if n < *bodies {
				entry.HTML = body
				r.SourceHTMLBytes += int64(len(body))
			}
			entries = append(entries, entry)
			if len(entries) == 200 {
				if _, e = s.IngestFeed(ctx, f.ID, entries); e != nil {
					return e
				}
				entries = entries[:0]
			}
		}
		if len(entries) > 0 {
			if _, e = s.IngestFeed(ctx, f.ID, entries); e != nil {
				return e
			}
		}
		if (feedIndex+1)%10 == 0 {
			fmt.Fprintf(os.Stderr, "seeded %d/%d feeds in %.1fs\n", feedIndex+1, *feeds, time.Since(start).Seconds())
		}
	}
	r.SeedSeconds = time.Since(start).Seconds()
	if _, e = st.Write.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); e != nil {
		return e
	}
	if info, e := os.Stat(dbPath); e == nil {
		r.DBBytes = info.Size()
	}
	var indexBytes int64
	if e = st.Read.QueryRowContext(ctx, `SELECT COALESCE(sum(pgsize),0) FROM dbstat WHERE name LIKE 'search%'`).Scan(&indexBytes); e == nil {
		r.IndexBytes = &indexBytes
	} else {
		r.Notes = append(r.Notes, "SQLite dbstat is unavailable; separate index size was not measured.")
	}
	for name, q := range map[string]app.ArticleQuery{"list_50": {Limit: 50}, "chinese_music": {Query: "音乐", Limit: 50}, "english_sqlite": {Query: "SQLite", Limit: 50}, "single_han": {Query: "音", Limit: 50}, "sparse_title": {Query: "000001", Limit: 50}} {
		if _, e = s.ListArticles(ctx, q); e != nil {
			return e
		}
		times := make([]float64, *samples)
		for i := range times {
			begin := time.Now()
			page, e := s.ListArticles(ctx, q)
			if e != nil {
				return e
			}
			if len(page.Items) == 0 {
				return fmt.Errorf("query %s returned no fixtures", name)
			}
			times[i] = float64(time.Since(begin).Microseconds()) / 1000
		}
		sort.Float64s(times)
		r.Queries[name] = latency{Samples: len(times), P50MS: times[len(times)/2], P95MS: times[int(math.Ceil(.95*float64(len(times))))-1], MaxMS: times[len(times)-1]}
	}
	runtime.GC()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	r.HeapBytes = mem.HeapAlloc
	if b, e := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output(); e == nil {
		kb, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		r.RSSBytes = kb * 1024
	}
	if *maintenance {
		fmt.Fprintln(os.Stderr, "starting synthetic backup/restore maintenance drill")
		m := &maintenanceReport{}
		for query, target := range map[string]*int{`SELECT count(*) FROM feeds`: &m.SourceFeeds, `SELECT count(*) FROM articles`: &m.SourceArticles, `SELECT count(*) FROM article_content WHERE state='available'`: &m.SourceBodies} {
			if e = st.Read.QueryRowContext(ctx, query).Scan(target); e != nil {
				return e
			}
		}
		begin := time.Now()
		backup, e := importer.Backup(ctx, s, filepath.Join(dir, "backups"), time.Now())
		if e != nil {
			return e
		}
		m.BackupSeconds = time.Since(begin).Seconds()
		if info, e := os.Stat(backup); e != nil {
			return e
		} else {
			m.BackupBytes = info.Size()
		}
		restoredPath := filepath.Join(dir, "restored.db")
		begin = time.Now()
		if e = importer.Restore(ctx, backup, restoredPath, time.Now()); e != nil {
			return e
		}
		m.RestoreSeconds = time.Since(begin).Seconds()
		if info, e := os.Stat(restoredPath); e != nil {
			return e
		} else {
			m.RestoredDBBytes = info.Size()
		}
		restored, e := store.Open(ctx, restoredPath)
		if e != nil {
			return e
		}
		for query, target := range map[string]*int{`SELECT count(*) FROM feeds`: &m.RestoredFeeds, `SELECT count(*) FROM articles`: &m.RestoredArticles, `SELECT count(*) FROM article_content WHERE state='available'`: &m.RestoredBodies} {
			if e = restored.Read.QueryRowContext(ctx, query).Scan(target); e != nil {
				restored.Close()
				return e
			}
		}
		if e = app.New(restored).Integrity(ctx); e != nil {
			restored.Close()
			return e
		}
		if e = restored.Close(); e != nil {
			return e
		}
		if m.SourceFeeds != m.RestoredFeeds || m.SourceArticles != m.RestoredArticles || m.SourceBodies != m.RestoredBodies {
			return fmt.Errorf("maintenance count reconciliation failed")
		}
		m.IntegrityPassed = true
		r.Maintenance = m
		r.Notes = append(r.Notes, "Maintenance elapsed includes consistency/integrity validation and filesystem synchronization; restore performs lifecycle cleanup before publication. Fresh synthetic bodies remain within retention. Temporary source/backup/restored databases are removed after the run.")
		fmt.Fprintf(os.Stderr, "backup %.2fs, restore %.2fs; restored %d articles/%d bodies\n", m.BackupSeconds, m.RestoreSeconds, m.RestoredArticles, m.RestoredBodies)
	}
	b, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		return e
	}
	if *output != "" {
		if e = os.WriteFile(*output, append(b, '\n'), 0600); e != nil {
			return e
		}
	}
	fmt.Println(string(b))
	return nil
}
