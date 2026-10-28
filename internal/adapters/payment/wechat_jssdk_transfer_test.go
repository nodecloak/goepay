package payment

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestWechatJSSDKSignsOnlyTheMerchantTransferPayeePage(t *testing.T) {
	now := time.Unix(1793000000, 0)
	var calls atomic.Int32
	_, client := miniLinkTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/cgi-bin/stable_token":
			fmt.Fprint(w, `{"access_token":"private-token207","expires_in":7200}`)
		case "/cgi-bin/ticket/getticket":
			fmt.Fprint(w, `{"errcode":0,"ticket":"private-ticket207","expires_in":7200}`)
		default:
			http.NotFound(w, r)
		}
	}))
	signer, err := NewWechatJSSDK202(WechatJSSDKConfig202{AppID: "wx-mp207", AppSecret: "synthetic-secret207", HTTPClient: client, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	var _ WechatTransferJSSigner = signer
	page := "https://pay.example.invalid/pay/wxtrans?n=7&s=" + "ab" + "&t=1793000000"
	config, err := signer.SignMerchantTransfer(t.Context(), page)
	if err != nil {
		t.Fatal(err)
	}
	want := sha1.Sum([]byte(fmt.Sprintf("jsapi_ticket=private-ticket207&noncestr=%s&timestamp=1793000000&url=%s", config.NonceStr, page)))
	if config.AppID != "wx-mp207" || config.Debug || config.Signature != hex.EncodeToString(want[:]) || len(config.JSAPIList) != 1 || config.JSAPIList[0] != "requestMerchantTransfer" {
		t.Fatal("payee page signature contract mismatch", config)
	}
	before := calls.Load()
	for _, bad := range []string{"https://pay.example.invalid/merchant/scanpay", "http://pay.example.invalid/pay/wxtrans?n=7", "https://pay.example.invalid/pay/wxtrans/../x", "https://pay.example.invalid/pay/wxtrans?n=7#frag", "https://user@pay.example.invalid/pay/wxtrans"} {
		if _, err := signer.SignMerchantTransfer(t.Context(), bad); err != ErrWechatJSSDK202 {
			t.Fatal("signed a page outside the payee confirmation", bad)
		}
	}
	if _, err := signer.Sign(t.Context(), page); err != ErrWechatJSSDK202 {
		t.Fatal("scan signer accepted the payee page")
	}
	if calls.Load() != before {
		t.Fatal("refused page reached WeChat")
	}
}
