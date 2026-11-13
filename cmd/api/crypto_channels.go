package main

import (
	"context"
	"errors"

	"goepay/internal/server"
)

// Imported process-local CNY identities must not reuse persisted custody
// channel IDs, including archived IDs referenced by historical invoices.
func validateCryptoChannelNamespace(ctx context.Context, app *server.Server) error {
	if app == nil || app.Pool == nil {
		return errors.New("payment channel namespace unavailable")
	}
	reserved := map[int64]bool{}
	if app.ProviderFlow != nil {
		for id := range app.ProviderFlow.Providers {
			reserved[id] = true
		}
		for id := range app.ProviderFlow.ParentFactories {
			reserved[id] = true
		}
	}
	rows, err := app.Pool.Query(ctx, `SELECT id FROM crypto_channels ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return err
		}
		if reserved[id] {
			return errors.New("imported payment channel collides with custody channel identity")
		}
	}
	return rows.Err()
}
