package ollama_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"claude-code-hist-viewer/internal/adapter/ollama"
)

func TestEmbedSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "mxbai-embed-large" || len(req.Input) != 2 {
			t.Fatalf("unexpected request: %+v", req)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{1, 2, 3}, {4, 5, 6}},
		})
	}))
	defer srv.Close()

	c := ollama.New(srv.URL, "mxbai-embed-large", "chat-model")
	vecs, err := c.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || len(vecs[0]) != 3 {
		t.Fatalf("unexpected vectors: %+v", vecs)
	}
	if c.Dim() != 3 {
		t.Fatalf("Dim() = %d, want 3", c.Dim())
	}
	if c.Model() != "mxbai-embed-large" {
		t.Fatalf("Model() = %q", c.Model())
	}
}

func TestEmbedRetriesOn5xxThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("boom"))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{1}}})
	}))
	defer srv.Close()

	c := ollama.New(srv.URL, "m", "chat")
	vecs, err := c.Embed(context.Background(), []string{"x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 1 {
		t.Fatalf("expected 1 vector, got %d", len(vecs))
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestEmbedModelMissingHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"model 'mxbai-embed-large' not found, try pulling it first"}`))
	}))
	defer srv.Close()

	c := ollama.New(srv.URL, "mxbai-embed-large", "chat")
	_, err := c.Embed(context.Background(), []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "ollama pull") {
		t.Fatalf("expected pull hint in error, got %v", err)
	}
}

func TestChatSendsSystemAndUserMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
			t.Fatalf("unexpected messages: %+v", req.Messages)
		}
		fmt.Fprint(w, `{"message":{"role":"assistant","content":"the answer"}}`)
	}))
	defer srv.Close()

	c := ollama.New(srv.URL, "embed-model", "chat-model")
	answer, err := c.Chat(context.Background(), "be helpful", "what is 2+2")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "the answer" {
		t.Fatalf("got %q", answer)
	}
}
