package metrics

import (
	"context"
	"time"
)

type Sample struct {
	T time.Time
	V float64
}

type Series struct {
	Labels  map[string]string
	Samples []Sample
}

type Source interface {
	Query(ctx context.Context, q string, at time.Time) ([]Series, error)
	QueryRange(ctx context.Context, q string, start, end time.Time, step time.Duration) ([]Series, error)
}
