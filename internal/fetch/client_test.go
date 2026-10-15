package fetch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"shiliu-rss/internal/app"
	"strings"
	"testing"
	"time"
)

func TestPrivateHostsRedirectLimitsAndSanitize(t *testing.T) {
	secret := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "secret") }))
	defer secret.Close()
	c := New()
	_, _, e := c.download(context.Background(), secret.URL, "", "", "")
	if e == nil || e.Error() != "private_address_blocked" {
		t.Fatalf("private access: %v", e)
	}
	u, _ := url.Parse(secret.URL)
	raw, _, e := c.download(context.Background(), secret.URL, u.Hostname(), "", "")
	if e != nil || string(raw) != "secret" {
		t.Fatal("explicit host allowance", e)
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.Replace(secret.URL, "127.0.0.1", "localhost", 1), 302)
	}))
	defer redirect.Close()
	_, _, e = c.download(context.Background(), redirect.URL, "127.0.0.1", "", "")
	if e == nil {
		t.Fatal("redirect expanded private host allowance")
	}
	clean, plain, e := Sanitize(`<script>alert(1)</script><a href="javascript:alert(1)">bad</a><a href="/read">good</a><img src="/x" onerror="alert(1)">文字`, "https://example.org/base")
	if e != nil || strings.Contains(clean, "script") || strings.Contains(clean, "onerror") || !strings.Contains(clean, "https://example.org/read") || !strings.Contains(plain, "文字") {
		t.Fatalf("sanitize: %s %v", clean, e)
	}
	large := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat("x", MaxBytes+1)) }))
	defer large.Close()
	_, _, e = c.download(context.Background(), large.URL, "127.0.0.1", "", "")
	if e == nil || e.Error() != "response_too_large" {
		t.Fatal(e)
	}
}
func TestFeedConditionalRequestsFormatsAndRetryAfter(t *testing.T) {
	formats := []string{`<rss version="2.0"><channel><title>测试</title><link>https://example.org</link><item><guid>a</guid><title>音乐</title><link>https://example.org/a</link><description><![CDATA[<p>正文<script>bad()</script></p>]]></description></item></channel></rss>`, `<feed xmlns="http://www.w3.org/2005/Atom"><title>测试</title><id>urn:test</id><entry><id>urn:a</id><title>音乐</title><link href="https://example.org/a"/><content type="html">&lt;p&gt;正文&lt;/p&gt;</content></entry></feed>`, `{"version":"https://jsonfeed.org/version/1.1","title":"测试","items":[{"id":"a","url":"https://example.org/a","title":"音乐","content_html":"<p>正文</p>"}]}`}
	for _, body := range formats {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("If-None-Match") == `"v1"` {
				w.WriteHeader(304)
				return
			}
			w.Header().Set("ETag", `"v1"`)
			fmt.Fprint(w, body)
		}))
		f := app.Feed{URL: srv.URL, AllowHost: "127.0.0.1"}
		got, e := New().Feed(context.Background(), f)
		if e != nil || len(got.Entries) != 1 || strings.Contains(got.Entries[0].HTML, "script") {
			t.Fatal(got, e)
		}
		f.ETag = got.ETag
		got, e = New().Feed(context.Background(), f)
		if e != nil || !got.NotModified {
			t.Fatal(got, e)
		}
		srv.Close()
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(429)
	}))
	defer srv.Close()
	_, e := New().Feed(context.Background(), app.Feed{URL: srv.URL, AllowHost: "127.0.0.1"})
	err, ok := e.(*Error)
	if !ok || err.Code != "http_429" || err.RetryAfter == nil || time.Until(*err.RetryAfter) < 59*time.Minute {
		t.Fatal(e)
	}
}
