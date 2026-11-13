package cryptopay

import (
	"context"
	"errors"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"net/url"
	"testing"
	"time"
)

func TestCompatibilityNotificationKeepsOriginalCNYAndSignsAssetDetails(t *testing.T) {
	v := Invoice{ID: "invoice", MerchantID: 1, OutTradeNo: "merchant-order", Network: "tron", Amount: "13.123456", CNYAmount: 10000, Name: "product", Version: "v1", State: "paid"}
	m := payment.Merchant{ID: 1, APIKey: "test-key"}
	raw, e := NotificationURL("https://merchant.example/callback?custom=1", v, m, nil, time.Unix(123, 0), false)
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(raw)
	p, _ := compat.Parse(u.Query())
	if p["money"] != "100.00" || p["crypto_amount"] != "13.123456" || p["network"] != "tron" || p["trade_no"] != "USDTinvoice" || p["sign"] != compat.SignMD5(p, m.APIKey) {
		t.Fatal(p)
	}
	v.Version = "native"
	raw, e = NotificationURL("https://merchant.example/callback", v, m, nil, time.Unix(123, 0), false)
	if e != nil {
		t.Fatal(e)
	}
	u, _ = url.Parse(raw)
	if u.Query().Has("money") || u.Query().Get("amount") != "13.123456" {
		t.Fatal(u.Query())
	}
}

type missingMerchant struct{}

func (missingMerchant) Merchant(context.Context, int64) (payment.Merchant, error) {
	return payment.Merchant{}, errors.New("unavailable")
}
func TestNotificationConstructionFailureBacksOffInsteadOfBlockingQueue(t *testing.T) {
	s := testService(t)
	v := invoice(t, s, "notify-failure")
	_, e := s.Pool.Exec(t.Context(), "UPDATE crypto_invoices SET notify_url='https://merchant.example/notify' WHERE id=$1", v.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RecordDeposit(t.Context(), Deposit{Network: "tron", TxID: "notifyfund", BlockHeight: 1, BlockHash: "b", Address: v.Address, Amount: "10", OccurredAt: v.ExpiresAt.Add(-time.Minute)}); e != nil {
		t.Fatal(e)
	}
	n := Notifier{Service: s, Merchants: missingMerchant{}}
	did, e := n.RunOne(t.Context())
	if !did || e == nil {
		t.Fatal(did, e)
	}
	var attempts int
	var deferred bool
	e = s.Pool.QueryRow(t.Context(), "SELECT attempts,next_attempt>clock_timestamp() FROM crypto_notifications WHERE invoice_id=$1", v.ID).Scan(&attempts, &deferred)
	if e != nil || attempts != 1 || !deferred {
		t.Fatalf("poisoned callback remains head %d %v %v", attempts, deferred, e)
	}
}
