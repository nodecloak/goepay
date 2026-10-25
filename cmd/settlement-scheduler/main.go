package main

import (
	"context"
	"errors"
	"goepay/internal/bootstrap"
	"goepay/internal/platform/jobs"
	"goepay/internal/settlementscheduler"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func run(ctx context.Context) error {
	switch os.Getenv("GOEPAY_SETTLEMENT_SCHEDULER_ENABLED") {
	case "", "false":
		slog.Info("settlement scheduler disabled; no database or generation work performed")
		return nil
	case "true":
	default:
		return errors.New("invalid settlement scheduler enable flag")
	}
	pool, store, err := bootstrap.Connect(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	slog.Info("settlement scheduler started; generates pending applications only")
	return (settlementscheduler.Runner{Scheduler: settlementscheduler.Scheduler{Store: store, Enabled: true}, Queue: jobs.Queue{Pool: pool}, Handler: settlementscheduler.Handler{Store: store}}).Run(ctx)
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("settlement scheduler startup or execution failed")
		os.Exit(1)
	}
}
