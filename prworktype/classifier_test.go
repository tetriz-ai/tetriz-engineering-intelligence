// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prworktype

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// resp writes a fixed status + body, modelling one model-server endpoint.
func resp(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// fakeModel points a ModelClient at an httptest server whose two endpoints are
// driven by the given handlers (a nil handler leaves that path a 404).
func fakeModel(t *testing.T, embed, llm http.HandlerFunc) *ModelClient {
	t.Helper()
	mux := http.NewServeMux()
	if embed != nil {
		mux.HandleFunc("/embed-classify", embed)
	}
	if llm != nil {
		mux.HandleFunc("/llm-classify", llm)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewModelClient(srv.URL)
}

const (
	embedConfident = `{"candidates":[{"bucket":"bugfix","score":0.91}],"top_score":0.91,"margin":0.5}`
	embedUnsure    = `{"candidates":[{"bucket":"bugfix","score":0.30},{"bucket":"feature","score":0.29}],"top_score":0.30,"margin":0.01}`
	embedUnsureDom = `{"candidates":[{"bucket":"ai-ml","score":0.30},{"bucket":"ui-design","score":0.29}],"top_score":0.30,"margin":0.01}`
)

func TestResolve(t *testing.T) {
	tests := []struct {
		name      string
		embed     http.HandlerFunc
		llm       http.HandlerFunc
		useLLM    bool
		minScore  float64 // 0 => default guard applies
		axis      string
		allowNull bool
		want      AxisResult
		wantErr   bool
	}{
		{
			name:  "confident embedding",
			embed: resp(200, embedConfident), axis: "type",
			want: AxisResult{Bucket: "bugfix", Method: MethodEmbedding, Score: 0.91},
		},
		{
			name:  "unsure, LLM off, abstains",
			embed: resp(200, embedUnsure), useLLM: false, axis: "type",
			want: AxisResult{Method: MethodAbstain},
		},
		{
			name:  "unsure, LLM picks a valid bucket",
			embed: resp(200, embedUnsure), llm: resp(200, `{"bucket":"refactor"}`), useLLM: true, axis: "type",
			want: AxisResult{Bucket: "refactor", Method: MethodLLM},
		},
		{
			name:  "LLM unavailable (503) degrades to abstain, not a fake label",
			embed: resp(200, embedUnsure), llm: resp(503, `{"detail":"no api key"}`), useLLM: true, axis: "type",
			want: AxisResult{Method: MethodAbstain},
		},
		{
			name:  "LLM returns a bucket outside the taxonomy is an error",
			embed: resp(200, embedUnsure), llm: resp(200, `{"bucket":"not-a-bucket"}`), useLLM: true, axis: "type",
			wantErr: true,
		},
		{
			name:  "embed server 500 is an error, not a silent abstain",
			embed: resp(500, `boom`), axis: "type",
			wantErr: true,
		},
		{
			name:  "embed malformed JSON is an error",
			embed: resp(200, `{"candidates":`), axis: "type",
			wantErr: true,
		},
		{
			name:  "type: available LLM declines, best-effort to embedding top",
			embed: resp(200, embedUnsure), llm: resp(200, `{"bucket":null}`), useLLM: true, axis: "type",
			want: AxisResult{Bucket: "bugfix", Method: MethodEmbedding, Score: 0.30},
		},
		{
			name:  "domain: available LLM returns null is a real null domain",
			embed: resp(200, embedUnsureDom), llm: resp(200, `{"bucket":null}`), useLLM: true, axis: "domain", allowNull: true,
			want: AxisResult{Method: MethodLLM},
		},
		{
			name:  "domain: LLM picks a valid domain bucket",
			embed: resp(200, embedUnsureDom), llm: resp(200, `{"bucket":"ai-ml"}`), useLLM: true, axis: "domain", allowNull: true,
			want: AxisResult{Bucket: "ai-ml", Method: MethodLLM},
		},
		{
			name:  "domain: a type bucket is outside the domain taxonomy, error",
			embed: resp(200, embedUnsureDom), llm: resp(200, `{"bucket":"bugfix"}`), useLLM: true, axis: "domain", allowNull: true,
			wantErr: true,
		},
		{
			name:  "zero-value thresholds use the defaults, so weak signal still abstains",
			embed: resp(200, embedUnsure), minScore: 0, useLLM: false, axis: "type",
			want: AxisResult{Method: MethodAbstain},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &Classifier{Client: fakeModel(t, tc.embed, tc.llm), MinScore: tc.minScore, UseLLM: tc.useLLM}
			got, err := c.resolve(context.Background(), tc.axis, Input{Title: "x", Branch: "y"}, tc.allowNull)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestClassify(t *testing.T) {
	t.Run("nil client is an error, not a panic", func(t *testing.T) {
		c := NewClassifier(nil, false)
		if _, err := c.Classify(context.Background(), Input{Title: "x"}); err == nil {
			t.Fatal("want error for nil client")
		}
	})

	t.Run("rule fires for the type axis, domain still resolves", func(t *testing.T) {
		// dependabot author -> chore-deps via the Layer-0 rule; domain confident via embed.
		mc := fakeModel(t, resp(200, `{"candidates":[{"bucket":"infra-devops","score":0.9}],"top_score":0.9,"margin":0.5}`), nil)
		c := NewClassifier(mc, false)
		got, err := c.Classify(context.Background(), Input{Title: "bump x", AuthorLogin: "dependabot[bot]"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got.Type.Bucket != "chore-deps" || got.Type.Method != MethodRule {
			t.Fatalf("type = %+v, want chore-deps/rule", got.Type)
		}
		if got.Domain.Bucket != "infra-devops" || got.Domain.Method != MethodEmbedding {
			t.Fatalf("domain = %+v, want infra-devops/embedding", got.Domain)
		}
	})

	t.Run("a model-server failure propagates as an error", func(t *testing.T) {
		mc := fakeModel(t, resp(500, `down`), nil)
		c := NewClassifier(mc, false)
		if _, err := c.Classify(context.Background(), Input{Title: "x"}); err == nil {
			t.Fatal("want error when the model server is down")
		}
	})
}

// TestNilHTTPDoesNotPanic covers a ModelClient built without NewModelClient:
// the nil HTTP field must fall back to the default client, not panic.
func TestNilHTTPDoesNotPanic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(resp(200, embedConfident)))
	t.Cleanup(srv.Close)
	mc := &ModelClient{BaseURL: srv.URL} // HTTP left nil
	got, err := mc.EmbedClassify(context.Background(), "type", "t", "b")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.TopScore != 0.91 {
		t.Fatalf("top_score = %v, want 0.91", got.TopScore)
	}
}
