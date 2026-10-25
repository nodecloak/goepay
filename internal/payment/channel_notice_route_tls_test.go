package payment

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goepay/internal/adapters/adminnotice"
)

func TestChannelNotice77ActualQQTLSClaimIsCommittedBeforeTokenAndMessageAndAcceptedOnce(t *testing.T) {
	s, job, lease, event := noticeSource77(t)
	ctx := context.Background()
	var tokens, messages atomic.Int32
	up := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This uses a separate pool connection, so an uncommitted attempt cannot
		// authorize even the first token fetch, before the message itself.
		v, e := s.readChannelNoticeClaim(ctx, s.pool, event)
		if e != nil || v.State != "attempted" || v.RouteVersion != 1 || v.RouteMode != "qqbot" {
			t.Error("outbound request preceded committed route proof", e)
			http.Error(w, "not claimed", 409)
			return
		}
		if r.TLS == nil || r.Method != "POST" || r.Host != "api.bot.qq.com" {
			t.Error("wrong actual TLS route")
		}
		switch r.URL.Path {
		case "/app/getAppAccessToken":
			tokens.Add(1)
			w.Write([]byte(`{"access_token":"synthetic-token77","expires_in":7200}`))
		case "/v2/users/local_openid77/messages":
			messages.Add(1)
			if r.Header.Get("Authorization") != "QQBot synthetic-token77" {
				t.Error("message token not bound")
			}
			w.Write([]byte(`{"id":"local-message77"}`))
		default:
			t.Error("unexpected external path")
			http.Error(w, "bad route", 404)
		}
	}))
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(7701), DNSNames: []string{"api.bot.qq.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	parsed, e := x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	up.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	up.StartTLS()
	defer up.Close()
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "api.bot.qq.com:443" {
			return nil, ErrInvalid
		}
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", up.Listener.Addr().String())
	}}
	defer transport.CloseIdleConnections()
	sender, e := adminnotice.NewQQBot("local_app77", "synthetic-secret77", &http.Client{Transport: transport, Timeout: 5 * time.Second})
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.DeliverChannelStopNotice(ctx, job, lease, "local_openid77", sender); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if tokens.Load() != 1 || messages.Load() != 1 {
		t.Fatal("QQ token or message duplicated", tokens.Load(), messages.Load())
	}
	status, e := s.ChannelWriteStatus(ctx, 7)
	if e != nil || status.NoticeState != "accepted" || status.NoticeTransport != "qqbot" {
		t.Fatal(status, e)
	}
	var recipientHash, scopeHash []byte
	if e = s.pool.QueryRow(ctx, `SELECT recipient_hash,route_scope_hash FROM channel_stop_notice_claims WHERE event_id=$1`, event).Scan(&recipientHash, &scopeHash); e != nil || len(recipientHash) != 32 || len(scopeHash) != 32 {
		t.Fatal(e)
	}
	var audit string
	if e = s.pool.QueryRow(ctx, `SELECT string_agg(changes::text,'') FROM backoffice_audit WHERE resource='channel-stop-notices' AND resource_id=$1`, event).Scan(&audit); e != nil || strings.Contains(audit, "synthetic-") || strings.Contains(audit, "local_openid77") {
		t.Fatal("claim/audit leaked profile", e)
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidChannelStops != 0 {
		t.Fatal(report, e)
	}
}
