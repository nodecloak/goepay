package main

import (
	"encoding/hex"
	"errors"
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
	providers, e := channelconfig.Build(imp, false)
	if e != nil || len(providers) == 0 {
		return nil, errors.New("invalid reconciliation providers")
	}
	return &payment.ProviderFlow{Store: store, Providers: providers, AllowLive: false}, nil
}
