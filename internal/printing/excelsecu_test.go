package printing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type printerTestTransport func(*http.Request) (*http.Response, error)

func (f printerTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func printerTLSConnector(t *testing.T, handler http.Handler) (*ExcelsecuConnector, *atomic.Int32) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	client := server.Client()
	base := client.Transport
	calls := &atomic.Int32{}
	client.Transport = printerTestTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Scheme != "https" || r.URL.Host != "saas.excelsecu.com" || r.URL.Path != ExcelsecuPrintPath || r.URL.RawQuery != "" || r.Method != http.MethodPost || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.GetBody != nil {
			t.Error("connector changed fixed destination, replay policy, or authentication surface")
		}
		local := r.Clone(r.Context())
		local.URL.Scheme, local.URL.Host, local.Host = u.Scheme, u.Host, u.Host
		return base.RoundTrip(local)
	})
	return newExcelsecuConnector(client), calls
}

func printerTransportCommand() PrintCommand {
	return PrintCommand{RequestNo: "PJ123456789", Device: "SYNTHETIC-DEVICE-ONLY", Copies: 2, Voice: true, Credential: Credential{AppID: "SYNTHETIC-APP", Secret: "SYNTHETIC-SECRET"}, Receipt: Receipt{TradeNo: "1001", MerchantName: "Local merchant", Name: "item", Amount: 123, Type: "wxpay", PaidAt: "2026-10-24T00:00:00+08:00"}}
}

func TestExcelsecuConnectorFixedTLSAndOriginalRequestSignature(t *testing.T) {
	c := printerTransportCommand()
	connector, calls := printerTLSConnector(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("JSON content type required")
		}
		var body struct {
			AppID     string `json:"appid"`
			ReqID     string `json:"reqid"`
			Timestamp int64  `json:"timestamp"`
			Sign      string `json:"sign"`
			Biz       string `json:"biz"`
		}
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		mac := hmac.New(sha256.New, []byte(c.Credential.Secret))
		mac.Write([]byte(body.AppID + strconv.FormatInt(body.Timestamp, 10) + body.Biz + "3" + body.ReqID))
		if body.AppID != c.Credential.AppID || body.ReqID != c.RequestNo || body.Timestamp <= 0 || body.Sign != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
			t.Error("original request or signature changed")
		}
		io.WriteString(w, `{"code":0,"data":{"accepted":true}}`)
	}))
	out, err := connector.Send(context.Background(), c)
	if err != nil || out.State != "accepted" || calls.Load() != 1 || connector.LocalOnly() {
		t.Fatal(out, err, calls.Load())
	}
}

func TestExcelsecuConnectorAmbiguousRepliesAndRedirectNeverRetryOrLeak(t *testing.T) {
	for _, scenario := range []string{"redirect", "redirect303", "status", "duplicate", "oversize", "broken", "disconnect"} {
		t.Run(scenario, func(t *testing.T) {
			connector, calls := printerTLSConnector(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch scenario {
				case "redirect", "redirect303":
					w.Header().Set("Location", "https://untrusted.invalid/steal")
					status := 307
					if scenario == "redirect303" {
						status = 303
					}
					w.WriteHeader(status)
				case "status":
					w.WriteHeader(503)
					io.WriteString(w, "SYNTHETIC-SECRET SYNTHETIC-DEVICE-ONLY")
				case "duplicate":
					io.WriteString(w, `{"code":0,"code":1}`)
				case "oversize":
					io.WriteString(w, `{"code":0,"data":"`+strings.Repeat("x", 4096)+`"}`)
				case "broken":
					io.WriteString(w, `{"code":0`)
				case "disconnect":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						conn.Close()
					}
				}
			}))
			out, err := connector.Send(context.Background(), printerTransportCommand())
			if err == nil || out.State != "" || calls.Load() != 1 || strings.Contains(err.Error(), "SYNTHETIC") || strings.Contains(err.Error(), "untrusted.invalid") {
				t.Fatal("ambiguous send must remain unknown without a retry or private error", out, err, calls.Load())
			}
		})
	}
}

func TestExcelsecuConnectorCancellationAfterTLSSendIsUnknown(t *testing.T) {
	started := make(chan struct{})
	connector, calls := printerTLSConnector(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := connector.Send(ctx, printerTransportCommand()); done <- e }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("local TLS request did not start")
	}
	cancel()
	select {
	case e := <-done:
		if e == nil || e != errPrinterOutcomeUnknown || calls.Load() != 1 {
			t.Fatal("cancelled send was retried or reported known", e, calls.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not stop HTTP client")
	}
}

func TestExcelsecuConnectorRejectsUntrustedTLSCertificate(t *testing.T) {
	var received atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Add(1); io.WriteString(w, `{"code":0}`) }))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	base := NewExcelsecuConnector().client.Transport
	client := &http.Client{Transport: printerTestTransport(func(r *http.Request) (*http.Response, error) {
		local := r.Clone(r.Context())
		local.URL.Scheme, local.URL.Host, local.Host = u.Scheme, u.Host, u.Host
		return base.RoundTrip(local)
	})}
	out, e := newExcelsecuConnector(client).Send(context.Background(), printerTransportCommand())
	if e == nil || out.State != "" || received.Load() != 0 {
		t.Fatal("untrusted certificate delivered printing credentials", out, e, received.Load())
	}
}

func TestExcelsecuConnectorDefaultsAndCancellationBeforeIO(t *testing.T) {
	c := NewExcelsecuConnector()
	if c.client.Timeout <= 0 || c.client.Timeout > 10*time.Second || c.client.Jar != nil || c.client.CheckRedirect == nil {
		t.Fatal("unbounded or stateful client")
	}
	transport := c.client.Transport.(*http.Transport)
	if transport.Proxy != nil || transport.TLSClientConfig == nil || transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("external printer must verify TLS and ignore environment proxies")
	}
	var calls int
	c = newExcelsecuConnector(&http.Client{Transport: printerTestTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("SYNTHETIC-SECRET") })})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Send(ctx, printerTransportCommand()); err == nil || calls != 0 {
		t.Fatal("cancelled request reached transport", calls, err)
	}
	if _, err := c.Send(context.Background(), printerTransportCommand()); err == nil || strings.Contains(err.Error(), "SYNTHETIC") || calls != 1 {
		t.Fatal("transport leaked internal details or retried", calls, err)
	}
}
