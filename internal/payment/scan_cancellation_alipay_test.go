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
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

type localAlipayScan78 struct {
	provider      *adapter.Alipay
	attempt       ProviderAttempt
	request       adapter.ScanRequest
	pending       error
	callback      []byte
	cancellations *atomic.Int32
}

func alipayScanFixture78(t *testing.T, s *Store, o Order, action string) localAlipayScan78 {
	t.Helper()
	return alipayScanPendingCodeFixture78(t, s, o, action, "10003")
}
func alipayScanPendingCodeFixture78(t *testing.T, s *Store, o Order, action, code string) localAlipayScan78 {
	t.Helper()
	merchant, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	var cancellations atomic.Int32
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := r.ParseForm(); e != nil {
			t.Error(e)
			return
		}
		params := map[string]string{}
		for key, values := range r.PostForm {
			if len(values) != 1 {
				t.Error("duplicate original request field")
				return
			}
			params[key] = values[0]
		}
		if sig, e := adapter.SignAlipayRSA2(params, merchant); e != nil || sig != params["sign"] {
			t.Error("original merchant request signature missing")
			return
		}
		var biz map[string]any
		if json.Unmarshal([]byte(params["biz_content"]), &biz) != nil || biz["out_trade_no"] != o.TradeNo {
			t.Error("changed original barcode number")
			return
		}
		node := map[string]string{"code": "10000", "out_trade_no": o.TradeNo, "app_id": "app78", "seller_id": "seller78"}
		field := ""
		switch params["method"] {
		case "alipay.trade.pay":
			if biz["auth_code"] != "280000000000000078" || biz["total_amount"] != "1.23" {
				t.Error("changed original barcode or amount")
			}
			field = "alipay_trade_pay_response"
			node["code"] = code
			node["total_amount"] = "1.23"
		case "alipay.trade.query":
			field = "alipay_trade_query_response"
			node["trade_no"] = "ALI78_LOCAL"
			node["trade_status"] = "WAIT_BUYER_PAY"
			node["total_amount"] = "1.23"
		case "alipay.trade.cancel":
			cancellations.Add(1)
			if len(biz) != 1 {
				t.Error("cancellation changed original request payload")
			}
			var claims, audits int
			if e := s.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM scan_cancellation_claims),(SELECT count(*) FROM backoffice_audit WHERE resource='scan-cancellations')`).Scan(&claims, &audits); e != nil || claims != 1 || audits != 1 {
				t.Error("cancellation I/O preceded committed permanent barrier", claims, audits, e)
			}
			field = "alipay_trade_cancel_response"
			node["trade_no"] = "ALI78_LOCAL"
			node["retry_flag"] = "N"
			node["action"] = action
		default:
			t.Error("unexpected financial endpoint", params["method"])
			http.Error(w, "unexpected78", 500)
			return
		}
		raw, _ := json.Marshal(node)
		digest := sha256.Sum256(raw)
		sig, e := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		if e != nil {
			t.Error(e)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{field: json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(sig)})
	}))
	t.Cleanup(up.Close)
	p, e := adapter.NewAlipay(adapter.AlipayConfig{EnableScanCancellation: true, AppID: "app78", SellerID: "seller78", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: up.URL, HTTPClient: up.Client(), Methods: []string{"scan"}})
	if e != nil {
		t.Fatal(e)
	}
	scope, _ := adapter.TransferAccountScope(p)
	a, owner, e := s.BeginRecoverableScanAttempt(context.Background(), o.TradeNo, 7, "app78", "280000000000000078", scope)
	if e != nil || !owner {
		t.Fatal(e)
	}
	request := adapter.ScanRequest{TradeNo: o.TradeNo, Subject: o.Name, Amount: o.Amount, ClientIP: "192.0.2.78", AuthCode: "280000000000000078", NotifyURL: "https://local.invalid/callback"}
	_, pending := p.Scan(context.Background(), request)
	if _, valid := adapter.VerifiedScanPending(pending); !valid {
		t.Fatal("local Alipay pending response is not authenticated", pending)
	}
	fields := map[string]string{"app_id": "app78", "seller_id": "seller78", "out_trade_no": o.TradeNo, "trade_no": "ALI78_LOCAL", "trade_status": "TRADE_SUCCESS", "total_amount": "1.23", "sign_type": "RSA2"}
	notificationDigest := sha256.Sum256([]byte(adapter.AlipayNotificationCanonical(fields)))
	notificationSignature, e := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, notificationDigest[:])
	if e != nil {
		t.Fatal(e)
	}
	fields["sign"] = base64.StdEncoding.EncodeToString(notificationSignature)
	form := url.Values{}
	for key, value := range fields {
		form.Set(key, value)
	}
	if _, e = p.VerifyNotification(context.Background(), nil, []byte(form.Encode()), adapter.ExpectedPayment{TradeNo: o.TradeNo, Amount: o.Amount}); e != nil {
		t.Fatal("synthetic notification fixture is not independently authenticated", e)
	}
	return localAlipayScan78{provider: p, attempt: a, request: request, pending: pending, callback: []byte(form.Encode()), cancellations: &cancellations}
}

func TestScanCancellation78AlipayOriginalTLSFinalityClosesWithoutIncome(t *testing.T) {
	for _, mode := range []string{"close", "refund", "detached-context"} {
		t.Run(mode, func(t *testing.T) {
			s, _, o := recoveryOriginFixtureProvider78(t, "alipay")
			ctx := context.Background()
			action := mode
			if mode == "detached-context" {
				action = "close"
			}
			fixture := alipayScanFixture78(t, s, o, action)
			p := fixture.provider
			a := fixture.attempt
			if e := s.RecordProviderAttemptResult(ctx, a.ID, "unknown", "", "", ""); e != nil {
				t.Fatal(e)
			}
			if e := s.RecordScanPending(ctx, a.ID, "app78", fixture.request, fixture.pending); e != nil {
				t.Fatal(e)
			}
			job, _ := claimScanFixtureJob78(t, s)
			sixPendingScanQueries78(t, s, a, p, job)
			scope, _ := adapter.TransferAccountScope(p)
			claim, owner, e := s.ClaimScanCancellation(ctx, job.ID, job.Token, scope)
			if e != nil || !owner {
				t.Fatal("original cancel claim unavailable", owner, e)
			}
			request := adapter.ScanCancellationRequest{TradeNo: claim.TradeNo, ProviderTradeNo: claim.ProviderTradeNo, Amount: claim.Amount}
			if mode == "detached-context" {
				request.ProviderTradeNo = ""
			}
			result, e := p.CancelScan(ctx, request)
			if e != nil {
				t.Fatal("local signed TLS cancel response failed", e)
			}
			if _, e = s.pool.Exec(ctx, "UPDATE jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", job.ID); e != nil {
				t.Fatal(e)
			}
			if mode == "detached-context" {
				if e = s.RecordScanCancellation(ctx, a.ID, job.Token, result, nil); !errors.Is(e, ErrConflict) {
					t.Fatal("different original query context finalized cancellation", e)
				}
				current, e := s.OrderByTradeNo(ctx, o.TradeNo)
				if e != nil || current.Status != "unknown" {
					t.Fatal("detached response closed original order", current.Status, e)
				}
				if invalid, unknown := scanLedgerCounters78(t, s); invalid != 0 || unknown != 1 {
					t.Fatal("detached acknowledgement hid original financial uncertainty", invalid, unknown)
				}
				return
			}
			// Saving the already authenticated response uses its permanent original
			// claim, even after the renewable worker lease or merchant has expired.
			if _, e = s.pool.Exec(ctx, "UPDATE merchants SET enabled=false WHERE id=78"); e != nil {
				t.Fatal(e)
			}
			for i := 0; i < 2; i++ {
				if e = s.RecordScanCancellation(ctx, a.ID, job.Token, result, nil); e != nil {
					t.Fatal("valid late original ACK was lost", e)
				}
			}
			current, e := s.OrderByTradeNo(ctx, o.TradeNo)
			if e != nil || current.Status != "closed" || fixture.cancellations.Load() != 1 {
				t.Fatal("final cancel missing or resent", current.Status, fixture.cancellations.Load(), e)
			}
			flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}}
			if e = flow.HandleNotification(ctx, 7, o.TradeNo, nil, fixture.callback); !errors.Is(e, ErrScanCancelled) {
				t.Fatal("Alipay callback reopened cancelled payment", e)
			}
			b, e := s.Balance(ctx, 78)
			if e != nil || b.Available != 0 || b.Reserved != 0 {
				t.Fatal("cancelled pending order generated money", b, e)
			}
			if invalid, unknown := scanLedgerCounters78(t, s); invalid != 0 || unknown != 0 {
				t.Fatal("authenticated final cancellation remained unresolved", invalid, unknown)
			}
		})
	}
}
