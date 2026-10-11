package prometheus

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func server(t *testing.T, status int, body string) (*Client, *http.Request) {
	t.Helper()
	got := &http.Request{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		*got = *r.Clone(context.Background())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return c, got
}

func TestQuery_Vector(t *testing.T) {
	c, req := server(t, 200, `{"status":"success","data":{"resultType":"vector","result":[
		{"metric":{"pod":"a"},"value":[1760000000,"0.25"]},
		{"metric":{"pod":"b"},"value":[1760000000,"NaN"]}]}}`)

	at := time.Unix(1760000000, 0)
	got, err := c.Query(context.Background(), `up{job="x"}`, at)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Path != "/api/v1/query" || req.Form.Get("query") != `up{job="x"}` {
		t.Errorf("request = %s %v", req.URL.Path, req.Form)
	}
	if len(got) != 2 || got[0].Labels["pod"] != "a" || got[0].Samples[0].V != 0.25 || !got[0].Samples[0].T.Equal(at) {
		t.Errorf("series[0] = %+v", got)
	}
	if !math.IsNaN(got[1].Samples[0].V) {
		t.Errorf("NaN sample = %v, want NaN preserved", got[1].Samples[0].V)
	}
}

func TestQueryRange_Matrix(t *testing.T) {
	c, req := server(t, 200, `{"status":"success","data":{"resultType":"matrix","result":[
		{"metric":{"pod":"a"},"values":[[1760000000,"1"],[1760000060,"3"]]}]}}`)

	start := time.Unix(1760000000, 0)
	got, err := c.QueryRange(context.Background(), "q", start, start.Add(time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Path != "/api/v1/query_range" || req.Form.Get("step") != "60" {
		t.Errorf("request = %s %v", req.URL.Path, req.Form)
	}
	if len(got) != 1 || len(got[0].Samples) != 2 || got[0].Samples[1].V != 3 {
		t.Errorf("series = %+v", got)
	}
}

func TestQuery_Scalar(t *testing.T) {
	c, _ := server(t, 200, `{"status":"success","data":{"resultType":"scalar","result":[1760000000,"42"]}}`)
	got, err := c.Query(context.Background(), "scalar(1)", time.Unix(1760000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Samples[0].V != 42 || len(got[0].Labels) != 0 {
		t.Errorf("series = %+v", got)
	}
}

func TestQuery_Empty(t *testing.T) {
	c, _ := server(t, 200, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
	got, err := c.Query(context.Background(), "q", time.Now())
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want empty, nil", got, err)
	}
}

func TestQuery_Error(t *testing.T) {
	c, _ := server(t, 400, `{"status":"error","errorType":"bad_data","error":"parse error at char 5"}`)
	_, err := c.Query(context.Background(), "sum(", time.Now())
	if err == nil || !strings.Contains(err.Error(), "parse error") {
		t.Errorf("err = %v, want Prometheus' message", err)
	}
}

func TestQuery_Warnings(t *testing.T) {
	c, _ := server(t, 200, `{"status":"success","warnings":["metric might not be a counter"],"data":{"resultType":"vector","result":[]}}`)
	var got []string
	c.Warn = func(q, w string) { got = append(got, q+": "+w) }
	if _, err := c.Query(context.Background(), "rate(x[5m])", time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "rate(x[5m]): metric might not be a counter" {
		t.Errorf("warnings = %v", got)
	}
}

func TestQuery_RejectsNativeHistograms(t *testing.T) {
	c, _ := server(t, 200, `{"status":"success","data":{"resultType":"vector","result":[
		{"metric":{},"histogram":[1760000000,{"count":"2","sum":"3","buckets":[[0,"0","1","2"]]}]}]}}`)
	if _, err := c.Query(context.Background(), "h", time.Now()); err == nil || !strings.Contains(err.Error(), "native histogram") {
		t.Errorf("err = %v, want native histogram error", err)
	}
}

func TestQuery_UnsupportedType(t *testing.T) {
	c, _ := server(t, 200, `{"status":"success","data":{"resultType":"string","result":[1760000000,"x"]}}`)
	if _, err := c.Query(context.Background(), "q", time.Now()); err == nil {
		t.Error("expected error for string result")
	}
}
