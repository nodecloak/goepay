package main

import (
	"context"
	"encoding/hex"
	"errors"
	adapter "goepay/internal/adapters/payment"
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
	var parentAccounts []adapter.ParentStartupAccount
	if store != nil {
		parentAccounts, e = store.ParentStartupAccounts(context.Background())
		if e != nil {
			return nil, errScanRecoveryConfiguration
		}
	}
	startup, e := channelconfig.PrepareParentStartup(imp, parentAccounts)
	if e != nil {
		return nil, errScanRecoveryConfiguration
	}
	imp = startup.Import
	providers, e := startup.BuildProviders(channelconfig.BuildOptions{AllowLive: true, ScanRecovery: true})
	if e != nil || !startup.HasRegisteredSources() {
		return nil, errScanRecoveryConfiguration
	}
	mapping, e := startup.MerchantAllowlist(os.Getenv("GOEPAY_MERCHANT_CHANNELS"))
	if e != nil || len(mapping) == 0 {
		return nil, errScanRecoveryConfiguration
	}
	factories, e := startup.BuildSubchannelFactories(channelconfig.BuildOptions{AllowLive: true, ScanRecovery: true})
	if e != nil {
		return nil, errScanRecoveryConfiguration
	}
	flow := &payment.ProviderFlow{SubchannelFactories: factories, Store: store, Providers: providers, MerchantChannels: mapping, AllowLive: true, ScanRecoveryEnabled: true}
	parentFactories, e := startup.BuildFactories(channelconfig.ParentBuildOptions{BuildOptions: channelconfig.BuildOptions{AllowLive: true, ScanRecovery: true, RuntimeWriteAllowed: func() bool { return flow.AllowLive }}})
	if e != nil || flow.InstallParentAccountFactories(parentFactories) != nil {
		return nil, errScanRecoveryConfiguration
	}
	return flow, nil
}
