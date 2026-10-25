package main

import (
	"context"
	"errors"
	"goepay/internal/bootstrap"
	"goepay/internal/referralscheduler"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func run(ctx context.Context) error {
	switch os.Getenv("GOEPAY_REFERRAL_SCHEDULER_ENABLED") {
	case "", "false":
		slog.Info("daily referral scheduler disabled; no database or ledger work performed")
		return nil
	case "true":
	default:
		return errors.New("invalid daily referral scheduler enable flag")
	}
	pool, store, err := bootstrap.Connect(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	slog.Info("daily referral scheduler started; funded local entitlements only")
	return (referralscheduler.Runner{Enabled: true, Clock: pool, Store: store, Limit: 20, Interval: time.Minute}).Run(ctx)
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("daily referral scheduler startup or execution failed")
		os.Exit(1)
	}
}
