package datadog

import (
	"math"
	"testing"
	"time"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV1"

	"github.com/shadi/triage-logprobs/internal/metrics"
)

func p(v float64) *float64 { return &v }

func TestConvert(t *testing.T) {
	ok := "ok"
	resp := datadogV1.MetricsQueryResponse{Status: &ok, Series: []datadogV1.MetricsQueryMetadata{
		{TagSet: []string{"service:echo", "url:http://x:80", "canary"}, Pointlist: [][]*float64{{p(60000), p(1.5)}, {p(120000), nil}, {nil, p(9)}, {p(180000)}}},
		{Pointlist: [][]*float64{{p(60000), p(2)}}},
	}}
	ss, err := convert(resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 2 {
		t.Fatalf("series = %+v", ss)
	}
	l := ss[0].Labels
	if len(l) != 3 || l["service"] != "echo" || l["url"] != "http://x:80" || l["canary"] != "" {
		t.Errorf("labels = %v", l)
	}
	s := ss[0].Samples
	if len(s) != 2 || s[0].V != 1.5 || !s[0].T.Equal(time.UnixMilli(60000)) || !math.IsNaN(s[1].V) {
		t.Errorf("samples = %+v, want 1.5 then NaN for the null point, malformed points dropped", s)
	}
	if len(ss[1].Labels) != 0 {
		t.Errorf("no tags: labels = %v", ss[1].Labels)
	}

	dup := datadogV1.MetricsQueryResponse{Status: &ok, Series: []datadogV1.MetricsQueryMetadata{{}, {}}}
	if _, err := convert(dup); err == nil {
		t.Error("two series with the same tags: expected error")
	}

	status, msg := "error", "bad query"
	if _, err := convert(datadogV1.MetricsQueryResponse{Status: &status, Error: &msg}); err == nil {
		t.Error("error status: expected error")
	}
}

func TestMean(t *testing.T) {
	in := []metrics.Series{
		{Labels: map[string]string{"a": "1"}, Samples: []metrics.Sample{{T: time.Unix(1, 0), V: 1}, {T: time.Unix(2, 0), V: math.NaN()}, {T: time.Unix(3, 0), V: 5}}},
		{Labels: map[string]string{"a": "2"}, Samples: []metrics.Sample{{T: time.Unix(1, 0), V: math.NaN()}}},
	}
	out := mean(in)
	if len(out) != 2 || len(out[0].Samples) != 1 || out[0].Samples[0].V != 3 || !out[0].Samples[0].T.Equal(time.Unix(3, 0)) {
		t.Errorf("out[0] = %+v, want one sample, mean 3 at the last finite point", out[0])
	}
	if len(out[1].Samples) != 0 || out[1].Labels["a"] != "2" {
		t.Errorf("out[1] = %+v, want labels kept and no samples", out[1])
	}
}

func TestNew(t *testing.T) {
	if _, err := New("", "app", ""); err == nil {
		t.Error("missing API key: expected error")
	}
	if _, err := New("api", "", ""); err == nil {
		t.Error("missing app key: expected error")
	}
	for _, site := range []string{"evil.com/x", "a..b", "https://datadoghq.com"} {
		if _, err := New("api", "app", site); err == nil {
			t.Errorf("site %q: expected error", site)
		}
	}
	if _, err := New("api", "app", "us5.datadoghq.com"); err != nil {
		t.Error(err)
	}
}
