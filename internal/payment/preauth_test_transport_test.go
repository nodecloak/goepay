package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	adapter "goepay/internal/adapters/payment"
)

var preauthorizationNotFoundFixture struct {
	sync.Once
	err error
}

// Shared synthetic provider doubles use a real local TLS/RSA query rejection
// instead of forging a ProviderError that lacks adapter authentication.
func preauthorizationTestQueryNotFound() error {
	preauthorizationNotFoundFixture.Do(func() {
		preauthorizationNotFoundFixture.err = preauthorizationTestRejection("alipay.trade.query", "40004", "ACQ.TRADE_NOT_EXIST", false)
	})
	return preauthorizationNotFoundFixture.err
}

func preauthorizationTestRejection(operation, code, subcode string, insecure bool) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid synthetic form", 400)
			return
		}
		node, _ := json.Marshal(map[string]string{"code": code, "sub_code": subcode})
		digest := sha256.Sum256(node)
		signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		if err != nil {
			http.Error(w, "synthetic signing failure", 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(r.Form.Get("method"), ".", "_") + "_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(signature)})
	}))
	defer up.Close()
	client := up.Client()
	if insecure {
		transport := client.Transport.(*http.Transport).Clone()
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		transport.TLSClientConfig.InsecureSkipVerify = true // Local negative case only.
		defer transport.CloseIdleConnections()
		client = &http.Client{Transport: transport}
	}
	p, err := adapter.NewAlipay(adapter.AlipayConfig{AppID: "preauth-rejection-app", SellerID: "2088123456789012", PrivateKey: key, PublicKey: &key.PublicKey, GatewayURL: up.URL, HTTPClient: client, Methods: []string{"preauth"}})
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch operation {
	case "alipay.trade.query":
		_, err = p.Query(ctx, adapter.QueryRequest{TradeNo: "synthetic-order"})
	case "alipay.trade.pay":
		_, err = p.CaptureAuthorization(ctx, adapter.AuthorizationCaptureRequest{TradeNo: "synthetic-order", AuthNo: "synthetic-auth", BuyerID: "2088000000000000", Subject: "synthetic", Amount: 125, NotifyURL: "https://merchant.example.invalid/notify"})
	case "alipay.fund.auth.order.unfreeze":
		_, err = p.UnfreezeAuthorization(ctx, adapter.AuthorizationRequest{TradeNo: "synthetic-order", RequestNo: "UA123", AuthNo: "synthetic-auth", Amount: 125})
	default:
		err = p.Close(ctx, adapter.QueryRequest{TradeNo: "synthetic-order"})
	}
	return err
}
