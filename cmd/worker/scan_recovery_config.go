package main

import (
	"encoding/hex"
	"errors"
	"os"

	"goepay/internal/channelconfig"
	"goepay/internal/payment"
)

var errScanRecoveryConfiguration = errors.New("invalid explicit original scan recovery configuration")

// A distinct flow is never passed to the ordinary read-only reconciler.
// Configured recovery needs all three opt-ins and an explicit merchant map.
func configuredScanRecoveryFlow(store *payment.Store) (*payment.ProviderFlow, error) {
	switch os.Getenv("GOEPAY_PROVIDER_MODE") {
	case "", "simulation":
		return nil, nil
	case "configured":
	default:
		return nil, errScanRecoveryConfiguration
	}
	switch os.Getenv("GOEPAY_ENABLE_SCAN_RECOVERY") {
	case "", "false":
		return nil, nil
	case "true":
	default:
		return nil, errScanRecoveryConfiguration
	}
	if os.Getenv("GOEPAY_ENABLE_PROVIDER_WRITES") != "true" || os.Getenv("GOEPAY_ENABLE_PROVIDER_QUERIES") != "true" {
		return nil, errScanRecoveryConfiguration
	}
	path := os.Getenv("GOEPAY_CHANNEL_CONFIG_FILE")
	master, e := hex.DecodeString(os.Getenv("GOEPAY_MASTER_KEY"))
	if path == "" || e != nil || len(master) != 32 {
		return nil, errScanRecoveryConfiguration
	}
	imp, e := channelconfig.Load(path, master)
	if e != nil {
		return nil, errScanRecoveryConfiguration
	}
	providers, e := channelconfig.BuildWithOptions(imp, channelconfig.BuildOptions{AllowLive: true, ScanRecovery: true})
	if e != nil || len(providers) == 0 {
		return nil, errScanRecoveryConfiguration
	}
	mapping, e := channelconfig.MerchantAllowlist(os.Getenv("GOEPAY_MERCHANT_CHANNELS"), providers)
	if e != nil || len(mapping) == 0 {
		return nil, errScanRecoveryConfiguration
	}
	return &payment.ProviderFlow{Store: store, Providers: providers, MerchantChannels: mapping, AllowLive: true, ScanRecoveryEnabled: true}, nil
}
