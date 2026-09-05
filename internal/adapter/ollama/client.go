// Package ollama implements domain.Embedder and domain.ChatModel against a
// local Ollama server (http://localhost:11434 by default).
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultDim = 1024 // mxbai-embed-large

type Client struct {
	baseURL    string
	embedModel string
	chatModel  string
	httpClient *http.Client
	dim        int
}

func New(baseURL, embedModel, chatModel string) *Client {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		embedModel: embedModel,
		chatModel:  chatModel,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *Client) Model() string { return c.embedModel }

func (c *Client) Dim() int {
	if c.dim == 0 {
		return defaultDim
	}
	return c.dim
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(embedRequest{Model: c.embedModel, Input: texts})
	if err != nil {
		return nil, err
	}
	var resp embedResponse
	if err := c.doJSON(ctx, "/api/embed", c.embedModel, body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama embed: got %d vectors for %d inputs", len(resp.Embeddings), len(texts))
	}
	if c.dim == 0 && len(resp.Embeddings) > 0 {
		c.dim = len(resp.Embeddings[0])
	}
	return resp.Embeddings, nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
}

func (c *Client) Chat(ctx context.Context, system, user string) (string, error) {
	var msgs []chatMessage
	if system != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: system})
	}
	msgs = append(msgs, chatMessage{Role: "user", Content: user})
	body, err := json.Marshal(chatRequest{Model: c.chatModel, Messages: msgs, Stream: false})
	if err != nil {
		return "", err
	}
	var resp chatResponse
	if err := c.doJSON(ctx, "/api/chat", c.chatModel, body, &resp); err != nil {
		return "", err
	}
	return resp.Message.Content, nil
}

// doJSON posts body to path, retrying 3x on 5xx, and returns a clear error
// (with an `ollama pull` hint) when the model is missing.
func (c *Client) doJSON(ctx context.Context, path, model string, body []byte, out any) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
			continue
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("ollama %s: %s: %s", path, resp.Status, strings.TrimSpace(string(data)))
			time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
			continue
		}
		if resp.StatusCode >= 400 {
			if strings.Contains(string(data), "not found") {
				return fmt.Errorf("ollama model %q not found — run `ollama pull %s`", model, model)
			}
			return fmt.Errorf("ollama %s: %s: %s", path, resp.Status, strings.TrimSpace(string(data)))
		}
		return json.Unmarshal(data, out)
	}
	return fmt.Errorf("ollama %s: %w", path, lastErr)
}
