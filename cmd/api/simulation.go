package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"goepay/internal/payment"
	"goepay/internal/server"
	"goepay/internal/simulation"
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
	env, e := simulation.New(seed, origin)
	if e != nil {
		return e
	}
	key := sha256.Sum256(append([]byte("checkout-signing:"), seed...))
	app.CheckoutKey = key[:]
	flow := &payment.ProviderFlow{Store: app.Store, Providers: env.Providers(), AllowLive: true, LocalSimulation: true, CallbackBaseURL: origin}
	app.ProviderFlow = flow
	flow.ReturnURLBuilder = app.ProviderReturnURL
	app.MerchantChannels = map[int64][]int64{1000: {1, 2}}
	env.Notify = func(ctx context.Context, id int64, trade string, raw []byte) error {
		return flow.HandleNotification(ctx, id, trade, nil, raw)
	}
	app.Simulation = env
	return nil
}
