package app

import (
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
)

var contentPolicy = bluemonday.UGCPolicy()

// CleanContent is shared by feed and import ingestion. Untrusted source HTML
// cannot become executable browser content, including on the import path.
func CleanContent(raw string) (string, string) {
	safe := contentPolicy.Sanitize(raw)
	tokenizer := html.NewTokenizer(strings.NewReader(safe))
	var text strings.Builder
	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			break
		}
		if kind == html.TextToken {
			text.Write(tokenizer.Text())
			text.WriteByte(' ')
		}
	}
	return safe, strings.Join(strings.Fields(text.String()), " ")
}

// prepareEntry performs untrusted HTML parsing before a write transaction is
// acquired. upsertEntry accepts only this prepared application-owned value.
func prepareEntry(entry FeedEntry) (FeedEntry, error) {
	if len(entry.HTML) > 8<<20 || len(entry.Text) > 8<<20 {
		return FeedEntry{}, ErrInvalid
	}
	if entry.HTML != "" {
		entry.HTML, entry.Text = CleanContent(entry.HTML)
	}
	return entry, nil
}
func prepareEntries(entries []FeedEntry) ([]FeedEntry, error) {
	out := make([]FeedEntry, len(entries))
	for i, entry := range entries {
		prepared, e := prepareEntry(entry)
		if e != nil {
			return nil, e
		}
		out[i] = prepared
	}
	return out, nil
}
