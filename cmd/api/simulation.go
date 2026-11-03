package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"goepay/internal/channelconfig"
	"goepay/internal/payment"
	"goepay/internal/server"
	"goepay/internal/simulation"
	"os"
	"strconv"
	"strings"
)

func configureSimulation(app *server.Server, mode string, seed []byte, origin string) error {
	if mode == "" {
		return nil
	}
	if mode != "simulation" || !app.Sandbox {
		return errors.New("only local simulation providers are enabled in this development build")
	}
	if origin == "" {
		origin = "http://127.0.0.1:8180"
	}
	// Explicit local fixtures may admit new merchants before they are created.
	// Never infer admission from an HTTP parameter or extend configured providers.
	raw := os.Getenv("GOEPAY_SIMULATION_MERCHANT_IDS")
	if raw == "" {
		raw = "1000"
	}
	ids := strings.Split(raw, ",")
	if len(ids) > 100 {
		return errors.New("invalid local simulation merchant allowlist")
	}
	mapping := make(map[int64][]int64, len(ids))
	for _, id := range ids {
		mid, err := strconv.ParseInt(id, 10, 64)
		if err != nil || mid <= 0 || strconv.FormatInt(mid, 10) != id || mapping[mid] != nil {
			return errors.New("invalid local simulation merchant allowlist")
		}
		mapping[mid] = []int64{1, 2}
	}
	env, e := simulation.New(seed, origin)
	if e != nil {
		return e
	}
	defaults, e := channelconfig.DefaultPaymentChannels(os.Getenv("GOEPAY_DEFAULT_PAYMENT_CHANNELS"), env.Providers())
	if e != nil {
		return errors.New("invalid local default payment channels")
	}
	key := sha256.Sum256(append([]byte("checkout-signing:"), seed...))
	app.CheckoutKey = key[:]
	flow := &payment.ProviderFlow{Store: app.Store, Providers: env.Providers(), AllowLive: true, LocalSimulation: true, CallbackBaseURL: origin}
	app.ProviderFlow = flow
	flow.ReturnURLBuilder = app.ProviderReturnURL
	app.MerchantChannels = mapping
	app.DefaultPaymentChannels = defaults
	env.Notify = func(ctx context.Context, id int64, trade string, raw []byte) error {
		return flow.HandleNotification(ctx, id, trade, nil, raw)
	}
	app.Simulation = env
	return nil
}
