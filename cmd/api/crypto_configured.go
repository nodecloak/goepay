package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/cryptochain"
	"goepay/internal/cryptopay"
	"goepay/internal/server"
)

func cryptoFromEnvironment(pool *pgxpool.Pool, getenv func(string) string) (*cryptopay.Service, error) {
	raw := getenv("GOEPAY_CRYPTO_NETWORKS")
	if raw == "" {
		return nil, nil
	}
	var settings map[string]struct {
		Enabled  bool   `json:"enabled"`
		FeeBPS   int64  `json:"fee_bps"`
		Treasury string `json:"treasury_address"`
		Hot      string `json:"hot_address"`
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(&settings); e != nil {
		return nil, errors.New("invalid trusted USDT network configuration")
	}
	var extra any
	if d.Decode(&extra) != io.EOF || len(settings) == 0 {
		return nil, errors.New("invalid trusted USDT network configuration")
	}
	key, e := hex.DecodeString(getenv("GOEPAY_MASTER_KEY"))
	if e != nil || len(key) != 32 {
		return nil, errors.New("USDT requires a durable 32-byte master key")
	}
	h := hmac.New(sha256.New, key)
	h.Write([]byte("GOEPAY/crypto-token-key/v1"))
	svc := &cryptopay.Service{Pool: pool, TokenKey: h.Sum(nil), Networks: make(map[string]cryptopay.NetworkConfig), RequireChannels: true}
	for network, config := range settings {
		if network != "tron" && network != "ethereum" || config.FeeBPS < 0 || config.FeeBPS > 10000 {
			return nil, errors.New("unsupported USDT network or fee")
		}
		for _, address := range []string{config.Treasury, config.Hot} {
			if address != "" {
				canonical, e := cryptochain.NormalizeAddress(cryptochain.Chain(network), address)
				if e != nil || canonical != address {
					return nil, errors.New("invalid trusted USDT address")
				}
			}
		}
		svc.Networks[network] = cryptopay.NetworkConfig{Enabled: config.Enabled, FeeBPS: config.FeeBPS, TreasuryAddress: config.Treasury, HotAddress: config.Hot}
	}
	return svc, nil
}

func configureCryptoCompatibility(app *server.Server, getenv func(string) string) error {
	rate := getenv("GOEPAY_CRYPTO_CNY_PER_USDT")
	if rate == "" {
		app.CryptoCNYPerUSDT = ""
		app.CryptoQuoteValidUntil = time.Time{}
		app.CryptoDefaultNetwork = ""
		app.CryptoPublicOrigin = ""
		if origin, e := server.NormalizeCryptoPublicOrigin(getenv("GOEPAY_PUBLIC_ORIGIN")); e == nil {
			app.CryptoPublicOrigin = origin
		}
		return nil
	}
	if app.Crypto == nil {
		return errors.New("USDT quote requires enabled service")
	}
	if _, e := server.CryptoQuoteCNY(100, rate); e != nil {
		return errors.New("invalid trusted USDT exchange rate")
	}
	var validUntil time.Time
	if raw := getenv("GOEPAY_CRYPTO_CNY_QUOTE_VALID_UNTIL"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil || parsed.IsZero() {
			return errors.New("USDT quote deadline must be an RFC3339 timestamp with a time zone")
		}
		validUntil = parsed.UTC()
	}
	var network string
	if app.Crypto.RequireChannels {
		enabled := false
		for _, config := range app.Crypto.Networks {
			enabled = enabled || config.Enabled
		}
		if !enabled {
			return errors.New("no USDT channel network enabled")
		}
	} else {
		network = getenv("GOEPAY_CRYPTO_DEFAULT_NETWORK")
		if network == "" {
			network = "tron"
		}
		if config := app.Crypto.Networks[network]; !config.Enabled {
			return errors.New("default USDT network unavailable")
		}
	}
	origin, e := server.NormalizeCryptoPublicOrigin(getenv("GOEPAY_PUBLIC_ORIGIN"))
	if e != nil {
		return errors.New("quoted USDT requires a trusted public origin")
	}
	app.CryptoCNYPerUSDT = rate
	app.CryptoQuoteValidUntil = validUntil
	app.CryptoDefaultNetwork = network
	app.CryptoPublicOrigin = origin
	return nil
}
