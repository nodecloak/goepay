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

// Simulation providers live inside the API process. A separate worker cannot
// reconstruct their in-memory states, so simulation keeps notification jobs only.
func configuredReadOnlyFlow(store *payment.Store) (*payment.ProviderFlow, error) {
	switch os.Getenv("GOEPAY_PROVIDER_MODE") {
	case "", "simulation":
		return nil, nil
	case "configured":
	default:
		return nil, errors.New("invalid worker provider mode")
	}
	switch os.Getenv("GOEPAY_ENABLE_PROVIDER_QUERIES") {
	case "", "false":
		return nil, nil
	case "true":
	default:
		return nil, errors.New("invalid provider query opt-in")
	}
	path := os.Getenv("GOEPAY_CHANNEL_CONFIG_FILE")
	master, e := hex.DecodeString(os.Getenv("GOEPAY_MASTER_KEY"))
	if path == "" || e != nil || len(master) != 32 {
		return nil, errors.New("explicit reconciliation configuration required")
	}
	imp, e := channelconfig.Load(path, master)
	if e != nil {
		return nil, errors.New("cannot load reconciliation configuration")
	}
	var parentAccounts []adapter.ParentStartupAccount
	if store != nil {
		parentAccounts, e = store.ParentStartupAccounts(context.Background())
		if e != nil {
			return nil, errors.New("invalid parent reconciliation accounts")
		}
	}
	startup, e := channelconfig.PrepareParentStartup(imp, parentAccounts)
	if e != nil {
		return nil, errors.New("invalid parent reconciliation accounts")
	}
	imp = startup.Import
	providers, e := startup.BuildProviders(channelconfig.BuildOptions{})
	if e != nil || !startup.HasRegisteredSources() {
		return nil, errors.New("invalid reconciliation providers")
	}
	factories, e := startup.BuildSubchannelFactories(channelconfig.BuildOptions{})
	if e != nil {
		return nil, errors.New("invalid reconciliation child templates")
	}
	flow := &payment.ProviderFlow{SubchannelFactories: factories, Store: store, Providers: providers, AllowLive: false}
	parentFactories, e := startup.BuildFactories(channelconfig.ParentBuildOptions{})
	if e != nil || flow.InstallParentAccountFactories(parentFactories) != nil {
		return nil, errors.New("invalid parent reconciliation factories")
	}
	return flow, nil
}
