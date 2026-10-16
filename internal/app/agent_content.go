package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

const agentMemberCols = `article_id,feed_id,feed_title,category_id,category_title,importance,title,snapshot_hash,basis_hash,depth,state,card,error,cache_key`

func scanAgentMember(row scanner) (AgentMember, error) {
	var v AgentMember
	var card string
	e := row.Scan(&v.ArticleID, &v.FeedID, &v.FeedTitle, &v.CategoryID, &v.CategoryTitle, &v.Importance, &v.Title, &v.SnapshotHash, &v.BasisHash, &v.Depth, &v.State, &card, &v.Error, &v.CacheKey)
	if e != nil {
		return v, e
	}
	v.Card = []AgentBlock{}
	if card != "" {
		e = json.Unmarshal([]byte(card), &v.Card)
	}
	return v, e
}
func (s *Service) AgentMembers(ctx context.Context, runID, after, feed string, limit int, pending bool) ([]AgentMember, error) {
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return nil, ErrInvalid
	}
	rows, e := s.Store.Read.QueryContext(ctx, `SELECT `+agentMemberCols+` FROM report_members WHERE run_id=? AND (?='' OR article_id>CAST(? AS INTEGER)) AND (?='' OR feed_id=?) AND (?=0 OR state='pending') ORDER BY article_id LIMIT ?`, runID, after, after, feed, feed, pending, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []AgentMember{}
	for rows.Next() {
		v, e := scanAgentMember(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Service) NextAgentFeed(ctx context.Context, runID string) (string, error) {
	var feed string
	e := s.Store.Read.QueryRowContext(ctx, `SELECT m.feed_id FROM report_members m WHERE m.run_id=? GROUP BY m.feed_id HAVING sum(m.state='pending')>0 ORDER BY CASE m.importance WHEN 'high' THEN 0 WHEN 'normal' THEN 1 ELSE 2 END,sum(m.state<>'pending'),m.feed_id LIMIT 1`, runID).Scan(&feed)
	if e == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return feed, e
}
func (s *Service) FindAgentCard(ctx context.Context, key string) ([]AgentBlock, bool, error) {
	var raw string
	e := s.Store.Read.QueryRowContext(ctx, `SELECT card FROM report_members WHERE cache_key=? AND state IN ('summarized','reused') ORDER BY run_id DESC LIMIT 1`, key).Scan(&raw)
	if e == sql.ErrNoRows {
		return nil, false, nil
	}
	if e != nil {
		return nil, false, e
	}
	var blocks []AgentBlock
	e = json.Unmarshal([]byte(raw), &blocks)
	return blocks, e == nil, e
}
func (s *Service) SaveAgentMembers(ctx context.Context, run AgentRun, members []AgentMember) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		if e := s.agentLease(ctx, tx, run); e != nil {
			return e
		}
		for _, m := range members {
			if m.State != "summarized" && m.State != "reused" && m.State != "fallback" && m.State != "unavailable" {
				return ErrInvalid
			}
			if m.Depth == "fulltext" || m.Depth == "feed_excerpt" {
				var valid int
				if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM article_content c JOIN articles a ON a.id=c.article_id JOIN feeds f ON f.id=a.feed_id WHERE a.id=? AND a.removed=0 AND c.hash=? AND c.state='available' AND (a.favorite=1 OR f.archive=1 OR max(c.expires_at,COALESCE(a.protection_until,''))>?)`, m.ArticleID, m.BasisHash, stamp(s.Now())).Scan(&valid); e != nil {
					return e
				}
				if valid < 1 {
					return ErrConflict
				}
			}
			body, e := json.Marshal(m.Card)
			if e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE report_members SET state=?,basis_hash=?,depth=?,card=?,error=?,cache_key=? WHERE run_id=? AND article_id=?`, m.State, m.BasisHash, m.Depth, string(body), m.Error, m.CacheKey, run.ID, m.ArticleID); e != nil {
				return e
			}
			if m.State == "summarized" && (m.Depth == "fulltext" || m.Depth == "feed_excerpt") {
				parts := []string{}
				for _, b := range m.Card {
					if b.Kind != "limitation" {
						parts = append(parts, b.Text)
					}
				}
				if len(parts) > 0 {
					r, e := tx.ExecContext(ctx, `INSERT INTO artifacts(article_id,actor,external_id,kind,content,basis_hash,created_at)VALUES(?,'system:agent',?,'summary',?,?,?) ON CONFLICT(actor,external_id) DO NOTHING`, m.ArticleID, "agent:"+m.CacheKey, strings.Join(parts, "\n\n"), m.BasisHash, stamp(s.Now()))
					if e != nil {
						return e
					}
					n, e := r.RowsAffected()
					if e != nil {
						return e
					}
					if n > 0 {
						if _, e = tx.ExecContext(ctx, `UPDATE articles SET version=version+1 WHERE id=?`, m.ArticleID); e != nil {
							return e
						}
						if e = s.indexArticle(ctx, tx, m.ArticleID); e != nil {
							return e
						}
						var version int64
						if e = tx.QueryRowContext(ctx, `SELECT version FROM articles WHERE id=?`, m.ArticleID).Scan(&version); e != nil {
							return e
						}
						if e = s.change(ctx, tx, "article", m.ArticleID, "agent_summary", version); e != nil {
							return e
						}
					}
				}
			}
		}
		_, e := tx.ExecContext(ctx, `UPDATE agent_runs SET completed=(SELECT count(*) FROM report_members WHERE run_id=? AND state<>'pending'),updated_at=? WHERE id=?`, run.ID, stamp(s.Now()), run.ID)
		return e
	})
}
func (s *Service) AgentCounts(ctx context.Context, runID string) (map[string]int, error) {
	out := map[string]int{"total": 0, "summarized": 0, "reused": 0, "fallback": 0, "unavailable": 0, "pending": 0, "fulltext": 0}
	rows, e := s.Store.Read.QueryContext(ctx, `SELECT state,count(*),sum(depth='fulltext') FROM report_members WHERE run_id=? GROUP BY state`, runID)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var state string
		var n, full int
		if e = rows.Scan(&state, &n, &full); e != nil {
			rows.Close()
			return nil, e
		}
		out[state] = n
		out["total"] += n
		out["fulltext"] += full
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	var summarized, unread int
	e = s.Store.Read.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(a.read_at IS NULL),0) FROM report_members m JOIN articles a ON a.id=m.article_id WHERE m.run_id=? AND a.removed=0 AND `+summarizedMember, runID).Scan(&summarized, &unread)
	out["summarized_articles"], out["unread_summarized"] = summarized, unread
	return out, e
}
func (s *Service) AgentGroups(ctx context.Context, runID string) ([]AgentGroup, error) {
	rows, e := s.Store.Read.QueryContext(ctx, `WITH g AS (SELECT feed_id,feed_title,category_id,category_title,importance,count(*) AS total FROM report_members WHERE run_id=? GROUP BY feed_id) SELECT feed_id,feed_title,category_id,category_title,importance,total FROM g ORDER BY min(CASE importance WHEN 'high' THEN 0 WHEN 'normal' THEN 1 ELSE 2 END) OVER(PARTITION BY category_id),sum(total*CASE importance WHEN 'high' THEN 4 WHEN 'normal' THEN 2 ELSE 1 END) OVER(PARTITION BY category_id) DESC,category_id,CASE importance WHEN 'high' THEN 0 WHEN 'normal' THEN 1 ELSE 2 END,total DESC,feed_id`, runID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []AgentGroup{}
	for rows.Next() {
		var g AgentGroup
		if e = rows.Scan(&g.ID, &g.Title, &g.CategoryID, &g.CategoryTitle, &g.Importance, &g.Total); e != nil {
			return nil, e
		}
		g.Blocks = []AgentBlock{}
		out = append(out, g)
	}
	return out, rows.Err()
}

// Only a fresh important-source policy may create automatic fulltext work.
// A prior attempt (including a failed attempt) is reused, never restarted daily.
func (s *Service) AgentFulltext(ctx context.Context, run AgentRun, articleID string) (Job, error) {
	var out Job
	e := s.write(ctx, func(tx *sql.Tx) error {
		if e := s.agentLease(ctx, tx, run); e != nil {
			return e
		}
		var e error
		out, e = scanJob(tx.QueryRowContext(ctx, `SELECT `+jobCols+` FROM jobs WHERE kind='fulltext' AND target_id=? ORDER BY id DESC LIMIT 1`, articleID))
		if e == nil {
			return nil
		}
		if e != ErrNotFound {
			return e
		}
		var eligible int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE a.id=? AND a.removed=0 AND NOT EXISTS(SELECT 1 FROM article_content WHERE article_id=a.id AND state='purged') AND EXISTS(SELECT 1 FROM report_members WHERE run_id=? AND article_id=a.id AND importance='high') AND (a.favorite=1 OR f.archive=1 OR max(julianday(a.first_seen_at)+90,COALESCE(julianday(a.protection_until),0))>julianday(?))`, articleID, run.ID, stamp(s.Now())).Scan(&eligible); e != nil {
			return e
		}
		if eligible != 1 {
			return ErrConflict
		}
		out, e = s.queue(ctx, tx, "fulltext", articleID)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE jobs SET reason=? WHERE id=?`, "morning_report:"+run.ID, out.ID)
		return e
	})
	return out, e
}

func (s *Service) CreateAgentConversation(ctx context.Context, title, reportID string) (AgentConversation, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "新对话"
	}
	if len([]rune(title)) > 100 {
		return AgentConversation{}, ErrInvalid
	}
	var out AgentConversation
	e := s.write(ctx, func(tx *sql.Tx) error {
		saved, e := s.idempotentResult(ctx, tx)
		if e != nil {
			return e
		}
		if saved != "" {
			out.ID = saved
			return nil
		}
		if reportID != "" {
			var n int
			if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM agent_runs WHERE id=? AND kind='report'`, reportID).Scan(&n); e != nil {
				return e
			}
			if n != 1 {
				return ErrNotFound
			}
		}
		r, e := tx.ExecContext(ctx, `INSERT INTO agent_conversations(title,report_id,created_at,updated_at)VALUES(?,NULLIF(?,''),?,?)`, title, reportID, stamp(s.Now()), stamp(s.Now()))
		if e != nil {
			return e
		}
		n, e := r.LastInsertId()
		if e != nil {
			return e
		}
		out.ID = id(n)
		return s.saveIdempotency(ctx, tx, out.ID)
	})
	if e != nil {
		return out, e
	}
	return s.GetAgentConversation(ctx, out.ID)
}
func (s *Service) GetAgentConversation(ctx context.Context, conversation string) (AgentConversation, error) {
	var v AgentConversation
	var created, updated string
	e := s.Store.Read.QueryRowContext(ctx, `SELECT id,title,COALESCE(report_id,''),created_at,updated_at FROM agent_conversations WHERE id=?`, conversation).Scan(&v.ID, &v.Title, &v.ReportID, &created, &updated)
	if e == sql.ErrNoRows {
		e = ErrNotFound
	}
	v.CreatedAt = parse(created)
	v.UpdatedAt = parse(updated)
	return v, e
}
func (s *Service) AgentConversations(ctx context.Context, before string) ([]AgentConversation, error) {
	rows, e := s.Store.Read.QueryContext(ctx, `SELECT id,title,COALESCE(report_id,''),created_at,updated_at FROM agent_conversations WHERE (?='' OR id<CAST(? AS INTEGER)) ORDER BY id DESC LIMIT 50`, before, before)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []AgentConversation{}
	for rows.Next() {
		var v AgentConversation
		var created, updated string
		if e = rows.Scan(&v.ID, &v.Title, &v.ReportID, &created, &updated); e != nil {
			return nil, e
		}
		v.CreatedAt = parse(created)
		v.UpdatedAt = parse(updated)
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Service) AgentMessages(ctx context.Context, conversation, before string) ([]AgentMessage, error) {
	if _, e := s.GetAgentConversation(ctx, conversation); e != nil {
		return nil, e
	}
	rows, e := s.Store.Read.QueryContext(ctx, `SELECT id,role,text,result,COALESCE(run_id,''),created_at FROM agent_messages WHERE conversation_id=? AND (?='' OR id<CAST(? AS INTEGER)) ORDER BY id DESC LIMIT 50`, conversation, before, before)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []AgentMessage{}
	for rows.Next() {
		var v AgentMessage
		var result, at string
		if e = rows.Scan(&v.ID, &v.Role, &v.Text, &result, &v.RunID, &at); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(result), &v.Result); e != nil {
			return nil, e
		}
		normalizeAgentResult(&v.Result)
		v.CreatedAt = parse(at)
		out = append(out, v)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}
func (s *Service) SendAgentMessage(ctx context.Context, conversation, text string) (AgentRun, error) {
	text = strings.TrimSpace(text)
	if text == "" || len([]rune(text)) > 10000 {
		return AgentRun{}, ErrInvalid
	}
	c, e := s.GetAgentConversation(ctx, conversation)
	if e != nil {
		return AgentRun{}, e
	}
	v, e := s.AgentSettings(ctx)
	if e != nil {
		return AgentRun{}, e
	}
	if !v.Enabled {
		return AgentRun{}, ErrAgentDisabled
	}
	input, _ := json.Marshal(map[string]string{"question": text, "report_id": c.ReportID})
	var runID string
	e = s.write(ctx, func(tx *sql.Tx) error {
		saved, e := s.idempotentResult(ctx, tx)
		if e != nil {
			return e
		}
		if saved != "" {
			runID = saved
			return nil
		}
		var active int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM agent_runs WHERE conversation_id=? AND state IN ('queued','running','waiting_retry')`, conversation).Scan(&active); e != nil {
			return e
		}
		if active > 0 {
			return ErrConflict
		}
		runID, e = s.createAgentRun(ctx, tx, v, "chat", string(input), conversation, "")
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO agent_messages(conversation_id,role,text,run_id,created_at)VALUES(?,'user',?,?,?)`, conversation, text, runID, stamp(s.Now())); e != nil {
			return e
		}
		runes := []rune(text)
		if len(runes) > 40 {
			runes = runes[:40]
		}
		if _, e = tx.ExecContext(ctx, `UPDATE agent_conversations SET title=CASE WHEN title='新对话' THEN ? ELSE title END,updated_at=? WHERE id=?`, string(runes), stamp(s.Now()), conversation); e != nil {
			return e
		}
		return s.saveIdempotency(ctx, tx, runID)
	})
	if e != nil {
		return AgentRun{}, e
	}
	return s.GetAgentRun(ctx, runID)
}

// Restrict ordinary retrieval to a fixed report cohort without exposing SQL.
func (s *Service) AgentReportContains(ctx context.Context, runID, articleID string) (bool, error) {
	if runID == "" {
		return true, nil
	}
	var n int
	e := s.Store.Read.QueryRowContext(ctx, `SELECT count(*) FROM report_members WHERE run_id=? AND article_id=?`, runID, articleID).Scan(&n)
	return n == 1, e
}
