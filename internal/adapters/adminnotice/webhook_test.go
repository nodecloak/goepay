package adminnotice

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// All official-looking hosts are dialed only to this ephemeral loopback TLS
// listener; its generated certificate is trusted only by this test client.
func noticeTLS77(t *testing.T, handler http.Handler) (*http.Client, *atomic.Int32) {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	hosts := []string{"oapi.dingtalk.com", "qyapi.weixin.qq.com", "open.feishu.cn", "open.larksuite.com", "api.telegram.org", "api.bot.qq.com", "api.weixin.qq.com", "iot.solomo-info.com", "dysmsapi.aliyuncs.com", "yun.tim.qq.com", "api.smsbao.com"}
	template := &x509.Certificate{SerialNumber: big.NewInt(77), Subject: pkix.Name{CommonName: "synthetic-notice77"}, DNSNames: hosts, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	cert, e := x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	up := httptest.NewUnstartedServer(handler)
	up.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	up.StartTLS()
	t.Cleanup(up.Close)
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	var dials atomic.Int32
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		dials.Add(1)
		host, port, e := net.SplitHostPort(address)
		if e != nil || port != "443" {
			return nil, ErrDelivery
		}
		allowed := false
		for _, expected := range hosts {
			if host == expected {
				allowed = true
			}
		}
		if !allowed {
			return nil, ErrDelivery
		}
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", up.Listener.Addr().String())
	}}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 5 * time.Second}, &dials
}

func TestAdminNotice77WebhookActualTLSOriginalProviderPayloadAndAcceptedReply(t *testing.T) {
	for _, tc := range []struct{ endpoint, host, kind string }{
		{"https://oapi.dingtalk.com/robot/send?access_token=synthetic-token77", "oapi.dingtalk.com", "ding"},
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-key77", "qyapi.weixin.qq.com", "work"},
		{"https://open.feishu.cn/open-apis/bot/v2/hook/local-hook77", "open.feishu.cn", "feishu"},
		{"https://open.larksuite.com/open-apis/bot/v2/hook/local-hook77", "open.larksuite.com", "lark"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			var calls atomic.Int32
			client, dials := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.TLS == nil || r.Method != "POST" || r.Host != tc.host || r.Header.Get("Content-Type") != "application/json" {
					t.Error("wrong original provider route or unauthenticated transport")
				}
				var payload map[string]any
				if e := json.NewDecoder(r.Body).Decode(&payload); e != nil {
					t.Error(e)
					return
				}
				switch tc.kind {
				case "ding":
					if payload["msgtype"] != "markdown" || payload["markdown"].(map[string]any)["title"] != "支付通道自动关闭提醒" {
						t.Error("wrong DingTalk payload", payload)
					}
				case "work":
					if payload["msgtype"] != "markdown" {
						t.Error("wrong enterprise WeChat payload", payload)
					}
				default:
					if payload["msg_type"] != "text" {
						t.Error("wrong Feishu/Lark payload", payload)
					}
				}
				raw, _ := json.Marshal(payload)
				if !strings.Contains(string(raw), "通道 7") || !strings.Contains(string(raw), "记录 31") || strings.Contains(string(raw), "synthetic-token77") {
					t.Error("wrong original notice or token in message")
				}
				if tc.kind == "ding" || tc.kind == "work" {
					w.Write([]byte(`{"errcode":0}`))
				} else {
					w.Write([]byte(`{"code":0}`))
				}
			}))
			transport, e := NewWebhook(tc.endpoint, client)
			if e != nil {
				t.Fatal(e)
			}
			if dials.Load() != 0 {
				t.Fatal("constructor contacted upstream")
			}
			if e = transport.SendChannelStopped(context.Background(), tc.endpoint, 7, 31, "40004:PAYER_BALANCE_NOT_ENOUGH"); e != nil {
				t.Fatal(e)
			}
			if calls.Load() != 1 {
				t.Fatal("wrong send count", calls.Load())
			}
		})
	}
}

func TestAdminNotice77WebhookRejectsSpoofedHostPathsAndUnsafeIdentityWithoutIO(t *testing.T) {
	client, dials := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("invalid configuration reached network") }))
	for _, endpoint := range []string{"http://oapi.dingtalk.com/robot/send?access_token=local77", "https://oapi.dingtalk.com.evil.example/robot/send?access_token=local77", "https://evil.example/oapi.dingtalk.com/robot/send?access_token=local77", "https://oapi.dingtalk.com@evil.example/robot/send?access_token=local77", "https://oapi.dingtalk.com/robot/send?access_token=a&access_token=b", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=local77#fragment", "https://open.feishu.cn/open-apis/bot/v2/hook/local77/../../other", "https://open.feishu.cn/open-apis/bot/v2/hook/encoded%2Fpath", "https://127.0.0.1/robot/send?access_token=local77", "https://oapi.dingtalk.com:8443/robot/send?access_token=local77", "https://qyapi.weixin.qq.com/wrong?key=local77"} {
		if _, e := NewWebhook(endpoint, client); e != ErrDelivery {
			t.Fatal("unsafe webhook accepted", endpoint)
		}
	}
	if dials.Load() != 0 {
		t.Fatal("invalid webhook caused IO", dials.Load())
	}
}
