package bootstrap

import (
	"context"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/payment"
	"goepay/internal/riskgeo"
	"os"
)

func Connect(ctx context.Context) (*pgxpool.Pool, *payment.Store, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, nil, errors.New("DATABASE_URL required")
	}
	key, e := hex.DecodeString(os.Getenv("GOEPAY_MASTER_KEY"))
	if e != nil || len(key) != 32 {
		return nil, nil, errors.New("GOEPAY_MASTER_KEY must be 64 hex characters")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		return nil, nil, e
	}
	cfg.MaxConns = 16
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return nil, nil, e
	}
	if e = pool.Ping(ctx); e != nil {
		pool.Close()
		return nil, nil, e
	}
	store, e := payment.NewWithKey(pool, key)
	if e != nil {
		pool.Close()
		return nil, nil, e
	}
	if v4, v6 := os.Getenv("GOEPAY_RISK_GEO_IPV4_XDB"), os.Getenv("GOEPAY_RISK_GEO_IPV6_XDB"); v4 != "" || v6 != "" {
		store.CoreRiskGeo = riskgeo.XDB{IPv4Path: v4, IPv6Path: v6}
	}
	return pool, store, nil
}
