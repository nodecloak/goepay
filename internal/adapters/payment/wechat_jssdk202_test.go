package payment

import (
	"crypto/sha1"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWechatJSSDK202FixedTLSProtocolSignatureAndEarlyCacheExpiry(t *testing.T) {
	now := time.Unix(1793000000, 0)
	var tokens, tickets atomic.Int32
	_, client := miniLinkTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "api.weixin.qq.com" || r.TLS == nil || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("unsafe native configuration request")
		}
		switch r.URL.Path {
		case "/cgi-bin/stable_token":
			tokens.Add(1)
			var body map[string]string
			if r.Method != "POST" || r.URL.RawQuery != "" || json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 3 || body["grant_type"] != "client_credential" || body["appid"] != "wx-native202" || body["secret"] != "synthetic-secret202" {
				t.Error("wrong stable-token protocol")
			}
			fmt.Fprint(w, `{"access_token":"private-token202","expires_in":7200}`)
		case "/cgi-bin/ticket/getticket":
			tickets.Add(1)
			if r.Method != "GET" || r.URL.Query().Get("access_token") != "private-token202" || r.URL.Query().Get("type") != "jsapi" || len(r.URL.Query()) != 2 {
				t.Error("wrong jsapi-ticket protocol")
			}
			fmt.Fprint(w, `{"errcode":0,"ticket":"private-ticket202","expires_in":400}`)
		default:
			t.Error("escaped official endpoints")
			http.NotFound(w, r)
		}
	}))
	signer, err := NewWechatJSSDK202(WechatJSSDKConfig202{AppID: "wx-native202", AppSecret: "synthetic-secret202", HTTPClient: client, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	page := "https://cashier.example.invalid/merchant/scanpay?terminal=1"
	for i := 0; i < 2; i++ {
		config, err := signer.Sign(t.Context(), page)
		if err != nil {
			t.Fatal(err)
		}
		want := sha1.Sum([]byte(fmt.Sprintf("jsapi_ticket=private-ticket202&noncestr=%s&timestamp=1793000000&url=https://cashier.example.invalid/merchant/scanpay?terminal=1", config.NonceStr)))
		if config.Debug || config.AppID != "wx-native202" || config.Timestamp != 1793000000 || len(config.NonceStr) != 32 || config.Signature != hex.EncodeToString(want[:]) || len(config.JSAPIList) != 1 || config.JSAPIList[0] != "scanQRCode" {
			t.Fatal("signature contract mismatch", config)
		}
		raw, _ := json.Marshal(config)
		for _, private := range []string{"private-token202", "private-ticket202", "synthetic-secret202", "access_token", "jsapi_ticket", "appsecret"} {
			if strings.Contains(string(raw), private) {
				t.Fatal("configuration leaked credentials", private)
			}
		}
	}
	if tokens.Load() != 1 || tickets.Load() != 1 {
		t.Fatal("fresh ticket not cached", tokens.Load(), tickets.Load())
	}
	now = now.Add(201 * time.Second)
	if _, err = signer.Sign(t.Context(), page); err != nil {
		t.Fatal(err)
	}
	if tokens.Load() != 1 || tickets.Load() != 2 {
		t.Fatal("ticket did not expire 200 seconds early or valid token not reused", tokens.Load(), tickets.Load())
	}
	now = now.Add(7000 * time.Second)
	if _, err = signer.Sign(t.Context(), page); err != nil {
		t.Fatal(err)
	}
	if tokens.Load() != 2 || tickets.Load() != 3 {
		t.Fatal("expired credentials reused", tokens.Load(), tickets.Load())
	}
}

func TestWechatJSSDK202RejectsMalformedSecretResponsesAndNeverFollowsRedirect(t *testing.T) {
	for _, body := range []string{`{"errcode":40013,"errmsg":"synthetic-secret202 private-token202"}`, `{"access_token":"private-token202","access_token":"other","expires_in":7200}`, `{"access_token":"private-token202","expires_in":200}`, strings.Repeat("x", (64<<10)+1), `{"access_token":"private-token202","expires_in":7200} trailing`} {
		t.Run(fmt.Sprint(len(body)), func(t *testing.T) {
			_, client := miniLinkTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			signer, err := NewWechatJSSDK202(WechatJSSDKConfig202{AppID: "wx-native202", AppSecret: "synthetic-secret202", HTTPClient: client})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = signer.Sign(t.Context(), "https://cashier.example.invalid/merchant/scanpay"); err != ErrWechatJSSDK202 {
				t.Fatal("malformed response did not fail privately", err)
			}
		})
	}
	var calls atomic.Int32
	_, client := miniLinkTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Redirect(w, r, "https://other.example.invalid/steal", 307)
	}))
	signer, err := NewWechatJSSDK202(WechatJSSDKConfig202{AppID: "wx-native202", AppSecret: "synthetic-secret202", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = signer.Sign(t.Context(), "https://cashier.example.invalid/merchant/scanpay"); err != ErrWechatJSSDK202 || calls.Load() != 1 {
		t.Fatal("redirect was followed", calls.Load(), err)
	}
	for _, page := range []string{"http://cashier.example.invalid/merchant/scanpay", "https://user@cashier.example.invalid/merchant/scanpay", "https://cashier.example.invalid/merchant/scanpay#secret", "https://cashier.example.invalid/merchant/orders", "https://cashier.example.invalid/merchant/%73canpay"} {
		if _, err = signer.Sign(t.Context(), page); err != ErrWechatJSSDK202 {
			t.Fatal("unsafe page accepted", page, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("invalid signing page reached network", calls.Load())
	}
	if _, err = NewWechatJSSDK202(WechatJSSDKConfig202{AppID: "wx-native202", AppSecret: "synthetic-secret202", HTTPClient: &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}}); err == nil {
		t.Fatal("insecure TLS override accepted")
	}
}
