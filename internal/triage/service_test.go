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
	ok := write("ok.txt", "# c\nrps=sum(rate(x[1m]))\nup=up\n")
	s, err := ParseService("checkout-api=" + ok)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "checkout-api" || len(s.Metrics) != 2 || s.Metrics[1].Name != "up" {
		t.Errorf("service = %+v", s)
	}
	for name, arg := range map[string]string{
		"no path":      "echo",
		"empty path":   "echo=",
		"bad name":     "Echo=" + ok,
		"underscore":   "my_svc=" + ok,
		"missing file": "echo=" + filepath.Join(dir, "nope.txt"),
		"empty file":   "echo=" + write("empty.txt", "# only comments\n\n"),
		"bad metric":   "echo=" + write("bad.txt", "nope\n"),
		"duplicate":    "echo=" + write("dup.txt", "a=up\na=up\n"),
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
