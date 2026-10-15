package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"shiliu-rss/internal/app"
	"shiliu-rss/internal/store"
)

func fixture(t *testing.T, base string) (*Runner, *app.Service) {
	t.Helper()
	st, e := store.Open(context.Background(), filepath.Join(t.TempDir(), "reader.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.Close() })
	s := app.New(st)
	v := app.DefaultAgentSettings()
	v.BaseURL = base + "/v1"
	v.Model = "gpt-6-luna"
	v.Enabled = true
	v.ScheduleEnabled = false
	v.ContextLimit = 64000
	_, e = s.SaveAgentSettings(context.Background(), v, nil)
	if e != nil {
		t.Fatal(e)
	}
	return New(s), s
}
func drain(t *testing.T, r *Runner, id string) app.AgentRun {
	t.Helper()
	for i := 0; i < 80; i++ {
		if e := r.Tick(context.Background()); e != nil {
			t.Fatal(e)
		}
		v, e := r.App.GetAgentRun(context.Background(), id)
		if e != nil {
			t.Fatal(e)
		}
		if v.State == "complete" || v.State == "partial" || v.State == "failed" {
			return v
		}
		if v.State == "waiting_retry" {
			t.Fatalf("unexpected wait: %+v", v)
		}
	}
	t.Fatal("did not complete")
	return app.AgentRun{}
}
func TestReportReuseLongTextAndQuery(t *testing.T) {
	var mu sync.Mutex
	seen := []string{}
	requests := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			t.Error("request")
		}
		if r.URL.Path != "/v1/responses" {
			t.Error("path", r.URL.Path)
		}
		var model string
		json.Unmarshal(req["model"], &model)
		if model != "gpt-6-luna" {
			t.Error("model changed")
		}
		var input string
		json.Unmarshal(req["input"], &input)
		var d struct {
			Evidence []Evidence `json:"evidence"`
			Articles []struct {
				ID string `json:"article_id"`
			} `json:"articles"`
		}
		json.Unmarshal([]byte(input), &d)
		mu.Lock()
		requests++
		for _, e := range d.Evidence {
			seen = append(seen, e.Text)
		}
		mu.Unlock()
		var out any
		if len(d.Articles) > 0 {
			cards := []map[string]any{}
			for _, a := range d.Articles {
				ids := []string{}
				for _, e := range d.Evidence {
					if e.ArticleID == a.ID {
						ids = append(ids, e.ID)
					}
				}
				cards = append(cards, map[string]any{"article_id": a.ID, "status": "ok", "points": []any{map[string]any{"text": "文章讨论合成的 SQLite 索引更新。", "evidence_ids": ids}}, "limitation": ""})
			}
			out = map[string]any{"schema_version": "1", "items": cards}
		} else if len(d.Evidence) == 0 {
			out = Plan{Queries: []string{"SQLite"}, ArticleIDs: []string{}}
		} else {
			ids := []string{}
			for _, e := range d.Evidence {
				ids = append(ids, e.ID)
			}
			out = Narrative{Version: "1", Blocks: []Point{{Kind: "fact", Text: "合成资料讨论 SQLite 更新。", EvidenceIDs: ids}}}
		}
		b, _ := json.Marshal(out)
		delta, _ := json.Marshal(map[string]any{"type": "response.output_text.delta", "delta": string(b)})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"total_tokens\":50}}}\n\n", delta)
	}))
	defer provider.Close()
	r, s := fixture(t, provider.URL)
	ctx := context.Background()
	c, _ := s.CreateCategory(ctx, "技术")
	f, e := s.CreateFeed(ctx, app.Feed{URL: "https://example.invalid/feed", Title: "合成源", Paused: true, CategoryID: c.ID, Importance: "high", UnreadDays: 1})
	if e != nil {
		t.Fatal(e)
	}
	ids, e := s.IngestFeed(ctx, f.ID, []app.FeedEntry{{GUID: "one", URL: "https://example.invalid/a", Title: "SQLite 合成文章", Text: "摘要"}})
	if e != nil {
		t.Fatal(e)
	}
	j, e := s.QueueFulltext(ctx, ids[0])
	if e != nil {
		t.Fatal(e)
	}
	j, e = s.ClaimJob(ctx, "test", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	long := strings.Repeat("begin ", 1000) + "MIDDLE_MUST_BE_READ " + strings.Repeat("ending ", 1200)
	if e = s.CompleteFulltextJob(ctx, j.ID, "test", app.FulltextResult{Text: long}); e != nil {
		t.Fatal(e)
	}
	run, e := s.QueueAgentRun(ctx, "report", "{}", "")
	if e != nil {
		t.Fatal(e)
	}
	done := drain(t, r, run.ID)
	if done.State != "complete" || done.Result.Counts["fulltext"] != 1 {
		t.Fatalf("not complete: %+v", done)
	}
	ms, _ := s.AgentMembers(ctx, run.ID, "", "", 50, false)
	if len(ms) != 1 || ms[0].Depth != "fulltext" {
		t.Fatal(ms)
	}
	a, _ := s.GetArticle(ctx, ids[0])
	if a.ReadAt != nil {
		t.Fatal("report marked read")
	}
	found := false
	for _, text := range seen {
		if strings.Contains(text, "MIDDLE_MUST_BE_READ") {
			found = true
		}
	}
	if !found {
		t.Fatal("middle omitted")
	}
	for _, b := range ms[0].Card {
		for _, c := range b.Citations {
			if c.URL != "/?scope=all&article="+ids[0] {
				t.Fatal("wrong citation", c)
			}
		}
	}
	run2, e := s.QueueAgentRun(ctx, "report", "{}", "")
	if e != nil {
		t.Fatal(e)
	}
	done2 := drain(t, r, run2.ID)
	if done2.Result.Counts["reused"] != 1 {
		t.Fatal("no cache reuse", done2)
	}
	conv, e := s.CreateAgentConversation(ctx, "", run.ID)
	if e != nil {
		t.Fatal(e)
	}
	msg, e := s.SendAgentMessage(ctx, conv.ID, "SQLite 有什么更新？")
	if e != nil {
		t.Fatal(e)
	}
	reply := drain(t, r, msg.ID)
	if reply.State != "complete" {
		t.Fatal(reply)
	}
	msgs, _ := s.AgentMessages(ctx, conv.ID, "")
	if len(msgs) != 2 || len(msgs[1].Result.Blocks) == 0 {
		t.Fatal(msgs)
	}
	if requests < 4 {
		t.Fatal("missing runtime calls")
	}
}
func TestOutputValidation(t *testing.T) {
	ev := []Evidence{{ID: "a1", ArticleID: "1"}}
	bad := []string{`{"schema_version":"1","blocks":[{"kind":"fact","text":"x","evidence_ids":["forged"]}]}`, `{"schema_version":"1","blocks":[{"kind":"fact","text":"x","evidence_ids":[]}]}`, `{"schema_version":"1","blocks":[{"kind":"fact","text":"https://evil.invalid","evidence_ids":["a1"]}]}`, `{"schema_version":"1","blocks":[],"tool":"delete"}`, `{"schema_version":"1","blocks":[]} {}`}
	for _, raw := range bad {
		if _, e := parseNarrative(raw, ev); e == nil {
			t.Fatal("accepted", raw)
		}
	}
}
func TestRealProviderSyntheticReport(t *testing.T) {
	if os.Getenv("SHILIU_AGENT_REAL_TEST") != "1" {
		t.Skip("explicit local live test only")
	}
	base := os.Getenv("SHILIU_AGENT_TEST_BASE")
	if base == "" {
		t.Fatal("base required")
	}
	r, s := fixture(t, base)
	key, e := os.ReadFile(os.Getenv("SHILIU_AGENT_TEST_KEY_FILE"))
	if e != nil {
		t.Fatal("key unavailable")
	}
	v, _ := s.AgentSettings(context.Background())
	v.ContextLimit = 64000
	v.MaxOutput = 4096
	secret := strings.TrimSpace(string(key))
	if _, e = s.SaveAgentSettings(context.Background(), v, &secret); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	c, e := s.CreateCategory(ctx, "合成验证")
	if e != nil {
		t.Fatal(e)
	}
	f, e := s.CreateFeed(ctx, app.Feed{URL: "https://example.invalid/synthetic", Title: "合成技术源", CategoryID: c.ID, Paused: true})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.IngestFeed(ctx, f.ID, []app.FeedEntry{{GUID: "synthetic-1", URL: "https://example.invalid/1", Title: "合成测试：索引增量更新", Text: "这是一则合成测试数据。演示项目把索引更新从每次全量重建改为按变更增量维护。旧格式第一次迁移仍需全量重建；没有性能测量数据。"}, {GUID: "synthetic-2", URL: "https://example.invalid/2", Title: "合成测试：离线阅读", Text: "这是一则合成测试数据。演示阅读器新增离线目录，下载只在用户明确发起时进行。离线包不包含视频。"}})
	if e != nil {
		t.Fatal(e)
	}
	run, e := s.QueueAgentRun(ctx, "report", "{}", "")
	if e != nil {
		t.Fatal(e)
	}
	done := drain(t, r, run.ID)
	if done.State != "complete" {
		t.Fatalf("real report failed: state=%s reason=%s", done.State, done.Error)
	}
	if done.Result.Counts["summarized"] != 2 {
		t.Fatal("wrong coverage")
	}
	citations := 0
	for _, g := range done.Result.Groups {
		for _, b := range g.Blocks {
			citations += len(b.Citations)
			for _, c := range b.Citations {
				if !strings.HasPrefix(c.URL, "/?scope=all&article=") {
					t.Fatal("external citation")
				}
			}
		}
	}
	if citations == 0 {
		t.Fatal("missing citations")
	}
	t.Logf("Responses report complete: %d members, %d internal citations", done.Total, citations)
	conv, e := s.CreateAgentConversation(ctx, "", run.ID)
	if e != nil {
		t.Fatal(e)
	}
	chat, e := s.SendAgentMessage(ctx, conv.ID, "报告里，哪些内容是离线阅读相关？请说明资料限制。")
	if e != nil {
		t.Fatal(e)
	}
	reply := drain(t, r, chat.ID)
	if reply.State != "complete" {
		t.Fatalf("real chat failed: %s %s", reply.State, reply.Error)
	}
	t.Logf("Responses conversation complete: %d blocks", len(reply.Result.Blocks))
}

func TestMalformedArticleDoesNotStopOtherReportMembers(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input string `json:"input"`
		}
		if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
			t.Error(e)
			return
		}
		var input struct {
			Articles []struct {
				ID string `json:"article_id"`
			} `json:"articles"`
			Evidence []Evidence `json:"evidence"`
		}
		if e := json.Unmarshal([]byte(req.Input), &input); e != nil {
			t.Error(e)
			return
		}
		var output any
		if len(input.Articles) > 0 {
			for _, a := range input.Articles {
				if a.ID == "2" || a.ID == "4" {
					json.NewEncoder(w).Encode(map[string]any{"output_text": "invalid synthetic JSON", "usage": map[string]int{"total_tokens": 10}})
					return
				}
			}
			cards := []ArticleCard{}
			for _, a := range input.Articles {
				refs := []string{}
				for _, e := range input.Evidence {
					if e.ArticleID == a.ID {
						refs = append(refs, e.ID)
					}
				}
				cards = append(cards, ArticleCard{ArticleID: a.ID, Status: "ok", Points: []ArticlePoint{{Text: "合成资料讨论增量索引。", EvidenceIDs: refs}}, Limitation: ""})
			}
			output = Batch{Version: "1", Articles: cards}
		} else {
			refs := []string{}
			for _, e := range input.Evidence {
				refs = append(refs, e.ID)
			}
			output = Narrative{Version: "1", Blocks: []Point{{Kind: "analysis", Text: "部分合成资料无法生成摘要。", EvidenceIDs: refs}}}
		}
		b, _ := json.Marshal(output)
		json.NewEncoder(w).Encode(map[string]any{"output_text": string(b), "usage": map[string]int{"total_tokens": 10}})
	}))
	defer provider.Close()
	r, s := fixture(t, provider.URL)
	ctx := context.Background()
	f, e := s.CreateFeed(ctx, app.Feed{URL: "https://example.invalid/normal", Paused: true})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.IngestFeed(ctx, f.ID, []app.FeedEntry{{GUID: "one", Title: "合成甲", Text: "索引资料"}, {GUID: "two", Title: "合成乙", Text: "索引资料"}, {GUID: "three", Title: "合成丙", Text: "索引资料"}})
	if e != nil {
		t.Fatal(e)
	}
	high, e := s.CreateFeed(ctx, app.Feed{URL: "https://example.invalid/high", Paused: true, Importance: "high"})
	if e != nil {
		t.Fatal(e)
	}
	ids, e := s.IngestFeed(ctx, high.ID, []app.FeedEntry{{GUID: "four", Title: "合成全文", URL: "https://example.invalid/article", Text: "摘要"}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.QueueFulltext(ctx, ids[0])
	if e != nil {
		t.Fatal(e)
	}
	job, e := s.ClaimJob(ctx, "fixture", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteFulltextJob(ctx, job.ID, "fixture", app.FulltextResult{Text: "完整合成文章"}); e != nil {
		t.Fatal(e)
	}
	run, e := s.QueueAgentRun(ctx, "report", "{}", "")
	if e != nil {
		t.Fatal(e)
	}
	done := drain(t, r, run.ID)
	if done.State != "partial" || done.Completed != 4 || done.Result.Counts["pending"] != 0 || done.Result.Counts["fallback"] != 2 || done.Result.Counts["summarized"] != 2 {
		t.Fatalf("isolated failure: %+v", done)
	}
	members, e := s.AgentMembers(ctx, run.ID, "", "", 50, false)
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range members {
		if m.State == "fallback" && (m.Depth != "title" || m.Error != "invalid_model_output" || len(m.Card[0].Citations) != 1) {
			t.Fatal("fallback claims unsupported coverage", m)
		}
		a, e := s.GetArticle(ctx, m.ArticleID)
		if e != nil || a.ReadAt != nil {
			t.Fatal("reading state changed", e)
		}
	}
}
