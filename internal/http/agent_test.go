package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"shiliu-rss/internal/app"
	"strings"
	"testing"
)

func TestReportReadHTTP(t *testing.T) {
	s, srv := testServer(t)
	ctx := context.Background()
	v := app.DefaultAgentSettings()
	v.Enabled, v.BaseURL, v.Model = true, "http://localhost:1234/v1", "synthetic"
	if _, e := s.App.SaveAgentSettings(ctx, v, nil); e != nil {
		t.Fatal(e)
	}
	run, e := s.App.QueueAgentRun(ctx, "report", "{}", "")
	if e != nil {
		t.Fatal(e)
	}
	path := "/api/v1/agent/reports/" + run.ID + "/read"
	cookie, csrf := login(t, srv)
	for _, tc := range []struct {
		cookie, csrf string
		code         int
	}{{"", "", 401}, {cookie, "", 403}, {cookie, csrf, 409}} {
		code, _, body := call(t, srv, "POST", path, nil, tc.cookie, tc.csrf, "")
		if code != tc.code {
			t.Fatal(code, string(body))
		}
	}
	if _, e = s.App.ControlAgentRun(ctx, run.ID, "cancel"); e != nil {
		t.Fatal(e)
	}
	code, _, body := call(t, srv, "POST", path, nil, cookie, csrf, "")
	if code != 200 || !strings.Contains(string(body), `"marked_read":0`) {
		t.Fatal(code, string(body))
	}
}

func TestReportGroupReadHTTP(t *testing.T) {
	s, srv := testServer(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		f, e := s.App.CreateFeed(ctx, app.Feed{URL: fmt.Sprintf("https://source%d.test/feed", i)})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.App.IngestFeed(ctx, f.ID, []app.FeedEntry{{GUID: "one", Title: "合成文章"}}); e != nil {
			t.Fatal(e)
		}
	}
	v := app.DefaultAgentSettings()
	v.Enabled, v.BaseURL, v.Model = true, "http://localhost:1234/v1", "synthetic"
	if _, e := s.App.SaveAgentSettings(ctx, v, nil); e != nil {
		t.Fatal(e)
	}
	run, e := s.App.QueueAgentRun(ctx, "report", "{}", "")
	if e != nil {
		t.Fatal(e)
	}
	run, e = s.App.ClaimAgentRun(ctx)
	if e != nil {
		t.Fatal(e)
	}
	members, e := s.App.AgentMembers(ctx, run.ID, "", "", 50, false)
	if e != nil || len(members) != 2 {
		t.Fatal(members, e)
	}
	for i := range members {
		members[i].State, members[i].Depth = "summarized", "title"
		members[i].Card = []app.AgentBlock{{Kind: "fact", Text: "合成摘要"}}
	}
	if e = s.App.SaveAgentMembers(ctx, run, members); e != nil {
		t.Fatal(e)
	}
	path := "/api/v1/agent/reports/" + run.ID + "/read"
	groupPath := path + "?group_id=" + members[0].FeedID
	cookie, csrf := login(t, srv)
	for _, tc := range []struct {
		cookie, csrf string
		code         int
	}{{"", "", 401}, {cookie, "", 403}, {cookie, csrf, 409}} {
		code, _, body := call(t, srv, "POST", groupPath, nil, tc.cookie, tc.csrf, "")
		if code != tc.code {
			t.Fatal(code, string(body))
		}
	}
	groups, e := s.App.AgentGroups(ctx, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.App.SetAgentRunState(ctx, run, "complete", "", s.App.Now(), &app.AgentResult{Groups: groups}); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		path string
		code int
	}{{path + "?group_id=", 400}, {path + "?group_id=999999", 404}, {groupPath, 200}, {groupPath, 200}, {path + "?group_id=" + members[1].FeedID, 409}, {path, 409}} {
		code, _, body := call(t, srv, "POST", tc.path, nil, cookie, csrf, "")
		if code != tc.code || (code == 200 && !strings.Contains(string(body), `"marked_read":1`)) {
			t.Fatal(tc.path, code, string(body))
		}
	}
	code, _, body := call(t, srv, "GET", "/api/v1/agent/runs/"+run.ID, nil, cookie, "", "")
	var loaded app.AgentRun
	if e = json.Unmarshal(body, &loaded); e != nil || code != 200 || len(loaded.Result.Groups) != 2 {
		t.Fatal(code, string(body), e)
	}
	for _, g := range loaded.Result.Groups {
		want := 1
		if g.ID == members[0].FeedID {
			want = 0
		}
		if g.UnreadSummarized != want {
			t.Fatal("HTTP lost live group counts", g)
		}
	}
	a, e := s.App.GetArticle(ctx, members[1].ArticleID)
	if e != nil || a.ReadAt != nil {
		t.Fatal("other group marked", a, e)
	}
}

func TestAgentConfigurationAsyncAPIAndExternalAuth(t *testing.T) {
	_, srv := testServer(t)
	cookie, csrf := login(t, srv)
	code, _, body := call(t, srv, "POST", "/api/v1/agent/report-runs", nil, cookie, csrf, "")
	if code != 409 || !strings.Contains(string(body), "agent_disabled") {
		t.Fatal(code, string(body))
	}
	code, _, body = call(t, srv, "GET", "/api/v1/agent/settings", nil, cookie, "", "")
	if code != 200 {
		t.Fatal(code)
	}
	var settings app.AgentSettings
	json.Unmarshal(body, &settings)
	settings.Enabled = true
	settings.BaseURL = "http://localhost:1234/v1"
	settings.Model = "synthetic"
	payload := map[string]any{"settings": settings, "api_key": "synthetic-secret"}
	code, _, body = call(t, srv, "PUT", "/api/v1/agent/settings", payload, cookie, "", "")
	if code != 403 {
		t.Fatal("CSRF", code)
	}
	code, _, body = call(t, srv, "PUT", "/api/v1/agent/settings", payload, cookie, csrf, "")
	if code != 200 || strings.Contains(string(body), "synthetic-secret") {
		t.Fatal("redaction", code, string(body))
	}
	code, _, body = call(t, srv, "POST", "/api/v1/agent/report-runs", nil, cookie, csrf, "")
	if code != 202 {
		t.Fatal(code, string(body))
	}
	var run app.AgentRun
	json.Unmarshal(body, &run)
	code, _, body = call(t, srv, "POST", "/api/v1/agent/report-runs", nil, cookie, csrf, "")
	var again app.AgentRun
	json.Unmarshal(body, &again)
	if code != 202 || again.ID != run.ID {
		t.Fatal("not idempotent", code)
	}
	code, _, body = call(t, srv, "POST", "/api/v1/tokens", map[string]any{"name": "existing-token", "scopes": []string{"articles:read"}}, cookie, csrf, "")
	if code != 201 {
		t.Fatal(code)
	}
	var tok struct {
		Token string `json:"token"`
	}
	json.Unmarshal(body, &tok)
	code, _, body = call(t, srv, "POST", "/api/v1/agent/queries", map[string]string{"question": "有多少未读？"}, "", "", tok.Token)
	if code != 202 {
		t.Fatal("new scopes introduced", code, string(body))
	}
	code, _, _ = call(t, srv, "POST", "/api/v1/agent/conversations", map[string]string{}, cookie, csrf, "")
	if code != 201 {
		t.Fatal(code)
	}
	code, _, _ = call(t, srv, "GET", "/api/v1/agent/runs", nil, "", "", "")
	if code != 401 {
		t.Fatal("missing existing authentication", code)
	}
}
