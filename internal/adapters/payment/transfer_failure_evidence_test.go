package payment

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTransferFailure76OnlyOriginalSignedAlipaySubmitCarriesBoundReceipt(t *testing.T) {
	for _, certificate := range []bool{false, true} {
		t.Run(fmt.Sprint("certificate=", certificate), func(t *testing.T) {
			cfg := AlipayConfig{}
			var platform *rsa.PrivateKey
			sn := ""
			if certificate {
				config, key, cert, _ := alipayCertificateFixture(t)
				cfg = config
				platform = key
				var e error
				sn, e = alipayCertSN(cert)
				if e != nil {
					t.Fatal(e)
				}
			} else {
				cfg.PrivateKey = testKey(t)
				platform = testKey(t)
				cfg.PublicKey = &platform.PublicKey
				cfg.AppID = "app76"
			}
			cfg.SellerID = "2088000000000076"
			mode := "signed"
			node := []byte(`{"code":"40004","sub_code":"PAYER_BALANCE_NOT_ENOUGH"}`)
			up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "http503" {
					w.WriteHeader(503)
					return
				}
				if e := r.ParseForm(); e != nil {
					t.Error(e)
					return
				}
				params := map[string]string{}
				for k, v := range r.PostForm {
					if len(v) != 1 {
						t.Error("duplicate vendor request field")
					}
					params[k] = v[0]
				}
				sig, e := SignAlipayRSA2(params, cfg.PrivateKey)
				if e != nil || sig != params["sign"] {
					t.Error("original transfer request signature", e)
				}
				signature, _ := signRSA(node, platform)
				if mode == "forged" {
					signature = "invalid"
				}
				result := map[string]any{strings.ReplaceAll(params["method"], ".", "_") + "_response": json.RawMessage(node), "sign": signature}
				if certificate {
					result["alipay_cert_sn"] = sn
				}
				json.NewEncoder(w).Encode(result)
			}))
			defer up.Close()
			cfg.GatewayURL = up.URL
			cfg.HTTPClient = up.Client()
			a, e := NewAlipay(cfg)
			if e != nil {
				t.Fatal(e)
			}
			request := TransferRequest{TransferNo: "TF76", Type: "alipay", Account: "2088000000001076", Amount: 450, Title: "local receipt", Remark: "local only"}
			_, sendErr := a.Transfer(context.Background(), request)
			receipt, ok := VerifiedTransferFailure(sendErr)
			scope, _ := TransferAccountScope(a)
			digest := sha256.Sum256(node)
			if !ok || receipt.Provider != "alipay" || receipt.Product != a.TransferProductIdentity() || receipt.Operation != a.TransferProductIdentity() || receipt.TransferNo != "TF76" || receipt.Type != "alipay" || receipt.Amount != 450 || receipt.AccountScope != scope || receipt.Code != "40004:PAYER_BALANCE_NOT_ENOUGH" || receipt.ResponseSHA256 != hex.EncodeToString(digest[:]) {
				t.Fatal("unbound authenticated transfer rejection", receipt, ok, sendErr)
			}
			if _, ok = VerifiedTransferFailure(fmt.Errorf("wrapped: %w", sendErr)); !ok {
				t.Fatal("ordinary wrapping lost original receipt")
			}
			for _, untrusted := range []error{&ProviderError{Code: receipt.Code}, errors.New(receipt.Code), &UnknownOutcome{Operation: "transfer", Cause: sendErr}, errors.Join(sendErr, context.DeadlineExceeded)} {
				if _, ok = VerifiedTransferFailure(untrusted); ok {
					t.Fatal("untrusted/unknown error acquired receipt", untrusted)
				}
				if _, ok = VerifiedTransferFailure(a.bindTransferFailure(untrusted, request, receipt.Operation, receipt.Product)); ok {
					t.Fatal("binding stripped original transfer uncertainty")
				}
			}
			changed := request
			changed.TransferNo = "TF77"
			if _, ok = VerifiedTransferFailure(a.bindTransferFailure(sendErr, changed, receipt.Operation, receipt.Product)); ok {
				t.Fatal("original submit failure rebound to another transfer")
			}
			var original *ProviderError
			if !errors.As(sendErr, &original) {
				t.Fatal(sendErr)
			}
			saved := original.Code
			original.Code = "40004:PAYEE_NOT_EXIST"
			if _, ok = VerifiedTransferFailure(sendErr); ok {
				t.Fatal("changed public error code retained source")
			}
			original.Code = saved
			_, queryErr := a.QueryTransfer(context.Background(), request)
			if _, ok = VerifiedTransferFailure(queryErr); ok {
				t.Fatal("read-only query error became submit evidence")
			}
			for _, bad := range []string{"forged", "http503"} {
				mode = bad
				_, e = a.Transfer(context.Background(), request)
				if _, ok = VerifiedTransferFailure(e); ok {
					t.Fatal("unverified transfer error acquired source", bad, e)
				}
			}
		})
	}
}
