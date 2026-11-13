package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/payment"
	"goepay/internal/server"
)

func TestCryptoChannelStartupRejectsImportedCNYIdentityCollisions(t *testing.T) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var nonce [12]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	schema := "crypto_channel_boot_" + hex.EncodeToString(nonce[:])
	if _, err = pool.Exec(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	scoped, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		scoped.Close()
		if _, e := pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); e != nil {
			t.Error(e)
		}
	}()
	if _, err = scoped.Exec(t.Context(), `CREATE TABLE crypto_channels(id bigint PRIMARY KEY,archived boolean);INSERT INTO crypto_channels VALUES(81,false),(82,true)`); err != nil {
		t.Fatal(err)
	}
	app := &server.Server{Pool: scoped, ProviderFlow: &payment.ProviderFlow{Providers: map[int64]adapter.Provider{80: nil}}}
	if err = validateCryptoChannelNamespace(t.Context(), app); err != nil {
		t.Fatal("unrelated channel rejected", err)
	}
	for _, id := range []int64{81, 82} {
		app.ProviderFlow.Providers = map[int64]adapter.Provider{id: nil}
		if err = validateCryptoChannelNamespace(t.Context(), app); err == nil {
			t.Fatal("imported provider reused crypto identity", id)
		}
	}
	app.ProviderFlow.Providers = nil
	app.ProviderFlow.ParentFactories = map[int64]adapter.ParentAccountFactory{82: nil}
	if err = validateCryptoChannelNamespace(t.Context(), app); err == nil {
		t.Fatal("factory reused archived crypto identity")
	}
	app.ProviderFlow = nil
	if err = validateCryptoChannelNamespace(t.Context(), app); err != nil {
		t.Fatal("unconfigured CNY providers blocked crypto catalog", err)
	}
}
