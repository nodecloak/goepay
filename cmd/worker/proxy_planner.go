package main

import (
	"context"

	"goepay/internal/notification"
	"goepay/internal/platform/httpclient"
	"goepay/internal/proxyconfig"
)

// proxyPlanner turns the stored 中转代理 settings into a per-send delivery
// decision for the notification handler. It builds a fresh proxied client each
// send so a settings change takes effect without a worker restart.
type proxyPlanner struct{ store *proxyconfig.Store }

func (p proxyPlanner) DeliveryPlan(ctx context.Context) (notification.Delivery, error) {
	plan, err := p.store.Plan(ctx)
	if err != nil {
		return notification.Delivery{}, err
	}
	switch plan.Mode {
	case 1:
		client, err := httpclient.NewProxied(httpclient.ProxyConfig{Type: plan.Type, Server: plan.Server, Port: plan.Port, User: plan.User, Password: plan.Password})
		if err != nil {
			return notification.Delivery{}, err
		}
		return notification.Delivery{Mode: 1, Client: client}, nil
	case 2:
		return notification.Delivery{Mode: 2, API: httpclient.NewProxyAPIClient(), APIURL: plan.APIURL, APIKey: plan.APIKey}, nil
	default:
		return notification.Delivery{Mode: 0}, nil
	}
}
