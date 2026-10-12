package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/shadi/triage-logprobe/internal/triage"
)

type CheckFunc func(ctx context.Context, svc triage.Service, at time.Time) (triage.Result, error)

type Runner struct {
	Services []triage.Service
	Check    CheckFunc
	Store    *Store
	Interval time.Duration
	Timeout  time.Duration
	Log      *slog.Logger
}

func (r *Runner) Run(ctx context.Context) {
	r.tick(ctx)
	t := time.NewTicker(r.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.tick(ctx)
		}
	}
}

func (r *Runner) tick(ctx context.Context) {
	for _, svc := range r.Services {
		if ctx.Err() != nil {
			return
		}
		at := time.Now()
		cctx, cancel := context.WithTimeout(ctx, r.Timeout)
		res, err := r.Check(cctx, svc, at)
		cancel()
		if err != nil && ctx.Err() != nil {
			return
		}
		r.Store.Record(svc.Name, res, err)
		if err != nil {
			r.Log.Error("check failed", "service", svc.Name, "err", err)
			continue
		}
		r.Log.Info("checked", "service", svc.Name, "severity", res.Severity,
			"confidence", res.Answer.Confidence, "metrics_ms", res.LatencyMS["metrics"], "model_ms", res.LatencyMS["model"])
	}
}
