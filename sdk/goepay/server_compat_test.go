package goepay

// Repository-only cross-checks use the actual Go service implementation. The
// standalone public ZIP excludes this file and retains all portable tests.
import (
	"net/url"
	"testing"

	"goepay/internal/compat"
	"goepay/internal/notification"
	"goepay/internal/payment"
)

func TestRequestInteroperatesWithGoServer(t *testing.T) {
	c := client(t, "https://pay.example.com")
	target, err := c.SubmitURL(url.Values{"money": {"1.00"}, "name": {"中文 +%"}, "space": {" \t\n"}, "out_trade_no": {"order-217"}, "type": {"alipay"}})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	p, err := compat.Parse(u.Query())
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Verify(p, compat.Credentials{Algorithm: "RSA", PublicKey: &merchantKey.PublicKey}, testNow); err != nil {
		t.Fatalf("actual server rejected client request: %v", err)
	}
}

func TestNotificationInteroperatesWithGoServer(t *testing.T) {
	o := payment.Order{Version: "v2", MerchantID: 1001, TradeNo: "trade-217", OutTradeNo: "order-217", Name: "中文 +%", Type: "alipay", Amount: 100, Param: "0"}
	target, err := notification.URL("https://merchant.example.com/payments/notify?tenant=217&space=%20%09", o, payment.Merchant{}, platformKey, testNow)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	p, err := client(t, "https://pay.example.com").VerifyNotification(u.Query())
	if err != nil || p["money"] != "1.00" || p["tenant"] != "217" || p["space"] != " \t" {
		t.Fatalf("actual server notification rejected: %v", err)
	}
}

func TestReplyInteroperatesWithGoSDKProfile(t *testing.T) {
	p := compat.Params{"code": "0", "money": "1.00", "space": " \t\n\r\x00\x0b", "zero": "0", "timestamp": "1793400000", "sign_type": "RSA"}
	var err error
	p["sign"], err = compat.SignRSAWithProfile(p, platformKey, compat.ProfileSDKV2)
	if err != nil {
		t.Fatal(err)
	}
	if err := verify(map[string]string(p), &platformKey.PublicKey, testNow); err != nil {
		t.Fatalf("actual server response signature rejected: %v", err)
	}
}
