package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
)

const (
	OverlapAllow = "allow"
	OverlapSkip  = "skip"
)

func init() {
	caddy.RegisterModule(Scheduler{})
	httpcaddyfile.RegisterGlobalOption("pogo_scheduler", parseGlobalOption)
}

type Scheduler struct {
	Command       []string       `json:"command,omitempty"`
	Dir           string         `json:"dir,omitempty"`
	Timeout       caddy.Duration `json:"timeout,omitempty"`
	Overlap       string         `json:"overlap,omitempty"`
	ShutdownGrace caddy.Duration `json:"shutdown_grace,omitempty"`

	logger       *slog.Logger
	scheduleCtx  context.Context
	cancelLoop   context.CancelFunc
	commandCtx   context.Context
	cancelCmds   context.CancelFunc
	mu           sync.Mutex
	stopping     bool
	active       bool
	wg           sync.WaitGroup
	cancelCmdsMu sync.Mutex
}

func (Scheduler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "pogo_scheduler",
		New: func() caddy.Module { return new(Scheduler) },
	}
}

func (s *Scheduler) Provision(ctx caddy.Context) error {
	return s.setDefaults(ctx.Slogger())
}

func (s *Scheduler) setDefaults(logger *slog.Logger) error {
	if logger != nil {
		s.logger = logger
	}
	if s.logger == nil {
		s.logger = slog.Default()
	}
	if len(s.Command) == 0 {
		s.Command = []string{"php", "artisan", "schedule:run"}
	}
	if s.Timeout <= 0 {
		s.Timeout = caddy.Duration(5 * time.Minute)
	}
	if s.Overlap == "" {
		s.Overlap = OverlapSkip
	}
	if s.Overlap != OverlapSkip && s.Overlap != OverlapAllow {
		return fmt.Errorf("invalid overlap %q: expected %q or %q", s.Overlap, OverlapSkip, OverlapAllow)
	}
	if s.ShutdownGrace <= 0 {
		s.ShutdownGrace = caddy.Duration(30 * time.Second)
	}

	return nil
}

func (s *Scheduler) Start() error {
	if err := s.setDefaults(nil); err != nil {
		return err
	}

	s.scheduleCtx, s.cancelLoop = context.WithCancel(context.Background())
	s.commandCtx, s.cancelCmds = context.WithCancel(context.Background())

	s.mu.Lock()
	s.stopping = false
	s.active = false
	s.wg.Add(1)
	s.mu.Unlock()

	go s.loop()

	s.logger.Info("scheduler started",
		slog.Any("command", s.Command),
		slog.String("dir", s.Dir),
		slog.String("overlap", s.Overlap),
	)
	return nil
}

func (s *Scheduler) Stop() error {
	s.mu.Lock()
	s.stopping = true
	if s.cancelLoop != nil {
		s.cancelLoop()
	}
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	grace := time.Duration(s.ShutdownGrace)
	timer := time.NewTimer(grace)
	select {
	case <-done:
		timer.Stop()
	case <-timer.C:
		s.logger.Warn("scheduler shutdown grace elapsed, cancelling active commands",
			slog.Duration("shutdown_grace", grace),
		)
		s.cancelCommands()
		<-done
	}

	s.logger.Info("scheduler stopped")
	return nil
}

func (s *Scheduler) loop() {
	defer s.wg.Done()

	for {
		now := time.Now()
		nextMinute := now.Truncate(time.Minute).Add(time.Minute)
		sleepDuration := nextMinute.Sub(now)
		timer := time.NewTimer(sleepDuration)

		select {
		case <-s.scheduleCtx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.logger.Info("scheduler tick", slog.Time("scheduled_at", nextMinute))
			s.startExec()
		}
	}
}

func (s *Scheduler) startExec() {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return
	}
	if s.Overlap == OverlapSkip && s.active {
		s.mu.Unlock()
		s.logger.Warn("scheduler skipped: previous run still active")
		return
	}
	if s.Overlap == OverlapSkip {
		s.active = true
	}
	s.wg.Add(1)
	s.mu.Unlock()

	go s.exec()
}

func (s *Scheduler) exec() {
	defer s.wg.Done()
	if s.Overlap == OverlapSkip {
		defer func() {
			s.mu.Lock()
			s.active = false
			s.mu.Unlock()
		}()
	}

	started := time.Now()
	s.logger.Info("scheduler command started",
		slog.Any("command", s.Command),
		slog.String("dir", s.Dir),
	)

	ctx, cancel := context.WithTimeout(s.commandCtx, time.Duration(s.Timeout))
	defer cancel()

	cmd := exec.CommandContext(ctx, s.Command[0], s.Command[1:]...)
	if s.Dir != "" {
		cmd.Dir = s.Dir
	}

	out, err := cmd.CombinedOutput()
	duration := time.Since(started)

	if err != nil {
		attrs := []any{
			slog.String("output", string(out)),
			slog.Any("error", err),
			slog.Duration("duration", duration),
		}
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			s.logger.Error("scheduler command timed out", attrs...)
		case errors.Is(ctx.Err(), context.Canceled):
			s.logger.Warn("scheduler command cancelled", attrs...)
		default:
			s.logger.Error("scheduler command failed", attrs...)
		}
		return
	}

	if len(out) > 0 {
		s.logger.Info("scheduler command output", slog.String("output", string(out)))
	}
	s.logger.Info("scheduler command finished", slog.Duration("duration", duration))
}

func (s *Scheduler) cancelCommands() {
	s.cancelCmdsMu.Lock()
	defer s.cancelCmdsMu.Unlock()
	if s.cancelCmds != nil {
		s.cancelCmds()
	}
}

// UnmarshalCaddyfile sets up the module from Caddyfile tokens.
// Syntax:
//
//	pogo_scheduler {
//	    command php artisan schedule:run
//	    dir /var/www/html
//	    timeout 2m
//	    overlap skip
//	    shutdown_grace 30s
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
			case "overlap":
				if !d.NextArg() {
					return d.ArgErr()
				}
				s.Overlap = d.Val()
				if s.Overlap != OverlapSkip && s.Overlap != OverlapAllow {
					return d.Errf("invalid overlap %q: expected %q or %q", s.Overlap, OverlapSkip, OverlapAllow)
				}
			case "shutdown_grace":
				if !d.NextArg() {
					return d.ArgErr()
				}
				dur, err := caddy.ParseDuration(d.Val())
				if err != nil {
					return d.Errf("invalid duration %s: %v", d.Val(), err)
				}
				s.ShutdownGrace = caddy.Duration(dur)
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
