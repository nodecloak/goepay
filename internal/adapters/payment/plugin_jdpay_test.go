package payment

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// jdpayTestPriv is a throwaway 2048-bit PKCS#8 key generated with node; the
// matching sign vector below was produced by node's crypto.privateEncrypt.
const jdpayTestPriv = `-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQDJ9ABf399oIr/V
ST9F3b9yirchDdoSMme1HsU5Bs7ThCE+6+UM5MWqsbVtmHQtzCBAetGfxpxgTTxi
/AvArIDo/uHLXLEfb8QVqc0BEi8mNChva4P8XfOxYHLbZFnWSaqt2gFXTSnOzK8y
QnasBA+hc0a1MvxoCvZBCA+9eRyqNv9T766c+53LD10Y/t13RtgZVSpJBbk0nE1m
MiQAczdAHMOamCPzoD5U/bIfUcu/YeDYINjnj7vXYTz/TTSkHDSBciVhj1ZoNmpD
TVy8Se5gNPBMu7jkmDyqch9eJgR1u23qcpeR6B7I1k8iPrBzSOJOBMcFcInPtSDv
oBXKEm5VAgMBAAECggEAIZySKWLNwHu0GM/5j2vCLtKmewJerpOkDhGCeqTG7I47
qEjDlPtIaMnt68EpaOvl3EV0uRZptV91cvYHELjFNcIiG2Igep5z8LUTkR+RKFca
+bxXubt7USS/yQLxLsiQ9yFnArwtfqUzbXaBYMzGOzU/6e2D898LMDSixgKvwiZc
QTZsEsTdwDalr6+Osdzf89FwT7rYrOXV6fQw07jusl6/YX5DMd2u6xwR64iqVeE/
aqC9226vJqhzFr1YDgV3Kk/1d3vqh99+1jL6Xxo/xIEZa5LYceH9pUGqVBImGXrz
34NpL7lg8cgrTfabKmEJtaFFejINedeV1h+Z0qnwcQKBgQD0u8ID/kZvzzgmKfbi
fQWsarzwSHBzY+cZV1Tr9Jm0zOwquJdDhkGCneq+uMLFs8ezszea1Ik4IySQwNfN
I9yOGcnhIYzReFAGA7MZmFtq2M87ypQZA0+6I/fPRLLQm1Yng3aGYoueWWdbQvH/
dcep1Ebo4Im5MMabC2QaAKo1+QKBgQDTQBFI0MtYvpALPIAOYKEk6AdLtN1pBsJZ
3JC2KjO+Aovj1DMcJ9S+9XgzNp54UfOvLuHWiCzo2iOK1HIfkXyn+IwGiWpQvpy2
zMeO0VlvULEOvzG6XFOiT0rUtiwMiaVCBqIdAsFBPQFYZDx7dJBST+sdlaa2zmMG
O5pqrTyiPQKBgQCuVqqJabQHplhbZ32GLoXCI29gxFnG+Z4BQWMvi1mLoIeEj2bC
JXu5boFxR/EIIV/btYmg9FKj+PTtYVLZwfg2Ckzz4ZCND+YBwtTtIMnOvZAzCcai
YueCtGit8ERpsp6XG2FtrfnD3fBw5Km7z81iP+QbtQinMgXpIR44H80hEQKBgQCx
b+PUvIPAk7ebqIKLEQDpZfcS0OUUXXgLUKdASyxQ0gS8JbWUVaRtjPMcWSk8OOGs
6H0K3DCUmP03lAATofkJgUvkBbIGBkTNii2oORGc3TMLBu728KvS7jUVe7p+AXOM
hNPP+Etox5wEqZTfwWWjrbr3i4aPj8U/K/sk1UPTpQKBgDxgeXjKNpVb8baw33ZG
4XV80E6+tNK4Ug2lpAnUrhgmfRDz9kIbkqQPjjZiV21EVEwotVjp2lvLr9IxOVFL
emmbBLf6mDOa1NiJBzllYTYQgF4UYlVrrIc4qZJ8qqDCNQWsiBvCzEWgdNHo/4Mv
b3ZAY8C31CTznbX4AMSvAsOG
-----END PRIVATE KEY-----`

func jdpayChannel() PluginChannel {
	return PluginChannel{
		ID: 14, Plugin: "jdpay", PayType: "jdpay",
		Config:       map[string]string{"appid": "M1", "appkey": "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3"}, // base64("0123456789abcdef01234567")
		Certificates: map[string]string{"rsa_private_key": jdpayTestPriv},
	}
}

func TestPluginJdpayRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("jdpay")
	if !ok || spec.Title != "京东支付" || !spec.Serves("jdpay") {
		t.Fatal("jdpay spec", ok)
	}
	if _, e := BuildPluginChannel(jdpayChannel()); e != nil {
		t.Fatal(e)
	}
	// missing private key
	missing := jdpayChannel()
	missing.Certificates = map[string]string{}
	if _, e := BuildPluginChannel(missing); e == nil {
		t.Fatal("accepted missing key")
	}
	// bad DES key (not base64 / wrong length)
	badkey := jdpayChannel()
	badkey.Config["appkey"] = "zzzz"
	if _, e := BuildPluginChannel(badkey); e == nil {
		t.Fatal("accepted bad des key")
	}
}

func TestPluginJdpay3DESVector(t *testing.T) {
	p, e := BuildPluginChannel(jdpayChannel())
	if e != nil {
		t.Fatal(e)
	}
	jp := p.(*jdpayPlugin)
	// node: 3DES-ede3 ECB no-padding of jdpayPad("hello") == this hex.
	enc, e := jp.encryptHex("hello")
	if e != nil || enc != "b4398221cb107d290637de805989a1a5" {
		t.Fatal(enc, e)
	}
	dec, e := jp.decryptHex(enc)
	if e != nil || dec != "hello" {
		t.Fatal(dec, e)
	}
}

func TestPluginJdpayRSASignVector(t *testing.T) {
	p, _ := BuildPluginChannel(jdpayChannel())
	jp := p.(*jdpayPlugin)
	// Sign the exact plaintext params node signed; compare to node's base64 sig.
	params := map[string]string{"version": "V2.0", "merchant": "M1", "tradeNum": "T1", "amount": "100", "currency": "CNY", "orderType": "1"}
	sig, e := jp.sign(params)
	if e != nil {
		t.Fatal(e)
	}
	const want = "IRJqaWIFqxi66nSvZchH904x4NpY0QXymvyI5ORrIPNzbbro8iwo92WIRVgVKGriolUCtn2qpjbjTvns7/NHlAlRAVJTftUcsnmQxN3gNsSgiVhmrt46jsWgjdr4mGFPWLHHqxEPIUFYrYt0LT5q4At9qmJp9EUSCBwtetphqG0AkngccQLsLzrD7pzP8X9ed4B9GQriZtyZyxlre3FO0/w5QHSULCxSy08pJUaIHE6CzdDH/veXLob8DXhPwFrs7tWFlkmEqUbTrxK2G/uftuNB1XXix27Aoq+uNY+MIzpS00Y2uTRO5a0skGps91jfK7GhoSNUbCZB5RQK36cf7g=="
	if sig != want {
		t.Fatal("sign drift", sig)
	}
	// Round-trip through our own public-decrypt against a matching public key.
	raw, _ := base64.StdEncoding.DecodeString(sig)
	got, e := jdpayPublicDecrypt(&jp.privateKey.PublicKey, raw)
	if e != nil {
		t.Fatal(e)
	}
	if string(got) != sha256Hex(jdpaySignString(params)) {
		t.Fatal("decrypt mismatch", string(got))
	}
}

func TestPluginJdpayCreateForm(t *testing.T) {
	p, _ := BuildPluginChannel(jdpayChannel())
	r := CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "h5", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 100, ClientIP: "1.1.1.1"}
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "html" {
		t.Fatal(res, e)
	}
	if !strings.Contains(res.Material, `action="https://h5pay.jd.com/jdpay/saveOrder"`) || !strings.Contains(res.Material, `name="sign"`) || !strings.Contains(res.Material, `name="merchant"`) {
		t.Fatal("form", res.Material)
	}
	// tradeNum must be TDES-hex (not the plaintext).
	if strings.Contains(res.Material, `name="tradeNum" value="T1"`) {
		t.Fatal("tradeNum not encrypted")
	}
	// native not offered
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "T", Subject: "s", Method: "native", NotifyURL: r.NotifyURL, Amount: 1}); !errors.Is(e, ErrInvalid) {
		t.Fatal("native must be rejected", e)
	}
}

// jdpaySignedResXML builds a notify/refund document the plugin can verify, by
// signing with the merchant key and verifying with the platform key. Since the
// plugin verifies with JD's FIXED platform key, a test cannot forge a valid
// reply without that private key — so we assert the REJECTION paths here and
// the acceptance path via a locally overridden platform key.
func TestPluginJdpayNotificationRejects(t *testing.T) {
	p, _ := BuildPluginChannel(jdpayChannel())
	jp := p.(*jdpayPlugin)
	// A random/garbage body is rejected (cannot be decrypted / verified).
	if _, e := jp.VerifyNotification(context.Background(), nil, []byte("<jdpay><encrypt>garbage</encrypt></jdpay>"), ExpectedPayment{TradeNo: "T1", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("garbage accepted", e)
	}
}

// TestPluginJdpayNotificationAccepts swaps the platform key to the test key so
// we can mint a correctly signed, 3DES-encrypted notification end-to-end.
func TestPluginJdpayNotificationAccepts(t *testing.T) {
	p, _ := BuildPluginChannel(jdpayChannel())
	jp := p.(*jdpayPlugin)
	jp.platformPub = &jp.privateKey.PublicKey // verify with the key we sign with

	// Build the inner <jdpay> body with status/tradeNum/amount, sign it, wrap.
	inner := `<?xml version="1.0" encoding="UTF-8"?><jdpay><amount>100</amount><status>2</status><tradeNum>T1</tradeNum></jdpay>`
	digest := sha256Hex(inner)
	signRaw, e := jdpayPrivateEncrypt(jp.privateKey, []byte(digest))
	if e != nil {
		t.Fatal(e)
	}
	sign := base64.StdEncoding.EncodeToString(signRaw)
	signed := inner[:len(inner)-len("</jdpay>")] + "<sign>" + sign + "</sign></jdpay>"
	encHex, e := jp.encryptHex(signed)
	if e != nil {
		t.Fatal(e)
	}
	outer := "<?xml version=\"1.0\" encoding=\"UTF-8\"?><jdpay><encrypt>" + base64.StdEncoding.EncodeToString([]byte(encHex)) + "</encrypt></jdpay>"

	ev, e := jp.VerifyNotification(context.Background(), nil, []byte(outer), ExpectedPayment{TradeNo: "T1", Amount: 100})
	if e != nil || ev.State != "SUCCESS" || ev.TradeNo != "T1" || ev.Amount != 100 {
		t.Fatal(ev, e)
	}
	// wrong amount
	if _, e = jp.VerifyNotification(context.Background(), nil, []byte(outer), ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	// tampering the ciphertext breaks verification
	if _, e = jp.VerifyNotification(context.Background(), nil, []byte(strings.Replace(outer, "T", "Z", 1)), ExpectedPayment{TradeNo: "T1", Amount: 100}); e == nil {
		t.Fatal("tamper accepted")
	}
	if st, _, bdy := jp.NotificationAck(nil); st != 200 || bdy != "success" {
		t.Fatal(st, bdy)
	}
}

func TestPluginJdpayRefund(t *testing.T) {
	p, _ := BuildPluginChannel(jdpayChannel())
	jp := p.(*jdpayPlugin)
	jp.platformPub = &jp.privateKey.PublicKey

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		outer, _ := jdpayParseFlat(b)
		if outer["encrypt"] == "" {
			t.Errorf("no encrypt in request")
		}
		// Reply with a signed status=1 document.
		inner := `<?xml version="1.0" encoding="UTF-8"?><jdpay><amount>50</amount><oTradeNum>JD-R</oTradeNum><status>1</status></jdpay>`
		digest := sha256Hex(inner)
		signRaw, _ := jdpayPrivateEncrypt(jp.privateKey, []byte(digest))
		sign := base64.StdEncoding.EncodeToString(signRaw)
		signed := inner[:len(inner)-len("</jdpay>")] + "<sign>" + sign + "</sign></jdpay>"
		encHex, _ := jp.encryptHex(signed)
		io.WriteString(w, "<jdpay><encrypt>"+base64.StdEncoding.EncodeToString([]byte(encHex))+"</encrypt></jdpay>")
	}))
	defer srv.Close()

	// Build a REAL refund request document and post it, exercising the full
	// encrypt (request) / decrypt (reply) crypto round-trip. The refund URL is
	// a const, so we drive pluginCall against the test server directly.
	reqXML, e := jp.encryptReqXML(map[string]string{"version": "V2.0", "merchant": "M1", "tradeNum": "R1", "oTradeNum": "JD1", "amount": "50", "currency": "CNY"})
	if e != nil {
		t.Fatal(e)
	}
	raw, e := jp.pluginCall(context.Background(), "jdpay.refund", http.MethodPost, srv.URL, reqXML, "application/xml;charset=utf-8", nil)
	if e != nil {
		t.Fatal(e)
	}
	data, ok := jp.decryptResXML(raw)
	if !ok || data["status"] != "1" || data["oTradeNum"] != "JD-R" {
		t.Fatal(data, ok)
	}
}
