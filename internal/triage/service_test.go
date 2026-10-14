package triage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseService(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	ok := write("ok.yaml", "sources:\n  - source: prometheus\n    metrics:\n      - {name: rps, query: 'sum(rate(x[1m]))'}\n      - {name: up, query: up}\n")
	s, err := ParseService("checkout-api=" + ok)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "checkout-api" || len(s.Metrics) != 2 || s.Metrics[1].Name != "up" {
		t.Errorf("service = %+v", s)
	}
	for name, arg := range map[string]string{
		"no path":    "echo",
		"empty path": "echo=",
		"bad name":   "Echo=" + ok,
		"underscore": "my_svc=" + ok,
		"bad file":   "echo=" + write("bad.yaml", "# only comments\n"),
	} {
		if _, err := ParseService(arg); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestCheckUniqueServices(t *testing.T) {
	if err := CheckUniqueServices([]Service{{Name: "a"}, {Name: "b"}}); err != nil {
		t.Error(err)
	}
	if err := CheckUniqueServices([]Service{{Name: "a"}, {Name: "a"}}); err == nil {
		t.Error("duplicate: expected error")
	}
}
