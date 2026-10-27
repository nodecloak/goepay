package payment

import (
	"context"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"testing"
)

type buyerPreauth104 struct {
	preauthFlowProvider
	freezeAmount, captureAmount int64
}

func (p *buyerPreauth104) Freeze(_ context.Context, r adapter.CreateRequest) (adapter.CreateResult, error) {
	p.freezeAmount = r.Amount
	return adapter.CreateResult{MaterialType: "preauth", Material: "synthetic104"}, nil
}
func (p *buyerPreauth104) CaptureAuthorization(ctx context.Context, r adapter.AuthorizationCaptureRequest) (adapter.PaymentEvent, error) {
	p.captureAmount = r.Amount
	return p.preauthFlowProvider.CaptureAuthorization(ctx, r)
}
func TestBuyerCharge104AuthorizationStoresCapturesAndReconcilesActual(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 10417, 600, "platform")
	buyer104PolicyFixture(t, s, "merchant", 10417, 1, nil, nil)
	o, e := s.CreateOrder(t.Context(), CreateOrderInput{MerchantID: 10417, OutTradeNo: "preauth104", Name: "local104", Type: "alipay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	p := &buyerPreauth104{}
	flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, LocalSimulation: true, CallbackBaseURL: "https://local.invalid"}
	if _, e = flow.SubmitPreauthorization(t.Context(), o.TradeNo, 7, "192.0.2.104", ""); e != nil || p.freezeAmount != 106 {
		t.Fatal("authorization froze requested M", p.freezeAmount, e)
	}
	flow.AllowLive = false
	if e = flow.HandlePreauthorizationNotification(t.Context(), 7, o.TradeNo, nil, []byte("valid-authorization")); e != nil {
		t.Fatal("actual authorization could not be persisted", e)
	}
	a, e := s.Preauthorization(t.Context(), o.TradeNo)
	if e != nil || a.Amount != 106 || a.CaptureState != "none" {
		t.Fatal("buyer actual authorization source", a, e)
	}
	if e = s.RecordPreauthorization(t.Context(), 7, adapter.AuthorizationEvent{TradeNo: o.TradeNo, RequestNo: o.TradeNo, AuthNo: a.AuthNo, BuyerID: a.BuyerID, Amount: 100, State: "SUCCESS"}); !errors.Is(e, ErrConflict) {
		t.Fatal("requested M authenticated as actual freeze", e)
	}
	flow.AllowLive = true
	if e = flow.HandlePreauthorizationNotification(t.Context(), 7, o.TradeNo, nil, []byte("valid-authorization")); e != nil {
		t.Fatal(e)
	}
	if e = flow.HandlePreauthorizationNotification(t.Context(), 7, o.TradeNo, nil, []byte("valid-authorization")); e != nil {
		t.Fatal(e)
	}
	if p.captures != 1 || p.captureAmount != 106 {
		t.Fatal("authorization capture changed actual or resent", p.captures, p.captureAmount)
	}
	a, e = s.Preauthorization(t.Context(), o.TradeNo)
	if e != nil || a.Amount != 106 || a.CaptureState != "succeeded" {
		t.Fatal(a, e)
	}
	if _, e = s.pool.Exec(t.Context(), `ALTER TABLE order_customer_charges104 DISABLE TRIGGER USER;DELETE FROM order_customer_charges104;ALTER TABLE order_customer_charges104 ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if e = s.RecordPreauthorizationCaptureState(t.Context(), o.TradeNo, "unknown"); !errors.Is(e, ErrConflict) {
		t.Fatal("capture state accepted missing actual source", e)
	}
	if _, _, e = s.BeginAuthorizationRelease(t.Context(), 10417, o.TradeNo); !errors.Is(e, ErrConflict) {
		t.Fatal("release accepted missing actual source", e)
	}
}
