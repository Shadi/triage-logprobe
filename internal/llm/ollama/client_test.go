package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const generateOKFixture = `{"model":"qwen3.5:4b","response":"1","done":true,"logprobs":[{"token":"1","logprob":-0.427,"bytes":[49],"top_logprobs":[{"token":"1","logprob":-0.427,"bytes":[49]},{"token":"2","logprob":-1.146,"bytes":[50]},{"token":"3","logprob":-3.73,"bytes":[51]},{"token":"","logprob":-6.2}]}]}`

type capturedRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

func newTestServer(t *testing.T, status int, body string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	got := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Method = r.Method
		got.Path = r.URL.Path
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request body: %v", err)
		}
		if err := json.Unmarshal(raw, &got.Body); err != nil {
			t.Errorf("request body is not JSON: %v\n%s", err, raw)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func TestNextTokenLogprobs_OK(t *testing.T) {
	srv, got := newTestServer(t, http.StatusOK, generateOKFixture)
	c := New(srv.URL)
	c.KeepAlive = "10m"

	toks, err := c.NextTokenLogprobs(context.Background(), "qwen3.5:4b", "prompt", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(toks) != 4 {
		t.Fatalf("got %d tokens, want 4", len(toks))
	}
	first := toks[0]
	if first.Token != "1" || first.Logprob != -0.427 || !bytes.Equal(first.Bytes, []byte("1")) {
		t.Errorf("first token = %+v, want {1 -0.427 [49]}", first)
	}

	if got.Method != http.MethodPost || got.Path != "/api/generate" {
		t.Errorf("request = %s %s, want POST /api/generate", got.Method, got.Path)
	}
	wantBody := map[string]any{
		"model":        "qwen3.5:4b",
		"prompt":       "prompt",
		"raw":          true,
		"stream":       false,
		"logprobs":     true,
		"top_logprobs": float64(10),
		"keep_alive":   "10m",
	}
	for k, want := range wantBody {
		if got.Body[k] != want {
			t.Errorf("body[%q] = %v, want %v", k, got.Body[k], want)
		}
	}
	opts, ok := got.Body["options"].(map[string]any)
	if !ok {
		t.Fatalf("options missing or wrong type: %v", got.Body["options"])
	}
	if opts["num_predict"] != float64(1) {
		t.Errorf("num_predict = %v, want 1", opts["num_predict"])
	}
	if temp, ok := opts["temperature"]; !ok || temp != float64(0) {
		t.Errorf("temperature = %v (present=%v), want 0 sent explicitly", temp, ok)
	}
}

func TestNextTokenLogprobs_HTTPError(t *testing.T) {
	srv, _ := newTestServer(t, http.StatusNotFound, `{"error":"model 'nope' not found"}`)

	_, err := New(srv.URL).NextTokenLogprobs(context.Background(), "nope", "prompt", 5)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not found") || !strings.Contains(err.Error(), "404") {
		t.Errorf("error %q should include status and server message", err)
	}
}

func TestNextTokenLogprobs_ErrorField(t *testing.T) {
	srv, _ := newTestServer(t, http.StatusOK, `{"error":"something broke"}`)

	_, err := New(srv.URL).NextTokenLogprobs(context.Background(), "m", "prompt", 5)
	if err == nil || !strings.Contains(err.Error(), "something broke") {
		t.Errorf("err = %v, want it to contain the server error", err)
	}
}

func TestNextTokenLogprobs_NoLogprobs(t *testing.T) {
	srv, _ := newTestServer(t, http.StatusOK, `{"model":"m","response":"1","done":true}`)

	_, err := New(srv.URL).NextTokenLogprobs(context.Background(), "m", "prompt", 5)
	if !errors.Is(err, ErrNoLogprobs) {
		t.Errorf("err = %v, want ErrNoLogprobs", err)
	}
}

func TestNextTokenLogprobs_ContextCanceled(t *testing.T) {
	srv, _ := newTestServer(t, http.StatusOK, generateOKFixture)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := New(srv.URL).NextTokenLogprobs(ctx, "m", "prompt", 5)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestNextTokenLogprobs_InvalidInput(t *testing.T) {
	c := New("http://unused.invalid")
	tests := []struct {
		name   string
		model  string
		prompt string
		topN   int
	}{
		{"empty model", "", "p", 5},
		{"empty prompt", "m", "", 5},
		{"topN zero", "m", "p", 0},
		{"topN too large", "m", "p", maxTopN + 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := c.NextTokenLogprobs(context.Background(), tt.model, tt.prompt, tt.topN); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestNew(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", DefaultBaseURL},
		{"http://gpu-box:11434/", "http://gpu-box:11434"},
		{"http://gpu-box:11434", "http://gpu-box:11434"},
	}
	for _, tt := range tests {
		c := New(tt.in)
		if c.BaseURL != tt.want {
			t.Errorf("New(%q).BaseURL = %q, want %q", tt.in, c.BaseURL, tt.want)
		}
		if c.HTTP == nil || c.HTTP.Timeout == 0 {
			t.Errorf("New(%q) should set an HTTP client with a timeout", tt.in)
		}
	}
}

func TestToTokenLogprobs_MissingBytes(t *testing.T) {
	got := toTokenLogprobs([]tokenLogprobWire{
		{Token: "", Logprob: -6.2},
		{Token: "1", Logprob: -0.4, Bytes: []int{49}},
	})
	if len(got) != 2 {
		t.Fatalf("got %d tokens, want 2", len(got))
	}
	if got[0].Token != "" || got[0].Logprob != -6.2 || got[0].Bytes != nil {
		t.Errorf("got[0] = %+v, want empty token with nil bytes", got[0])
	}
	if !bytes.Equal(got[1].Bytes, []byte("1")) {
		t.Errorf("got[1].Bytes = %v, want [49]", got[1].Bytes)
	}
}

func TestNextTokenLogprobs_KeepAliveOmittedWhenUnset(t *testing.T) {
	srv, got := newTestServer(t, http.StatusOK, generateOKFixture)

	if _, err := New(srv.URL).NextTokenLogprobs(context.Background(), "m", "prompt", 5); err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Body["keep_alive"]; ok {
		t.Errorf("keep_alive should be omitted when unset, body = %v", got.Body)
	}
}

func TestNextTokenLogprobs_EmptyTopLogprobs(t *testing.T) {
	srv, _ := newTestServer(t, http.StatusOK, `{"logprobs":[{"token":"1","logprob":-0.1,"top_logprobs":[]}]}`)

	_, err := New(srv.URL).NextTokenLogprobs(context.Background(), "m", "prompt", 5)
	if !errors.Is(err, ErrNoLogprobs) {
		t.Errorf("err = %v, want ErrNoLogprobs", err)
	}
}

func TestNextTokenLogprobs_MalformedJSON(t *testing.T) {
	srv, _ := newTestServer(t, http.StatusOK, `{"logprobs":[{"token":`)

	if _, err := New(srv.URL).NextTokenLogprobs(context.Background(), "m", "prompt", 5); err == nil {
		t.Error("expected decode error")
	}
}

func TestNextTokenLogprobs_OversizedBody(t *testing.T) {
	huge := `{"error":"` + strings.Repeat("x", maxResponseBytes) + `"}`
	srv, _ := newTestServer(t, http.StatusOK, huge)

	if _, err := New(srv.URL).NextTokenLogprobs(context.Background(), "m", "prompt", 5); err == nil {
		t.Error("expected error for body over the size limit")
	}
}

func TestNextTokenLogprobs_CanceledInFlight(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := New(srv.URL).NextTokenLogprobs(ctx, "m", "prompt", 5)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestNextTokenLogprobs_NilHTTPClient(t *testing.T) {
	srv, _ := newTestServer(t, http.StatusOK, generateOKFixture)
	c := &Client{BaseURL: srv.URL}

	if _, err := c.NextTokenLogprobs(context.Background(), "m", "prompt", 5); err != nil {
		t.Errorf("zero-value HTTP client should fall back to default, got %v", err)
	}
}

func TestToTokenLogprobs_OutOfRangeBytes(t *testing.T) {
	got := toTokenLogprobs([]tokenLogprobWire{{Token: "x", Bytes: []int{49, 300}}, {Token: "y", Bytes: []int{-1}}})
	for _, tk := range got {
		if tk.Bytes != nil {
			t.Errorf("token %q bytes = %v, want nil for out-of-range input", tk.Token, tk.Bytes)
		}
	}
}
