package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigRejectsUnknownFieldsAndMissingSecuritySettings(t *testing.T) {
	for _, raw := range []string{
		`{"listen":"127.0.0.1:9443","private_key":"do-not-accept-inline-secrets"}`,
		`{"listen":"127.0.0.1:9443"}`,
		`{"listen":"127.0.0.1:9443","server_certificate":"a","server_key":"b","client_ca":"c","allowed_client_sans":["payments-api"],"keys":{"treasury":"key"},"state_path":"journal","rules":[]} {}`,
	} {
		path := filepath.Join(t.TempDir(), "signer.json")
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadConfig(path); err == nil {
			t.Fatalf("insecure config accepted: %s", raw)
		}
	}
}

func TestLoadConfigAllowsOnlyFileKeyReferencesAndExplicitMTLS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "signer.json")
	raw := `{"listen":"127.0.0.1:9443","server_certificate":"server.crt","server_key":"server.key","client_ca":"client-ca.crt","allowed_client_sans":["payments-api"],"keys":{"treasury":"/signer-only/treasury.key"},"state_path":"/signer-only/journal.jsonl","rules":[{"chain":"ethereum","network":"mainnet","chain_id":"1","contract":"0xdac17f958d2ee523a2206206994597c13d831ec7","from":"0x7e5f4552091a69125d5dfcb7b8c2659029395bdf","key_id":"treasury","destinations":["0x2222222222222222222222222222222222222222"],"max_amount":"1000000","daily_limit":"1000000","max_daily_transactions":100,"max_daily_fee_atomic":"1000000000000000000","max_gas_limit":100000,"max_gas_price":"50000000000","max_nonce":100}]}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Keys["treasury"] != "/signer-only/treasury.key" || cfg.ClientCA != "client-ca.crt" {
		t.Fatal("key references or trust configuration lost")
	}
}
