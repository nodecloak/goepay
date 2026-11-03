package channelconfig

import (
	"context"
	"errors"
	"testing"

	provider "goepay/internal/adapters/payment"
)

type browserCheckout239Provider struct {
	provider.Provider
	desktop bool
	err     error
}

func (p browserCheckout239Provider) PrepareBrowserCheckout(material, tradeNo, returnURL string, desktop bool) (provider.BrowserCheckoutPresentation, error) {
	if material != "stored-239" || tradeNo != "SYNTHETIC-239" || returnURL != "https://return.example.invalid/fresh239" || desktop != p.desktop {
		return provider.BrowserCheckoutPresentation{}, provider.ErrInvalid
	}
	if p.err != nil {
		return provider.BrowserCheckoutPresentation{}, p.err
	}
	return provider.BrowserCheckoutPresentation{HTML: "verified-existing-order239", Method: "wap", GatewayOrigin: "https://gateway.example.invalid:8443"}, nil
}

func TestGuardedForwardsBrowserCheckout239WithoutFinancialWriteAuthorization(t *testing.T) {
	type browserCheckout interface {
		PrepareBrowserCheckout(string, string, string, bool) (provider.BrowserCheckoutPresentation, error)
	}
	for _, desktop := range []bool{false, true} {
		wrapped := guarded{Provider: browserCheckout239Provider{desktop: desktop}, runtimeWriteAllowed: func() bool {
			t.Fatal("read-only checkout presentation must not evaluate financial write authorization")
			return false
		}}
		checkout, ok := any(wrapped).(browserCheckout)
		if !ok {
			t.Fatal("guarded provider dropped verified browser checkout presentation")
		}
		got, err := checkout.PrepareBrowserCheckout("stored-239", "SYNTHETIC-239", "https://return.example.invalid/fresh239", desktop)
		want := provider.BrowserCheckoutPresentation{HTML: "verified-existing-order239", Method: "wap", GatewayOrigin: "https://gateway.example.invalid:8443"}
		if err != nil || got != want {
			t.Fatalf("verified presentation forwarding: got %+v, error %v", got, err)
		}
		if _, err := wrapped.Create(context.Background(), provider.CreateRequest{}); !errors.Is(err, ErrLiveDisabled) {
			t.Fatalf("presentation expanded financial write authorization: %v", err)
		}
	}
	wrapped := guarded{Provider: browserCheckout239Provider{err: provider.ErrAuthentication}}
	checkout, ok := any(wrapped).(browserCheckout)
	if !ok {
		t.Fatal("guarded provider dropped verified browser checkout presentation")
	}
	if got, err := checkout.PrepareBrowserCheckout("stored-239", "SYNTHETIC-239", "https://return.example.invalid/fresh239", false); !errors.Is(err, provider.ErrAuthentication) || got != (provider.BrowserCheckoutPresentation{}) {
		t.Fatalf("provider authentication error was not preserved: got %+v, error %v", got, err)
	}
	unsupported, ok := any(guarded{}).(browserCheckout)
	if !ok {
		t.Fatal("guarded provider must fail closed for unsupported browser presentation")
	}
	if got, err := unsupported.PrepareBrowserCheckout("stored-239", "SYNTHETIC-239", "https://return.example.invalid/fresh239", true); !errors.Is(err, provider.ErrUnsupported) || got != (provider.BrowserCheckoutPresentation{}) {
		t.Fatalf("unsupported provider: got %+v, error %v", got, err)
	}
}
