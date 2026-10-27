package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"goepay/db"
	"goepay/internal/bootstrap"
	"goepay/internal/taskscheduler"
)

func run(ctx context.Context, once bool) error {
	switch os.Getenv("GOEPAY_TASK_SCHEDULER_ENABLED") {
	case "", "false":
		slog.Info("scheduled task runtime disabled; no database or credentials loaded")
		return nil
	case "true":
	default:
		return errors.New("invalid scheduled task enable flag")
	}
	pool, store, e := bootstrap.Connect(ctx)
	if e != nil {
		return e
	}
	defer pool.Close()
	if e = db.VerifySchema(ctx, pool); e != nil {
		return e
	}
	runner := taskscheduler.Runner{Enabled: true, Store: store, Limit: 20, Interval: time.Second}
	runner.AutoQueue, e = scheduledAutomaticQueueFlow(store)
	if e != nil {
		return e
	}
	if once {
		tick, e := runner.RunOnce(ctx)
		if e != nil {
			return e
		}
		slog.Info("scheduled task tick finished", "claimed", tick.Claimed, "completed", tick.Completed, "failed", tick.Failed)
		if tick.Failed != 0 {
			return errors.New("scheduled task business execution failed")
		}
		return nil
	}
	return runner.Run(ctx)
}
func main() {
	once := flag.Bool("once", false, "process one bounded local scheduler tick")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if e := run(ctx, *once); e != nil {
		slog.Error("scheduled task runtime startup or execution failed")
		os.Exit(1)
	}
}
