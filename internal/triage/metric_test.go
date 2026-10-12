package triage

import (
	"strings"
	"testing"
)

func TestParseMetric(t *testing.T) {
	m, err := ParseMetric(` p99_ms = histogram_quantile(0.99, sum by (le) (rate(x_bucket{a="b=c"}[5m]))) `)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "p99_ms" || m.Query != `histogram_quantile(0.99, sum by (le) (rate(x_bucket{a="b=c"}[5m])))` {
		t.Errorf("got %+v", m)
	}
	for _, bad := range []string{"noequals", "=up", "name=", "Bad=up", "9x=up", "a-b=up", "a b=up"} {
		if _, err := ParseMetric(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func TestParseMetrics(t *testing.T) {
	ms, err := ParseMetrics(strings.NewReader("# comment\n\nrps=sum(rate(x[1m]))\n  # indented comment\nup=up\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0].Name != "rps" || ms[1].Query != "up" {
		t.Errorf("got %+v", ms)
	}
	_, err = ParseMetrics(strings.NewReader("ok=up\nbroken\n"))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("err = %v, want line number", err)
	}
}

func TestCheckUnique(t *testing.T) {
	if err := CheckUnique([]Metric{{Name: "a"}, {Name: "b"}}); err != nil {
		t.Error(err)
	}
	if err := CheckUnique([]Metric{{Name: "a"}, {Name: "a"}}); err == nil {
		t.Error("expected duplicate error")
	}
}

func TestParseMetric_Targets(t *testing.T) {
	m, err := ParseMetric(`err_ratio[max=0.01]=sum(rate(x{code=~"5.."}[2m])) / sum(rate(x[2m]))`)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "err_ratio" || m.Target == nil || m.Target.Min != nil || *m.Target.Max != 0.01 || m.Query != `sum(rate(x{code=~"5.."}[2m])) / sum(rate(x[2m]))` {
		t.Errorf("got %+v", m)
	}
	m, err = ParseMetric(" replicas [ min = 2 , max=5 ] = up ")
	if err != nil || *m.Target.Min != 2 || *m.Target.Max != 5 || m.Query != "up" {
		t.Errorf("spaced range: %+v %v", m, err)
	}
	if m, err := ParseMetric("rps=sum(rate(x[2m]))"); err != nil || m.Target != nil {
		t.Errorf("brackets in the query must not be read as a target: %+v %v", m, err)
	}
	for _, bad := range []string{"a[]=up", "a[max]=up", "a[max=x]=up", "a[max=NaN]=up", "a[max=Inf]=up", "a[avg=1]=up", "a[max=1,max=2]=up", "a[min=5,max=1]=up", "a[max=1=up", "a[max=1]="} {
		if _, err := ParseMetric(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}
