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
