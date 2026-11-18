package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/cryptopay"
)

func scanListener(ctx context.Context, scanner *cryptopay.Scanner, network, fingerprint string) error {
	if fingerprint == "" {
		return scanner.Scan(ctx, network)
	}
	checkedAt, err := scanner.ScanForReadiness(ctx, network)
	if err != nil || checkedAt.IsZero() {
		return err
	}
	publishCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := publishListenerProof(publishCtx, scanner.Service.Pool, network, fingerprint, checkedAt); err != nil {
		// Collection cannot use a missing or stale proof. Keep reconciling
		// historical operations even if the new readiness write fails.
		slog.Warn("USDT listener readiness could not be recorded", "network", network)
	}
	return nil
}

func publishListenerProof(ctx context.Context, pool *pgxpool.Pool, network, fingerprint string, checkedAt time.Time) error {
	_, err := pool.Exec(ctx, `INSERT INTO crypto_listener_health(network,fingerprint,checked_at) VALUES($1,$2,$3)
		ON CONFLICT(network) DO UPDATE SET fingerprint=EXCLUDED.fingerprint,checked_at=EXCLUDED.checked_at
		WHERE crypto_listener_health.checked_at<EXCLUDED.checked_at`, network, fingerprint, checkedAt)
	return err
}
