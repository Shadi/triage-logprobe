package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/shadi/triage-logprobe/internal/server"
	"github.com/shadi/triage-logprobe/internal/triage"
)

const (
	minInterval   = 10 * time.Second
	staleAfter    = 3
	shutdownGrace = 10 * time.Second
)

type serviceFlags []triage.Service

func (s *serviceFlags) String() string {
	names := make([]string, len(*s))
	for i, x := range *s {
		names[i] = x.Name
	}
	return strings.Join(names, ",")
}

func (s *serviceFlags) Set(v string) error {
	x, err := triage.ParseService(v)
	if err != nil {
		return err
	}
	*s = append(*s, x)
	return nil
}

type serveConfig struct {
	common
	services serviceFlags
	listen   string
	interval time.Duration
}

func serveMain(args []string) int {
	cfg, err := parseServeFlags(args)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := serve(cfg, log); err != nil {
		log.Error("serve", "err", err)
		return 1
	}
	return 0
}

func parseServeFlags(args []string) (serveConfig, error) {
	var cfg serveConfig
	fs := flag.NewFlagSet("triage-logprobe serve", flag.ContinueOnError)
	addCommonFlags(fs, &cfg.common, 0, "timeout per check (default: -interval)")
	fs.Var(&cfg.services, "service", "service as name=path/to/metrics.txt; repeatable")
	fs.StringVar(&cfg.listen, "listen", ":8080", "HTTP listen address")
	fs.DurationVar(&cfg.interval, "interval", 60*time.Second, "how often every service is checked")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return cfg, err
		}
		return cfg, fmt.Errorf("run with -h for usage")
	}
	if fs.NArg() > 0 {
		return cfg, fmt.Errorf("unexpected arguments %q", fs.Args())
	}
	if len(cfg.services) == 0 {
		return cfg, fmt.Errorf("at least one -service is required")
	}
	if err := triage.CheckUniqueServices(cfg.services); err != nil {
		return cfg, err
	}
	if cfg.interval < minInterval {
		return cfg, fmt.Errorf("-interval must be at least %s", minInterval)
	}
	if cfg.timeout == 0 {
		cfg.timeout = cfg.interval
	}
	if cfg.timeout > cfg.interval {
		return cfg, fmt.Errorf("-timeout must not exceed -interval")
	}
	if err := cfg.validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func serve(cfg serveConfig, log *slog.Logger) error {
	prom, engine, err := cfg.clients(func(q, w string) { log.Warn("prometheus warning", "warning", w, "query", q) })
	if err != nil {
		return err
	}
	names := make([]string, len(cfg.services))
	for i, s := range cfg.services {
		names[i] = s.Name
	}
	cycle := max(cfg.interval, time.Duration(len(names))*cfg.timeout)
	store := server.NewStore(names, staleAfter*cycle)
	runner := &server.Runner{
		Services: cfg.services,
		Store:    store,
		Interval: cfg.interval,
		Timeout:  cfg.timeout,
		Log:      log,
		Check: func(ctx context.Context, svc triage.Service, at time.Time) (triage.Result, error) {
			return triage.Check(ctx, prom, engine, cfg.rubric, svc.Metrics, triage.Params{At: at})
		},
	}

	ln, err := net.Listen("tcp", cfg.listen)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           server.Handler(store),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Serve(ln) }()
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		runner.Run(ctx)
	}()
	log.Info("serving", "listen", ln.Addr().String(), "services", names, "interval", cfg.interval.String(), "model", cfg.model)

	select {
	case <-ctx.Done():
		err = nil
	case err = <-srvErr:
	}
	stop()
	sctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if serr := srv.Shutdown(sctx); serr != nil && err == nil {
		err = serr
	}
	<-runDone
	log.Info("stopped")
	return err
}
