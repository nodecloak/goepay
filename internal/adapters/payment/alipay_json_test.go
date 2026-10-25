package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAlipaySignedDuplicateJSONCannotConfirmReceiverBinding(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := []byte(`{"code":"10000","result_code":"FAIL","result_code":"SUCCESS"}`)
		digest := sha256.Sum256(raw)
		signature, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		encoded, _ := json.Marshal(base64.StdEncoding.EncodeToString(signature))
		io.WriteString(w, `{"alipay_trade_royalty_relation_bind_response":`+string(raw)+`,"sign":`+string(encoded)+`}`)
	}))
	defer upstream.Close()
	p, e := NewAlipay(AlipayConfig{AppID: "local-app", SellerID: "2088000000000099", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: upstream.URL, HTTPClient: upstream.Client(), EnableChannelSharing: true})
	if e != nil {
		t.Fatal(e)
	}
	e = p.BindChannelSharingReceiver(context.Background(), "BIND_1", SharingReceiver{Type: "userId", Account: "receiver-one"})
	var unknown *UnknownOutcome
	if !errors.As(e, &unknown) {
		t.Fatal("ambiguous signed JSON was accepted as success", e)
	}
}
