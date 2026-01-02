package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"
	"unsafe"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/dunglas/frankenphp"
	frankenphpCaddy "github.com/dunglas/frankenphp/caddy"
)

var (
	globalDispatcher   *schedulerDispatcher
	globalDispatcherMu sync.RWMutex
)

func init() {
	caddy.RegisterModule(Scheduler{})
	httpcaddyfile.RegisterGlobalOption("pogo_scheduler", parseGlobalOption)
}

type Scheduler struct {
	Worker     string `json:"worker,omitempty"`
	NumThreads int    `json:"numthreads,omitempty"`
	Name       string `json:"name,omitempty"`

	dispatcher *schedulerDispatcher
}

func (Scheduler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "pogo_scheduler",
		New: func() caddy.Module { return new(Scheduler) },
	}
}

func (s *Scheduler) Provision(ctx caddy.Context) error {
	if s.Name == "" {
		s.Name = "pogo-scheduler"
	}

	if s.Worker == "" {
		return fmt.Errorf("scheduler worker path is required")
	}

	if s.NumThreads > 1 {
		slog.Warn("pogo_scheduler: num_threads > 1 is not recommended for scheduler, enforcing 1")
		s.NumThreads = 1
	} else if s.NumThreads <= 0 {
		s.NumThreads = 1
	}

	// Use the Caddy integration package to register workers
	w := frankenphpCaddy.RegisterWorkers(s.Name, s.Worker, s.NumThreads)
	s.dispatcher = newSchedulerDispatcher(w, ctx.Slogger())

	globalDispatcherMu.Lock()
	if globalDispatcher != nil {
		go globalDispatcher.shutdown()
	}
	globalDispatcher = s.dispatcher
	globalDispatcherMu.Unlock()

	return nil
}

func (s *Scheduler) Cleanup() error {
	if s.dispatcher != nil {
		s.dispatcher.shutdown()
	}

	globalDispatcherMu.Lock()
	if globalDispatcher == s.dispatcher {
		globalDispatcher = nil
	}
	globalDispatcherMu.Unlock()

	return nil
}

func (s *Scheduler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for d.NextBlock(0) {
			switch d.Val() {
			case "worker":
				if !d.NextArg() {
					return d.ArgErr()
				}
				s.Worker = d.Val()
			case "name":
				if !d.NextArg() {
					return d.ArgErr()
				}
				s.Name = d.Val()
			case "num_threads":
				if !d.NextArg() {
					return d.ArgErr()
				}
				t, err := strconv.Atoi(d.Val())
				if err != nil {
					return d.Errf("failed to parse num_threads: %v", err)
				}
				s.NumThreads = t
			default:
				return d.Errf(`unrecognized subdirective "%s"`, d.Val())
			}
		}
	}

	return nil
}

func parseGlobalOption(d *caddyfile.Dispenser, _ any) (any, error) {
	app := &Scheduler{}
	if err := app.UnmarshalCaddyfile(d); err != nil {
		return nil, err
	}

	return httpcaddyfile.App{
		Name:  "pogo_scheduler",
		Value: caddyconfig.JSON(app, nil),
	}, nil
}

var (
	_ caddy.Module       = (*Scheduler)(nil)
	_ caddy.Provisioner  = (*Scheduler)(nil)
	_ caddy.CleanerUpper = (*Scheduler)(nil)
)

type schedulerDispatcher struct {
	worker frankenphp.Workers
	logger *slog.Logger
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	once   sync.Once
}

func newSchedulerDispatcher(w frankenphp.Workers, l *slog.Logger) *schedulerDispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	d := &schedulerDispatcher{
		worker: w,
		logger: l,
		ctx:    ctx,
		cancel: cancel,
	}

	d.wg.Add(1)
	go d.loop()

	return d
}

func (d *schedulerDispatcher) loop() {
	defer d.wg.Done()

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	// Align to the next minute start
	now := time.Now()
	nextMinute := now.Truncate(time.Minute).Add(time.Minute)
	sleepDuration := nextMinute.Sub(now)

	d.logger.Info("scheduler: aligned to next minute", slog.Duration("sleep", sleepDuration))
	time.Sleep(sleepDuration)

	// Trigger immediately after alignment (the :00 second mark)
	d.trigger()

	for {
		select {
		case <-d.ctx.Done():
			d.logger.Info("scheduler: shutting down ticker loop")
			return
		case <-ticker.C:
			d.trigger()
		}
	}
}

func (d *schedulerDispatcher) trigger() {
	d.logger.Debug("scheduler: tick received, requesting worker")

	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second) // Timeout slightly longer than the interval
	defer cancel()

	// SendMessage is used for headless worker interaction.
	// We pass nil for the body (unsafe.Pointer) as we are just sending a signal.
	_, err := d.worker.SendMessage(ctx, unsafe.Pointer(nil), nil)

	if err != nil {
		d.logger.Error("scheduler: failed to request worker", slog.Any("error", err))
	}
}

func (d *schedulerDispatcher) shutdown() {
	d.once.Do(func() {
		d.cancel()
		d.wg.Wait()
		d.logger.Info("scheduler: dispatcher shut down")
	})
}
