package contract_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestPreauthRemaining75HTTPRealSignedTLSMandatoryGrantAndOriginalReadRecovery(t *testing.T) {
	for _, role := range []string{"admin", "merchant"} {
		t.Run(role, func(t *testing.T) {
			s, p, key := fixture(t)
			ctx := context.Background()
			s.Sandbox = false
			s.AuthorizationObservationEnabled = true
			s.Sessions = identity.NewPersistentSessions(p, false)
			s.FundsActionResolver = s.ResolveFundsAction
			platform, e := rsa.GenerateKey(rand.Reader, 2048)
			if e != nil {
				t.Fatal(e)
			}
			trade := ""
			writes, reads := 0, 0
			ua := ""
			badRecovery := true
			up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if e := r.ParseForm(); e != nil {
					t.Error(e)
					return
				}
				params := map[string]string{}
				for k, v := range r.PostForm {
					if len(v) != 1 {
						t.Error("duplicate vendor field")
					}
					params[k] = v[0]
				}
				sig, e := adapter.SignAlipayRSA2(params, key)
				if e != nil || sig != params["sign"] {
					t.Error("invalid original request signature", e)
				}
				var b map[string]any
				if json.Unmarshal([]byte(params["biz_content"]), &b) != nil {
					t.Error("invalid vendor body")
				}
				node := map[string]any{}
				field := ""
				switch params["method"] {
				case "alipay.trade.query":
					reads++
					field = "alipay_trade_query_response"
					node = map[string]any{"code": "40004", "sub_code": "ACQ.TRADE_NOT_EXIST"}
				case "alipay.fund.auth.order.unfreeze":
					writes++
					ua, _ = b["out_request_no"].(string)
					if b["amount"] != "4.50" || !strings.HasPrefix(ua, "UA") || b["auth_no"] != "remaining-http-auth75" {
						t.Error("wrong effective unfreeze", b)
					}
					w.WriteHeader(503)
					return
				case "alipay.fund.auth.operation.detail.query":
					reads++
					field = "alipay_fund_auth_operation_detail_query_response"
					request, _ := b["out_request_no"].(string)
					amount, rest, op := "10.00", "4.50", "FREEZE"
					if strings.HasPrefix(request, "UA") {
						if request != ua {
							t.Error("replacement recovery request", request, ua)
						}
						amount, rest, op = "4.50", "0.00", "UNFREEZE"
						if badRecovery {
							amount = "4.51"
						}
					}
					if b["out_order_no"] != trade {
						t.Error("wrong original freeze", b)
					}
					node = map[string]any{"code": "10000", "out_order_no": trade, "out_request_no": request, "auth_no": "remaining-http-auth75", "payer_user_id": "2088000000000075", "amount": amount, "rest_amount": rest, "status": "SUCCESS", "order_status": "AUTHORIZED", "operation_type": op}
				default:
					t.Error("unexpected vendor operation", params["method"])
					w.WriteHeader(500)
					return
				}
				raw, _ := json.Marshal(node)
				d := sha256.Sum256(raw)
				signature, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, d[:])
				json.NewEncoder(w).Encode(map[string]any{field: json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(signature)})
			}))
			defer up.Close()
			ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "2021000000000075", SellerID: "2088000000000075", PrivateKey: key, PublicKey: &platform.PublicKey, Methods: []string{"preauth"}, HTTPClient: &http.Client{Transport: oauthContractTLSTransport{up, t}}})
			if e != nil {
				t.Fatal(e)
			}
			s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: ali}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
			s.MerchantChannels = map[int64][]int64{1: {7}}
			o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "remaining-http75", Name: "synthetic remainder", Type: "alipay", Amount: 1000})
			if e != nil {
				t.Fatal(e)
			}
			trade = o.TradeNo
			scope, e := adapter.TransferAccountScope(ali)
			if e != nil {
				t.Fatal(e)
			}
			if _, _, e = s.Store.BeginProviderAttemptWithAccount(ctx, trade, 7, "preauth", ali.ApplicationID(), "", scope); e != nil {
				t.Fatal(e)
			}
			if e = s.Store.RecordPreauthorization(ctx, 7, adapter.AuthorizationEvent{TradeNo: trade, RequestNo: trade, AuthNo: "remaining-http-auth75", BuyerID: "2088000000000075", Amount: 1000, State: "SUCCESS"}); e != nil {
				t.Fatal(e)
			}
			user := identity.User{Username: role, Role: role, ID: 1, AuthVersion: 1}
			if role == "admin" {
				user.AdminClass = "super"
			}
			token, csrf, e := s.Sessions.Create(user)
			if e != nil {
				t.Fatal(e)
			}
			call := func(path, body, grant, csrfValue string) *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", "/console/v1/"+role+path, strings.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("X-CSRF-Token", csrfValue)
				if grant != "" {
					r.Header.Set("X-Payment-Authorization", grant)
				}
				r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, r)
				return w
			}
			base := "/orders/" + trade
			observed := call(base+"/authorization-observations", `{"client_request_no":"`+strings.Repeat("7", 64)+`"}`, "", csrf)
			if observed.Code != 200 || reads != 1 {
				t.Fatal("original observation", observed.Code, observed.Body.String(), reads)
			}
			id := lookupData(t, observed)["request_id"].(string)
			target := base + "/release-remaining-authorization"
			body := fmt.Sprintf(`{"expected_observation_id":"%s","expected_remaining_cents":"450","expected_source_revision":"0"}`, id)
			if w := call(target, body, "", ""); w.Code != 403 || reads != 1 || writes != 0 {
				t.Fatal("CSRF reached vendor", w.Code, w.Body.String())
			}
			for _, bad := range []string{`{}`, strings.Replace(body, `"450"`, `450`, 1), strings.Replace(body, `"450"`, `"0"`, 1), strings.Replace(body, `"0"`, `"100"`, 1), strings.TrimSuffix(body, "}") + `,"amount":"4.50"}`, strings.TrimSuffix(body, "}") + `,"expected_source_revision":"0"}`} {
				if w := call(target, bad, "", csrf); w.Code != 400 || reads != 1 || writes != 0 {
					t.Fatal("ambiguous remainder intent", w.Code, w.Body.String(), reads, writes)
				}
			}
			if w := call(target+"?x=1", body, "", csrf); w.Code != 400 || reads != 1 || writes != 0 {
				t.Fatal("query parameters reached vendor", w.Code, w.Body.String())
			}
			if role == "admin" {
				originalToken, originalCSRF := token, csrf
				if _, e = p.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class)VALUES('remaining-operator75','admin',75,'operator','unused','operator')`); e != nil {
					t.Fatal(e)
				}
				token, csrf, e = s.Sessions.Create(identity.User{Username: "remaining-operator75", Role: "admin", ID: 75, AuthVersion: 1, AdminClass: "operator"})
				if e != nil {
					t.Fatal(e)
				}
				if w := call(target, body, "", csrf); w.Code != 403 || reads != 1 || writes != 0 {
					t.Fatal("operator acquired remaining release authority", w.Code, w.Body.String())
				}
				token, csrf = originalToken, originalCSRF
			}
			grant := ""
			if role == "admin" {
				if w := call(target, body, "", csrf); w.Code != 428 || reads != 1 || writes != 0 {
					t.Fatal("optional-global-off bypassed mandatory confirmation", w.Code, w.Body.String())
				}
				funds := identity.NewFundsAuthorization(p, nil)
				if e = funds.SetPassword(ctx, identity.FundsActor{User: user, SessionToken: token, ClientIP: "192.0.2.75"}, "contract-password-123", "", "remaining-local-funds-password75", ""); e != nil {
					t.Fatal(e)
				}
				challenge, _ := json.Marshal(map[string]any{"target": "/console/v1/admin" + target, "body": json.RawMessage(body)})
				w := call("/security/funds-authorizations/challenges", string(challenge), "", csrf)
				if w.Code != 201 {
					t.Fatal("concrete preview", w.Code, w.Body.String())
				}
				d := lookupData(t, w)
				summary := d["summary"].(map[string]any)
				if summary["release_remaining_amount"] != "4.50" || summary["release_source_revision"] != "0" {
					t.Fatal(summary)
				}
				confirm, _ := json.Marshal(map[string]any{"challenge_id": d["challenge_id"], "payment_password": "remaining-local-funds-password75"})
				w = call("/security/funds-authorizations/confirm", string(confirm), "", csrf)
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				grant = lookupData(t, w)["authorization"].(string)
			}
			w := call(target, body, grant, csrf)
			if w.Code != 202 || writes != 1 {
				t.Fatal("bound initial send", w.Code, w.Body.String(), reads, writes)
			}
			d := lookupData(t, w)
			if d["amount"] != "450" || d["original_amount"] != "1000" || d["source_revision"] != "1" || d["sent"] != true || d["state"] != "unknown" {
				t.Fatal("remaining source view", d)
			}
			// A signed but wrong amount must remain unknown; never retry the write.
			w = call(base+"/query-authorization-release", `{}`, "", csrf)
			if w.Code != 202 || writes != 1 {
				t.Fatal("wrong signed recovery accepted", w.Code, w.Body.String(), writes)
			}
			s.Sessions.Delete(token)
			token, csrf, e = s.Sessions.Create(user)
			if e != nil {
				t.Fatal(e)
			}
			s.ProviderFlow.AllowLive = false
			badRecovery = false
			w = call(base+"/query-authorization-release", `{}`, "", csrf)
			if w.Code != 200 || lookupData(t, w)["state"] != "succeeded" || writes != 1 {
				t.Fatal("original readonly recovery after revocation", w.Code, w.Body.String(), writes)
			}
			var journals int
			if e = p.QueryRow(ctx, "SELECT count(*) FROM journals").Scan(&journals); e != nil || journals != 0 {
				t.Fatal("buyer release created merchant ledger", journals, e)
			}
			report, e := s.Store.CheckLedger(ctx)
			if e != nil || report != (payment.LedgerCheck{}) {
				t.Fatal(report, e)
			}
		})
	}
}
