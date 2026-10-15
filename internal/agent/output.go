package agent

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"shiliu-rss/internal/app"
	"strings"
)

//go:embed prompts/system.txt
var systemPrompt string

//go:embed prompts/articles.txt
var articlePrompt string

//go:embed prompts/narrative.txt
var narrativePrompt string

//go:embed prompts/answer.txt
var answerPrompt string

const promptVersion = "shiliu-agent/2"

var ErrFormat = errors.New("invalid_model_output")

type Evidence struct {
	ID        string `json:"evidence_id"`
	ArticleID string `json:"article_id"`
	Title     string `json:"title"`
	Hash      string `json:"basis_hash"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
}
type Point struct {
	Kind        string   `json:"kind"`
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidence_ids"`
}
type Narrative struct {
	Version string  `json:"schema_version"`
	Blocks  []Point `json:"blocks"`
}
type ArticlePoint struct {
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidence_ids"`
}
type ArticleCard struct {
	ArticleID  string         `json:"article_id"`
	Status     string         `json:"status"`
	Points     []ArticlePoint `json:"points"`
	Limitation string         `json:"limitation"`
}
type Batch struct {
	Version  string        `json:"schema_version"`
	Articles []ArticleCard `json:"items"`
}

func hash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func decode(raw string, v any) error {
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return ErrFormat
	}
	if d.Decode(new(any)) != io.EOF {
		return ErrFormat
	}
	return nil
}
func blocks(points []Point, ev []Evidence) ([]app.AgentBlock, error) {
	if len(points) > 40 {
		return nil, ErrFormat
	}
	byID := map[string]Evidence{}
	for _, e := range ev {
		byID[e.ID] = e
	}
	out := []app.AgentBlock{}
	for _, p := range points {
		if p.Kind != "fact" && p.Kind != "analysis" && p.Kind != "limitation" {
			return nil, ErrFormat
		}
		if strings.TrimSpace(p.Text) == "" || len([]rune(p.Text)) > 2000 || strings.ContainsAny(p.Text, "<>") || strings.Contains(strings.ToLower(p.Text), "http:") || strings.Contains(strings.ToLower(p.Text), "https:") || strings.Contains(p.Text, "](") {
			return nil, ErrFormat
		}
		if len(p.EvidenceIDs) > 40 || p.EvidenceIDs == nil {
			return nil, ErrFormat
		}
		refs := map[string]bool{}
		for _, id := range p.EvidenceIDs {
			if refs[id] {
				return nil, ErrFormat
			}
			refs[id] = true
		}
		if p.Kind != "limitation" && len(p.EvidenceIDs) == 0 {
			return nil, ErrFormat
		}
		for _, e := range ev {
			if e.Kind == "fulltext" || e.Kind == "fulltext_chunk" || e.Kind == "feed_excerpt" {
				runes := []rune(p.Text)
				for start := 0; start+120 <= len(runes); start += 40 {
					if strings.Contains(e.Text, string(runes[start:start+120])) {
						return nil, ErrFormat
					}
				}
			}
		}
		b := app.AgentBlock{Kind: p.Kind, Text: p.Text, EvidenceIDs: p.EvidenceIDs, Citations: []app.AgentCitation{}}
		seen := map[string]bool{}
		for _, eid := range p.EvidenceIDs {
			e, ok := byID[eid]
			if !ok {
				return nil, ErrFormat
			}
			if e.ArticleID != "" && !seen[e.ArticleID] {
				seen[e.ArticleID] = true
				b.Citations = append(b.Citations, app.AgentCitation{ArticleID: e.ArticleID, Title: e.Title, URL: "/?scope=all&article=" + url.QueryEscape(e.ArticleID), BasisHash: e.Hash, Kind: e.Kind})
			}
		}
		out = append(out, b)
	}
	return out, nil
}
func parseNarrative(raw string, ev []Evidence) ([]app.AgentBlock, error) {
	var v Narrative
	if !shape(raw, "blocks") || decode(raw, &v) != nil || v.Version != "1" || len(v.Blocks) == 0 {
		return nil, ErrFormat
	}
	return blocks(v.Blocks, ev)
}
func parseBatch(raw string, ev []Evidence, ids []string) (map[string][]app.AgentBlock, error) {
	var v Batch
	if !shape(raw, "items") || decode(raw, &v) != nil || v.Version != "1" || len(v.Articles) != len(ids) {
		return nil, ErrFormat
	}
	out := map[string][]app.AgentBlock{}
	for i, c := range v.Articles {
		if len(c.Points) > 5 || len([]rune(c.Limitation)) > 500 {
			return nil, ErrFormat
		}
		if c.ArticleID != ids[i] || (c.Status != "ok" && c.Status != "insufficient") {
			return nil, ErrFormat
		}
		own := []Evidence{}
		for _, e := range ev {
			if e.ArticleID == c.ArticleID {
				own = append(own, e)
			}
		}
		if c.Status == "ok" && len(c.Points) == 0 {
			return nil, ErrFormat
		}
		points := []Point{}
		for _, p := range c.Points {
			if len([]rune(p.Text)) > 1000 {
				return nil, ErrFormat
			}
			points = append(points, Point{Kind: "fact", Text: p.Text, EvidenceIDs: p.EvidenceIDs})
		}
		if c.Limitation != "" {
			points = append(points, Point{Kind: "limitation", Text: c.Limitation, EvidenceIDs: []string{}})
		}
		b, e := blocks(points, own)
		if e != nil {
			return nil, e
		}
		out[c.ArticleID] = b
	}
	return out, nil
}

var narrativeSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["schema_version","blocks"],"properties":{"schema_version":{"type":"string","enum":["1"]},"blocks":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["kind","text","evidence_ids"],"properties":{"kind":{"type":"string","enum":["fact","analysis","limitation"]},"text":{"type":"string"},"evidence_ids":{"type":"array","items":{"type":"string"}}}}}}}`)

func schemaText(s any) string { b, _ := json.Marshal(s); return string(bytes.TrimSpace(b)) }

//go:embed prompts/article-batch.schema.json
var articleSchemaRaw string
var articleSchema = json.RawMessage(articleSchemaRaw)

// Required fields are checked separately from Go's zero values (including null arrays).
func shape(raw, kind string) bool {
	var root map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &root) != nil || !present(root, "schema_version", kind) {
		return false
	}
	var items []map[string]json.RawMessage
	if json.Unmarshal(root[kind], &items) != nil {
		return false
	}
	for _, item := range items {
		if kind == "blocks" {
			if !present(item, "kind", "text", "evidence_ids") {
				return false
			}
		} else {
			if !present(item, "article_id", "status", "points", "limitation") {
				return false
			}
			var points []map[string]json.RawMessage
			if json.Unmarshal(item["points"], &points) != nil {
				return false
			}
			for _, p := range points {
				if !present(p, "text", "evidence_ids") {
					return false
				}
			}
		}
	}
	return true
}
func present(v map[string]json.RawMessage, keys ...string) bool {
	for _, key := range keys {
		b, ok := v[key]
		if !ok || string(b) == "null" {
			return false
		}
	}
	return true
}
