package scheduler

import (
	"context"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
)

func init() {
	caddy.RegisterModule(Scheduler{})
	httpcaddyfile.RegisterGlobalOption("pogo_scheduler", parseGlobalOption)
}

type Scheduler struct {
	Command []string       `json:"command,omitempty"`
	Dir     string         `json:"dir,omitempty"`
	Timeout caddy.Duration `json:"timeout,omitempty"`

	logger *slog.Logger
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func (Scheduler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "pogo_scheduler",
		New: func() caddy.Module { return new(Scheduler) },
	}
}

func (s *Scheduler) Provision(ctx caddy.Context) error {
	s.logger = ctx.Slogger()

	if len(s.Command) == 0 {
		s.Command = []string{"php", "artisan", "schedule:run"}
	}
	if s.Timeout <= 0 {
		s.Timeout = caddy.Duration(5 * time.Minute)
	}

	return nil
}

func (s *Scheduler) Start() error {
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.wg.Add(1)

	go s.loop()

	s.logger.Info("scheduler started",
		slog.Any("command", s.Command),
		slog.String("dir", s.Dir),
	)
	return nil
}

func (s *Scheduler) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	s.logger.Info("scheduler stopped")
	return nil
}

func (s *Scheduler) loop() {
	defer s.wg.Done()

	for {
		now := time.Now()
		nextMinute := now.Truncate(time.Minute).Add(time.Minute)
		sleepDuration := nextMinute.Sub(now)

		select {
		case <-s.ctx.Done():
			return
		case <-time.After(sleepDuration):
			go s.exec()
		}
	}
}

func (s *Scheduler) exec() {
	ctx, cancel := context.WithTimeout(s.ctx, time.Duration(s.Timeout))
	defer cancel()

	cmd := exec.CommandContext(ctx, s.Command[0], s.Command[1:]...)
	if s.Dir != "" {
		cmd.Dir = s.Dir
	}

	out, err := cmd.CombinedOutput()

	if err != nil {
		s.logger.Error("schedule run failed",
			slog.String("output", string(out)),
			slog.Any("error", err),
		)
		return
	}

	if len(out) > 0 {
		s.logger.Info("schedule run output", slog.String("output", string(out)))
	}
}

// UnmarshalCaddyfile sets up the module from Caddyfile tokens.
// Syntax:
//
//	pogo_scheduler {
//	    command php artisan schedule:run
//	    dir /var/www/html
//	    timeout 2m
//	}
func (s *Scheduler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for d.NextBlock(0) {
			switch d.Val() {
			case "command":
				s.Command = d.RemainingArgs()
				if len(s.Command) == 0 {
					return d.ArgErr()
				}
			case "dir":
				if !d.NextArg() {
					return d.ArgErr()
				}
				s.Dir = d.Val()
			case "timeout":
				if !d.NextArg() {
					return d.ArgErr()
				}
				dur, err := caddy.ParseDuration(d.Val())
				if err != nil {
					return d.Errf("invalid duration %s: %v", d.Val(), err)
				}
				s.Timeout = caddy.Duration(dur)
			default:
				return d.Errf("unrecognized subdirective: %s", d.Val())
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
	_ caddy.Module          = (*Scheduler)(nil)
	_ caddy.App             = (*Scheduler)(nil)
	_ caddy.Provisioner     = (*Scheduler)(nil)
	_ caddyfile.Unmarshaler = (*Scheduler)(nil)
)
