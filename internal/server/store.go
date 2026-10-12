package server

import (
	"sync"
	"time"

	"github.com/shadi/triage-logprobe/internal/triage"
)

const (
	StatusPending = "pending"
	StatusOK      = "ok"
	StatusStale   = "stale"
	StatusError   = "error"
)

type Status struct {
	Service     string         `json:"service"`
	Status      string         `json:"status"`
	Last        *triage.Result `json:"last,omitempty"`
	LastOKAt    *time.Time     `json:"last_ok_at,omitempty"`
	CheckedAt   *time.Time     `json:"checked_at,omitempty"`
	LastError   string         `json:"last_error,omitempty"`
	LastErrorAt *time.Time     `json:"last_error_at,omitempty"`
}

type entry struct {
	last            *triage.Result
	okAt, checkedAt time.Time
	lastErr         string
	errAt           time.Time
}

type Store struct {
	mu         sync.RWMutex
	staleAfter time.Duration
	now        func() time.Time
	order      []string
	entries    map[string]*entry
}

func NewStore(names []string, staleAfter time.Duration) *Store {
	s := &Store{staleAfter: staleAfter, now: time.Now, entries: make(map[string]*entry, len(names))}
	for _, n := range names {
		if _, ok := s.entries[n]; ok {
			continue
		}
		s.order = append(s.order, n)
		s.entries[n] = &entry{}
	}
	return s
}

func (s *Store) Record(name string, res triage.Result, err error) {
	at := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[name]
	if !ok {
		return
	}
	e.checkedAt = at
	if err != nil {
		e.lastErr, e.errAt = err.Error(), at
		return
	}
	e.last, e.okAt = &res, at
	e.lastErr, e.errAt = "", time.Time{}
}

func (s *Store) Get(name string) (Status, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[name]
	if !ok {
		return Status{}, false
	}
	return s.status(name, e), true
}

func (s *Store) All() []Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Status, len(s.order))
	for i, n := range s.order {
		out[i] = s.status(n, s.entries[n])
	}
	return out
}

func (s *Store) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.entries {
		if e.last == nil {
			return false
		}
	}
	return true
}

func (s *Store) status(name string, e *entry) Status {
	st := Status{Service: name, Last: e.last, LastError: e.lastErr}
	switch {
	case e.checkedAt.IsZero():
		st.Status = StatusPending
	case e.last == nil:
		st.Status = StatusError
	case s.now().Sub(e.okAt) > s.staleAfter:
		st.Status = StatusStale
	default:
		st.Status = StatusOK
	}
	st.LastOKAt = timePtr(e.okAt)
	st.CheckedAt = timePtr(e.checkedAt)
	st.LastErrorAt = timePtr(e.errAt)
	return st
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}
