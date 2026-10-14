package server

import (
	"encoding/json"
	"net/http"
	"strconv"
)

func Handler(s *Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/services", func(w http.ResponseWriter, r *http.Request) {
		all := s.All()
		if !verbose(r) {
			for i := range all {
				all[i] = trim(all[i])
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"services": all})
	})
	mux.HandleFunc("GET /v1/services/{name}", func(w http.ResponseWriter, r *http.Request) {
		st, ok := s.Get(r.PathValue("name"))
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown service"})
			return
		}
		if !verbose(r) {
			st = trim(st)
		}
		writeJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("GET /status", serveStatus(s))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if !s.Ready() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "pending"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}

func verbose(r *http.Request) bool {
	v, _ := strconv.ParseBool(r.URL.Query().Get("verbose"))
	return v
}

func trim(st Status) Status {
	if st.Last != nil {
		last := *st.Last
		last.Readings = nil
		st.Last = &last
	}
	return st
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
