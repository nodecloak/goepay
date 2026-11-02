package payment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestKYC233AlipaySignedProofKeyAndCertificate(t *testing.T) {
	for _, cert := range []bool{false, true} {
		t.Run(map[bool]string{false: "key", true: "certificate"}[cert], func(t *testing.T) {
			merchant, platform := testKey(t), testKey(t)
			config := AlipayConfig{AppID: "2021000000000000", PrivateKey: merchant, PublicKey: &platform.PublicKey}
			sn := ""
			if cert {
				fixture, signing, certificate, _ := alipayCertificateFixture(t)
				config = fixture
				platform = signing
				merchant = config.PrivateKey
				var e error
				sn, e = alipayCertSN(certificate)
				if e != nil {
					t.Fatal(e)
				}
			}
			bad := false
			calls := 0
			peer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				r.ParseForm()
				params := map[string]string{}
				for k, v := range r.Form {
					params[k] = v[0]
				}
				if !verifyRSA([]byte(AlipayRequestCanonical(params)), params["sign"], &merchant.PublicKey) {
					t.Error("unsigned KYC request")
				}
				if cert && (params["app_cert_sn"] == "" || params["alipay_root_cert_sn"] == "") {
					t.Error("certificate identity not signed")
				}
				node := ""
				switch params["method"] {
				case "alipay.user.certify.open.initialize":
					var biz map[string]json.RawMessage
					json.Unmarshal([]byte(params["biz_content"]), &biz)
					if text(biz, "outer_order_no") != strings.Repeat("a", 32) || text(biz, "biz_code") != "SMART_FACE" {
						t.Error("wrong KYC identity request")
					}
					node = `{"code":"10000","msg":"Success","certify_id":"opaque"}`
				case "alipay.user.certify.open.query":
					node = `{"code":"10000","msg":"Success","passed":"T"}`
				case "alipay.user.certdoc.certverify.preconsult":
					node = `{"code":"10000","verify_id":"verify"}`
				case "alipay.system.oauth.token":
					if params["code"] != "bound-code" || params["grant_type"] != "authorization_code" || params["biz_content"] != "" {
						t.Error("OAuth code in wrong signed field")
					}
					node = `{"access_token":"access-token","expires_in":"3600","user_id":"2088000000000000"}`
				case "alipay.user.certdoc.certverify.consult":
					if params["auth_token"] != "access-token" {
						t.Error("missing signed auth token")
					}
					node = `{"code":"10000","passed":"T"}`
				default:
					t.Error("wrong method")
				}
				sig := independentSignature(t, node, platform)
				if bad {
					sig = "invalid"
				}
				out := map[string]any{strings.ReplaceAll(params["method"], ".", "_") + "_response": json.RawMessage(node), "sign": sig}
				if cert {
					out["alipay_cert_sn"] = sn
				}
				json.NewEncoder(w).Encode(out)
			}))
			defer peer.Close()
			config.HTTPClient = &http.Client{Transport: oauthTLSRoundTripper{t, peer, "openapi.alipay.com"}}
			ali, e := NewAlipay(config)
			if e != nil {
				t.Fatal(e)
			}
			token, link, e := ali.KYC233Initialize(context.Background(), strings.Repeat("a", 32), "Synthetic Person", "110101199001010015", 0, "https://pay.example.invalid/merchant/account/certificate?state=bound")
			if e != nil || token != "opaque" {
				t.Fatal(e)
			}
			u, _ := url.Parse(link)
			if u.Scheme != "https" || u.Host != "openapi.alipay.com" || u.Query().Get("method") != "alipay.user.certify.open.certify" {
				t.Fatal("invalid signed launch")
			}
			status, e := ali.KYC233Query(context.Background(), token)
			if e != nil || status != "passed" {
				t.Fatal(status, e)
			}
			verify, link, e := ali.KYC233Preconsult(context.Background(), "Synthetic Person", "110101199001010015", strings.Repeat("s", 64), "https://pay.example.invalid/merchant/account/certificate?state=bound")
			if e != nil || verify != "verify" {
				t.Fatal(e)
			}
			u, _ = url.Parse(link)
			if u.Host != "openauth.alipay.com" || u.Query().Get("scope") != "id_verify" || u.Query().Get("cert_verify_id") != "verify" {
				t.Fatal("wrong certdoc OAuth launch")
			}
			status, e = ali.KYC233Consult(context.Background(), verify, "bound-code")
			if e != nil || status != "passed" {
				t.Fatal(status, e)
			}
			bad = true
			if _, e = ali.KYC233Query(context.Background(), token); e == nil {
				t.Fatal("forged result accepted")
			}
			if calls != 6 {
				t.Fatal("unexpected KYC retry", calls)
			}
		})
	}
}
