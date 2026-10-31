package channelconfig

import (
	"errors"
	"testing"

	provider "goepay/internal/adapters/payment"
)

type checkoutHTMLStub struct{ provider.Provider }

func (checkoutHTMLStub) PrepareCheckoutHTML(material, trade string) (string, error) {
	return material + ":" + trade, nil
}

func (checkoutHTMLStub) RefreshCheckoutHTML(material, trade, returnURL string) (string, error) {
	return material + ":" + trade + ":" + returnURL, nil
}

func TestGuardedForwardsCheckoutHTMLValidation(t *testing.T) {
	type prepare interface {
		PrepareCheckoutHTML(string, string) (string, error)
	}
	wrapped := guarded{Provider: checkoutHTMLStub{}}
	p, ok := any(wrapped).(prepare)
	if !ok {
		t.Fatal("guarded provider dropped checkout validation")
	}
	got, err := p.PrepareCheckoutHTML("material", "trade")
	if err != nil || got != "material:trade" {
		t.Fatalf("forwarding: %q %v", got, err)
	}
	unsupported := guarded{}
	if _, err = any(unsupported).(prepare).PrepareCheckoutHTML("material", "trade"); !errors.Is(err, provider.ErrUnsupported) {
		t.Fatalf("unsupported provider: %v", err)
	}
	type refresh interface {
		RefreshCheckoutHTML(string, string, string) (string, error)
	}
	r, ok := any(wrapped).(refresh)
	if !ok {
		t.Fatal("guarded provider dropped checkout refresh")
	}
	got, err = r.RefreshCheckoutHTML("material", "trade", "return")
	if err != nil || got != "material:trade:return" {
		t.Fatalf("refresh forwarding: %q %v", got, err)
	}
	if _, err = any(unsupported).(refresh).RefreshCheckoutHTML("material", "trade", "return"); !errors.Is(err, provider.ErrUnsupported) {
		t.Fatalf("unsupported refresh: %v", err)
	}
}
