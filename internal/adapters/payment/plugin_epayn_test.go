package payment

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Fixed 2048-bit keypair; the signatures below were computed independently
// with node's crypto module (RSA-SHA256 over the ksort'd "k=v&" base).
const epaynTestPriv = "MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQCdw+flv0+n7/mM0hiZe1CJzfx4Jw8hoSr4UVB013DIXztS5PrscfdougWCWmAHQAXVfv+AAhMN08uabo0aQrvjYPpw/y+WXYex0MchHb78GOi1SPpdL9ZKiugHGvXkI99vh0k3SZCTwmtRU/PIyFlLTAWm184DrQfMKa0ukF7DXA3MFDX2vXTtnZCXyq/wEUKuSiwm+oMYoCUmKsVfnLOONY5GOPCs7aqu3wzRJ51m/BXi5lh6mlR2nOvJYdnInr701io8wJoedS+4OisEF1jrihQmKgO6jnUOF6mD8oz0RtQ5mL2YjBOHM0WrOhPyCFjxdRGuJEF5balArt0edkdbAgMBAAECggEACe/SwDSbuqBRpQN/UNIktqahPht5B1gPN02+gLrTG+PCU6HK24VVcnoI/8wGcV51ykaHUZOP31aVjo1jriGW6PxCOfIbbV/zEd7sXpMbj5BYE+xb90Ock9xflJb89tjQPsY+ynsuiTKn2hsCEgWVzjl5v6enDlyMea5ZqJhSR6wAGOD+KiHfK5zYt0tA25hPLapmhrIdLI7WDx69TA5CYu97hJ6drfXBXEZIkT+3C14heVOAgnXPB9CPslSCa2Wvrv7AwucDzfxFSx+y1QDKSUxlv+9uK5tED36a8qL8NXEfeS7I3AEyZehIc7AGSXJFKcVO4cjeCWRlcJ3fMHCnYQKBgQDW8ZmXCh6ViMT1/EtDze8G2F3P6ogE8JwhhcLvphh7sZBgc36ygH+5cbdwIBMC/NuwW9Ig3psFgI5JH8WCGL0at64B2kTUBMeGqlQKPiGfh4ggbdO7Wv3+aqqjViyIV/YnwDSc+UQTqdTdfa7GS2U3gTmkNslB5Lv/6guE7gBTYQKBgQC75l7aziWWhhONZjH2e/d/KGG1B5QasyRhW/NBqLdoRonsynssNEyo/xN8mGvQNcXBuv1SdIYVoNFsROa/p9qWUJ/GSyht8RKTzzBbIUtiVSHwo87pWV3QvAF3wpcB8sdXEReDDCkkJi1RbfDj8WrFJy/h/ghkIH9AUgosC00QOwKBgF0uMbJLvcR/nTsyW4KSdQvXopuvBbZ2hIj3zvFLZRPCf1+A0i0aDcTb6nUeQn+qFKcYtzhuqG+zMnu63Umv5dL+S94bvHjZdZIIpxQlqTfaboX4drCjGrc/kKfshLO6xkinR2fBm+SWXGSExs7KAqAUUmPBKHB7c5DW6pV/A/8hAoGBAJW7ucEBX8C4rWITmyMBnLOu+VWqaWJxeT9AZ/Pdv/IPrB+PJ2KNdr2BLwUt7VRpVWLRYMdRZfxcQCbYLU1zNGRucTQydlBOOHCjPbSRLqJ4luhNkTgw5+rLtxu5bF64GhQO4b6jdTGCXsCyLLFok53yYVzk88jegKk/9y0sAEg7AoGAQUrYgYcKIgy9Nz2JYFe1zdt6XihRNSMojCdhQsEx6KOH+k2wGUPJplKs1v7ScHS3sXI6YBn3yntCdtt3sT075CydswjblucYMsZSoVidmWp7IG6iYRgqx+fkaWT7Y8CvB1vzgym0WtpVzw1KjcDTJKH3xb6leSW/6vw6K5NHV/E="
const epaynTestPub = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAncPn5b9Pp+/5jNIYmXtQic38eCcPIaEq+FFQdNdwyF87UuT67HH3aLoFglpgB0AF1X7/gAITDdPLmm6NGkK742D6cP8vll2HsdDHIR2+/BjotUj6XS/WSoroBxr15CPfb4dJN0mQk8JrUVPzyMhZS0wFptfOA60HzCmtLpBew1wNzBQ19r107Z2Ql8qv8BFCrkosJvqDGKAlJirFX5yzjjWORjjwrO2qrt8M0SedZvwV4uZYeppUdpzryWHZyJ6+9NYqPMCaHnUvuDorBBdY64oUJioDuo51Dhepg/KM9EbUOZi9mIwThzNFqzoT8ghY8XURriRBeW2pQK7dHnZHWwIDAQAB"

func epaynChannel(api, sw, payType string) PluginChannel {
	return PluginChannel{ID: 11, Plugin: "epayn", PayType: payType, Config: map[string]string{
		"appurl": api, "appid": "1000", "appkey": epaynTestPub, "appsecret": epaynTestPriv, "appswitch": sw,
	}}
}

func TestPluginEpaynRegistryAndBuild(t *testing.T) {
	spec, ok := LookupPlugin("epayn")
	if !ok || spec.Title != "彩虹易支付V2" || !spec.Serves("wxpay") || spec.Serves("paypal") {
		t.Fatal("epayn spec", ok)
	}
	if _, e := BuildPluginChannel(epaynChannel("https://up.example/", "1", "alipay")); e != nil {
		t.Fatal(e)
	}
	bad := []PluginChannel{
		epaynChannel("https://up.example/", "1", "paypal"),       // type not served
		epaynChannel("https://up.example/submit", "1", "alipay"), // no trailing /
		epaynChannel("ftp://up.example/", "1", "alipay"),         // scheme
	}
	missingKey := epaynChannel("https://up.example/", "1", "alipay")
	delete(missingKey.Config, "appsecret")
	badKey := epaynChannel("https://up.example/", "1", "alipay")
	badKey.Config["appkey"] = "not-a-key"
	bad = append(bad, missingKey, badKey)
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

func TestPluginEpaynRequestSignature(t *testing.T) {
	defer func(f func() int64) { epaynNow = f }(epaynNow)
	epaynNow = func() int64 { return 1700000000 }
	p, e := BuildPluginChannel(epaynChannel("https://up.example/", "1", "alipay"))
	if e != nil {
		t.Fatal(e)
	}
	ep := p.(*epaynPlugin)
	params := map[string]string{
		"method": "web", "type": "alipay", "device": "pc", "clientip": "192.0.2.1",
		"notify_url": "https://pay.example/n", "return_url": "https://pay.example/r",
		"out_trade_no": "T1", "name": "VIP", "money": "1.23",
	}
	if e := ep.sign(params); e != nil {
		t.Fatal(e)
	}
	const want = "dO5gt0qQczY7aKVId1aTRDafVC4FEQCVEelrZ4Qrt6taZP1Dyme3eVdZNKjjtn7wSeY5SMWcF8vlfwPJMVxsFMS9+qHOmwu0tR/NuyMztftDmopRG7iNY7etytR16cQIhMSatJ0sf2729oh1wBQWbI19Dd7PfPRwUuJZ9Xx7dUtSeV+W3r/cHJ3mHhGgikWOppStimfkUoI9LMTlnz5Lr5Cnqw3dbJiYsHbUyrGBo7MPf326mmoFVJtmRNTdBG244cGCZm2ZWa33m/PLf3WRCJ+Gp3p1/GZNlyQ52ktwMf9RRfYqZRa6rSnKxWnnukRLHQ+sd1dT53bxfTTAAXB8Wg=="
	if params["sign"] != want {
		t.Fatalf("sign mismatch\n got %s", params["sign"])
	}
	if params["sign_type"] != "RSA" || params["pid"] != "1000" || params["timestamp"] != "1700000000" {
		t.Fatal("envelope", params["sign_type"], params["pid"], params["timestamp"])
	}
}

func TestPluginEpaynPageMode(t *testing.T) {
	defer func(f func() int64) { epaynNow = f }(epaynNow)
	epaynNow = func() int64 { return 1700000000 }
	p, _ := BuildPluginChannel(epaynChannel("https://up.example/", "0", "wxpay"))
	if got := p.Capabilities().Methods; len(got) != 1 || got[0] != "h5" {
		t.Fatal(got)
	}
	r := CreateRequest{TradeNo: "T1", Subject: `a"<b>`, Method: "h5", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 123}
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "html" || !strings.Contains(res.Material, `action="https://up.example/api/pay/submit"`) || strings.Contains(res.Material, "<b>") || !strings.Contains(res.Material, `name="sign"`) {
		t.Fatal(res, e)
	}
}

func epaynSignReply(t *testing.T, m map[string]string) {
	t.Helper()
	p, _ := BuildPluginChannel(epaynChannel("https://up.example/", "1", "alipay"))
	if e := p.(*epaynPlugin).sign(m); e != nil {
		t.Fatal(e)
	}
}

func TestPluginEpaynMapiCreateAndOutcomes(t *testing.T) {
	defer func(f func() int64) { epaynNow = f }(epaynNow)
	epaynNow = func() int64 { return 1700000000 }
	var reply string
	var status int
	var gotForm url.Values
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(b))
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, e := BuildPluginChannel(epaynChannel(srv.URL+"/", "1", "alipay"))
	if e != nil {
		t.Fatal(e)
	}
	// Build a signed success reply (pay_type qrcode).
	reply = epaynSignedJSON(t, map[string]string{"code": "0", "pay_type": "qrcode", "pay_info": "https://qr.example/U1", "trade_no": "U1"})
	status = 200
	r := CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", ClientIP: "192.0.2.1", Amount: 123}
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.example/U1" || res.ProviderTradeNo != "U1" {
		t.Fatal(res, e)
	}
	if gotPath != "/api/pay/create" || gotForm.Get("method") != "web" || gotForm.Get("sign_type") != "RSA" || gotForm.Get("money") != "1.23" || gotForm.Get("sign") == "" {
		t.Fatal("request", gotPath, gotForm)
	}
	// Business refusal is definitive.
	reply, status = `{"code":-1,"msg":"x"}`, 200
	var pe *ProviderError
	if _, e = p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("refusal", e)
	}
	// Unreadable 2xx and 5xx are unknown-outcome.
	var unknown *UnknownOutcome
	for _, c := range []struct {
		s int
		b string
	}{{200, "not json"}, {502, "bad"}} {
		status, reply = c.s, c.b
		if _, e = p.Create(context.Background(), r); !errors.As(e, &unknown) {
			t.Fatal("unknown", c, e)
		}
	}
	// A 2xx reply we cannot verify (bad signature) is also unknown-outcome.
	reply, status = `{"code":"0","pay_type":"qrcode","pay_info":"x","sign":"AA==","timestamp":"1700000000"}`, 200
	if _, e = p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("unverifiable reply must be unknown", e)
	}
}

// epaynSignedJSON builds a reply object signed with the merchant key so the
// client's platform-key verification (same keypair here) passes.
func epaynSignedJSON(t *testing.T, fields map[string]string) string {
	t.Helper()
	m := map[string]string{}
	for k, v := range fields {
		m[k] = v
	}
	// Reuse the plugin's sign (adds pid/timestamp/sign). code stays as given.
	epaynSignReply(t, m)
	var b strings.Builder
	b.WriteByte('{')
	first := true
	for k, v := range m {
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(`"` + k + `":"` + v + `"`)
	}
	b.WriteByte('}')
	return b.String()
}

func TestPluginEpaynNotification(t *testing.T) {
	defer func(f func() int64) { epaynNow = f }(epaynNow)
	epaynNow = func() int64 { return 1700000000 }
	p, _ := BuildPluginChannel(epaynChannel("https://up.example/", "0", "alipay"))
	v := p.(NotificationVerifier)
	base := map[string]string{"trade_status": "TRADE_SUCCESS", "out_trade_no": "T1", "trade_no": "U1", "money": "1.23", "buyer": "2088", "pid": "1000", "timestamp": "1700000000"}
	base["sign"] = "J58CwufC4pJFF+M1urkRd7kC3MqmctSi4nKdqXrhowbY1D0BQc3Xo9/4ksQieVLGeSeIi7QjIE05CNS2rsnDHE1o0QSe22WklSQRCfTcidBnDbBp1VITVMmlXfexz7Oru31dJfqJNN9y6/I/G7QYhSBqB19EiDjPCPY2XrC+dfcanDBOgrcZWiZMVcgBATC7nVWv/7YamfN9dgZ9abvy5Q9xzGUtE8u0Fh7NWAdieE3NqrkRdF77cIPxkBLSqt3KzC5iRL97WQVhcT7dCB014glX7rN3U9OqRuOkLeLeiwfODw5+s+JLFNTnU00hGV785RX3ZpGCN7neXCGqDFe8fw=="
	base["sign_type"] = "RSA"
	encode := func(m map[string]string) []byte {
		q := url.Values{}
		for k, val := range m {
			q.Set(k, val)
		}
		return []byte(q.Encode())
	}
	exp := ExpectedPayment{TradeNo: "T1", Amount: 123}
	ev, e := v.VerifyNotification(context.Background(), nil, encode(base), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "U1" || ev.Amount != 123 || ev.BuyerID != "2088" {
		t.Fatal(ev, e)
	}
	tamper := func(k, val string) map[string]string {
		m := map[string]string{}
		for kk, vv := range base {
			m[kk] = vv
		}
		m[k] = val
		return m
	}
	for name, m := range map[string]map[string]string{
		"bad sign": tamper("sign", "AA=="),
		"amount":   tamper("money", "9.99"),
	} {
		if _, e = v.VerifyNotification(context.Background(), nil, encode(m), exp); !errors.Is(e, ErrAuthentication) {
			t.Fatal(name, e)
		}
	}
	if _, e = v.VerifyNotification(context.Background(), nil, encode(base), ExpectedPayment{TradeNo: "OTHER", Amount: 123}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade")
	}
	if _, e = v.VerifyNotification(context.Background(), nil, encode(base), ExpectedPayment{TradeNo: "T1", Amount: 999}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount expected")
	}
	// Non-success status rejected.
	bad := tamper("trade_status", "WAIT_BUYER_PAY")
	epaynResign(t, p.(*epaynPlugin), bad)
	if _, e = v.VerifyNotification(context.Background(), nil, encode(bad), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("status", e)
	}
	if got := p.(NotificationMethodsProvider).NotificationMethods(); len(got) != 2 || got[0] != http.MethodGet {
		t.Fatal(got)
	}
}

// epaynResign re-signs a tampered map so only the status (not the signature) is
// invalid, to exercise the status branch.
func epaynResign(t *testing.T, p *epaynPlugin, m map[string]string) {
	t.Helper()
	delete(m, "sign")
	delete(m, "sign_type")
	delete(m, "pid")
	delete(m, "timestamp")
	if e := p.sign(m); e != nil {
		t.Fatal(e)
	}
}

func TestPluginEpaynRefund(t *testing.T) {
	defer func(f func() int64) { epaynNow = f }(epaynNow)
	epaynNow = func() int64 { return 1700000000 }
	var reply string
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(epaynChannel(srv.URL+"/", "1", "alipay"))
	ep := p.(*epaynPlugin)
	reply = epaynSignedJSON(t, map[string]string{"code": "0", "refund_no": "RN1"})
	rr, e := ep.Refund(context.Background(), RefundRequest{TradeNo: "T1", ProviderTradeNo: "U1", RefundNo: "R1", Amount: 100, TotalAmount: 123})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 100 || rr.ProviderRefundNo != "RN1" || gotPath != "/api/pay/refund" {
		t.Fatal(rr, e, gotPath)
	}
	reply = `{"code":-1}`
	var pe *ProviderError
	if _, e = ep.Refund(context.Background(), RefundRequest{TradeNo: "T1", ProviderTradeNo: "U1", RefundNo: "R1", Amount: 100, TotalAmount: 123}); !errors.As(e, &pe) {
		t.Fatal(e)
	}
}
