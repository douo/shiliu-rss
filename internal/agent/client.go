package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"shiliu-rss/internal/app"
)

type ProviderError struct {
	Code       string
	RetryAfter time.Duration
	Retryable  bool
}

func (e *ProviderError) Error() string { return e.Code }

type Client struct{ HTTP *http.Client }

func NewClient() *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // Configured model endpoints are reached directly, including the LAN gateway.
	return &Client{HTTP: &http.Client{Transport: transport, Timeout: 100 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}

type Completion struct {
	Text   string
	Tokens int
}

func (c *Client) Call(ctx context.Context, v app.AgentSettings, key, instructions string, input any, schema any) (Completion, error) {
	var out Completion
	payload := map[string]any{"model": v.Model, "stream": true, "store": false}
	b, e := json.Marshal(input)
	if e != nil {
		return out, e
	}
	endpoint := strings.TrimRight(v.BaseURL, "/")
	endpoint = strings.TrimSuffix(strings.TrimSuffix(endpoint, "/responses"), "/chat/completions")
	if v.Protocol == "responses" {
		endpoint += "/responses"
		payload["instructions"] = instructions
		payload["input"] = string(b)
		payload["max_output_tokens"] = v.MaxOutput
		if v.OutputMode != "text_json" {
			f := map[string]any{"type": v.OutputMode}
			if v.OutputMode == "json_schema" {
				f["name"] = "shiliu_output"
				f["schema"] = schema
				f["strict"] = true
			}
			payload["text"] = map[string]any{"format": f}
		}
	} else {
		endpoint += "/chat/completions"
		delete(payload, "store")
		payload["max_tokens"] = v.MaxOutput
		payload["stream_options"] = map[string]bool{"include_usage": true}
		payload["messages"] = []any{map[string]string{"role": "system", "content": instructions}, map[string]string{"role": "user", "content": string(b)}}
		if v.OutputMode != "text_json" {
			f := map[string]any{"type": v.OutputMode}
			if v.OutputMode == "json_schema" {
				f["json_schema"] = map[string]any{"name": "shiliu_output", "schema": schema, "strict": true}
			}
			payload["response_format"] = f
		}
	}
	body, e := json.Marshal(payload)
	if e != nil {
		return out, e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if e != nil {
		return out, &ProviderError{Code: "invalid_endpoint"}
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	r, e := c.HTTP.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return out, &ProviderError{Code: "provider_connection_failed", Retryable: true}
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		delay := time.Duration(0)
		if n, e := strconv.Atoi(r.Header.Get("Retry-After")); e == nil {
			delay = time.Duration(n) * time.Second
		} else if t, e := http.ParseTime(r.Header.Get("Retry-After")); e == nil {
			delay = time.Until(t)
		}
		return out, &ProviderError{Code: fmt.Sprintf("provider_http_%d", r.StatusCode), Retryable: r.StatusCode == 429 || r.StatusCode >= 500, RetryAfter: delay}
	}
	limited := io.LimitReader(r.Body, 4<<20)
	if !strings.Contains(r.Header.Get("Content-Type"), "text/event-stream") {
		var d struct {
			OutputText string `json:"output_text"`
			Output     []struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"output"`
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Usage struct {
				Total int `json:"total_tokens"`
			} `json:"usage"`
		}
		if json.NewDecoder(limited).Decode(&d) != nil {
			return out, &ProviderError{Code: "provider_invalid_json"}
		}
		out.Text = d.OutputText
		for _, item := range d.Output {
			for _, p := range item.Content {
				out.Text += p.Text
			}
		}
		if len(d.Choices) > 0 {
			out.Text = d.Choices[0].Message.Content
		}
		out.Tokens = d.Usage.Total
		if out.Text == "" {
			return out, &ProviderError{Code: "provider_empty_output"}
		}
		return out, nil
	}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 8192), 2<<20)
	complete := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if line == "[DONE]" {
			complete = true
			continue
		}
		var d struct {
			Type    string `json:"type"`
			Delta   string `json:"delta"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				Finish *string `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				Total int `json:"total_tokens"`
			} `json:"usage"`
			Response struct {
				Status string `json:"status"`
				Usage  struct {
					Total int `json:"total_tokens"`
				} `json:"usage"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(line), &d) != nil {
			return out, &ProviderError{Code: "provider_invalid_stream"}
		}
		switch d.Type {
		case "response.output_text.delta":
			out.Text += d.Delta
		case "response.completed":
			complete = true
			out.Tokens = d.Response.Usage.Total
		case "response.failed", "response.incomplete", "error":
			return out, &ProviderError{Code: "provider_incomplete"}
		}
		for _, ch := range d.Choices {
			out.Text += ch.Delta.Content
			if ch.Finish != nil {
				if *ch.Finish == "length" {
					return out, &ProviderError{Code: "provider_output_limit"}
				}
				complete = true
			}
		}
		if d.Usage.Total > 0 {
			out.Tokens = d.Usage.Total
		}
	}
	if scanner.Err() != nil || !complete {
		return out, &ProviderError{Code: "provider_interrupted", Retryable: true}
	}
	if out.Text == "" {
		return out, &ProviderError{Code: "provider_empty_output"}
	}
	return out, nil
}
