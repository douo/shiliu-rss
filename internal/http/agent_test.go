package httpapi

import (
	"encoding/json"
	"shiliu-rss/internal/app"
	"strings"
	"testing"
)

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
