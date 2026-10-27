package main

import (
	"encoding/hex"
	"errors"
	"os"

	"goepay/internal/channelconfig"
	"goepay/internal/payment"
)

func scheduledAutomaticQueueFlow(store *payment.Store) (*payment.ProviderFlow, error) {
	switch os.Getenv("GOEPAY_TASK_SCHEDULER_AUTO_QUEUE_ENABLED") {
	case "", "false":
		return nil, nil
	case "true":
	default:
		return nil, errors.New("invalid scheduled automatic queue enable flag")
	}
	denied := errors.New("independent automatic queue runtime configuration required")
	if os.Getenv("GOEPAY_PROVIDER_MODE") != "configured" || os.Getenv("GOEPAY_SANDBOX") != "false" || os.Getenv("GOEPAY_ENABLE_PROVIDER_WRITES") != "true" {
		return nil, denied
	}
	master, e := hex.DecodeString(os.Getenv("GOEPAY_MASTER_KEY"))
	path := os.Getenv("GOEPAY_CHANNEL_CONFIG_FILE")
	if e != nil || len(master) != 32 || path == "" {
		return nil, denied
	}
	imported, e := channelconfig.Load(path, master)
	if e != nil {
		return nil, denied
	}
	providers, e := channelconfig.Build(imported, true)
	if e != nil || len(providers) == 0 {
		return nil, denied
	}
	mapping, e := channelconfig.MerchantAllowlist(os.Getenv("GOEPAY_MERCHANT_CHANNELS"), providers)
	if e != nil {
		return nil, denied
	}
	flow := &payment.ProviderFlow{Store: store, Providers: providers, MerchantChannels: mapping, AllowLive: true}
	if len(flow.SettlementAutoWalletChannels()) == 0 {
		return nil, denied
	}
	return flow, nil
}
