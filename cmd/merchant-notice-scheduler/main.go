package main

import (
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"goepay/internal/bootstrap"
	notice "goepay/internal/merchantnotice"
)

func run(ctx context.Context) error {
	switch os.Getenv("GOEPAY_MERCHANT_NOTICE_SCHEDULER_ENABLED") {
	case "", "false":
		slog.Info("merchant notice scheduler disabled; no database or credentials loaded")
		return nil
	case "true":
	default:
		return errors.New("invalid merchant notice scheduler enable flag")
	}
	hour := 9
	if raw := os.Getenv("GOEPAY_MERCHANT_NOTICE_SEND_HOUR"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || value > 23 || strconv.Itoa(value) != raw {
			return errors.New("invalid merchant notice send hour")
		}
		hour = value
	}
	pool, payments, err := bootstrap.Connect(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	key, err := hex.DecodeString(os.Getenv("GOEPAY_MASTER_KEY"))
	if err != nil {
		return errors.New("invalid notice master key")
	}
	store, err := notice.NewStore(pool, key)
	if err != nil {
		return err
	}
	slog.Info("merchant notice scheduler started; generates source events only")
	return (notice.ScheduledRunner{Enabled: true, Store: store, Reader: payments, SendHour: hour, Interval: time.Minute}).Run(ctx)
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("merchant notice scheduler startup or execution failed")
		os.Exit(1)
	}
}
