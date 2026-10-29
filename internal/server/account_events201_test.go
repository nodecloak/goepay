package server

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"goepay/internal/identity"
	"goepay/internal/payment"
	"goepay/internal/platform/jobs"
)

type eventSubscriptionView201 struct {
	EventURL            string   `json:"event_url"`
	EventTypes          []string `json:"event_types"`
	Fingerprint         string   `json:"fingerprint"`
	ConsecutiveFailures int      `json:"consecutive_failures"`
	LastDeliveryState   string   `json:"last_delivery_state"`
	SubscriptionNotice  string   `json:"subscription_notice"`
}

type eventFixture201 struct {
	channel168Fixture
	merchantToken, merchantCSRF string
	key                         *rsa.PrivateKey
}

func eventLocal201(t *testing.T) *eventFixture201 {
	t.Helper()
	f := &eventFixture201{channel168Fixture: channel168Local(t)}
	if _, err := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('events201','merchant',1,'Events201','unused-synthetic201')`); err != nil {
		t.Fatal(err)
	}
	var err error
	f.merchantToken, f.merchantCSRF, err = f.s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "events201", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	f.key, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *eventFixture201) request(method, suffix, body, token, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/merchant/account/event-subscription"+suffix, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func eventView201(t *testing.T, w *httptest.ResponseRecorder) eventSubscriptionView201 {
	t.Helper()
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("merchant refund event subscription HTTP=%d; want private successful account response; body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Code int                      `json:"code"`
		Data eventSubscriptionView201 `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Code != 0 {
		t.Fatal("invalid event subscription response", err)
	}
	if len(out.Data.Fingerprint) != 64 || strings.Trim(out.Data.Fingerprint, "0123456789abcdef") != "" || out.Data.EventTypes == nil {
		t.Fatal("event settings lack a protected revision or explicit event array", out.Data)
	}
	for _, private := range []string{"proof_hash", "lease_token", "private_key", "api_key"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("event account response exposed private source material", private)
		}
	}
	return out.Data
}

func (f *eventFixture201) view(t *testing.T) eventSubscriptionView201 {
	t.Helper()
	return eventView201(t, f.request("GET", "", "", f.merchantToken, ""))
}

func (f *eventFixture201) save(t *testing.T, endpoint string, types []string) eventSubscriptionView201 {
	t.Helper()
	prior := f.view(t)
	raw, err := json.Marshal(map[string]any{"event_url": endpoint, "event_types": types, "expected_fingerprint": prior.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	return eventView201(t, f.request("PATCH", "", string(raw), f.merchantToken, f.merchantCSRF))
}

func (f *eventFixture201) paidOrder(t *testing.T, suffix string) payment.Order {
	t.Helper()
	o, err := f.s.Store.CreateOrder(t.Context(), payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "订单/" + suffix, Name: "中文/退款201", Type: "alipay", Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.Store.MarkPaid(t.Context(), o.TradeNo, "paid201:"+suffix); err != nil {
		t.Fatal(err)
	}
	return o
}

func (f *eventFixture201) manual(t *testing.T, o payment.Order, suffix string, amount int64) payment.Refund {
	t.Helper()
	r, err := f.s.Store.ManualRefund(t.Context(), 1, o.TradeNo, "退款/"+suffix, amount, payment.IncomeHoldActor{ID: 168, Name: "admin168", Class: "super"}, "买家已收到线下退款", true)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The runtime interface keeps RED compilable before the worker is implemented.
// All successful paths still need a real captured job, lease, and store method.
type refundEventDelivery201 interface {
	DeliverMerchantRefundEvent201(context.Context, jobs.Job, *rsa.PrivateKey, *http.Client) error
}

type eventTransport201 func(*http.Request) (*http.Response, error)

func (f eventTransport201) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func eventResponse201(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func (f *eventFixture201) claim(t *testing.T) jobs.Job {
	t.Helper()
	j, err := (jobs.Queue{Pool: f.p}).Claim(t.Context(), []string{"merchant.refund-event"}, time.Minute)
	if err != nil {
		t.Fatal("successful platform refund has no claimable event job", err)
	}
	return j
}

func (f *eventFixture201) deliver(t *testing.T, j jobs.Job, transport eventTransport201) {
	t.Helper()
	delivery, ok := any(f.s.Store).(refundEventDelivery201)
	if !ok {
		t.Fatal("payment store does not deliver captured merchant refund events")
	}
	client := &http.Client{Transport: transport, Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if err := delivery.DeliverMerchantRefundEvent201(t.Context(), j, f.key, client); err != nil {
		t.Fatal("actual delivery outcome must be recorded without scheduling an automatic resend", err)
	}
	if err := (jobs.Queue{Pool: f.p}).Complete(t.Context(), j); err != nil {
		t.Fatal(err)
	}
}

func TestRefundEvent201HTTPSettingsPersistOriginalFieldsWithCASAndNoSend(t *testing.T) {
	f := eventLocal201(t)
	initial := f.view(t)
	if initial.EventURL != "" || len(initial.EventTypes) != 0 || initial.ConsecutiveFailures != 0 || initial.LastDeliveryState != "none" || initial.SubscriptionNotice != "" {
		t.Fatal("unconfigured subscription invented settings or delivery", initial)
	}
	const target = "https://events.example.invalid/中文/退款201"
	raw, _ := json.Marshal(map[string]any{"event_url": target, "event_types": []string{"refund"}, "expected_fingerprint": initial.Fingerprint})
	saved := eventView201(t, f.request("PATCH", "", string(raw), f.merchantToken, f.merchantCSRF))
	if saved.EventURL != target || len(saved.EventTypes) != 1 || saved.EventTypes[0] != "refund" || saved.Fingerprint == initial.Fingerprint {
		t.Fatal("original URL and refund subscription did not persist", saved)
	}
	if w := f.request("PATCH", "", string(raw), f.merchantToken, f.merchantCSRF); w.Code != 409 {
		t.Fatal("stale event settings write accepted", w.Code)
	}
	if after := f.view(t); after.Fingerprint != saved.Fingerprint || after.EventURL != target {
		t.Fatal("stale write changed event settings", after)
	}
	var sideEffects int
	if err := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM jobs)+(SELECT count(*) FROM orders)+(SELECT count(*) FROM journals)`).Scan(&sideEffects); err != nil || sideEffects != 0 {
		t.Fatal("event settings save sent a message or touched money", sideEffects, err)
	}
	disabled := f.save(t, "", []string{})
	if disabled.EventURL != "" || len(disabled.EventTypes) != 0 {
		t.Fatal("explicit empty subscription did not persist", disabled)
	}
}

func TestRefundEvent201HTTPRejectsAmbiguousUnsafeAndForeignWrites(t *testing.T) {
	f := eventLocal201(t)
	prior := f.view(t)
	valid := fmt.Sprintf(`{"event_url":"https://events.example.invalid/refund","event_types":["refund"],"expected_fingerprint":%q}`, prior.Fingerprint)
	for _, tc := range []struct{ name, method, suffix, body, token, csrf string }{
		{"missing session", "GET", "", "", "", ""},
		{"missing csrf", "PATCH", "", valid, f.merchantToken, ""},
		{"caller merchant", "PATCH", "", strings.TrimSuffix(valid, "}") + `,"merchant_id":"2"}`, f.merchantToken, f.merchantCSRF},
		{"caller query", "GET", "?merchant_id=2", "", f.merchantToken, ""},
		{"duplicate field", "PATCH", "", strings.TrimSuffix(valid, "}") + `,"event_url":"https://other.example.invalid"}`, f.merchantToken, f.merchantCSRF},
		{"unknown event", "PATCH", "", strings.Replace(valid, `["refund"]`, `["chargeback"]`, 1), f.merchantToken, f.merchantCSRF},
		{"duplicate event", "PATCH", "", strings.Replace(valid, `["refund"]`, `["refund","refund"]`, 1), f.merchantToken, f.merchantCSRF},
		{"private IP", "PATCH", "", strings.Replace(valid, "https://events.example.invalid/refund", "http://127.0.0.1/refund", 1), f.merchantToken, f.merchantCSRF},
		{"URL credentials", "PATCH", "", strings.Replace(valid, "https://events.example.invalid/refund", "https://user:secret@events.example.invalid/refund", 1), f.merchantToken, f.merchantCSRF},
		{"URL fragment", "PATCH", "", strings.Replace(valid, "https://events.example.invalid/refund", "https://events.example.invalid/refund#fragment", 1), f.merchantToken, f.merchantCSRF},
		{"oversized URL", "PATCH", "", strings.Replace(valid, "https://events.example.invalid/refund", "https://events.example.invalid/"+strings.Repeat("x", 500), 1), f.merchantToken, f.merchantCSRF},
		{"trailing JSON", "PATCH", "", valid + `{}`, f.merchantToken, f.merchantCSRF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := f.request(tc.method, tc.suffix, tc.body, tc.token, tc.csrf)
			if w.Code < 400 || w.Code >= 500 {
				t.Fatal("unsafe event settings boundary was accepted or crashed", w.Code)
			}
			if after := f.view(t); after.Fingerprint != prior.Fingerprint || after.EventURL != "" || len(after.EventTypes) != 0 {
				t.Fatal("rejected event request changed protected settings", after)
			}
		})
	}
}

func TestRefundEvent201SendsOriginalRSAFormAndUnescapedRefundContent(t *testing.T) {
	f := eventLocal201(t)
	const endpoint = "https://events.example.invalid/receive201"
	f.save(t, endpoint, []string{"refund"})
	o := f.paidOrder(t, "原始/字段")
	r := f.manual(t, o, "原始/字段", 30)
	j := f.claim(t)
	calls := 0
	f.deliver(t, j, func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != "POST" || req.URL.String() != endpoint || !strings.HasPrefix(req.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			t.Fatal("event is not the original form POST", req.Method, req.URL)
		}
		raw, err := io.ReadAll(io.LimitReader(req.Body, 32<<10))
		if err != nil {
			t.Fatal(err)
		}
		form, err := url.ParseQuery(string(raw))
		if err != nil || len(form) != 6 || form.Get("event") != "refund" || form.Get("pid") != "1" || form.Get("sign_type") != "RSA" {
			t.Fatal("original event form fields missing or polluted", form, err)
		}
		stamp, err := strconv.ParseInt(form.Get("timestamp"), 10, 64)
		if err != nil || stamp < time.Now().Add(-time.Minute).Unix() || stamp > time.Now().Add(time.Minute).Unix() {
			t.Fatal("event timestamp is not current integer seconds", form.Get("timestamp"))
		}
		content := form.Get("content")
		if strings.Contains(content, `\u`) || strings.Contains(content, `\/`) || !strings.Contains(content, "订单/原始/字段") || !strings.Contains(content, "退款/原始/字段") {
			t.Fatal("refund JSON lost original Unicode/slash representation", content)
		}
		var fields map[string]json.RawMessage
		if err = json.Unmarshal([]byte(content), &fields); err != nil || len(fields) != 9 {
			t.Fatal("original refund content shape changed", content, err)
		}
		for field, want := range map[string]string{"code": "0", "uid": "1", "refund_no": fmt.Sprintf("RF%d", r.ID), "out_refund_no": r.RefundNo, "trade_no": o.TradeNo, "out_trade_no": o.OutTradeNo} {
			got := strings.Trim(string(fields[field]), `"`)
			if got != want {
				t.Fatalf("refund content %s=%q want %q", field, got, want)
			}
		}
		for _, field := range []string{"money", "refundmoney", "reducemoney"} {
			got, err := strconv.ParseFloat(strings.Trim(string(fields[field]), `"`), 64)
			if err != nil || got != 0.30 {
				t.Fatal("refund content amount is not exact original yuan value", field, string(fields[field]), err)
			}
		}
		// Independently spell out the original Pro canonical order; do not use
		// the production canonicalizer to construct this test's expectation.
		canonical := "content=" + content + "&event=refund&pid=1&timestamp=" + form.Get("timestamp")
		digest := sha256.Sum256([]byte(canonical))
		signature, err := base64.StdEncoding.Strict().DecodeString(form.Get("sign"))
		if err != nil || rsa.VerifyPKCS1v15(&f.key.PublicKey, crypto.SHA256, digest[:], signature) != nil {
			t.Fatal("platform RSA SHA256 signature does not cover the original form")
		}
		return eventResponse201(200, "received Success!"), nil
	})
	if state := f.view(t); calls != 1 || state.LastDeliveryState != "delivered" || state.ConsecutiveFailures != 0 {
		t.Fatal("queue completion was mistaken for delivery, or acknowledged event not recorded", calls, state)
	}
}

func TestRefundEvent201ReadTimeSubscriptionAndURLControlCapturedEvent(t *testing.T) {
	f := eventLocal201(t)
	f.save(t, "https://old.example.invalid/event201", []string{"refund"})
	o := f.paidOrder(t, "read-time201")
	f.manual(t, o, "first201", 10)
	f.save(t, "https://new.example.invalid/event201", []string{"refund"})
	f.deliver(t, f.claim(t), func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://new.example.invalid/event201" {
			t.Fatal("delivery used stale captured subscription URL", req.URL)
		}
		return eventResponse201(200, "success"), nil
	})
	f.manual(t, o, "second201", 10)
	f.save(t, "https://new.example.invalid/event201", []string{})
	calls := 0
	f.deliver(t, f.claim(t), func(*http.Request) (*http.Response, error) {
		calls++
		return eventResponse201(200, "success"), nil
	})
	if calls != 0 {
		t.Fatal("disabled read-time subscription still sent a captured event", calls)
	}
}

func TestRefundEvent201Requires2xxAndExactAcknowledgementWithoutAutomaticResend(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		accepted   bool
	}{{"lowercase", "prefix success suffix", 200, true}, {"uppercase", "SUCCESS", 201, true}, {"titlecase", "Success", 202, true}, {"mixedcase", "sUcCeSs", 200, false}, {"empty", "", 200, false}, {"no content", "", 204, false}, {"error status", "success", 500, false}, {"redirect", "success", 302, false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := eventLocal201(t)
			f.save(t, "https://events.example.invalid/ack201", []string{"refund"})
			o := f.paidOrder(t, tc.name)
			f.manual(t, o, "ack201", 10)
			j := f.claim(t)
			calls := 0
			transport := eventTransport201(func(*http.Request) (*http.Response, error) {
				calls++
				return eventResponse201(tc.status, tc.body), nil
			})
			f.deliver(t, j, transport)
			state := f.view(t)
			wantState, wantFailures := "failed", 1
			if tc.accepted {
				wantState, wantFailures = "delivered", 0
			}
			if calls != 1 || state.LastDeliveryState != wantState || state.ConsecutiveFailures != wantFailures {
				t.Fatal("actual acknowledgement and recorded delivery disagree", calls, state)
			}
			if _, err := (jobs.Queue{Pool: f.p}).Claim(t.Context(), []string{"merchant.refund-event"}, time.Minute); !jobs.IsEmpty(err) {
				t.Fatal("actual event failure scheduled an automatic resend", err)
			}
			if delivery, ok := any(f.s.Store).(refundEventDelivery201); ok {
				_ = delivery.DeliverMerchantRefundEvent201(t.Context(), j, f.key, &http.Client{Transport: transport})
			}
			if calls != 1 || f.view(t).ConsecutiveFailures != wantFailures {
				t.Fatal("replayed completed job sent again or incremented independent failure count", calls)
			}
		})
	}
}

func TestRefundEvent201UnknownNetworkResultRemainsUnknownAndIsNeverResent(t *testing.T) {
	f := eventLocal201(t)
	f.save(t, "https://events.example.invalid/unknown201", []string{"refund"})
	o := f.paidOrder(t, "unknown-network201")
	f.manual(t, o, "unknown-network201", 10)
	j := f.claim(t)
	calls := 0
	transport := eventTransport201(func(req *http.Request) (*http.Response, error) {
		calls++
		if trace := httptrace.ContextClientTrace(req.Context()); trace != nil && trace.WroteRequest != nil {
			trace.WroteRequest(httptrace.WroteRequestInfo{})
		}
		return nil, io.ErrUnexpectedEOF
	})
	f.deliver(t, j, transport)
	state := f.view(t)
	if calls != 1 || state.LastDeliveryState != "unknown" || state.ConsecutiveFailures != 1 {
		t.Fatal("unknown outcome was treated as delivered or safe to retry", calls, state)
	}
	if delivery, ok := any(f.s.Store).(refundEventDelivery201); ok {
		_ = delivery.DeliverMerchantRefundEvent201(t.Context(), j, f.key, &http.Client{Transport: transport})
	}
	if calls != 1 || f.view(t).LastDeliveryState != "unknown" {
		t.Fatal("unknown result automatically resent or hidden by completed queue state", calls)
	}
}

func TestRefundEvent201TenIndependentFailuresCloseTypesKeepURLAndSuccessResetsCount(t *testing.T) {
	f := eventLocal201(t)
	const endpoint = "https://events.example.invalid/fail201"
	f.save(t, endpoint, []string{"refund"})
	o := f.paidOrder(t, "consecutive201")
	send := func(no int, body string) {
		f.manual(t, o, fmt.Sprintf("independent-%d", no), 1)
		f.deliver(t, f.claim(t), func(*http.Request) (*http.Response, error) { return eventResponse201(200, body), nil })
	}
	for i := 1; i <= 3; i++ {
		send(i, "rejected")
	}
	if state := f.view(t); state.ConsecutiveFailures != 3 || len(state.EventTypes) != 1 {
		t.Fatal("independent failed events were not counted", state)
	}
	send(4, "SUCCESS")
	if state := f.view(t); state.ConsecutiveFailures != 0 || state.LastDeliveryState != "delivered" {
		t.Fatal("acknowledgement did not clear consecutive failures", state)
	}
	for i := 1; i <= 10; i++ {
		send(4+i, "rejected")
		state := f.view(t)
		if i < 10 && (state.ConsecutiveFailures != i || len(state.EventTypes) != 1) {
			t.Fatal("subscription closed before ten independent failures", i, state)
		}
	}
	state := f.view(t)
	if state.EventURL != endpoint || len(state.EventTypes) != 0 || state.ConsecutiveFailures != 0 || state.LastDeliveryState != "failed" || state.SubscriptionNotice == "" {
		t.Fatal("tenth independent failure did not close types, retain URL, reset count and expose subscription notice", state)
	}
}
