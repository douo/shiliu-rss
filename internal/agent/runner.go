package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"shiliu-rss/internal/app"
)

type Runner struct {
	App       *app.Service
	Client    *Client
	wake      chan struct{}
	FetchWake func()
	mu        sync.Mutex
	lastError string
}

func New(s *app.Service) *Runner {
	return &Runner{App: s, Client: NewClient(), wake: make(chan struct{}, 1)}
}
func (r *Runner) Wake() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}
func (r *Runner) Run(ctx context.Context) {
	timer := time.NewTicker(2 * time.Second)
	defer timer.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if e := r.Tick(ctx); e == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-r.wake:
		}
	}
}
func (r *Runner) Tick(ctx context.Context) error {
	if e := r.App.ScheduleAgentDue(ctx); e != nil {
		return e
	}
	run, e := r.App.ClaimAgentRun(ctx)
	if e != nil {
		return e
	}
	v, e := r.App.AgentSettings(ctx)
	if e != nil {
		return e
	}
	if !v.Enabled || v.Version != run.ConfigVersion {
		return r.App.SetAgentRunState(ctx, run, "cancelled", "configuration_changed", r.App.Now(), nil)
	}
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		t := time.NewTicker(25 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-work.Done():
				return
			case <-t.C:
				if r.App.RenewAgentRun(work, run) != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-heartbeatDone }()
	if run.Kind == "report" {
		e = r.report(work, run, v)
	} else {
		e = r.chat(work, run, v)
	}
	if e == nil || errors.Is(e, app.ErrLease) || ctx.Err() != nil {
		return e
	}
	var p *ProviderError
	if errors.As(e, &p) && p.Retryable {
		delay := max(p.RetryAfter, 15*time.Second)
		return r.App.SetAgentRunState(ctx, run, "waiting_retry", p.Code, r.App.Now().Add(delay), nil)
	}
	reason := "agent_processing_failed"
	if errors.Is(e, app.ErrAgentBudget) {
		reason = "daily_budget_exhausted"
	} else if errors.Is(e, ErrFormat) {
		reason = "invalid_model_output"
	} else if errors.As(e, &p) {
		reason = p.Code
	}
	if run.Kind == "report" {
		result, fe := r.finalize(ctx, run, v, false)
		if fe == nil {
			return r.App.SetAgentRunState(ctx, run, "partial", reason, r.App.Now(), &result)
		}
	}
	result := app.AgentResult{Blocks: []app.AgentBlock{{Kind: "limitation", Text: "处理未完成：" + reason, Citations: []app.AgentCitation{}, EvidenceIDs: []string{}}}, Groups: []app.AgentGroup{}, Counts: map[string]int{}, Trace: []string{}, Degraded: true}
	return r.App.SetAgentRunState(ctx, run, "failed", reason, r.App.Now(), &result)
}

// Inputs remain transient. Only validated derived outputs are cached.
func (r *Runner) call(ctx context.Context, run app.AgentRun, v app.AgentSettings, task string, input any, schema any, valid func(string) error) (string, error) {
	key := hash([]any{promptVersion, v.Protocol, v.BaseURL, v.Model, v.OutputMode, v.MaxOutput, task, input})
	if raw, ok, e := r.App.CachedAgentStep(ctx, key); e != nil {
		return "", e
	} else if ok && valid(raw) == nil {
		return raw, nil
	}
	secret, e := r.App.AgentCredential(ctx)
	if e != nil {
		return "", &ProviderError{Code: "api_key_unavailable"}
	}
	b, _ := json.Marshal(input)
	instructions := systemPrompt + "\n" + task + "\n严格使用以下 JSON Schema：" + schemaText(schema)
	if task == narrativePrompt {
		instructions += "\n输出最多两个主题块；每块可综合多篇资料，保留关键条件。"
	}
	// One UTF-8 byte per token is deliberately conservative for reservation/context limits.
	tokens := len(b) + len(instructions) + v.MaxOutput
	if tokens > v.ContextLimit {
		return "", &ProviderError{Code: "context_limit_exceeded"}
	}
	for attempt := 0; attempt < 3; attempt++ {
		if r.App.Now().After(run.CreatedAt.Add(time.Duration(v.RunMinutes) * time.Minute)) {
			return "", &ProviderError{Code: "run_deadline_exceeded"}
		}
		step, e := r.App.ReserveAgentCall(ctx, run, v, key, tokens)
		if e != nil {
			return "", e
		}
		completion, e := r.Client.Call(ctx, v, secret, instructions, input, schema)
		if e != nil {
			_ = r.App.FinishAgentCall(ctx, run, step, "unknown", "", 0)
			var p *ProviderError
			if errors.As(e, &p) && p.Retryable && attempt < 2 && p.RetryAfter <= 20*time.Second {
				wait := max(p.RetryAfter, time.Duration(attempt+1)*time.Second)
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(wait):
					continue
				}
			}
			return "", e
		}
		used := completion.Tokens
		if used == 0 {
			used = tokens
		}
		if valid(completion.Text) != nil {
			if e = r.App.FinishAgentCall(ctx, run, step, "invalid", "", used); e != nil {
				return "", e
			}
			if attempt == 0 {
				instructions += "\n上次输出不符合约束。仅返回上述 schema 的 JSON，检查每个引用 ID、文章顺序和必填字段。"
				continue
			}
			return "", ErrFormat
		}
		if e = r.App.FinishAgentCall(ctx, run, step, "done", completion.Text, used); e != nil {
			return "", e
		}
		return completion.Text, nil
	}
	return "", ErrFormat
}
func limitText(text string, n int) string {
	v := []rune(text)
	if len(v) > n {
		return string(v[:n])
	}
	return text
}
func selected(a app.Article) (app.Content, bool) {
	for _, kind := range []string{"fulltext", "feed"} {
		for _, c := range a.Content {
			if c.Kind == kind && c.State == "available" && c.Text != "" {
				return c, true
			}
		}
	}
	return app.Content{}, false
}
func evidenceFor(a app.Article, chars int) Evidence {
	e := Evidence{ID: "a" + a.ID, ArticleID: a.ID, Title: a.Title, Kind: "title", Hash: hash(a.Title), Text: a.Title}
	if c, ok := selected(a); ok && chars > 0 {
		e.Hash = c.Hash
		e.Kind = "feed_excerpt"
		e.Text = limitText(c.Text, chars)
	} else if len(a.Artifacts) > 0 {
		e.Kind = "saved_summary"
		e.Hash = a.Artifacts[len(a.Artifacts)-1].BasisHash
		e.Text = limitText(a.Artifacts[len(a.Artifacts)-1].Content, chars)
	}
	return e
}
func cardEvidence(m app.AgentMember) Evidence {
	p := []string{}
	for _, b := range m.Card {
		p = append(p, b.Text)
	}
	return Evidence{ID: "a" + m.ArticleID, ArticleID: m.ArticleID, Title: m.Title, Hash: m.BasisHash, Kind: "saved_summary", Text: strings.Join(p, "\n")}
}
func (r *Runner) articleBatch(ctx context.Context, run app.AgentRun, v app.AgentSettings, ev []Evidence) (map[string][]app.AgentBlock, error) {
	ids := []string{}
	articles := []map[string]any{}
	for _, e := range ev {
		ids = append(ids, e.ArticleID)
		articles = append(articles, map[string]any{"article_id": e.ArticleID, "required_depth": e.Kind, "evidence_ids": []string{e.ID}})
	}
	input := map[string]any{"articles": articles, "evidence": ev, "prompt_version": promptVersion}
	var out map[string][]app.AgentBlock
	_, e := r.call(ctx, run, v, articlePrompt, input, articleSchema, func(raw string) error { var e error; out, e = parseBatch(raw, ev, ids); return e })
	return out, e
}
func (r *Runner) narrative(ctx context.Context, run app.AgentRun, v app.AgentSettings, task string, ev []Evidence, extra any) ([]app.AgentBlock, error) {
	var out []app.AgentBlock
	input := map[string]any{"evidence": ev, "task": extra, "prompt_version": promptVersion}
	_, e := r.call(ctx, run, v, task, input, narrativeSchema, func(raw string) error {
		var e error
		out, e = parseNarrative(raw, ev)
		if e == nil && task == narrativePrompt && len(out) > 2 {
			return ErrFormat
		}
		return e
	})
	return out, e
}
func (r *Runner) report(ctx context.Context, run app.AgentRun, v app.AgentSettings) error {
	if r.App.Now().After(run.CreatedAt.Add(time.Duration(v.RunMinutes) * time.Minute)) {
		res, e := r.finalize(ctx, run, v, false)
		if e != nil {
			return e
		}
		return r.App.SetAgentRunState(ctx, run, "partial", "run_deadline_exceeded", r.App.Now(), &res)
	}
	feed, e := r.App.NextAgentFeed(ctx, run.ID)
	if errors.Is(e, app.ErrNotFound) {
		res, e := r.finalize(ctx, run, v, true)
		if e != nil {
			return e
		}
		state := "complete"
		if res.Degraded {
			state = "partial"
		}
		return r.App.SetAgentRunState(ctx, run, state, "", r.App.Now(), &res)
	}
	if e != nil {
		return e
	}
	ms, e := r.App.AgentMembers(ctx, run.ID, "", feed, 8, true)
	if e != nil {
		return e
	}
	if len(ms) == 0 {
		return app.ErrConflict
	}
	if ms[0].Importance == "high" {
		ms = ms[:1]
	}
	prepared := []app.AgentMember{}
	evs := []Evidence{}
	need := []app.AgentMember{}
	for _, m := range ms {
		a, e := r.App.GetArticle(ctx, m.ArticleID)
		if e != nil {
			return e
		}
		if a.Removed {
			m.State = "unavailable"
			m.Depth = "title"
			m.Error = "article_removed"
			prepared = append(prepared, m)
			continue
		}
		chars := v.NormalChars
		if m.Importance == "low" {
			chars = v.LowChars
		}
		ev := evidenceFor(a, chars)
		m.Depth = ev.Kind
		m.BasisHash = ev.Hash
		c, available := selected(a)
		if m.Importance == "high" && (!available || c.Kind != "fulltext") {
			job, je := r.App.AgentFulltext(ctx, run, m.ArticleID)
			if je == nil && (job.State == "queued" || job.State == "running" || job.State == "waiting_retry") {
				if r.FetchWake != nil {
					r.FetchWake()
				}
				return r.App.SetAgentRunState(ctx, run, "waiting_retry", "waiting_for_fulltext", r.App.Now().Add(5*time.Second), nil)
			}
			m.Error = "fulltext_unavailable" // Explicitly partial, never claims full-text coverage.
		}
		if m.Importance == "high" && available && c.Kind == "fulltext" {
			ev.Kind = "fulltext"
			ev.Hash = c.Hash
			m.Depth = ev.Kind
			m.BasisHash = ev.Hash
		}
		m.CacheKey = hash([]any{promptVersion, v.Protocol, v.BaseURL, v.Model, v.OutputMode, v.MaxOutput, m.ArticleID, m.Title, m.BasisHash, m.Depth, chars})
		if b, ok, e := r.App.FindAgentCard(ctx, m.CacheKey); e != nil {
			return e
		} else if ok {
			m.Card = b
			m.State = "reused"
			prepared = append(prepared, m)
			continue
		}
		if m.Importance == "high" && available && c.Kind == "fulltext" {
			// Every chunk is processed; checkpointed model outputs allow restart reuse.
			chunkSize := max(256, min(5000, (v.ContextLimit-v.MaxOutput-8000)/4))
			runes := []rune(c.Text)
			parts := []app.AgentBlock{}
			for start := 0; start < len(runes); start += chunkSize {
				ch := ev
				ch.ID = fmt.Sprintf("a%s-c%d", m.ArticleID, start/chunkSize)
				ch.Text = string(runes[start:min(start+chunkSize, len(runes))])
				cards, e := r.articleBatch(ctx, run, v, []Evidence{ch})
				if e != nil {
					return e
				}
				parts = append(parts, cards[m.ArticleID]...)
			}
			// Hierarchical reduction keeps long documents bounded without omitting chunks.
			reduced, e := r.reduce(ctx, run, v, parts, "归并同一篇文章的全部块，保留关键条件。", m.ArticleID, m.Title, m.BasisHash)
			if e != nil {
				return e
			}
			m.Card = reduced
			m.State = "summarized"
			prepared = append(prepared, m)
			continue
		}
		// Each batch has a fixed input cap; a low configured context uses smaller batches.
		evs = append(evs, ev)
		need = append(need, m)
	}
	// Small batches avoid provider truncation and preserve exact membership.
	for len(evs) > 0 {
		n := min(3, len(evs))
		for n > 1 {
			b, _ := json.Marshal(evs[:n])
			if len(b)+11000+v.MaxOutput <= v.ContextLimit {
				break
			}
			n--
		}
		cards, e := r.articleBatch(ctx, run, v, evs[:n])
		if e != nil {
			return e
		}
		for _, m := range need[:n] {
			m.Card = cards[m.ArticleID]
			m.State = "summarized"
			if m.Error != "" {
				m.State = "fallback"
			}
			prepared = append(prepared, m)
		}
		evs = evs[n:]
		need = need[n:]
	}
	if e = r.App.SaveAgentMembers(ctx, run, prepared); e != nil {
		return e
	}
	return r.App.SetAgentRunState(ctx, run, "queued", "", r.App.Now(), nil)
}
func (r *Runner) reduce(ctx context.Context, run app.AgentRun, v app.AgentSettings, bs []app.AgentBlock, title, articleID, name, basis string) ([]app.AgentBlock, error) {
	for len(bs) > 5 {
		next := []app.AgentBlock{}
		for start := 0; start < len(bs); start += 5 {
			ev := []Evidence{}
			for i, b := range bs[start:min(start+5, len(bs))] {
				ev = append(ev, Evidence{ID: fmt.Sprintf("chunk%d", i), ArticleID: articleID, Title: name, Hash: basis, Kind: "saved_summary", Text: b.Text})
			}
			out, e := r.narrative(ctx, run, v, narrativePrompt, ev, title+" 输出最多2个主题块。")
			if e != nil {
				return nil, e
			}

			next = append(next, out...)
		}
		bs = next
	}
	return bs, nil
}
func (r *Runner) finalize(ctx context.Context, run app.AgentRun, v app.AgentSettings, model bool) (app.AgentResult, error) {
	counts, e := r.App.AgentCounts(ctx, run.ID)
	if e != nil {
		return app.AgentResult{}, e
	}
	groups, e := r.App.AgentGroups(ctx, run.ID)
	if e != nil {
		return app.AgentResult{}, e
	}
	res := app.AgentResult{Groups: groups, Counts: counts, Blocks: []app.AgentBlock{}, Trace: []string{"完整未读快照（含归档）；不更改已读状态"}, Degraded: counts["pending"]+counts["fallback"]+counts["unavailable"] > 0}
	for i, g := range groups {
		after := ""
		summaries := []app.AgentBlock{}
		for {
			ms, e := r.App.AgentMembers(ctx, run.ID, after, g.ID, 3, false)
			if e != nil {
				return res, e
			}
			if len(ms) == 0 {
				break
			}
			ev := []Evidence{}
			for _, m := range ms {
				if len(m.Card) > 0 {
					ev = append(ev, cardEvidence(m))
				}
			}
			if len(ev) > 0 {
				var bs []app.AgentBlock
				if model {
					bs, e = r.narrative(ctx, run, v, narrativePrompt, ev, map[string]any{"group": g.Title, "importance": g.Importance, "instruction": "按主题聚合；最多2个信息密集的块，保留不同文章差异。"})
					if e != nil {
						model = false
						res.Degraded = true
						res.Trace = append(res.Trace, "聚合未完成，展示已完成的文章卡片")
						if errors.Is(e, app.ErrLease) {
							return res, e
						}
					}
				}
				if !model {
					for _, m := range ms {
						if len(m.Card) > 0 && len(bs) < 5 {
							bs = append(bs, m.Card[0])
						}
					}
				}
				summaries = append(summaries, bs...)
				if !model && len(summaries) > 8 {
					summaries = summaries[:8]
				}
			}
			after = ms[len(ms)-1].ArticleID
			if len(ms) < 3 {
				break
			}
		}
		// Preserve evidence mappings through each bounded reduction level.
		for model && len(summaries) > 8 {
			next := []app.AgentBlock{}
			for start := 0; start < len(summaries); start += 4 {
				part := summaries[start:min(start+4, len(summaries))]
				ev := []Evidence{}
				for j, b := range part {
					for _, c := range b.Citations {
						ev = append(ev, Evidence{ID: fmt.Sprintf("p%d-a%s", j, c.ArticleID), ArticleID: c.ArticleID, Title: c.Title, Hash: c.BasisHash, Kind: "saved_summary", Text: b.Text})
					}
				}
				if len(ev) == 0 {
					continue
				}
				out, e := r.narrative(ctx, run, v, narrativePrompt, ev, map[string]string{"group": g.Title, "instruction": "聚合为最多两个主题块。"})
				if e != nil {
					model = false
					res.Degraded = true
					break
				}

				next = append(next, out...)
			}
			if model {
				summaries = next
			}
		}
		if len(summaries) > 8 {
			summaries = summaries[:8]
			summaries = append(summaries, app.AgentBlock{Kind: "limitation", Text: "聚合处理未完成；完整文章卡片可在成员列表查看。", Citations: []app.AgentCitation{}, EvidenceIDs: []string{}})
		}
		res.Groups[i].Blocks = summaries
	}
	if counts["total"] == 0 {
		res.Blocks = append(res.Blocks, app.AgentBlock{Kind: "limitation", Text: "截止时没有未读文章。", Citations: []app.AgentCitation{}, EvidenceIDs: []string{}})
	} else if res.Degraded {
		res.Blocks = append(res.Blocks, app.AgentBlock{Kind: "limitation", Text: "本报告存在缺文或未完成处理，请结合覆盖统计与文章卡片阅读。", Citations: []app.AgentCitation{}, EvidenceIDs: []string{}})
	}
	return res, nil
}
