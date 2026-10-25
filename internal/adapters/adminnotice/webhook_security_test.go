package adminnotice

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAdminNotice77WebhookFailureCannotAcceptOrRetryOrExposeCredentials(t *testing.T) {
	const endpoint = "https://oapi.dingtalk.com/robot/send?access_token=private-synthetic-webhook77"
	for _, mode := range []string{"status503", "redirect", "duplicate", "string-zero", "wrong-field", "oversize", "trailing", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			client, _ := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch mode {
				case "status503":
					w.WriteHeader(503)
					w.Write([]byte(`{"errcode":0}`))
				case "redirect":
					w.Header().Set("Location", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=must-not-follow77")
					w.WriteHeader(302)
				case "duplicate":
					w.Write([]byte(`{"errcode":1,"errcode":0}`))
				case "string-zero":
					w.Write([]byte(`{"errcode":"0"}`))
				case "wrong-field":
					w.Write([]byte(`{"code":0}`))
				case "oversize":
					w.Write([]byte(strings.Repeat(" ", 32769) + `{"errcode":0}`))
				case "trailing":
					w.Write([]byte(`{"errcode":0}{"errcode":1}`))
				default:
					w.Write([]byte(`not JSON private-synthetic-webhook77`))
				}
			}))
			transport, e := NewWebhook(endpoint, client)
			if e != nil {
				t.Fatal(e)
			}
			e = transport.SendChannelStopped(context.Background(), endpoint, 7, 31, "SYSTEMERROR")
			if e != ErrDelivery || calls.Load() != 1 {
				t.Fatal("failure accepted or retried", mode, e, calls.Load())
			}
			if strings.Contains(e.Error(), "private-synthetic-webhook77") || strings.Contains(fmt.Sprint(transport), "private-synthetic-webhook77") {
				t.Fatal("notice error or representation leaked endpoint credential")
			}
		})
	}
}

func TestAdminNotice77WebhookInvalidInputAndUntrustedTLSCannotDeliver(t *testing.T) {
	const endpoint = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=private-synthetic-key77"
	var calls atomic.Int32
	client, dials := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.Write([]byte(`{"errcode":0}`)) }))
	transport, e := NewWebhook(endpoint, client)
	if e != nil {
		t.Fatal(e)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		ctx            context.Context
		to             string
		channel, event int64
		code           string
	}{
		{context.Background(), endpoint, 0, 31, "SYSTEMERROR"}, {context.Background(), endpoint, 7, 0, "SYSTEMERROR"}, {context.Background(), endpoint, 7, 31, "SYSTEMERROR\r\nBcc: injected"}, {context.Background(), "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=another", 7, 31, "SYSTEMERROR"}, {cancelled, endpoint, 7, 31, "SYSTEMERROR"},
	} {
		if e = transport.SendChannelStopped(tc.ctx, tc.to, tc.channel, tc.event, tc.code); e != ErrDelivery {
			t.Fatal("unsafe input accepted")
		}
	}
	if dials.Load() != 0 {
		t.Fatal("invalid input contacted endpoint")
	}
	bad := *client
	base := client.Transport.(*http.Transport).Clone()
	base.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: x509.NewCertPool()}
	bad.Transport = base
	t.Cleanup(base.CloseIdleConnections)
	untrusted, e := NewWebhook(endpoint, &bad)
	if e != nil {
		t.Fatal(e)
	}
	if e = untrusted.SendChannelStopped(context.Background(), endpoint, 7, 31, "SYSTEMERROR"); e != ErrDelivery || calls.Load() != 0 {
		t.Fatal("untrusted TLS reached message submission", e, calls.Load())
	}
	insecure := *client
	insecureBase := base.Clone()
	insecureBase.TLSClientConfig.InsecureSkipVerify = true
	insecure.Transport = insecureBase
	t.Cleanup(insecureBase.CloseIdleConnections)
	if _, e = NewWebhook(endpoint, &insecure); e != ErrDelivery {
		t.Fatal("insecure TLS client accepted")
	}
	if _, e = NewWebhook("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=local%09key77", client); e != ErrDelivery {
		t.Fatal("encoded credential control character accepted")
	}
}
