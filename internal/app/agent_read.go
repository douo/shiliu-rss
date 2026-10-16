package app

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Format-failure cards contain only a limitation. They are not a summary.
// A successful excerpt fallback is eligible even when full text was unavailable.
const summarizedMember = `m.state IN ('summarized','reused','fallback') AND EXISTS (SELECT 1 FROM json_each(CASE WHEN json_valid(m.card) THEN m.card ELSE '[]' END) b WHERE json_extract(b.value,'$.kind') IN ('fact','analysis') AND trim(COALESCE(json_extract(b.value,'$.text'),''))<>'')`

type AgentReadResult struct {
	MarkedRead int `json:"marked_read"`
}

// MarkAgentReportRead applies to the finished report's fixed, summarized members,
// including archived articles, without affecting later arrivals or coverage gaps.
func (s *Service) MarkAgentReportRead(ctx context.Context, runID string) (AgentReadResult, error) {
	out := AgentReadResult{}
	e := s.write(ctx, func(tx *sql.Tx) error {
		saved, e := s.idempotentResult(ctx, tx)
		if e != nil {
			return e
		}
		if saved != "" {
			return json.Unmarshal([]byte(saved), &out)
		}
		var kind, state string
		e = tx.QueryRowContext(ctx, `SELECT kind,state FROM agent_runs WHERE id=?`, runID).Scan(&kind, &state)
		if e == sql.ErrNoRows {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		if kind != "report" {
			return ErrInvalid
		}
		if state == "queued" || state == "running" || state == "waiting_retry" {
			return ErrConflict
		}
		rows, e := tx.QueryContext(ctx, `SELECT a.id,a.version FROM report_members m JOIN articles a ON a.id=m.article_id WHERE m.run_id=? AND a.read_at IS NULL AND a.removed=0 AND `+summarizedMember, runID)
		if e != nil {
			return e
		}
		type target struct {
			id      string
			version int64
		}
		targets := []target{}
		for rows.Next() {
			var t target
			if e = rows.Scan(&t.id, &t.version); e != nil {
				rows.Close()
				return e
			}
			targets = append(targets, t)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, t := range targets {
			if _, e = tx.ExecContext(ctx, `UPDATE articles SET read_at=?,read_at_unknown=0,version=version+1 WHERE id=?`, stamp(s.Now()), t.id); e != nil {
				return e
			}
			if e = s.change(ctx, tx, "article", t.id, "updated", t.version+1); e != nil {
				return e
			}
		}
		out.MarkedRead = len(targets)
		raw, e := json.Marshal(out)
		if e != nil {
			return e
		}
		return s.saveIdempotency(ctx, tx, string(raw))
	})
	return out, e
}
