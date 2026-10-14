package server

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
	"time"
)

//go:embed status.html
var statusHTML string

var statusPage = template.Must(template.New("status").Parse(statusHTML))

type statusRow struct {
	Service, Status, Severity, Checked, Error string
	HasResult                                 bool
}

func statusRows(all []Status, now time.Time) []statusRow {
	out := make([]statusRow, len(all))
	for i, st := range all {
		r := statusRow{Service: st.Service, Status: st.Status, Error: st.LastError, Checked: ago(st.CheckedAt, now)}
		if st.Last != nil {
			r.HasResult, r.Severity = true, st.Last.Severity
		}
		out[i] = r
	}
	return out
}

func ago(t *time.Time, now time.Time) string {
	if t == nil {
		return "never"
	}
	d := max(now.Sub(*t), 0)
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%dm ago", int(d.Hours()), int(d.Minutes())%60)
}

func serveStatus(s *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b bytes.Buffer
		if err := statusPage.ExecuteTemplate(&b, "page", statusRows(s.All(), s.now())); err != nil {
			http.Error(w, "render status page: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = b.WriteTo(w)
	}
}
