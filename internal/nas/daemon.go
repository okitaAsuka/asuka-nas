package nas

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// Daemon 仅包含单机 source 和 job 两条循环，没有通用调度平台或 HTTP listener。
type Daemon struct {
	Config Config
	Engine *Engine
	Source *SourceClient
	Logger *slog.Logger
}

// PollSources 各订阅先预约，再请求；某订阅失败不阻塞下一订阅。
func (d *Daemon) PollSources(ctx context.Context, now time.Time) error {
	var failures []error
	for _, sub := range d.Config.Subscriptions {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		due, err := d.Engine.Store.ClaimSource(ctx, sub.ID, now.Unix(), d.Config.PollSeconds)
		if err != nil {
			return errors.New("source_store_failed")
		}
		if !due {
			continue
		}
		pollErr := d.Source.Poll(ctx, sub, d.Engine.Store, d.Config.SeedDays)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = d.Engine.Store.FinishSource(ctx, sub.ID, now.Unix(), d.Config.PollSeconds, pollErr != nil); err != nil {
			return errors.New("source_store_failed")
		}
		if pollErr != nil {
			failures = append(failures, pollErr)
		}
	}
	return errors.Join(failures...)
}

// Run 响应 context 取消并等待在途 HTTP/哈希结束；失败日志不包含 err 原文。
func (d *Daemon) Run(ctx context.Context) error {
	d.Config.defaults()
	if err := d.Config.Validate(); err != nil {
		return err
	}
	if d.Logger == nil {
		d.Logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	var wg sync.WaitGroup
	loop := func(code string, step func(context.Context, time.Time) error) {
		defer wg.Done()
		ticker := time.NewTicker(time.Duration(d.Config.TickSeconds) * time.Second)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			if err := step(ctx, time.Now()); err != nil && ctx.Err() == nil {
				d.Logger.Warn(code)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}
	wg.Add(2)
	go loop("source_cycle_failed", d.PollSources)
	go loop("job_cycle_failed", func(ctx context.Context, now time.Time) error {
		return d.Engine.RetryTick(ctx, now, d.Config.TickSeconds)
	})
	wg.Wait()
	return nil
}

// AcquireInstance 防止两个 daemon 同时执行同一 SQLite 的外部副作用。
func AcquireInstance(database string) (*os.File, error) {
	fd, err := unix.Open(database+".lock", unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, errors.New("instance_lock_failed")
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		return nil, errors.New("instance_already_running")
	}
	return os.NewFile(uintptr(fd), "instance-lock"), nil
}
