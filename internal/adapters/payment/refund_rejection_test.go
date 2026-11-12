package payment

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Preserve the public request identity while routing the actual verified TLS
// exchange to a synthetic loopback server.
type refundRejectionTLSTransport struct{ v3TLSTransport }

func (x refundRejectionTLSTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := x.v3TLSTransport.RoundTrip(r)
	if response != nil {
		response.Request = r
	}
	return response, err
}

func TestWechatV3RefundRejectionCarriesAuthenticatedProvenance(t *testing.T) {
	merchant, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	platform, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v3/refund/domestic/refunds" {
			t.Errorf("unexpected refund request %s %s", r.Method, r.URL.Path)
		}
		v3SignedResponse(t, w, platform, http.StatusBadRequest, `{"code":"PARAM_ERROR"}`, "PUB_KEY_ID_REFUND")
	}))
	defer up.Close()
	p, err := NewWechatV3(WechatV3Config{AppID: "refund-app", MerchantID: "refund-mch", MerchantSerial: "LOCAL_REFUND", PrivateKey: merchant, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_REFUND": &platform.PublicKey}, HTTPClient: &http.Client{Transport: refundRejectionTLSTransport{v3TLSTransport{upstream: up, t: t}}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Refund(context.Background(), RefundRequest{TradeNo: "TEST_REFUND_ORDER", RefundNo: "RF123", TotalAmount: 100, Amount: 60})
	proof, ok := VerifiedProviderRejection(err)
	if !ok || proof.Provider != "wxpay" || proof.Operation != "/v3/refund/domestic/refunds" || proof.Code != "PARAM_ERROR" || proof.Authentication != "wechat_v3_rsa_sha256" {
		t.Fatal("signed V3 refund rejection lost its provenance", proof, err)
	}
	for _, uncertain := range []error{&UnknownOutcome{Operation: "refund", Cause: err}, errors.Join(err, context.Canceled), errors.Join(err, context.DeadlineExceeded)} {
		if _, ok := VerifiedProviderRejection(uncertain); ok {
			t.Fatal("uncertainty wrapper must override authenticated rejection")
		}
	}
}
