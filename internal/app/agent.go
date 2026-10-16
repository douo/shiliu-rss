package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"
	"time"
)

var ErrAgentDisabled = errors.New("agent_disabled")
var ErrAgentBudget = errors.New("agent_budget_exhausted")

type AgentSettings struct {
	Version         int64      `json:"version"`
	Enabled         bool       `json:"enabled"`
	Configured      bool       `json:"configured"`
	KeyConfigured   bool       `json:"key_configured"`
	Name            string     `json:"name"`
	BaseURL         string     `json:"base_url"`
	Model           string     `json:"model"`
	Protocol        string     `json:"protocol"`
	OutputMode      string     `json:"output_mode"`
	ContextLimit    int        `json:"context_limit"`
	MaxOutput       int        `json:"max_output"`
	DailyTokens     int        `json:"daily_tokens"`
	DailyCalls      int        `json:"daily_calls"`
	NormalChars     int        `json:"normal_chars"`
	LowChars        int        `json:"low_chars"`
	RunMinutes      int        `json:"run_minutes"`
	ScheduleEnabled bool       `json:"schedule_enabled"`
	DailyTime       string     `json:"daily_time"`
	Timezone        string     `json:"timezone"`
	NextReportAt    *time.Time `json:"next_report_at"`
}
type AgentCitation struct {
	ArticleID string `json:"article_id"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	BasisHash string `json:"basis_hash"`
	Kind      string `json:"kind"`
}
type AgentBlock struct {
	Kind        string          `json:"kind"`
	Text        string          `json:"text"`
	EvidenceIDs []string        `json:"evidence_ids"`
	Citations   []AgentCitation `json:"citations"`
}
type AgentGroup struct {
	ID            string       `json:"id"`
	Title         string       `json:"title"`
	CategoryID    string       `json:"category_id"`
	CategoryTitle string       `json:"category_title"`
	Importance    string       `json:"importance"`
	Total         int          `json:"total"`
	Blocks        []AgentBlock `json:"blocks"`
}
type AgentResult struct {
	Blocks   []AgentBlock   `json:"blocks"`
	Groups   []AgentGroup   `json:"groups"`
	Counts   map[string]int `json:"counts"`
	Trace    []string       `json:"trace"`
	Degraded bool           `json:"degraded"`
}
type AgentRun struct {
	ID             string      `json:"id"`
	Kind           string      `json:"kind"`
	State          string      `json:"state"`
	ConfigVersion  int64       `json:"config_version"`
	Cutoff         time.Time   `json:"cutoff"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
	DueAt          time.Time   `json:"due_at"`
	Fence          int64       `json:"-"`
	Error          string      `json:"error"`
	Input          string      `json:"-"`
	Result         AgentResult `json:"result"`
	ConversationID string      `json:"conversation_id"`
	Total          int         `json:"total"`
	Completed      int         `json:"completed"`
	HighWatermark  string      `json:"high_watermark"`
}
type AgentMember struct {
	ArticleID     string       `json:"article_id"`
	FeedID        string       `json:"feed_id"`
	FeedTitle     string       `json:"feed_title"`
	CategoryID    string       `json:"category_id"`
	CategoryTitle string       `json:"category_title"`
	Importance    string       `json:"importance"`
	Title         string       `json:"title"`
	CacheKey      string       `json:"-"`
	SnapshotHash  string       `json:"snapshot_hash"`
	BasisHash     string       `json:"basis_hash"`
	Depth         string       `json:"depth"`
	State         string       `json:"state"`
	Card          []AgentBlock `json:"card"`
	Error         string       `json:"error"`
}
type AgentConversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	ReportID  string    `json:"report_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type AgentMessage struct {
	ID        string      `json:"id"`
	Role      string      `json:"role"`
	Text      string      `json:"text"`
	Result    AgentResult `json:"result"`
	RunID     string      `json:"run_id"`
	CreatedAt time.Time   `json:"created_at"`
}
type AgentUsage struct {
	Tokens       int `json:"tokens"`
	Calls        int `json:"calls"`
	UnknownCalls int `json:"unknown_calls"`
}

func DefaultAgentSettings() AgentSettings {
	return AgentSettings{Version: 1, Name: "阅读助理", Protocol: "responses", OutputMode: "text_json", ContextLimit: 32000, MaxOutput: 2048, DailyTokens: 1000000, DailyCalls: 200, NormalChars: 3000, LowChars: 500, RunMinutes: 30, ScheduleEnabled: true, DailyTime: "08:00", Timezone: "Asia/Shanghai"}
}
func nextAgentTime(v AgentSettings, now time.Time) time.Time {
	loc, _ := time.LoadLocation(v.Timezone)
	t, _ := time.Parse("15:04", v.DailyTime)
	n := now.In(loc)
	at := time.Date(n.Year(), n.Month(), n.Day(), t.Hour(), t.Minute(), 0, 0, loc)
	if !at.After(now) {
		at = at.AddDate(0, 0, 1)
	}
	return at.UTC()
}
func (s *Service) AgentSettings(ctx context.Context) (AgentSettings, error) {
	v := DefaultAgentSettings()
	var raw string
	var key bool
	var next sql.NullString
	e := s.Store.Read.QueryRowContext(ctx, `SELECT config,version,api_key<>'',next_report_at FROM agent_settings WHERE id=1`).Scan(&raw, &v.Version, &key, &next)
	if e == sql.ErrNoRows {
		return v, nil
	}
	if e != nil {
		return v, e
	}
	version := v.Version
	if e = json.Unmarshal([]byte(raw), &v); e != nil {
		return v, e
	}
	v.Version = version
	v.Configured = v.BaseURL != "" && v.Model != ""
	v.Enabled = v.Enabled && v.Configured
	v.KeyConfigured = key || os.Getenv("SHILIU_AGENT_API_KEY_FILE") != ""
	v.NextReportAt = ptrTime(next)
	return v, nil
}
func (s *Service) SaveAgentSettings(ctx context.Context, v AgentSettings, key *string) (AgentSettings, error) {
	v.BaseURL = strings.TrimRight(strings.TrimSpace(v.BaseURL), "/")
	v.Model = strings.TrimSpace(v.Model)
	v.Name = strings.TrimSpace(v.Name)
	if len(v.Name) > 200 || len(v.Model) > 200 || len(v.BaseURL) > 2048 || v.ContextLimit < 4096 || v.ContextLimit > 2000000 || v.MaxOutput < 256 || v.MaxOutput > 32768 || v.MaxOutput >= v.ContextLimit/2 || v.DailyTokens < 4096 || v.DailyTokens > 1000000000 || v.DailyCalls < 1 || v.DailyCalls > 10000 || v.NormalChars < 100 || v.NormalChars > 20000 || v.LowChars < 0 || v.LowChars > 5000 || v.RunMinutes < 1 || v.RunMinutes > 180 {
		return AgentSettings{}, ErrInvalid
	}
	if v.BaseURL != "" {
		u, e := url.Parse(v.BaseURL)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return AgentSettings{}, ErrInvalid
		}
	}
	if v.Protocol != "responses" && v.Protocol != "chat_completions" {
		return AgentSettings{}, ErrInvalid
	}
	if v.OutputMode != "text_json" && v.OutputMode != "json_object" && v.OutputMode != "json_schema" {
		return AgentSettings{}, ErrInvalid
	}
	if _, e := time.LoadLocation(v.Timezone); e != nil {
		return AgentSettings{}, ErrInvalid
	}
	if _, e := time.Parse("15:04", v.DailyTime); e != nil {
		return AgentSettings{}, ErrInvalid
	}
	if key != nil && (len(*key) > 8192 || strings.ContainsAny(*key, "\r\n")) {
		return AgentSettings{}, ErrInvalid
	}
	v.Configured = v.BaseURL != "" && v.Model != ""
	v.Enabled = v.Enabled && v.Configured
	v.KeyConfigured = false
	v.NextReportAt = nil
	err := s.write(ctx, func(tx *sql.Tx) error {
		var current int64
		e := tx.QueryRowContext(ctx, `SELECT version FROM agent_settings WHERE id=1`).Scan(&current)
		if e == sql.ErrNoRows {
			current = 1
		} else if e != nil {
			return e
		}
		if current != v.Version {
			return ErrVersion
		}
		v.Version++
		body, e := json.Marshal(v)
		if e != nil {
			return e
		}
		secret := ""
		if key != nil {
			secret = *key
		}
		var next any
		if v.Enabled && v.ScheduleEnabled {
			next = stamp(nextAgentTime(v, s.Now()))
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO agent_settings(id,config,version,api_key,next_report_at)VALUES(1,?,?,?,?) ON CONFLICT(id) DO UPDATE SET config=excluded.config,version=excluded.version,next_report_at=excluded.next_report_at,api_key=CASE WHEN ? THEN excluded.api_key ELSE agent_settings.api_key END`, string(body), v.Version, secret, next, key != nil)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE agent_runs SET state='cancelled',error='configuration_changed',lease_until=NULL,updated_at=? WHERE state IN ('queued','running','waiting_retry')`, stamp(s.Now())); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE jobs SET state='cancelled',lease_owner='',lease_until=NULL,error='agent_configuration_changed',updated_at=? WHERE reason LIKE 'morning_report:%' AND state IN ('queued','running','waiting_retry')`, stamp(s.Now())); e != nil {
			return e
		}
		return s.change(ctx, tx, "agent_settings", "1", "updated", v.Version)
	})
	if err != nil {
		return AgentSettings{}, err
	}
	return s.AgentSettings(ctx)
}
func (s *Service) AgentCredential(ctx context.Context) (string, error) {
	if path := os.Getenv("SHILIU_AGENT_API_KEY_FILE"); path != "" {
		f, e := os.Open(path)
		if e != nil {
			return "", e
		}
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, 8193))
		if e != nil {
			return "", e
		}
		if len(b) > 8192 {
			return "", ErrInvalid
		}
		key := strings.TrimSpace(string(b))
		if strings.ContainsAny(key, "\r\n") {
			return "", ErrInvalid
		}
		return key, nil
	}
	var key string
	e := s.Store.Read.QueryRowContext(ctx, `SELECT api_key FROM agent_settings WHERE id=1`).Scan(&key)
	if e == sql.ErrNoRows {
		return "", nil
	}
	return key, e
}

const agentRunCols = `id,kind,state,config_version,cutoff,created_at,updated_at,due_at,fence,error,input,result,COALESCE(conversation_id,''),total,completed,high_watermark`

func scanAgentRun(row scanner) (AgentRun, error) {
	var v AgentRun
	var cutoff, created, updated, due, result string
	e := row.Scan(&v.ID, &v.Kind, &v.State, &v.ConfigVersion, &cutoff, &created, &updated, &due, &v.Fence, &v.Error, &v.Input, &result, &v.ConversationID, &v.Total, &v.Completed, &v.HighWatermark)
	if e == sql.ErrNoRows {
		return v, ErrNotFound
	}
	if e != nil {
		return v, e
	}
	v.Cutoff = parse(cutoff)
	v.CreatedAt = parse(created)
	v.UpdatedAt = parse(updated)
	v.DueAt = parse(due)
	e = json.Unmarshal([]byte(result), &v.Result)
	normalizeAgentResult(&v.Result)
	return v, e
}
func (s *Service) GetAgentRun(ctx context.Context, runID string) (AgentRun, error) {
	v, e := scanAgentRun(s.Store.Read.QueryRowContext(ctx, `SELECT `+agentRunCols+` FROM agent_runs WHERE id=?`, runID))
	if e == nil && v.Kind == "report" {
		v.Result.Counts, e = s.AgentCounts(ctx, runID)
	}
	return v, e
}
func (s *Service) ListAgentRuns(ctx context.Context, kind, before string, limit int) ([]AgentRun, error) {
	if limit == 0 {
		limit = 30
	}
	if limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	rows, e := s.Store.Read.QueryContext(ctx, `SELECT `+strings.ReplaceAll(agentRunCols, "result,", "'{}' AS result,")+` FROM agent_runs WHERE (?='' OR kind=?) AND (?='' OR id<CAST(? AS INTEGER)) ORDER BY id DESC LIMIT ?`, kind, kind, before, before, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []AgentRun{}
	for rows.Next() {
		v, e := scanAgentRun(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Service) createAgentRun(ctx context.Context, tx *sql.Tx, v AgentSettings, kind, input, conversation, scheduleKey string) (string, error) {
	var current int64
	if e := tx.QueryRowContext(ctx, `SELECT version FROM agent_settings WHERE id=1`).Scan(&current); e != nil {
		return "", e
	}
	if current != v.Version {
		return "", ErrVersion
	}
	if !v.Enabled || !v.Configured {
		return "", ErrAgentDisabled
	}
	now := stamp(s.Now())
	var key any
	if scheduleKey != "" {
		key = scheduleKey
		var existing string
		e := tx.QueryRowContext(ctx, `SELECT id FROM agent_runs WHERE schedule_key=?`, scheduleKey).Scan(&existing)
		if e == nil {
			return existing, nil
		}
		if e != sql.ErrNoRows {
			return "", e
		}
	}
	r, e := tx.ExecContext(ctx, `INSERT INTO agent_runs(kind,config_version,cutoff,created_at,updated_at,due_at,input,conversation_id,schedule_key)VALUES(?,?,?,?,?,?,?,NULLIF(?,''),?)`, kind, v.Version, now, now, now, now, input, conversation, key)
	if e != nil {
		return "", e
	}
	n, e := r.LastInsertId()
	if e != nil {
		return "", e
	}
	runID := id(n)
	if kind == "report" {
		_, e = tx.ExecContext(ctx, `INSERT INTO report_members(run_id,article_id,feed_id,feed_title,category_id,category_title,importance,title,snapshot_hash) SELECT ?,a.id,f.id,f.title,COALESCE(f.category_id,''),COALESCE(c.title,'未分类'),f.importance,a.title,COALESCE((SELECT hash FROM article_content WHERE article_id=a.id ORDER BY kind DESC LIMIT 1),'') FROM articles a JOIN feeds f ON f.id=a.feed_id LEFT JOIN categories c ON c.id=f.category_id WHERE a.read_at IS NULL AND a.removed=0`, runID)
		if e != nil {
			return "", e
		}
		_, e = tx.ExecContext(ctx, `UPDATE agent_runs SET total=(SELECT count(*) FROM report_members WHERE run_id=?),high_watermark=(SELECT CAST(COALESCE(max(seq),0) AS TEXT) FROM changes) WHERE id=?`, runID, runID)
		if e != nil {
			return "", e
		}
	}
	return runID, nil
}
func (s *Service) QueueAgentRun(ctx context.Context, kind, input, conversation string) (AgentRun, error) {
	if kind != "report" && kind != "chat" && kind != "query" {
		return AgentRun{}, ErrInvalid
	}
	v, e := s.AgentSettings(ctx)
	if e != nil {
		return AgentRun{}, e
	}
	if !v.Enabled {
		return AgentRun{}, ErrAgentDisabled
	}
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
		runID, e = s.createAgentRun(ctx, tx, v, kind, input, conversation, "")
		if e != nil {
			return e
		}
		return s.saveIdempotency(ctx, tx, runID)
	})
	if e != nil {
		return AgentRun{}, e
	}
	return s.GetAgentRun(ctx, runID)
}
func (s *Service) ScheduleAgentDue(ctx context.Context) error {
	v, e := s.AgentSettings(ctx)
	if e != nil || !v.Enabled || !v.ScheduleEnabled || v.NextReportAt == nil || s.Now().Before(*v.NextReportAt) {
		return e
	}
	loc, _ := time.LoadLocation(v.Timezone)
	scheduled := nextAgentTime(v, s.Now()).In(loc).AddDate(0, 0, -1)
	key := id(v.Version) + ":" + scheduled.Format("2006-01-02")
	return s.write(ctx, func(tx *sql.Tx) error {
		var next sql.NullString
		if e := tx.QueryRowContext(ctx, `SELECT next_report_at FROM agent_settings WHERE id=1 AND version=?`, v.Version).Scan(&next); e != nil {
			return e
		}
		if !next.Valid || next.String > stamp(s.Now()) {
			return nil
		}
		if _, e := s.createAgentRun(ctx, tx, v, "report", "{}", "", key); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, `UPDATE agent_settings SET next_report_at=? WHERE id=1`, stamp(nextAgentTime(v, s.Now())))
		return e
	})
}
func (s *Service) ClaimAgentRun(ctx context.Context) (AgentRun, error) {
	var out AgentRun
	e := s.write(ctx, func(tx *sql.Tx) error {
		now := stamp(s.Now())
		var runID string
		e := tx.QueryRowContext(ctx, `SELECT id FROM agent_runs WHERE (state IN ('queued','waiting_retry') AND due_at<=?) OR (state='running' AND lease_until<=?) ORDER BY CASE WHEN kind IN ('chat','query') THEN 0 ELSE 1 END,due_at,id LIMIT 1`, now, now).Scan(&runID)
		if e == sql.ErrNoRows {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE agent_runs SET state='running',fence=fence+1,lease_until=?,updated_at=? WHERE id=?`, stamp(s.Now().Add(2*time.Minute)), now, runID)
		if e != nil {
			return e
		}
		out, e = scanAgentRun(tx.QueryRowContext(ctx, `SELECT `+agentRunCols+` FROM agent_runs WHERE id=?`, runID))
		return e
	})
	return out, e
}
func (s *Service) agentLease(ctx context.Context, tx *sql.Tx, run AgentRun) error {
	var n int
	e := tx.QueryRowContext(ctx, `SELECT count(*) FROM agent_runs r JOIN agent_settings s ON s.id=1 WHERE r.id=? AND r.state='running' AND r.fence=? AND r.lease_until>? AND r.config_version=s.version`, run.ID, run.Fence, stamp(s.Now())).Scan(&n)
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrLease
	}
	return nil
}
func (s *Service) RenewAgentRun(ctx context.Context, run AgentRun) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		if e := s.agentLease(ctx, tx, run); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, `UPDATE agent_runs SET lease_until=? WHERE id=?`, stamp(s.Now().Add(2*time.Minute)), run.ID)
		return e
	})
}
func (s *Service) SetAgentRunState(ctx context.Context, run AgentRun, state, reason string, due time.Time, result *AgentResult) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		if e := s.agentLease(ctx, tx, run); e != nil {
			return e
		}
		var raw any
		if result != nil {
			b, e := json.Marshal(result)
			if e != nil {
				return e
			}
			raw = string(b)
		}
		_, e := tx.ExecContext(ctx, `UPDATE agent_runs SET state=?,error=?,due_at=?,result=COALESCE(?,result),lease_until=NULL,updated_at=? WHERE id=?`, state, reason, stamp(due), raw, stamp(s.Now()), run.ID)
		if e != nil {
			return e
		}
		if result != nil && run.ConversationID != "" {
			body, _ := json.Marshal(result)
			_, e = tx.ExecContext(ctx, `INSERT INTO agent_messages(conversation_id,role,result,run_id,created_at) SELECT ?,'assistant',?,?,? WHERE NOT EXISTS(SELECT 1 FROM agent_messages WHERE run_id=? AND role='assistant')`, run.ConversationID, string(body), run.ID, stamp(s.Now()), run.ID)
			if e != nil {
				return e
			}
		}
		return s.change(ctx, tx, "agent_run", run.ID, state, run.Fence)
	})
}
func (s *Service) ControlAgentRun(ctx context.Context, runID, action string) (AgentRun, error) {
	v, e := s.AgentSettings(ctx)
	if e != nil {
		return AgentRun{}, e
	}
	if action != "cancel" && action != "retry" {
		return AgentRun{}, ErrInvalid
	}
	e = s.write(ctx, func(tx *sql.Tx) error {
		r, e := scanAgentRun(tx.QueryRowContext(ctx, `SELECT `+agentRunCols+` FROM agent_runs WHERE id=?`, runID))
		if e != nil {
			return e
		}
		state := "cancelled"
		if action == "retry" {
			if !v.Enabled {
				return ErrAgentDisabled
			}
			if r.State == "running" || r.State == "queued" || r.State == "waiting_retry" {
				return nil
			}
			state = "queued"
			if _, e = tx.ExecContext(ctx, `UPDATE report_members SET state='pending',error='' WHERE run_id=? AND state IN ('fallback','unavailable')`, runID); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `DELETE FROM agent_messages WHERE run_id=? AND role='assistant'`, runID); e != nil {
				return e
			}
		} else if r.State == "complete" || r.State == "partial" || r.State == "failed" {
			return nil
		}
		_, e = tx.ExecContext(ctx, `UPDATE agent_runs SET state=?,config_version=?,error='',fence=fence+1,lease_until=NULL,due_at=?,updated_at=?,created_at=CASE WHEN ?='queued' THEN ? ELSE created_at END WHERE id=?`, state, v.Version, stamp(s.Now()), stamp(s.Now()), state, stamp(s.Now()), runID)
		return e
	})
	if e != nil {
		return AgentRun{}, e
	}
	return s.GetAgentRun(ctx, runID)
}

func (s *Service) AgentUsage(ctx context.Context, v AgentSettings) (AgentUsage, error) {
	loc, _ := time.LoadLocation(v.Timezone)
	var out AgentUsage
	e := s.Store.Read.QueryRowContext(ctx, `SELECT COALESCE(sum(CASE WHEN state IN ('reserved','unknown') THEN reserved_tokens ELSE used_tokens END),0),count(*),COALESCE(sum(state IN ('reserved','unknown')),0) FROM agent_steps WHERE day=?`, s.Now().In(loc).Format("2006-01-02")).Scan(&out.Tokens, &out.Calls, &out.UnknownCalls)
	return out, e
}
func (s *Service) CachedAgentStep(ctx context.Context, key string) (string, bool, error) {
	var raw string
	e := s.Store.Read.QueryRowContext(ctx, `SELECT result FROM agent_steps WHERE step_key=? AND state='done' ORDER BY id DESC LIMIT 1`, key).Scan(&raw)
	if e == sql.ErrNoRows {
		return "", false, nil
	}
	return raw, e == nil, e
}
func (s *Service) ReserveAgentCall(ctx context.Context, run AgentRun, v AgentSettings, key string, tokens int) (string, error) {
	var stepID string
	e := s.write(ctx, func(tx *sql.Tx) error {
		if e := s.agentLease(ctx, tx, run); e != nil {
			return e
		}
		loc, _ := time.LoadLocation(v.Timezone)
		day := s.Now().In(loc).Format("2006-01-02")
		var used, calls int
		if e := tx.QueryRowContext(ctx, `SELECT COALESCE(sum(CASE WHEN state IN ('reserved','unknown') THEN reserved_tokens ELSE used_tokens END),0),count(*) FROM agent_steps WHERE day=?`, day).Scan(&used, &calls); e != nil {
			return e
		}
		if used+tokens > v.DailyTokens || calls >= v.DailyCalls {
			return ErrAgentBudget
		}
		r, e := tx.ExecContext(ctx, `INSERT INTO agent_steps(run_id,step_key,day,reserved_tokens,created_at)VALUES(?,?,?,?,?)`, run.ID, key, day, tokens, stamp(s.Now()))
		if e != nil {
			return e
		}
		n, e := r.LastInsertId()
		stepID = id(n)
		return e
	})
	return stepID, e
}
func (s *Service) FinishAgentCall(ctx context.Context, run AgentRun, stepID, state, output string, tokens int) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		if e := s.agentLease(ctx, tx, run); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, `UPDATE agent_steps SET state=?,result=?,used_tokens=? WHERE id=? AND run_id=?`, state, output, max(0, tokens), stepID, run.ID)
		return e
	})
}

func normalizeAgentResult(v *AgentResult) {
	if v.Blocks == nil {
		v.Blocks = []AgentBlock{}
	}
	if v.Groups == nil {
		v.Groups = []AgentGroup{}
	}
	if v.Counts == nil {
		v.Counts = map[string]int{}
	}
	if v.Trace == nil {
		v.Trace = []string{}
	}
}
