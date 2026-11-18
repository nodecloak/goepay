package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"goepay/internal/cryptopay"
	"goepay/internal/server"
)

func TestCryptoCompatibilityEthereumOnlyUsesChannelRouting(t *testing.T) {
	values := map[string]string{
		"GOEPAY_CRYPTO_NETWORKS":              `{"ethereum":{"enabled":true}}`,
		"GOEPAY_MASTER_KEY":                   strings.Repeat("01", 32),
		"GOEPAY_CRYPTO_CNY_PER_USDT":          "7.2",
		"GOEPAY_CRYPTO_CNY_QUOTE_VALID_UNTIL": "2026-11-18T12:00:00+08:00",
		"GOEPAY_PUBLIC_ORIGIN":                "https://payments.example",
	}
	getenv := func(key string) string { return values[key] }
	svc, err := cryptoFromEnvironment(nil, getenv)
	if err != nil {
		t.Fatal(err)
	}
	for _, legacyDefault := range []string{"", "tron"} {
		values["GOEPAY_CRYPTO_DEFAULT_NETWORK"] = legacyDefault
		app := &server.Server{Crypto: svc}
		if err = configureCryptoCompatibility(app, getenv); err != nil {
			t.Fatalf("Ethereum-only channel routing rejected at startup: %v", err)
		}
		if app.CryptoCNYPerUSDT != "7.2" || app.CryptoPublicOrigin != "https://payments.example" || app.CryptoDefaultNetwork != "" {
			t.Fatal("invalid compatibility quote configuration")
		}
		if got := app.CryptoQuoteValidUntil.Format(time.RFC3339); got != "2026-11-18T04:00:00Z" {
			t.Fatal("absolute quote deadline was not normalized", got)
		}
	}
	svc.Networks["ethereum"] = cryptopay.NetworkConfig{Enabled: false}
	if err = configureCryptoCompatibility(&server.Server{Crypto: svc}, getenv); err == nil {
		t.Fatal("accepted CNY quotes with all channel networks closed")
	}
}

func TestCryptoConfigurationRequiresExplicitNetworksAndStableSeparatedKey(t *testing.T) {
	get := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	if svc, e := cryptoFromEnvironment(nil, get(nil)); e != nil || svc != nil {
		t.Fatal("USDT unexpectedly enabled", e)
	}
	values := map[string]string{"GOEPAY_CRYPTO_NETWORKS": `{"tron":{"enabled":true,"fee_bps":50},"ethereum":{"enabled":false,"fee_bps":80}}`, "GOEPAY_MASTER_KEY": strings.Repeat("01", 32)}
	a, e := cryptoFromEnvironment(nil, get(values))
	if e != nil {
		t.Fatal(e)
	}
	b, e := cryptoFromEnvironment(nil, get(values))
	if e != nil {
		t.Fatal(e)
	}
	if a == nil || !a.RequireChannels || !a.ManageNetworks || !a.Networks["tron"].Enabled || !a.Networks["ethereum"].Enabled || a.Networks["tron"].FeeBPS != 50 || len(a.TokenKey) != 32 || !bytes.Equal(a.TokenKey, b.TokenKey) || bytes.Equal(a.TokenKey, bytes.Repeat([]byte{1}, 32)) {
		t.Fatal("unusable or unseparated USDT configuration")
	}
	for _, raw := range []string{`{"tron":{"enabled":true,"fee_bps":10001}}`, `{"bsc":{"enabled":true}}`, `{"tron":{"enabled":true,"private_key":"no"}}`, `{"tron":{"enabled":true}} {}`} {
		values["GOEPAY_CRYPTO_NETWORKS"] = raw
		if _, e = cryptoFromEnvironment(nil, get(values)); e == nil {
			t.Fatal("accepted invalid network settings", raw)
		}
	}
	values["GOEPAY_CRYPTO_NETWORKS"] = `{"tron":{"enabled":true}}`
	values["GOEPAY_MASTER_KEY"] = ""
	if _, e = cryptoFromEnvironment(nil, get(values)); e == nil {
		t.Fatal("enabled checkout with no durable key")
	}
}

func TestCryptoCompatibilityRequiresTrustedOriginOnlyForCNYQuotes(t *testing.T) {
	for _, origin := range []string{"", "http://payments.example", "https://user:secret@payments.example", "https://user%40other.example@payments.example", "https://payments.example/subpath", "https://payments.example/%2F", "https://payments.example/?query=1", "https://payments.example/#fragment", "https://payments.example/#", "https://payments.example?", "//payments.example", "https://payments.example:99999"} {
		t.Run(origin, func(t *testing.T) {
			app := &server.Server{Crypto: &cryptopay.Service{Networks: map[string]cryptopay.NetworkConfig{"tron": {Enabled: true}}}}
			get := func(key string) string {
				switch key {
				case "GOEPAY_CRYPTO_CNY_PER_USDT":
					return "7.2"
				case "GOEPAY_PUBLIC_ORIGIN":
					return origin
				}
				return ""
			}
			if e := configureCryptoCompatibility(app, get); e == nil {
				t.Fatal("quoted USDT accepted missing or untrusted public origin", origin)
			}
		})
	}
	for _, origin := range []string{"https://payments.example", "https://payments.example/", "http://127.0.0.1:8187", "http://localhost:8187", "http://[::1]:8187"} {
		app := &server.Server{Crypto: &cryptopay.Service{Networks: map[string]cryptopay.NetworkConfig{"tron": {Enabled: true}}}}
		get := func(key string) string {
			if key == "GOEPAY_CRYPTO_CNY_PER_USDT" {
				return "7.2"
			}
			if key == "GOEPAY_PUBLIC_ORIGIN" {
				return origin
			}
			return ""
		}
		if e := configureCryptoCompatibility(app, get); e != nil {
			t.Fatal("trusted public origin rejected", origin, e)
		}
		if app.CryptoPublicOrigin != strings.TrimSuffix(origin, "/") {
			t.Fatal("public origin was not normalized", app.CryptoPublicOrigin)
		}
	}
	if e := configureCryptoCompatibility(&server.Server{}, func(string) string { return "" }); e != nil {
		t.Fatal("native service acquired a compatibility-only origin requirement", e)
	}
}

func TestCryptoCompatibilityRetainsTrustedOriginWhenNewQuotesAreDisabled(t *testing.T) {
	app := &server.Server{Crypto: &cryptopay.Service{Networks: map[string]cryptopay.NetworkConfig{"tron": {Enabled: true}}}}
	if e := configureCryptoCompatibility(app, func(key string) string {
		if key == "GOEPAY_PUBLIC_ORIGIN" {
			return "https://payments.example/"
		}
		return ""
	}); e != nil {
		t.Fatal(e)
	}
	if app.CryptoPublicOrigin != "https://payments.example" || app.CryptoCNYPerUSDT != "" {
		t.Fatal("disabling new quotes lost the origin needed to replay existing invoices", app.CryptoPublicOrigin)
	}
}

func TestCryptoCompatibilityRejectsMalformedTrustedQuoteDeadline(t *testing.T) {
	values := map[string]string{
		"GOEPAY_CRYPTO_CNY_PER_USDT":          "7.2",
		"GOEPAY_CRYPTO_CNY_QUOTE_VALID_UNTIL": "2026-11-18T12:00:00",
		"GOEPAY_PUBLIC_ORIGIN":                "https://payments.example",
	}
	app := &server.Server{Crypto: &cryptopay.Service{Networks: map[string]cryptopay.NetworkConfig{"tron": {Enabled: true}}}}
	if err := configureCryptoCompatibility(app, func(key string) string { return values[key] }); err == nil {
		t.Fatal("accepted quote deadline without a time zone")
	}
}
