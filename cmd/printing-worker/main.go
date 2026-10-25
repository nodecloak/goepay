package main

import (
	"context"
	"errors"
	"goepay/internal/bootstrap"
	"goepay/internal/printing"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func printingWorkerMode(local, external, sandbox string) (string, error) {
	flag := func(v string) (bool, error) {
		switch v {
		case "", "false":
			return false, nil
		case "true":
			return true, nil
		default:
			return false, errors.New("invalid printing enable flag")
		}
	}
	l, e := flag(local)
	if e != nil {
		return "", e
	}
	x, e := flag(external)
	if e != nil {
		return "", e
	}
	if l && x {
		return "", errors.New("printing execution modes are mutually exclusive")
	}
	if l {
		if sandbox != "true" {
			return "", errors.New("local printing requires explicit sandbox mode")
		}
		return "local", nil
	}
	if x {
		if sandbox != "false" {
			return "", errors.New("external printing requires explicit non-sandbox mode")
		}
		return "external", nil
	}
	return "", nil
}

func run(ctx context.Context) error {
	mode, e := printingWorkerMode(os.Getenv("GOEPAY_LOCAL_PRINTING_FAKE_ENABLED"), os.Getenv("GOEPAY_PRINTING_EXTERNAL_ENABLED"), os.Getenv("GOEPAY_SANDBOX"))
	if e != nil {
		return e
	}
	if mode == "" {
		slog.Info("printing worker disabled; no database or gateway work performed")
		return nil
	}
	pool, source, e := bootstrap.Connect(ctx)
	if e != nil {
		return e
	}
	defer pool.Close()
	s, e := printing.New(pool, source)
	if e != nil {
		return e
	}
	for ctx.Err() == nil {
		var worked bool
		if mode == "external" {
			worked, e = s.RunExcelsecuOne(ctx, true)
		} else {
			worked, e = s.RunOne(ctx, true, true)
		}
		if e != nil {
			slog.Error("print task unresolved; no automatic resend")
		}
		if !worked || e != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
			}
		}
	}
	return nil
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if e := run(ctx); e != nil {
		slog.Error("printing worker rejected startup")
		os.Exit(1)
	}
}
