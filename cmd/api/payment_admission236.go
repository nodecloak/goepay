package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/payment"
	"goepay/internal/server"
)

// This host command never enters the regular API bootstrap (search rebuilding
// and default-style seeding write). Its own database sessions are read-only.
func runPaymentInspection236(ctx context.Context, args []string, w io.Writer) error {
	if _, err := inspectionMerchantIDs236(args); err != nil {
		return err
	}
	key, err := hex.DecodeString(os.Getenv("GOEPAY_MASTER_KEY"))
	if err != nil || len(key) != 32 {
		return errors.New("inspection configuration unavailable")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("inspection configuration unavailable")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return errors.New("inspection configuration unavailable")
	}
	config.MaxConns = 2
	config.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	config.ConnConfig.RuntimeParams["statement_timeout"] = "10000"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return errors.New("inspection database unavailable")
	}
	defer pool.Close()
	store, err := payment.NewWithKey(pool, key)
	if err != nil {
		return errors.New("inspection protection unavailable")
	}
	app := &server.Server{Pool: pool, Store: store, Sandbox: os.Getenv("GOEPAY_SANDBOX") == "true"}
	if err = configureProviders(app, os.Getenv("GOEPAY_PROVIDER_MODE"), key, os.Getenv("GOEPAY_PUBLIC_ORIGIN")); err != nil {
		return errors.New("inspection providers unavailable")
	}
	return inspectPaymentAdmission236(ctx, app, args, w)
}

func inspectionMerchantIDs236(args []string) ([]int64, error) {
	if len(args) != 2 || args[0] != "check-payment-admission" || len(args[1]) > 1024 {
		return nil, errors.New("usage: api check-payment-admission merchant_id[,merchant_id]")
	}
	parts := strings.Split(args[1], ",")
	if len(parts) > 50 {
		return nil, errors.New("too many merchant IDs")
	}
	ids := []int64{}
	seen := map[int64]bool{}
	for _, text := range parts {
		id, err := strconv.ParseInt(text, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != text || seen[id] {
			return nil, errors.New("invalid merchant ID")
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

func inspectPaymentAdmission236(ctx context.Context, app *server.Server, args []string, w io.Writer) error {
	ids, err := inspectionMerchantIDs236(args)
	if err != nil {
		return err
	}
	rows := []server.PaymentAdmissionReport236{}
	for _, id := range ids {
		r, err := app.InspectPaymentAdmission236(ctx, id)
		if err != nil {
			return errors.New("payment admission inspection unavailable")
		}
		rows = append(rows, r)
	}
	return json.NewEncoder(w).Encode(map[string]any{"read_only": true, "provider_calls": 0, "merchants": rows})
}
