package contract_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goepay/internal/compat"
)

func TestGlobalAccess179GlobalCloseCannotBeOverriddenByPersonalAndLegacyKey(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint(legacy), func(t *testing.T) {
			f, p, o := merchant174Runtime(t)
			merchant174Set(t, f, 1, 1, 0)
			if legacy {
				state := signatureState163(t, f.request("GET", "signature-policy", "", f.csrf))
				signatureState163(t, f.request("PATCH", "signature-policy", signaturePatch163(state["fingerprint"].(string), 0), f.csrf))
			}
			globalSetting179(t, f, "user_refund", "0")
			path := "/api/pay/refund"
			params := compat.Params{"out_trade_no": o.OutTradeNo, "out_refund_no": "global-personal179", "money": "1.00"}
			if legacy {
				path = "/api.php"
				params["act"] = "refund"
			}
			out := merchant174Invoke(t, f, path, params)
			var rows int
			if e := f.p.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM refunds)+(SELECT count(*) FROM provider_operations WHERE kind='refund')`).Scan(&rows); e != nil || rows != 0 || p.refundWrites.Load() != 0 || out["code"] != float64(-1) {
				t.Fatal("global close bypassed by personal/legacy", out, rows, p.refundWrites.Load(), e)
			}
		})
	}
}

func TestGlobalAccess179PersonalAPIClosureRetainsOwnSelfAndInternalRefund(t *testing.T) {
	for _, internal := range []bool{false, true} {
		t.Run(fmt.Sprint(internal), func(t *testing.T) {
			f, p, o := merchant174Runtime(t)
			merchant174Set(t, f, 1, 0, 0)
			if internal {
				globalSetting179(t, f, "user_refund", "0")
				if _, e := f.s.ProviderFlow.Refund(context.Background(), 2, o.TradeNo, "internal179", 100); e != nil {
					t.Fatal("platform closure leaked to internal authorized business", e)
				}
			} else {
				globalSetting179(t, f, "user_refund", "1")
				w := merchantRequest179(f, "POST", "orders/"+o.TradeNo+"/refund", `{"amount":"1.00","external_no":"self-personal179"}`)
				if w.Code != 200 && w.Code != 202 {
					t.Fatal("personal API-only close blocked own self refund", w.Code)
				}
			}
			if p.refundWrites.Load() != 1 {
				t.Fatal("positive refund did not send exactly once", p.refundWrites.Load())
			}
		})
	}
}

func TestGlobalAccess179ClosedRefundPreservesOriginalQueryAndFunds(t *testing.T) {
	f, p, o := merchant174Runtime(t)
	params := compat.Params{"out_trade_no": o.OutTradeNo, "out_refund_no": "recover-query179", "money": "1.00"}
	if out := merchant174Invoke(t, f, "/api/pay/refund", params); out["code"] != float64(0) {
		t.Fatal(out)
	}
	globalSetting179(t, f, "user_refund", "0")
	merchant174Set(t, f, 1, 0, 0)
	if out := merchant174Invoke(t, f, "/api/pay/refundquery", compat.Params{"out_refund_no": "recover-query179"}); out["code"] != float64(0) {
		t.Fatal("original refund query was blocked", out)
	}
	if p.refundWrites.Load() != 1 {
		t.Fatal("query resent refund", p.refundWrites.Load())
	}
	var state string
	if e := f.p.QueryRow(context.Background(), `SELECT status FROM refunds WHERE refund_no='recover-query179'`).Scan(&state); e != nil || state != "succeeded" {
		t.Fatal("trusted original outcome did not resolve", state, e)
	}
	if _, e := f.s.Store.CheckLedger(context.Background()); e != nil {
		t.Fatal(e)
	}
}

func TestGlobalAccess179KeyCloseKeepsPasswordSessionAndSignedPayment(t *testing.T) {
	f, _, _ := merchant174Runtime(t)
	globalSetting179(t, f, "close_keylogin", "1")
	m, e := f.s.Store.Merchant(context.Background(), 2)
	if e != nil {
		t.Fatal(e)
	}
	if w := merchant174KeyLogin(f, "2", m.APIKey, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := f.request("GET", "api-switches", "", f.csrf); w.Code != 200 {
		t.Fatal("existing session disabled", w.Code)
	}
	r := httptest.NewRequest("POST", "/console/v1/merchant/login", strings.NewReader(`{"username":"merchant","password":"contract-password-123","code":""}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatal("password login disabled", w.Code)
	}
	if out := merchant174Invoke(t, f, "/api/pay/create", compat.Params{"out_trade_no": "keyclosed-new-payment179", "type": "alipay", "method": "native", "money": "1.00", "name": "local synthetic", "channel_id": "7"}); out["code"] != float64(0) {
		t.Fatal("web key close disabled payment signature", out)
	}
}

func TestGlobalAccess179KeyWaitRechecksCurrentGlobalState(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		t.Run(fmt.Sprint(prepared), func(t *testing.T) {
			f := signatureFixture(t, 2)
			c := runtimeAdmin179(t, f)
			var secret string
			if prepared {
				secret, f = merchant174EnrollMFA(t, f)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			m, e := f.s.Store.Merchant(ctx, 2)
			if e != nil {
				t.Fatal(e)
			}
			block, e := f.p.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer block.Rollback(context.Background())
			sql := `SELECT id FROM merchants WHERE id=2 FOR UPDATE`
			if prepared {
				sql = `SELECT username FROM console_mfa WHERE username='merchant' AND role='merchant' FOR UPDATE`
			}
			if _, e = block.Exec(ctx, sql); e != nil {
				t.Fatal(e)
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				code := ""
				if prepared {
					code = merchant174TOTP(t, secret, time.Now())
				}
				done <- merchant174KeyLogin(f, "2", m.APIKey, code)
			}()
			merchant174WaitForBlocker(t, f, block.Conn().PgConn().PID(), ctx)
			state := platformState179(t, platformAdmin179(c, "GET", "settings/merchant-access", "", c.csrf))
			platformState179(t, platformAdmin179(c, "PATCH", "settings/merchant-access", platformPatch179(state["fingerprint"].(string), "1", "1"), c.csrf))
			if e = block.Commit(ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case w := <-done:
				if w.Code != 401 || len(w.Result().Cookies()) != 0 {
					t.Fatal("waited key login ignored current global state", w.Code)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}
