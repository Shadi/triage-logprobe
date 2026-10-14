package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shadi/triage-logprobs/internal/llm"
)

const (
	DefaultBaseURL = "http://localhost:11434"
	maxTopN        = 20

	maxResponseBytes = 1 << 20
	maxErrorBytes    = 4 << 10
	maxDrainBytes    = 64 << 10
)

var _ llm.LogprobSource = (*Client)(nil)

var ErrNoLogprobs = errors.New("ollama: response has no logprobs")

type Client struct {
	BaseURL   string
	HTTP      *http.Client
	KeepAlive string
}

func New(baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: 2 * time.Minute},
	}
}

func (c *Client) NextTokenLogprobs(ctx context.Context, model, rawPrompt string, topN int) ([]llm.TokenLogprob, error) {
	if model == "" {
		return nil, errors.New("ollama: model is required")
	}
	if rawPrompt == "" {
		return nil, errors.New("ollama: prompt is required")
	}
	if topN < 1 || topN > maxTopN {
		return nil, fmt.Errorf("ollama: topN must be between 1 and %d, got %d", maxTopN, topN)
	}

	body, err := json.Marshal(generateRequest{
		Model:       model,
		Prompt:      rawPrompt,
		Raw:         true,
		Stream:      false,
		Logprobs:    true,
		TopLogprobs: topN,
		KeepAlive:   c.KeepAlive,
		Options:     requestOptions{NumPredict: 1, Temperature: 0},
	})
	if err != nil {
		return nil, fmt.Errorf("ollama: encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrainBytes))
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBytes))
		return nil, fmt.Errorf("ollama: %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	var gr generateResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&gr); err != nil {
		return nil, fmt.Errorf("ollama: decoding response: %w", err)
	}
	if gr.Error != "" {
		return nil, fmt.Errorf("ollama: %s", gr.Error)
	}
	if len(gr.Logprobs) == 0 || len(gr.Logprobs[0].TopLogprobs) == 0 {
		return nil, fmt.Errorf("model %q: %w", model, ErrNoLogprobs)
	}

	return toTokenLogprobs(gr.Logprobs[0].TopLogprobs), nil
}

func toTokenLogprobs(w []tokenLogprobWire) []llm.TokenLogprob {
	out := make([]llm.TokenLogprob, 0, len(w))
	for _, t := range w {
		var b []byte
		if len(t.Bytes) > 0 {
			b = make([]byte, len(t.Bytes))
			for i, v := range t.Bytes {
				if v < 0 || v > 255 {
					b = nil
					break
				}
				b[i] = byte(v)
			}
		}
		out = append(out, llm.TokenLogprob{Token: t.Token, Logprob: t.Logprob, Bytes: b})
	}
	return out
}
