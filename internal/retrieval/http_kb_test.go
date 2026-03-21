package retrieval

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/infercore/infercore/internal/config"
)

func TestHTTPJSONKB_Retrieve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method %s", r.Method)
		}
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		if body["query"] != "hello world" {
			t.Fatalf("query=%v", body["query"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"chunks": []map[string]any{
				{"text": "chunk one", "source": "a", "score": 0.9},
				{"content": "chunk two", "source": "b"},
			},
		})
	}))
	defer srv.Close()

	kb, err := NewHTTPJSONKB(config.KnowledgeBaseConfig{Name: "remote", Type: "http", Endpoint: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	res, err := kb.Retrieve(context.Background(), "hello world", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Chunks) != 2 {
		t.Fatalf("chunks=%d", len(res.Chunks))
	}
	if res.Chunks[0].Text != "chunk one" || res.Chunks[1].Text != "chunk two" {
		t.Fatalf("%+v", res.Chunks)
	}
}

func TestParseOpenSearchHits(t *testing.T) {
	raw := `{
  "hits": {
    "hits": [
      {"_index": "i", "_id": "1", "_score": 2.5, "_source": {"text": "alpha", "meta": "x"}},
      {"_index": "i", "_id": "2", "_score": 1.0, "_source": {"content": "beta"}}
    ]
  }
}`
	res, err := parseOpenSearchHits([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Chunks) != 2 || res.Chunks[0].Text != "alpha" || res.Chunks[1].Text != "beta" {
		t.Fatalf("%+v", res.Chunks)
	}
}

func TestParseMeilisearchHits(t *testing.T) {
	raw := `{
  "hits": [
    {"id": "x", "text": "hello", "_rankingScore": 0.99},
    {"id": "y", "body": "there"}
  ]
}`
	res, err := parseMeilisearchHits([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Chunks) != 2 || res.Chunks[0].Text != "hello" || res.Chunks[1].Text != "there" {
		t.Fatalf("%+v", res.Chunks)
	}
}
