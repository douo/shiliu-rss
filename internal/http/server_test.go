package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"shiliu-rss/api"
	"shiliu-rss/internal/app"
	"shiliu-rss/internal/store"
)

func testServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	db, e := store.Open(context.Background(), filepath.Join(t.TempDir(), "reader.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	a := app.New(db)
	if e = Initialize(context.Background(), a, "synthetic-test-password"); e != nil {
		t.Fatal(e)
	}
	s := &Server{App: a, Assets: fstest.MapFS{"200.html": {Data: []byte("reader shell")}, "_app/immutable/x.js": {Data: []byte("export {}")}}}
	srv := httptest.NewServer(s.Handler())
	s.Origin = srv.URL
	t.Cleanup(srv.Close)
	return s, srv
}
func call(t *testing.T, srv *httptest.Server, method, path string, body any, cookie, csrf, token string) (int, http.Header, []byte) {
	t.Helper()
	var data []byte
	switch b := body.(type) {
	case string:
		data = []byte(b)
	case nil:
	default:
		data, _ = json.Marshal(b)
	}
	req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(data))
	req.Header.Set("Origin", srv.URL)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Idempotency-Key", "synthetic-request")
	resp, e := srv.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, b
}
func login(t *testing.T, srv *httptest.Server) (string, string) {
	code, h, b := call(t, srv, "POST", "/api/v1/session", map[string]string{"password": "synthetic-test-password"}, "", "", "")
	if code != 200 {
		t.Fatalf("login %d %s", code, b)
	}
	var v struct {
		CSRF string `json:"csrf_token"`
	}
	json.Unmarshal(b, &v)
	cookie := strings.Split(h.Get("Set-Cookie"), ";")[0]
	if !strings.Contains(h.Get("Set-Cookie"), "HttpOnly") {
		t.Fatal("cookie not httponly")
	}
	return cookie, v.CSRF
}
func TestAuthCSRFScopesRevocationAndConflict(t *testing.T) {
	s, srv := testServer(t)
	code, h, _ := call(t, srv, "GET", "/api/v1/articles", nil, "", "", "")
	if code != 401 || h.Get("Cache-Control") != "no-store" {
		t.Fatal(code, h)
	}
	cookie, csrf := login(t, srv)
	feed := map[string]any{"url": "https://example.org/feed", "title": "Synthetic"}
	code, _, _ = call(t, srv, "POST", "/api/v1/feeds", feed, cookie, "", "")
	if code != 403 {
		t.Fatal("CSRF", code)
	}
	code, _, b := call(t, srv, "POST", "/api/v1/feeds", feed, cookie, csrf, "")
	if code != 201 {
		t.Fatalf("create %d %s", code, b)
	}
	var f app.Feed
	json.Unmarshal(b, &f)
	ids, e := s.App.IngestFeed(context.Background(), f.ID, []app.FeedEntry{{GUID: "synthetic", Title: "音乐", URL: "https://example.org/article", HTML: "<p>参考资料</p>"}})
	if e != nil {
		t.Fatal(e)
	}
	a, e := s.App.GetArticle(context.Background(), ids[0])
	if e != nil {
		t.Fatal(e)
	}
	code, _, b = call(t, srv, "POST", "/api/v1/tokens", map[string]any{"name": "reader", "scopes": []string{"articles:read", "artifacts:write"}}, cookie, csrf, "")
	if code != 201 {
		t.Fatal(code, string(b))
	}
	var issued struct {
		Token  string `json:"token"`
		Record struct {
			ID string `json:"id"`
		} `json:"record"`
	}
	json.Unmarshal(b, &issued)
	code, _, _ = call(t, srv, "GET", "/api/v1/articles?scope=all", nil, "", "", issued.Token)
	if code != 200 {
		t.Fatal(code)
	}
	code, _, _ = call(t, srv, "POST", "/api/v1/articles/"+a.ID+"/fulltext-jobs", nil, "", "", issued.Token)
	if code != 403 {
		t.Fatal("scope", code)
	}
	artifact := map[string]string{"external_id": "summary1", "kind": "summary", "content": "摘要", "basis_hash": a.ContentHash}
	code, _, first := call(t, srv, "POST", "/api/v1/articles/"+a.ID+"/artifacts", artifact, "", "", issued.Token)
	if code != 201 {
		t.Fatal(code, string(first))
	}
	code, _, second := call(t, srv, "POST", "/api/v1/articles/"+a.ID+"/artifacts", artifact, "", "", issued.Token)
	if code != 201 || !bytes.Equal(first, second) {
		t.Fatal("idempotency", code, string(second))
	}
	artifact["content"] = "changed"
	code, _, _ = call(t, srv, "POST", "/api/v1/articles/"+a.ID+"/artifacts", artifact, "", "", issued.Token)
	if code != 409 {
		t.Fatal("key conflict", code)
	}
	code, _, _ = call(t, srv, "PATCH", "/api/v1/articles/"+a.ID, map[string]any{"version": a.Version, "favorite": true}, cookie, csrf, "")
	if code != 412 {
		t.Fatal("version conflict", code)
	}
	code, _, _ = call(t, srv, "DELETE", "/api/v1/tokens/"+issued.Record.ID, nil, cookie, csrf, "")
	if code != 204 {
		t.Fatal(code)
	}
	code, _, _ = call(t, srv, "GET", "/api/v1/articles", nil, "", "", issued.Token)
	if code != 401 {
		t.Fatal("revocation", code)
	}
	code, _, _ = call(t, srv, "DELETE", "/api/v1/session", nil, cookie, csrf, "")
	if code != 204 {
		t.Fatal(code)
	}
	code, _, _ = call(t, srv, "GET", "/api/v1/articles", nil, cookie, "", "")
	if code != 401 {
		t.Fatal("logout", code)
	}
}
func TestStaticDeepLinkErrorsAndRateLimit(t *testing.T) {
	_, srv := testServer(t)
	for path, want := range map[string]int{"/article/42": 200, "/_app/missing.js": 404, "/api/missing": 404, "/api/v1/missing": 404, "/missing.svg": 404} {
		code, _, _ := call(t, srv, "GET", path, nil, "", "", "")
		if code != want {
			t.Fatalf("%s %d", path, code)
		}
	}
	for i := 0; i < 6; i++ {
		code, _, b := call(t, srv, "POST", "/api/v1/session", map[string]string{"password": "wrong"}, "", "", "")
		want := 401
		if i == 5 {
			want = 429
		}
		if code != want {
			t.Fatalf("attempt %d %d %s", i, code, b)
		}
	}
}
func TestExpiredContentNeverLeavesHTTP(t *testing.T) {
	s, srv := testServer(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.App.Now = func() time.Time { return now }
	f, e := s.App.CreateFeed(context.Background(), app.Feed{Title: "test", URL: "https://example.org/feed"})
	if e != nil {
		t.Fatal(e)
	}
	ids, e := s.App.IngestFeed(context.Background(), f.ID, []app.FeedEntry{{GUID: "old", Title: "retained title", URL: "https://example.org/a", HTML: "<p>uniquebodysecret</p>"}})
	if e != nil {
		t.Fatal(e)
	}
	now = now.AddDate(0, 0, 91)
	cookie, csrf := login(t, srv)
	_ = csrf
	code, _, body := call(t, srv, "GET", "/api/v1/articles/"+ids[0], nil, cookie, "", "")
	if code != 200 || strings.Contains(string(body), "uniquebodysecret") {
		t.Fatal(code, string(body))
	}
	code, _, body = call(t, srv, "GET", "/api/v1/articles?scope=all&q=uniquebodysecret", nil, cookie, "", "")
	if code != 200 || !strings.Contains(string(body), `"items":[]`) {
		t.Fatal(code, string(body))
	}
}

func TestRevokedSessionCannotCreateTokenAfterAuthentication(t *testing.T) {
	s, srv := testServer(t)
	cookie, _ := login(t, srv)
	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("Cookie", cookie)
	p, e := s.authenticate(req)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.App.Store.Write.Exec(`DELETE FROM sessions WHERE hash=?`, p.Hash); e != nil {
		t.Fatal(e)
	}
	ctx := context.WithValue(context.Background(), principalKey{}, p)
	_, e = s.CreateToken(ctx, api.CreateTokenRequestObject{Body: &api.TokenInput{Name: "must not exist", Scopes: []string{"articles:read"}}})
	if e != unauthorized {
		t.Fatalf("revocation race: %v", e)
	}
	var n int
	if e = s.App.Store.Read.QueryRow(`SELECT count(*) FROM tokens`).Scan(&n); e != nil || n != 0 {
		t.Fatal("token created after logout", n, e)
	}
}

func TestExplicitZeroLimitViolatesContract(t *testing.T) {
	_, srv := testServer(t)
	cookie, _ := login(t, srv)
	for _, path := range []string{"/api/v1/articles?limit=0", "/api/v1/changes?limit=0", "/api/v1/articles?limit=201", "/api/v1/changes?limit=-1"} {
		code, _, _ := call(t, srv, "GET", path, nil, cookie, "", "")
		if code != 400 {
			t.Fatalf("%s: %d", path, code)
		}
	}
}
