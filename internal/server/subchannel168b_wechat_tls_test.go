package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type subchannel168BWechatFactory struct {
	url    string
	client *http.Client
}

func (f subchannel168BWechatFactory) Schema() adapter.SubchannelSchema {
	return adapter.SubchannelSchema{ParentID: 7, Revision: 1, PayType: "wxpay", Fingerprint: strings.Repeat("d", 64), Fields: []adapter.SubchannelField{{Name: "app", Required: true}, {Name: "mch", Required: true}, {Name: "key", Required: true, Secret: true}}}
}
func (f subchannel168BWechatFactory) Build(v map[string]string) (adapter.SubchannelExecution, error) {
	if len(v) != 3 {
		return adapter.SubchannelExecution{}, adapter.ErrInvalid
	}
	p, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: v["app"], MerchantID: v["mch"], APIKey: v["key"], Methods: []string{"native"}, BaseURL: f.url, HTTPClient: f.client, RefundClient: f.client})
	return adapter.SubchannelExecution{Payment: p, SchemaFingerprint: f.Schema().Fingerprint}, e
}
func (f subchannel168BWechatFactory) Snapshot(v map[string]string) ([]byte, error) {
	if _, e := f.Build(v); e != nil {
		return nil, e
	}
	return json.Marshal(v)
}
func (f subchannel168BWechatFactory) Restore(raw []byte) (adapter.SubchannelExecution, error) {
	var v map[string]string
	if json.Unmarshal(raw, &v) != nil {
		return adapter.SubchannelExecution{}, adapter.ErrInvalid
	}
	return f.Build(v)
}
func subchannel168BXML(fields map[string]string, key string) []byte {
	fields["sign"], _ = adapter.SignWechatV2(fields, key, "MD5")
	var b bytes.Buffer
	b.WriteString("<xml>")
	for k, v := range fields {
		fmt.Fprintf(&b, "<%s>", k)
		xml.EscapeText(&b, []byte(v))
		fmt.Fprintf(&b, "</%s>", k)
	}
	b.WriteString("</xml>")
	return b.Bytes()
}
func TestSubchannel168BActualWechatSignedMutualTLSKeepsOriginalAccountAfterReplacement(t *testing.T) {
	f, _ := subchannel168BLocal(t)
	const key = "synthetic-child168b-key-original-32"
	var creates, queries, refunds atomic.Int64
	var originalTrade atomic.Value
	up := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var doc struct {
			Fields []struct {
				XMLName xml.Name
				Value   string `xml:",chardata"`
			} `xml:",any"`
		}
		if xml.Unmarshal(raw, &doc) != nil {
			t.Error("bad XML")
			return
		}
		v := map[string]string{}
		for _, field := range doc.Fields {
			v[field.XMLName.Local] = field.Value
		}
		if v["appid"] != "wx-child-original" || v["mch_id"] != "wx-child-mch" || !adapter.VerifyWechatV2(v, key, "MD5", v["sign"]) || r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
			t.Error("original child signature or mutual TLS identity lost")
			http.Error(w, "wrong synthetic identity", 400)
			return
		}
		out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": v["appid"], "mch_id": v["mch_id"], "nonce_str": "synthetic168b"}
		switch r.URL.Path {
		case "/pay/unifiedorder":
			creates.Add(1)
			originalTrade.Store(v["out_trade_no"])
			out["code_url"] = "weixin://synthetic168b"
		case "/pay/orderquery":
			queries.Add(1)
			out["trade_state"] = "SUCCESS"
			out["total_fee"] = "100"
			out["out_trade_no"] = v["out_trade_no"]
			out["transaction_id"] = "wx-upstream-168b"
		case "/secapi/pay/refund":
			refunds.Add(1)
			out["out_trade_no"] = v["out_trade_no"]
			out["out_refund_no"] = v["out_refund_no"]
			out["refund_id"] = "wx-refund-168b"
			out["refund_fee"] = "100"
		case "/pay/refundquery":
			out["out_trade_no"] = originalTrade.Load().(string)
			out["total_fee"] = "100"
			out["out_refund_no_0"] = v["out_refund_no"]
			out["refund_fee_0"] = "100"
			out["refund_id_0"] = "wx-refund-168b"
			out["refund_status_0"] = "SUCCESS"
		default:
			t.Error("unexpected local endpoint", r.URL.Path)
			http.Error(w, "unexpected", 500)
			return
		}
		w.Write(subchannel168BXML(out, key))
	}))
	up.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert, MinVersion: tls.VersionTLS12}
	up.StartTLS()
	defer up.Close()
	private, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(168), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &private.PublicKey, private)
	if e != nil {
		t.Fatal(e)
	}
	cert, e := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(private)}))
	if e != nil {
		t.Fatal(e)
	}
	client := up.Client()
	transport := client.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.Certificates = []tls.Certificate{cert}
	client.Transport = transport
	factory := subchannel168BWechatFactory{up.URL, client}
	parent, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "parent-only", MerchantID: "parent-only", APIKey: "synthetic-parent-only-key", Methods: []string{"native"}, BaseURL: up.URL, HTTPClient: client})
	if e != nil {
		t.Fatal(e)
	}
	f.s.ProviderFlow.Providers[7] = parent
	f.s.ProviderFlow.SubchannelFactories = map[int64]adapter.SubchannelFactory{7: factory}
	if _, e = f.p.Exec(context.Background(), `UPDATE backoffice_records SET body=body||'{"info":{"wxpay":{"channel":"-2","type":"channel"}}}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	w := f.childHTTP("POST", "", fmt.Sprintf(`{"merchant_id":"1","parent_id":"7","name":"Synthetic Wechat TLS","enabled":true,"weight":1,"min_amount":"0","max_amount":"0","values":{"app":"wx-child-original","mch":"wx-child-mch","key":%q}}`, key))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var child struct{ Data map[string]any }
	json.Unmarshal(w.Body.Bytes(), &child)
	p := compat.Params{"pid": "1", "out_trade_no": "wx168b-real-tls", "type": "wxpay", "name": "Synthetic Wechat", "money": "1.00", "channel_id": "7", "method": "native", "sign_type": "MD5"}
	p["sign"] = compat.SignMD5(p, "test-secret")
	q := url.Values{}
	for k, v := range p {
		q.Set(k, v)
	}
	r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(q.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	paid := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(paid, r)
	var out map[string]any
	json.Unmarshal(paid.Body.Bytes(), &out)
	if out["code"] != float64(1) {
		t.Fatal("actual child create", paid.Code, paid.Body.String())
	}
	patch, _ := json.Marshal(map[string]any{"expected_fingerprint": child.Data["fingerprint"], "values": map[string]any{"app": map[string]string{"operation": "replace", "value": "wx-child-new"}, "key": map[string]string{"operation": "replace", "value": "synthetic-child-new-different-key"}}})
	w = f.childHTTP("PATCH", "/"+child.Data["id"].(string), string(patch))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	o, e := f.s.Store.Order(context.Background(), 1, "wx168b-real-tls")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Query(context.Background(), 1, o.TradeNo); e != nil {
		t.Fatal("original signed query", e)
	}
	refund, e := f.s.ProviderFlow.Refund(context.Background(), 1, o.TradeNo, "wx-refund-original", 100)
	if e != nil {
		t.Fatal("original signed refund", e)
	}
	if _, e = f.s.ProviderFlow.QueryRefund(context.Background(), 1, refund.RefundNo); e != nil {
		t.Fatal("original signed refund query", e)
	}
	if creates.Load() != 1 || queries.Load() != 1 || refunds.Load() != 1 {
		t.Fatal("resend or absent original I/O", creates.Load(), queries.Load(), refunds.Load())
	}
}
