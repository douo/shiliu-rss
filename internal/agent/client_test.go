package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"shiliu-rss/internal/app"
	"strings"
	"testing"
)

func TestProtocolsInterruptedStreamsAndSanitizedErrors(t *testing.T) {
	for _, protocol := range []string{"responses", "chat_completions"} {
		t.Run(protocol, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var d map[string]any
				json.NewDecoder(r.Body).Decode(&d)
				if r.Header.Get("Authorization") != "Bearer synthetic-secret" {
					t.Error("missing credential")
				}
				if protocol == "responses" {
					if r.URL.Path != "/v1/responses" || d["store"] != false || d["stream"] != true {
						t.Error(d, r.URL.Path)
					}
				} else if r.URL.Path != "/v1/chat/completions" {
					t.Error(r.URL.Path)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if protocol == "responses" {
					fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"total_tokens\":10}}}\n\n")
				} else {
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"total_tokens\":10}}\n\ndata: [DONE]\n\n")
				}
			}))
			defer srv.Close()
			v := app.DefaultAgentSettings()
			v.Protocol = protocol
			v.BaseURL = srv.URL + "/v1"
			got, e := NewClient().Call(context.Background(), v, "synthetic-secret", "test", map[string]string{}, narrativeSchema)
			if e != nil || got.Text != "OK" || got.Tokens != 10 {
				t.Fatal(got, e)
			}
		})
	}
	t.Run("interrupted", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
		}))
		defer srv.Close()
		v := app.DefaultAgentSettings()
		v.BaseURL = srv.URL
		if _, e := NewClient().Call(context.Background(), v, "", "", nil, nil); e == nil {
			t.Fatal("accepted partial stream")
		}
	})
	t.Run("429", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(429)
			fmt.Fprint(w, "synthetic-secret and private content")
		}))
		defer srv.Close()
		v := app.DefaultAgentSettings()
		v.BaseURL = srv.URL
		_, e := NewClient().Call(context.Background(), v, "", "", nil, nil)
		p, ok := e.(*ProviderError)
		if !ok || !p.Retryable || p.RetryAfter.Seconds() != 120 || strings.Contains(e.Error(), "synthetic-secret") {
			t.Fatal(e)
		}
	})
}
