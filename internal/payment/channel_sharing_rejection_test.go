package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
)

type uncertainSharingProvider struct {
	*adapter.WechatV3
	calls int
}

func (p *uncertainSharingProvider) rejected() error {
	p.calls++
	return &adapter.UnknownOutcome{Operation: "synthetic lost sharing response", Cause: &adapter.ProviderError{Code: "PARAM_ERROR"}}
}
func (p *uncertainSharingProvider) SubmitChannelSharing(context.Context, adapter.ChannelSharingRequest) (adapter.ChannelSharingResult, error) {
	return adapter.ChannelSharingResult{}, p.rejected()
}
func (p *uncertainSharingProvider) ReturnChannelSharing(context.Context, adapter.ChannelSharingReturnRequest) (adapter.ChannelSharingReturnResult, error) {
	return adapter.ChannelSharingReturnResult{}, p.rejected()
}
func (p *uncertainSharingProvider) FinishChannelSharing(context.Context, adapter.ChannelSharingFinishRequest) (adapter.ChannelSharingFinishResult, error) {
	return adapter.ChannelSharingFinishResult{}, p.rejected()
}
func (p *uncertainSharingProvider) BindChannelSharingReceiver(context.Context, string, adapter.SharingReceiver) error {
	return p.rejected()
}
func (p *uncertainSharingProvider) UnbindChannelSharingReceiver(context.Context, string, adapter.SharingReceiver) error {
	return p.rejected()
}

func TestChannelSharingUncertainSubmitCannotAuthorizeFinish(t *testing.T) {
	f := newSharingExecutionFixture(t)
	o := f.paid(t, "uncertain-sharing-submit", false)
	p := &uncertainSharingProvider{WechatV3: f.Flow.Providers[7].(*adapter.WechatV3)}
	f.Flow.Providers[7] = p
	view, err := f.Flow.SubmitChannelSharing(t.Context(), 751, o.TradeNo, "uncertain-submit", channelSharingActor)
	_, finishErr := f.Flow.PreviewChannelSharingFinish(t.Context(), 751, o.TradeNo, channelSharingActor)
	if err == nil || view.State != "unknown" || !errors.Is(finishErr, ErrState) || p.calls != 1 {
		t.Fatal("uncertain sharing submission authorized finishing", view.State, err, finishErr, p.calls)
	}
}

func TestChannelSharingUncertainReturnCannotReopenCapacity(t *testing.T) {
	f := newSharingReturnFixture(t)
	p := &uncertainSharingProvider{WechatV3: f.Flow.Providers[7].(*adapter.WechatV3)}
	f.Flow.Providers[7] = p
	receiver := f.Sharing.Items[0].ReceiverID
	first, err := f.Flow.SubmitChannelSharingReturn(t.Context(), 751, f.Sharing.ExternalNo, receiver, "uncertain-return", 12, channelSharingActor)
	_, retryErr := f.Flow.SubmitChannelSharingReturn(t.Context(), 751, f.Sharing.ExternalNo, receiver, "new-return", 12, channelSharingActor)
	if err == nil || first.State != "unknown" || !errors.Is(retryErr, ErrConflict) || p.calls != 1 {
		t.Fatal("uncertain return reopened recipient capacity", first.State, err, retryErr, p.calls)
	}
}

func TestChannelSharingUncertainFinishCannotGrantAnotherSend(t *testing.T) {
	f := newSharingFinishFixture(t)
	p := &uncertainSharingProvider{WechatV3: f.Flow.Providers[7].(*adapter.WechatV3)}
	f.Flow.Providers[7] = p
	first, err := f.Flow.SubmitChannelSharingFinish(t.Context(), 751, f.Order.TradeNo, "uncertain-finish", channelSharingActor)
	_, retryErr := f.Flow.SubmitChannelSharingFinish(t.Context(), 751, f.Order.TradeNo, "new-finish", channelSharingActor)
	if err == nil || first.State != "unknown" || !errors.Is(retryErr, ErrConflict) || p.calls != 1 {
		t.Fatal("uncertain finish granted another provider send", first.State, err, retryErr, p.calls)
	}
}

func TestChannelSharingUncertainReceiverCannotGrantAnotherSend(t *testing.T) {
	for _, action := range []string{"bind", "unbind"} {
		t.Run(action, func(t *testing.T) {
			_, flow, _, _ := sharingReceiverFlowFixture(t)
			receiver, err := flow.CreateChannelSharingReceiver(t.Context(), 751, 7, "uncertain-receiver", channelSharingReceiverInput, channelSharingActor)
			if err != nil {
				t.Fatal(err)
			}
			if action == "unbind" {
				if _, err = flow.BindChannelSharingReceiver(t.Context(), 751, receiver.ID, "initial-bind", channelSharingActor); err != nil {
					t.Fatal(err)
				}
			}
			p := &uncertainSharingProvider{WechatV3: flow.Providers[7].(*adapter.WechatV3)}
			flow.Providers[7] = p
			first, err := flow.receiverOperation(t.Context(), 751, receiver.ID, "uncertain-"+action, action, channelSharingActor)
			_, retryErr := flow.receiverOperation(t.Context(), 751, receiver.ID, "new-"+action, action, channelSharingActor)
			if err == nil || first.State != "unknown" || retryErr == nil || p.calls != 1 {
				t.Fatal("uncertain receiver operation granted another send", first.State, err, retryErr, p.calls)
			}
		})
	}
}

func sharingRejectionProvider(t *testing.T, code string) *adapter.WechatV3 {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := `{"code":"` + code + `"}`
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		nonce := "synthetic-sharing-refusal"
		digest := sha256.Sum256([]byte(stamp + "\n" + nonce + "\n" + raw + "\n"))
		signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		if err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Wechatpay-Timestamp", stamp)
		w.Header().Set("Wechatpay-Nonce", nonce)
		w.Header().Set("Wechatpay-Serial", "PUB_KEY_ID_REJECTION")
		w.Header().Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(signature))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(raw))
	}))
	t.Cleanup(up.Close)
	p, err := adapter.NewWechatV3(adapter.WechatV3Config{AppID: "local-app", MerchantID: "original-mch", MerchantSerial: "LOCAL_REJECTION", PrivateKey: key, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_REJECTION": &key.PublicKey}, EnableChannelSharing: true, HTTPClient: &http.Client{Transport: sharingReceiverTLSTransport{up}}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func authenticatedSharingBindRejection(t *testing.T) error {
	t.Helper()
	p := sharingRejectionProvider(t, "NO_AUTH")
	err := p.BindChannelSharingReceiver(t.Context(), "PB1", channelSharingReceiverInput)
	if proof, ok := adapter.VerifiedProviderRejection(err); !ok || proof.Operation != "/v3/profitsharing/receivers/add" {
		t.Fatal("fixture requires authenticated receiver submission refusal", proof, err)
	}
	return err
}

func TestChannelSharingRejectionMatchesOperationAndUncertainty(t *testing.T) {
	p := sharingRejectionProvider(t, "PARAM_ERROR")
	ctx := t.Context()
	errorsByAction := map[string]error{
		"bind":   p.BindChannelSharingReceiver(ctx, "PB1", channelSharingReceiverInput),
		"unbind": p.UnbindChannelSharingReceiver(ctx, "PU1", channelSharingReceiverInput),
	}
	_, errorsByAction["submit"] = p.SubmitChannelSharing(ctx, adapter.ChannelSharingRequest{OrderNo: "PS1", ProviderTradeNo: "WX1", TotalAmount: 100, Receivers: []adapter.SharingAllocation{{SharingReceiver: channelSharingReceiverInput, Amount: 12, Description: "local"}}, UnfreezeUnsplit: true})
	_, errorsByAction["return"] = p.ReturnChannelSharing(ctx, adapter.ChannelSharingReturnRequest{OrderNo: "PS1", ProviderOrderNo: "WXPS1", ProviderTradeNo: "WX1", ReturnNo: "PR1", ReceiverType: "MERCHANT_ID", ReceiverAccount: channelSharingReceiverInput.Account, Amount: 12, Description: "local"})
	_, errorsByAction["finish"] = p.FinishChannelSharing(ctx, adapter.ChannelSharingFinishRequest{OrderNo: "PF1", ProviderTradeNo: "WX1", ExpectedUnsplit: 100, Description: "local"})
	for action, err := range errorsByAction {
		t.Run(action, func(t *testing.T) {
			if !definiteAuthenticatedSharingRejection(err, "wxpay", action) {
				t.Fatal("authenticated same-operation refusal lost", err)
			}
			if definiteAuthenticatedSharingRejection(err, "alipay", action) || definiteAuthenticatedSharingRejection(&adapter.UnknownOutcome{Operation: action, Cause: err}, "wxpay", action) || definiteAuthenticatedSharingRejection(errors.Join(err, context.Canceled), "wxpay", action) {
				t.Fatal("another provider or uncertain response authorized retry")
			}
			for other := range errorsByAction {
				if other != action && definiteAuthenticatedSharingRejection(err, "wxpay", other) {
					t.Fatal("another operation authorized retry", other)
				}
			}
		})
	}
}
