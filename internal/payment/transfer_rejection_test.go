package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestSignedTransferRejectionReleasesFullCostOnlyWhenDefinite(t *testing.T) {
	for _, code := range []string{"PAYEE_NOT_EXIST", "NAME_MISMATCH", "OPENID_ERROR", "SYSTEMERROR", "NOT_FOUND"} {
		t.Run(code, func(t *testing.T) {
			s, flow, _ := transferFixture(t)
			ctx := context.Background()
			key, e := rsa.GenerateKey(rand.Reader, 2048)
			if e != nil {
				t.Fatal(e)
			}
			var calls atomic.Int32
			upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				node := fmt.Sprintf(`{"code":"40004","sub_code":"%s"}`, code)
				digest := sha256.Sum256([]byte(node))
				sig, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
				fmt.Fprintf(w, `{"alipay_fund_trans_toaccount_transfer_response":%s,"sign":"%s"}`, node, base64.StdEncoding.EncodeToString(sig))
			}))
			defer upstream.Close()
			ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "synthetic", PrivateKey: key, PublicKey: &key.PublicKey, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
			if e != nil {
				t.Fatal(e)
			}
			flow.Providers[7] = ali
			req := adapter.TransferRequest{Type: "alipay", Account: "payee@example.invalid", Amount: 100, Title: "test"}
			transfer, e := flow.Transfer(ctx, 501, 7, "reject", req)
			if e == nil {
				t.Fatal("rejection unexpectedly successful")
			}
			definite := code == "PAYEE_NOT_EXIST" || code == "NAME_MISMATCH" || code == "OPENID_ERROR"
			b, _ := s.Balance(ctx, 501)
			if definite {
				if transfer.Status != "failed" || b.Available != 1000 || b.Reserved != 0 {
					t.Fatalf("definite rejection left reserve %+v %+v", transfer, b)
				}
			} else if transfer.Status != "unknown" || b.Available != 890 || b.Reserved != 110 {
				t.Fatalf("uncertain rejection released %+v %+v", transfer, b)
			}
			flow.Transfer(ctx, 501, 7, "reject", req)
			if calls.Load() != 1 {
				t.Fatal("rejected transfer retried")
			}
		})
	}
}
