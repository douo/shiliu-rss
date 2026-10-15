// Package fetch owns bounded, SSRF-checked network access; no database transactions.
package fetch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/microcosm-cc/bluemonday"
	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"
	"golang.org/x/net/publicsuffix"
	"shiliu-rss/internal/app"
)

const MaxBytes = 8 << 20
const MaxNodes = 50000

// Error deliberately contains no private URL, response content, or credentials.
type Error struct {
	Code       string
	RetryAfter *time.Time
}

func (e *Error) Error() string { return e.Code }

type Client struct {
	Resolver *net.Resolver
	Timeout  time.Duration
}

func New() *Client { return &Client{Resolver: net.DefaultResolver, Timeout: 30 * time.Second} }
func HostKey(raw string) string {
	u, e := url.Parse(raw)
	if e != nil {
		return "invalid"
	}
	h := strings.ToLower(u.Hostname())
	if v, e := publicsuffix.EffectiveTLDPlusOne(h); e == nil {
		return v
	}
	return h
}
func blocked(ip net.IP) bool {
	return ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || !ip.IsGlobalUnicast()
}
func valid(u *url.URL) bool {
	return u != nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil
}
func (c *Client) download(ctx context.Context, raw, allowHost, etag, modified string) ([]byte, *http.Response, error) {
	u, e := url.Parse(raw)
	if e != nil || !valid(u) {
		return nil, nil, &Error{Code: "invalid_url"}
	}
	originalHost := strings.ToLower(u.Hostname())
	tr := &http.Transport{Proxy: nil, DisableKeepAlives: true, MaxResponseHeaderBytes: 64 << 10, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(addr)
		if e != nil {
			return nil, &Error{Code: "invalid_address"}
		}
		ips, e := c.Resolver.LookupIPAddr(ctx, host)
		if e != nil || len(ips) == 0 {
			return nil, &Error{Code: "dns_failed"}
		}
		allow := allowHost != "" && strings.EqualFold(allowHost, host) && strings.EqualFold(originalHost, host)
		for _, ip := range ips {
			if blocked(ip.IP) && !allow {
				return nil, &Error{Code: "private_address_blocked"}
			}
		}
		var conn net.Conn
		for _, ip := range ips {
			conn, e = (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if e == nil {
				return conn, nil
			}
		}
		return nil, &Error{Code: "connect_failed"}
	}
	defer tr.CloseIdleConnections()
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Transport: tr, Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return &Error{Code: "too_many_redirects"}
		}
		if !valid(req.URL) {
			return &Error{Code: "invalid_redirect"}
		}
		if req.URL.Host != via[0].URL.Host {
			req.Header.Del("Authorization")
			req.Header.Del("Cookie")
			req.Header.Del("If-None-Match")
			req.Header.Del("If-Modified-Since")
		}
		return nil
	}}
	req, e := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if e != nil {
		return nil, nil, &Error{Code: "invalid_url"}
	}
	req.Header.Set("User-Agent", "ShiliuRSS/1.0")
	req.Header.Set("Accept", "application/atom+xml, application/rss+xml, application/feed+json, text/html;q=0.8, */*;q=0.5")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if modified != "" {
		req.Header.Set("If-Modified-Since", modified)
	}
	resp, e := client.Do(req)
	if e != nil {
		var fe *Error
		if errors.As(e, &fe) {
			return nil, nil, fe
		}
		return nil, nil, &Error{Code: "network_failed"}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return nil, resp, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var retry *time.Time
		if v := resp.Header.Get("Retry-After"); v != "" {
			if n, e := strconv.Atoi(v); e == nil && n >= 0 {
				t := time.Now().Add(time.Duration(min(n, 7*86400)) * time.Second)
				retry = &t
			} else if t, e := http.ParseTime(v); e == nil {
				retry = &t
			}
		}
		return nil, resp, &Error{Code: fmt.Sprintf("http_%d", resp.StatusCode), RetryAfter: retry}
	}
	body, e := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if e != nil {
		return nil, resp, &Error{Code: "read_failed"}
	}
	if len(body) > MaxBytes {
		return nil, resp, &Error{Code: "response_too_large"}
	}
	return body, resp, nil
}
func Sanitize(raw, base string) (string, string, error) {
	doc, e := html.Parse(strings.NewReader(raw))
	if e != nil {
		return "", "", &Error{Code: "invalid_html"}
	}
	u, _ := url.Parse(base)
	nodes := 0
	var walk func(*html.Node) error
	walk = func(n *html.Node) error {
		nodes++
		if nodes > MaxNodes {
			return &Error{Code: "html_too_complex"}
		}
		for j, a := range n.Attr {
			if a.Key == "href" || a.Key == "src" {
				if x, e := url.Parse(a.Val); e == nil && u != nil {
					n.Attr[j].Val = u.ResolveReference(x).String()
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if e := walk(c); e != nil {
				return e
			}
		}
		return nil
	}
	if e = walk(doc); e != nil {
		return "", "", e
	}
	var buf bytes.Buffer
	if e = html.Render(&buf, doc); e != nil {
		return "", "", e
	}
	policy := bluemonday.UGCPolicy()
	policy.RequireNoReferrerOnLinks(true)
	policy.RequireNoFollowOnLinks(true)
	policy.AllowAttrs("loading").OnElements("img")
	clean := policy.Sanitize(buf.String())
	plain := strings.TrimSpace(bluemonday.StrictPolicy().Sanitize(clean))
	return clean, plain, nil
}
func (c *Client) Feed(ctx context.Context, f app.Feed) (app.FeedResult, error) {
	data, resp, e := c.download(ctx, f.URL, f.AllowHost, f.ETag, f.LastModified)
	if e != nil {
		return app.FeedResult{}, e
	}
	result := app.FeedResult{ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified"), NotModified: resp.StatusCode == 304}
	if result.NotModified {
		return result, nil
	}
	parsed, e := gofeed.NewParser().Parse(bytes.NewReader(data))
	if e != nil {
		return result, &Error{Code: "invalid_feed"}
	}
	if len(parsed.Items) > 2000 {
		return result, &Error{Code: "too_many_entries"}
	}
	result.Title = parsed.Title
	result.SiteURL = parsed.Link
	result.Entries = []app.FeedEntry{}
	for _, item := range parsed.Items {
		body := item.Content
		if body == "" {
			body = item.Description
		}
		base := resp.Request.URL.String()
		link := item.Link
		if u, e := url.Parse(link); e == nil {
			link = resp.Request.URL.ResolveReference(u).String()
		}
		if link != "" {
			base = link
		}
		clean, plain, e := Sanitize(body, base)
		if e != nil {
			return result, e
		}
		author := ""
		if item.Author != nil {
			author = item.Author.Name
		}
		result.Entries = append(result.Entries, app.FeedEntry{GUID: item.GUID, URL: link, Title: item.Title, Author: author, PublishedAt: item.PublishedParsed, HTML: clean, Text: plain})
	}
	return result, nil
}
func (c *Client) Fulltext(ctx context.Context, a app.Article, allowHost string) (app.FulltextResult, error) {
	data, resp, e := c.download(ctx, a.URL, allowHost, "", "")
	if e != nil {
		return app.FulltextResult{}, e
	}
	if _, _, e = Sanitize(string(data), a.URL); e != nil {
		return app.FulltextResult{}, e
	}
	article, e := readability.FromReader(bytes.NewReader(data), resp.Request.URL)
	if e != nil {
		return app.FulltextResult{}, &Error{Code: "extract_failed"}
	}
	var extracted bytes.Buffer
	if e = article.RenderHTML(&extracted); e != nil {
		return app.FulltextResult{}, &Error{Code: "extract_failed"}
	}
	clean, plain, e := Sanitize(extracted.String(), resp.Request.URL.String())
	if e != nil {
		return app.FulltextResult{}, e
	}
	if strings.TrimSpace(plain) == "" {
		return app.FulltextResult{}, &Error{Code: "empty_fulltext"}
	}
	return app.FulltextResult{HTML: clean, Text: plain}, nil
}
