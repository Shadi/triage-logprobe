package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/shadi/triage-logprobe/internal/decision"
	"github.com/shadi/triage-logprobe/internal/triage"
)

var t0 = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

func result(sev string) triage.Result {
	return triage.Result{
		Severity: sev,
		Answer:   decision.Answer{Choice: sev, Probs: map[string]float64{sev: 1}},
		State:    "s",
		Readings: []triage.Reading{{Metric: triage.Metric{Name: "m", Query: "q"}}},
		At:       t0,
	}
}

func TestStore_Transitions(t *testing.T) {
	s := NewStore([]string{"b", "a", "b"}, time.Minute)
	now := t0
	s.now = func() time.Time { return now }

	if st, _ := s.Get("a"); st.Status != StatusPending || st.CheckedAt != nil || s.Ready() {
		t.Fatalf("initial: %+v ready=%v", st, s.Ready())
	}
	if _, ok := s.Get("nope"); ok {
		t.Error("unknown service found")
	}

	s.Record("a", triage.Result{}, errors.New("boom"))
	if st, _ := s.Get("a"); st.Status != StatusError || st.Last != nil || st.LastError != "boom" || st.LastErrorAt == nil {
		t.Errorf("first failure: %+v", st)
	}

	s.Record("a", result("SEV2"), nil)
	if st, _ := s.Get("a"); !st.LastOKAt.Equal(t0) || st.Status != StatusOK || st.Last.Severity != "SEV2" || st.LastError != "" || st.LastErrorAt != nil {
		t.Errorf("success: %+v", st)
	}

	now = t0.Add(30 * time.Second)
	s.Record("a", triage.Result{}, errors.New("ollama down"))
	st, _ := s.Get("a")
	if st.Status != StatusOK || st.Last == nil || st.Last.Severity != "SEV2" || st.LastError != "ollama down" {
		t.Errorf("failure after success must keep last: %+v", st)
	}
	if !st.CheckedAt.Equal(now) || !st.LastOKAt.Equal(t0) {
		t.Errorf("checked_at=%v last_ok_at=%v", st.CheckedAt, st.LastOKAt)
	}

	now = t0.Add(2 * time.Minute)
	if st, _ := s.Get("a"); st.Status != StatusStale {
		t.Errorf("status = %q, want stale", st.Status)
	}

	if s.Ready() {
		t.Error("ready before b was checked")
	}
	s.Record("b", triage.Result{}, errors.New("x"))
	if s.Ready() {
		t.Error("ready although b has never succeeded")
	}
	s.Record("b", result("SEV3"), nil)
	if !s.Ready() {
		t.Error("not ready after every service succeeded once")
	}
	all := s.All()
	if len(all) != 2 || all[0].Service != "b" || all[1].Service != "a" {
		t.Errorf("all = %+v, want flag order b, a without duplicates", all)
	}
	s.Record("nope", result("SEV1"), nil)
}

type recorder struct {
	mu    sync.Mutex
	calls []string
	fail  map[string]bool
}

func (r *recorder) check(ctx context.Context, svc triage.Service, _ time.Time) (triage.Result, error) {
	r.mu.Lock()
	r.calls = append(r.calls, svc.Name)
	fail := r.fail[svc.Name]
	r.mu.Unlock()
	if _, ok := ctx.Deadline(); !ok {
		return triage.Result{}, errors.New("no per-check deadline")
	}
	if fail {
		return triage.Result{}, errors.New("boom")
	}
	return result("SEV3"), nil
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func TestRunner(t *testing.T) {
	svcs := []triage.Service{{Name: "a"}, {Name: "b"}}
	store := NewStore([]string{"a", "b"}, time.Hour)
	rec := &recorder{fail: map[string]bool{"b": true}}
	r := &Runner{Services: svcs, Check: rec.check, Store: store, Interval: 20 * time.Millisecond, Timeout: time.Second,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(ctx)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for rec.count() < 6 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runner did not stop")
	}
	if rec.count() < 6 {
		t.Fatalf("calls = %d, want at least 3 ticks", rec.count())
	}
	if st, _ := store.Get("a"); st.Status != StatusOK || st.Last.Severity != "SEV3" {
		t.Errorf("a = %+v", st)
	}
	if st, _ := store.Get("b"); st.Status != StatusError || st.LastError != "boom" {
		t.Errorf("b = %+v", st)
	}
	n := rec.count()
	time.Sleep(50 * time.Millisecond)
	if rec.count() != n {
		t.Error("checks continued after cancel")
	}
}

func TestRunner_ShutdownDoesNotRecordCancellation(t *testing.T) {
	store := NewStore([]string{"a"}, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{Services: []triage.Service{{Name: "a"}}, Store: store, Interval: time.Hour, Timeout: time.Hour,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Check: func(ctx context.Context, _ triage.Service, _ time.Time) (triage.Result, error) {
			cancel()
			<-ctx.Done()
			return triage.Result{}, ctx.Err()
		}}
	r.Run(ctx)
	if st, _ := store.Get("a"); st.Status != StatusPending {
		t.Errorf("status = %q, want pending: a shutdown is not a failed check", st.Status)
	}
}

func get(t *testing.T, h http.Handler, method, path string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s %s: %v: %s", method, path, err, w.Body)
	}
	return w, body
}

func TestHandler(t *testing.T) {
	store := NewStore([]string{"echo", "cart"}, time.Hour)
	h := Handler(store)

	if w, _ := get(t, h, "GET", "/healthz"); w.Code != 200 {
		t.Errorf("healthz = %d", w.Code)
	}
	if w, _ := get(t, h, "GET", "/readyz"); w.Code != 503 {
		t.Errorf("readyz before checks = %d, want 503", w.Code)
	}
	if w, b := get(t, h, "GET", "/v1/services/echo"); w.Code != 200 || b["status"] != StatusPending {
		t.Errorf("pending echo = %d %v", w.Code, b)
	}

	store.Record("echo", result("SEV1"), nil)
	store.Record("cart", triage.Result{}, errors.New("boom"))
	if w, _ := get(t, h, "GET", "/readyz"); w.Code != 503 {
		t.Errorf("readyz with a service that never succeeded = %d, want 503", w.Code)
	}

	w, b := get(t, h, "GET", "/v1/services/echo")
	last, _ := b["last"].(map[string]any)
	if w.Code != 200 || b["status"] != StatusOK || last["severity"] != "SEV1" {
		t.Errorf("echo = %d %v", w.Code, b)
	}
	if _, ok := last["readings"]; ok {
		t.Error("readings must be hidden without verbose")
	}
	_, b = get(t, h, "GET", "/v1/services/echo?verbose=1")
	if last, _ := b["last"].(map[string]any); last["readings"] == nil {
		t.Error("verbose must include readings")
	}
	if st, _ := store.Get("echo"); len(st.Last.Readings) != 1 {
		t.Error("trimming the response must not modify the store")
	}

	w, b = get(t, h, "GET", "/v1/services")
	list, _ := b["services"].([]any)
	if w.Code != 200 || len(list) != 2 {
		t.Fatalf("list = %d %v", w.Code, b)
	}
	if first, _ := list[0].(map[string]any); first["service"] != "echo" {
		t.Errorf("first = %v", first)
	} else if l, _ := first["last"].(map[string]any); l["readings"] != nil {
		t.Error("list must hide readings without verbose")
	}
	if second, _ := list[1].(map[string]any); second["status"] != StatusError || second["last_error"] != "boom" {
		t.Errorf("second = %v", second)
	}

	if w, b := get(t, h, "GET", "/v1/services/nope"); w.Code != 404 || b["error"] == nil {
		t.Errorf("unknown = %d %v", w.Code, b)
	}
	if w, _ = get(t, h, "GET", "/v1/services/echo"); w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("content-type = %q", w.Header().Get("Content-Type"))
	}
}

func TestHandler_Stale(t *testing.T) {
	store := NewStore([]string{"echo"}, time.Minute)
	now := t0
	store.now = func() time.Time { return now }
	store.Record("echo", result("SEV2"), nil)
	now = t0.Add(2 * time.Minute)
	if w, b := get(t, Handler(store), "GET", "/v1/services/echo"); w.Code != 200 || b["status"] != StatusStale {
		t.Errorf("echo = %d %v, want stale", w.Code, b)
	}
}
