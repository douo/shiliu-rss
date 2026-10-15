package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// AgentSystemFacts returns exact counts, independently of retrieval limits.
func (s *Service) AgentSystemFacts(ctx context.Context) (map[string]int, error) {
	out := map[string]int{}
	var total, unread, favorites int
	e := s.Store.Read.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(read_at IS NULL),0),COALESCE(sum(favorite),0) FROM articles WHERE removed=0`).Scan(&total, &unread, &favorites)
	if e != nil {
		return nil, e
	}
	out["articles"] = total
	out["unread_including_archive"] = unread
	out["favorites"] = favorites
	var feeds, failed int
	e = s.Store.Read.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(error<>''),0) FROM feeds`).Scan(&feeds, &failed)
	out["feeds"] = feeds
	out["failed_feeds"] = failed
	return out, e
}

// Accepts literal search phrases only; membership is applied before LIMIT.
func (s *Service) AgentSearch(ctx context.Context, phrase, reportID string, limit int) ([]Article, error) {
	if limit < 1 || limit > 30 {
		return nil, ErrInvalid
	}
	q, e := searchQuery(phrase)
	if e != nil {
		return nil, e
	}
	where := []string{"search MATCH ?", "a.removed=0", "(?='' OR EXISTS(SELECT 1 FROM report_members m WHERE m.run_id=? AND m.article_id=a.id))"}
	args := []any{q, reportID, reportID}
	now := stamp(s.Now())
	for _, term := range strings.Fields(phrase) {
		match, _ := searchQuery(term)
		where = append(where, `(a.id IN (SELECT rowid FROM search WHERE search MATCH ?) OR (EXISTS(SELECT 1 FROM article_content c WHERE c.article_id=a.id AND c.kind='feed' AND c.state='available' AND (a.favorite=1 OR f.archive=1 OR max(c.expires_at,COALESCE(a.protection_until,''))>?)) AND a.id IN (SELECT rowid FROM search WHERE search MATCH ?)) OR (EXISTS(SELECT 1 FROM article_content c WHERE c.article_id=a.id AND c.kind='fulltext' AND c.state='available' AND (a.favorite=1 OR f.archive=1 OR max(c.expires_at,COALESCE(a.protection_until,''))>?)) AND a.id IN (SELECT rowid FROM search WHERE search MATCH ?)))`)
		args = append(args, "{title url organization} : ("+match+")", now, "feed_content : ("+match+")", now, "fulltext : ("+match+")")
	}
	args = append(args, limit)
	rows, e := s.Store.Read.QueryContext(ctx, `SELECT a.id FROM search JOIN articles a ON a.id=search.rowid JOIN feeds f ON f.id=a.feed_id WHERE `+strings.Join(where, " AND ")+` ORDER BY rank LIMIT ?`, args...)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []Article{}
	for _, id := range ids {
		a, e := s.GetArticle(ctx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, nil
}

// External reports use the same fixed cohort and citation builder as built-in reports.
func (s *Service) SubmitAgentReport(ctx context.Context, runID string, result AgentResult) (AgentRun, error) {
	if len(result.Groups) > 2000 || len(result.Blocks) > 60 {
		return AgentRun{}, ErrInvalid
	}
	e := s.write(ctx, func(tx *sql.Tx) error {
		run, e := scanAgentRun(tx.QueryRowContext(ctx, `SELECT `+agentRunCols+` FROM agent_runs WHERE id=? AND kind='report'`, runID))
		if e != nil {
			return e
		}
		if run.State == "running" {
			return ErrConflict
		}
		check := func(bs []AgentBlock, feed string) error {
			if len(bs) > 60 {
				return ErrInvalid
			}
			for i := range bs {
				b := &bs[i]
				if len(b.Text) > 12000 || strings.ContainsAny(b.Text, "<>") || strings.Contains(b.Text, "://") || strings.Contains(b.Text, "](") {
					return ErrInvalid
				}
				if b.Kind != "fact" && b.Kind != "analysis" && b.Kind != "limitation" {
					return ErrInvalid
				}
				if b.Kind != "limitation" && len(b.Citations) == 0 {
					return ErrInvalid
				}
				for j := range b.Citations {
					c := &b.Citations[j]
					var title string
					var n int
					if e := tx.QueryRowContext(ctx, `SELECT title FROM report_members WHERE run_id=? AND article_id=? AND (?='' OR feed_id=?)`, runID, c.ArticleID, feed, feed).Scan(&title); e == sql.ErrNoRows {
						return ErrInvalid
					} else if e != nil {
						return e
					}
					if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM article_content ac JOIN articles a ON a.id=ac.article_id JOIN feeds f ON f.id=a.feed_id WHERE a.id=? AND a.removed=0 AND ac.hash=? AND ac.state='available' AND (a.favorite=1 OR f.archive=1 OR max(ac.expires_at,COALESCE(a.protection_until,''))>?)`, c.ArticleID, c.BasisHash, stamp(s.Now())).Scan(&n); e != nil {
						return e
					}
					if n < 1 {
						return ErrConflict
					}
					c.Title = title
					c.URL = "/?scope=all&article=" + c.ArticleID
					c.Kind = "external_summary"
				}
			}
			return nil
		}
		if e = check(result.Blocks, ""); e != nil {
			return e
		}
		seen := map[string]bool{}
		for i := range result.Groups {
			g := &result.Groups[i]
			if g.ID == "" || seen[g.ID] {
				return ErrInvalid
			}
			seen[g.ID] = true
			if e = tx.QueryRowContext(ctx, `SELECT feed_title,category_id,category_title,importance,count(*) FROM report_members WHERE run_id=? AND feed_id=? GROUP BY feed_id`, runID, g.ID).Scan(&g.Title, &g.CategoryID, &g.CategoryTitle, &g.Importance, &g.Total); e == sql.ErrNoRows {
				return ErrInvalid
			} else if e != nil {
				return e
			}
			if e = check(result.Groups[i].Blocks, result.Groups[i].ID); e != nil {
				return e
			}
		}
		// Submission does not certify processing of members. Counts come from the ledger.
		result.Degraded = true
		result.Counts = map[string]int{"total": run.Total, "pending": 0, "summarized": 0, "reused": 0, "fallback": 0, "unavailable": 0, "fulltext": 0}
		rows, e := tx.QueryContext(ctx, `SELECT state,count(*),sum(depth='fulltext') FROM report_members WHERE run_id=? GROUP BY state`, runID)
		if e != nil {
			return e
		}
		for rows.Next() {
			var state string
			var n, full int
			if e = rows.Scan(&state, &n, &full); e != nil {
				rows.Close()
				return e
			}
			result.Counts[state] = n
			result.Counts["fulltext"] += full
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		result.Trace = []string{"外部汇报；成员处理覆盖未由本系统核验"}
		raw, _ := json.Marshal(result)
		_, e = tx.ExecContext(ctx, `UPDATE agent_runs SET result=?,state='partial',fence=fence+1,lease_until=NULL,updated_at=? WHERE id=?`, string(raw), stamp(s.Now()), runID)
		if e != nil {
			return e
		}
		return s.change(ctx, tx, "agent_run", runID, "external_report", run.Fence+1)
	})
	if e != nil {
		return AgentRun{}, e
	}
	return s.GetAgentRun(ctx, runID)
}

type AgentSourceFact struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Importance  string `json:"importance"`
	Unread      int    `json:"unread_including_archive"`
	Paused      bool   `json:"paused"`
	Error       string `json:"error"`
	LastSuccess string `json:"last_success_at"`
}

func (s *Service) AgentSourceFacts(ctx context.Context) ([]AgentSourceFact, error) {
	rows, e := s.Store.Read.QueryContext(ctx, `SELECT f.id,f.title,f.importance,count(a.id),f.paused,f.error,COALESCE(f.last_success_at,'') FROM feeds f LEFT JOIN articles a ON a.feed_id=f.id AND a.read_at IS NULL AND a.removed=0 GROUP BY f.id ORDER BY (f.error<>'') DESC,count(a.id) DESC,f.id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []AgentSourceFact{}
	for rows.Next() {
		var v AgentSourceFact
		if e = rows.Scan(&v.ID, &v.Title, &v.Importance, &v.Unread, &v.Paused, &v.Error, &v.LastSuccess); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
