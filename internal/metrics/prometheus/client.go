package prometheus

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"github.com/shadi/triage-logprobs/internal/metrics"
)

type Client struct {
	api  v1.API
	Warn func(query, warning string)
}

var _ metrics.Source = (*Client)(nil)

func New(address string) (*Client, error) {
	c, err := api.NewClient(api.Config{
		Address: address,
		Client:  &http.Client{Timeout: 30 * time.Second},
	})
	if err != nil {
		return nil, fmt.Errorf("prometheus: %w", err)
	}
	return &Client{api: v1.NewAPI(c)}, nil
}

func (c *Client) Query(ctx context.Context, q string, at time.Time) ([]metrics.Series, error) {
	v, warns, _, err := c.api.Query(ctx, q, at)
	if err != nil {
		return nil, fmt.Errorf("prometheus: instant query: %w", err)
	}
	c.warn(q, warns)
	return convert(v)
}

func (c *Client) QueryRange(ctx context.Context, q string, start, end time.Time, step time.Duration) ([]metrics.Series, error) {
	v, warns, _, err := c.api.QueryRange(ctx, q, v1.Range{Start: start, End: end, Step: step})
	if err != nil {
		return nil, fmt.Errorf("prometheus: range query: %w", err)
	}
	c.warn(q, warns)
	return convert(v)
}

func (c *Client) warn(q string, warns v1.Warnings) {
	if c.Warn == nil {
		return
	}
	for _, w := range warns {
		c.Warn(q, w)
	}
}

func convert(v model.Value) ([]metrics.Series, error) {
	switch val := v.(type) {
	case model.Vector:
		out := make([]metrics.Series, 0, len(val))
		for _, s := range val {
			if s.Histogram != nil {
				return nil, errNativeHistogram
			}
			out = append(out, metrics.Series{
				Labels:  labels(s.Metric),
				Samples: []metrics.Sample{{T: s.Timestamp.Time(), V: float64(s.Value)}},
			})
		}
		return out, nil
	case model.Matrix:
		out := make([]metrics.Series, 0, len(val))
		for _, s := range val {
			if len(s.Histograms) > 0 {
				return nil, errNativeHistogram
			}
			samples := make([]metrics.Sample, 0, len(s.Values))
			for _, p := range s.Values {
				samples = append(samples, metrics.Sample{T: p.Timestamp.Time(), V: float64(p.Value)})
			}
			out = append(out, metrics.Series{Labels: labels(s.Metric), Samples: samples})
		}
		return out, nil
	case *model.Scalar:
		return []metrics.Series{{
			Labels:  map[string]string{},
			Samples: []metrics.Sample{{T: val.Timestamp.Time(), V: float64(val.Value)}},
		}}, nil
	default:
		return nil, fmt.Errorf("prometheus: unsupported result type %T", v)
	}
}

var errNativeHistogram = errors.New("prometheus: native histogram results are not supported; wrap the query in histogram_quantile or a similar function")

func labels(m model.Metric) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[string(k)] = string(v)
	}
	return out
}
