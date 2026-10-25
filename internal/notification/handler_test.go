package notification

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"goepay/internal/platform/jobs"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestV2OutboundWhitespaceMatchesMerchantSDKProfile(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	target, err := URL("https://merchant.example/notify?extension=%20%20", payment.Order{Version: "v2", TradeNo: "123", MerchantID: 1, OutTradeNo: "m1", Amount: 100, Param: "   "}, payment.Merchant{}, key, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	p, err := compat.Parse(parsed.Query())
	if err != nil {
		t.Fatal(err)
	}
	if !compat.VerifyRSAWithProfile(p, p["sign"], &key.PublicKey, compat.ProfileSDKV2) {
		t.Fatal("outbound notification cannot be verified by original V2 merchant SDK")
	}
}

type fakeRepo struct {
	o payment.Order
	m payment.Merchant
}

func (f fakeRepo) OrderByTradeNo(context.Context, string) (payment.Order, error) { return f.o, nil }
func (f fakeRepo) Merchant(context.Context, int64) (payment.Merchant, error)     { return f.m, nil }
func TestNotifyCapturesSignedGETAndAck(t *testing.T) {
	var captured compat.Params
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("method %s", r.Method)
		}
		captured, _ = compat.Parse(r.URL.Query())
		io.WriteString(w, "success")
	}))
	defer target.Close()
	repo := fakeRepo{o: payment.Order{TradeNo: "123", MerchantID: 1, OutTradeNo: "m1", Type: "alipay", Name: "中文 & goods", Amount: 101, NotifyURL: target.URL, Status: "paid", Version: "v1"}, m: payment.Merchant{ID: 1, APIKey: "secret"}}
	h := Handler{Store: repo, Client: target.Client()}
	if e := h.Handle(context.Background(), jobs.Job{Kind: "payment.notify", Payload: []byte(`{"trade_no":"123"}`)}); e != nil {
		t.Fatal(e)
	}
	if captured["money"] != "1.01" || captured["trade_status"] != "TRADE_SUCCESS" || !compat.VerifyMD5(captured, "secret", captured["sign"]) {
		t.Fatalf("invalid notification %+v", captured)
	}
}
func TestV2NotificationFreshSignatureAndStrictAck(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Unix(1800000000, 0)
	var timestamps []string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, e := compat.Parse(r.URL.Query())
		if e != nil || !compat.VerifyRSA(p, p["sign"], &key.PublicKey) {
			t.Error("RSA verify failed")
		}
		timestamps = append(timestamps, p["timestamp"])
		io.WriteString(w, "prefix success suffix")
	}))
	defer target.Close()
	repo := fakeRepo{o: payment.Order{TradeNo: "123", MerchantID: 1, OutTradeNo: "m1", Amount: 100, NotifyURL: target.URL, Status: "paid", Version: "v2"}}
	h := Handler{Store: repo, Client: target.Client(), PlatformKey: key, Now: func() time.Time { return now }}
	for i := 0; i < 2; i++ {
		if e = h.Handle(context.Background(), jobs.Job{Kind: "payment.notify", Payload: []byte(`{"trade_no":"123"}`)}); e == nil {
			t.Fatal("non-exact ack accepted")
		}
		now = now.Add(time.Hour)
	}
	if len(timestamps) != 2 || timestamps[0] == timestamps[1] {
		t.Fatal("retry timestamp stale")
	}
	for _, ts := range timestamps {
		if _, e = strconv.ParseInt(ts, 10, 64); e != nil {
			t.Fatal(e)
		}
	}
}
func TestBoundedResponse(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, strings.Repeat("x", 8192)+"success") }))
	defer target.Close()
	h := Handler{Store: fakeRepo{o: payment.Order{Status: "paid", NotifyURL: target.URL, Version: "v1"}}, Client: target.Client()}
	if e := h.Handle(context.Background(), jobs.Job{Kind: "payment.notify", Payload: []byte(`{"trade_no":"123"}`)}); e == nil {
		t.Fatal("oversize ack accepted")
	}
}
