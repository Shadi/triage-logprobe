package datadog

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV1"

	"github.com/shadi/triage-logprobs/internal/metrics"
)

const (
	DefaultSite  = "datadoghq.com"
	nowWindow    = 3 * time.Minute
	nowLag       = 2 * time.Minute
	maxErrorBody = 512
)

var siteName = regexp.MustCompile(`^[a-z0-9]+([.-][a-z0-9]+)*$`)

type Client struct {
	api  *datadogV1.MetricsApi
	keys map[string]datadog.APIKey
	site map[string]string
}

var _ metrics.Source = (*Client)(nil)

func New(apiKey, appKey, site string) (*Client, error) {
	if apiKey == "" || appKey == "" {
		return nil, errors.New("datadog: API key and application key are required")
	}
	if site == "" {
		site = DefaultSite
	}
	if !siteName.MatchString(site) {
		return nil, fmt.Errorf("datadog: site %q: want a hostname such as datadoghq.eu", site)
	}
	cfg := datadog.NewConfiguration()
	cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	cfg.RetryConfiguration.EnableRetry = true
	cfg.RetryConfiguration.MaxRetries = 2
	cfg.RetryConfiguration.RetryJitter = time.Second
	return &Client{
		api:  datadogV1.NewMetricsApi(datadog.NewAPIClient(cfg)),
		keys: map[string]datadog.APIKey{"apiKeyAuth": {Key: apiKey}, "appKeyAuth": {Key: appKey}},
		site: map[string]string{"site": site},
	}, nil
}

func (c *Client) Query(ctx context.Context, q string, at time.Time) ([]metrics.Series, error) {
	end := at.Add(-nowLag)
	ss, err := c.QueryRange(ctx, q, end.Add(-nowWindow), end, 0)
	if err != nil {
		return nil, err
	}
	return mean(ss), nil
}

func (c *Client) QueryRange(ctx context.Context, q string, start, end time.Time, _ time.Duration) ([]metrics.Series, error) {
	ctx = context.WithValue(ctx, datadog.ContextAPIKeys, c.keys)
	ctx = context.WithValue(ctx, datadog.ContextServerVariables, c.site)
	resp, _, err := c.api.QueryMetrics(ctx, start.Unix(), end.Unix(), q)
	if err != nil {
		var oe datadog.GenericOpenAPIError
		if errors.As(err, &oe) && len(oe.Body()) > 0 {
			body := oe.Body()
			if len(body) > maxErrorBody {
				body = body[:maxErrorBody]
			}
			return nil, fmt.Errorf("datadog: query: %s: %s", oe.Error(), body)
		}
		return nil, fmt.Errorf("datadog: query: %w", err)
	}
	return convert(resp)
}

func convert(r datadogV1.MetricsQueryResponse) ([]metrics.Series, error) {
	if r.GetStatus() != "ok" {
		return nil, fmt.Errorf("datadog: query status %q: %s", r.GetStatus(), r.GetError())
	}
	out := make([]metrics.Series, 0, len(r.Series))
	seen := make(map[string]bool, len(r.Series))
	for _, s := range r.Series {
		key := strings.Join(slices.Sorted(slices.Values(s.TagSet)), ",")
		if seen[key] {
			return nil, fmt.Errorf("datadog: several series with tags {%s}; use one query per metric and group by distinct tags", key)
		}
		seen[key] = true
		samples := make([]metrics.Sample, 0, len(s.Pointlist))
		for _, p := range s.Pointlist {
			if len(p) < 2 || p[0] == nil {
				continue
			}
			v := math.NaN()
			if p[1] != nil {
				v = *p[1]
			}
			samples = append(samples, metrics.Sample{T: time.UnixMilli(int64(*p[0])), V: v})
		}
		out = append(out, metrics.Series{Labels: labels(s.TagSet), Samples: samples})
	}
	return out, nil
}

func labels(tags []string) map[string]string {
	out := make(map[string]string, len(tags))
	for _, t := range tags {
		k, v, _ := strings.Cut(t, ":")
		out[k] = v
	}
	return out
}

func mean(ss []metrics.Series) []metrics.Series {
	out := make([]metrics.Series, len(ss))
	for i, s := range ss {
		out[i].Labels = s.Labels
		var sum float64
		var n int
		var last time.Time
		for _, p := range s.Samples {
			if math.IsNaN(p.V) || math.IsInf(p.V, 0) {
				continue
			}
			sum += p.V
			n++
			last = p.T
		}
		if n > 0 {
			out[i].Samples = []metrics.Sample{{T: last, V: sum / float64(n)}}
		}
	}
	return out
}
