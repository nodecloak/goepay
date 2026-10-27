package server

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goepay/internal/enrollment"
	"goepay/internal/mailaccount195"
)

type mail195TLS struct {
	port              string
	roots             *x509.CertPool
	connections, data atomic.Int32
	mu                sync.Mutex
	messages          []string
}

func mail195LocalTLS(t *testing.T, lost bool, extraRecipients ...string) *mail195TLS {
	t.Helper()
	allowedRecipients := map[string]bool{"recipient195@example.test": true}
	for _, email := range extraRecipients {
		allowedRecipients[email] = true
	}
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(195), Subject: pkix.Name{CommonName: "Synthetic SMTP195"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	certificate, e := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if e != nil {
		t.Fatal(e)
	}
	listener, e := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	if e != nil {
		t.Fatal(e)
	}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	roots := x509.NewCertPool()
	parsed, _ := x509.ParseCertificate(der)
	roots.AddCert(parsed)
	f := &mail195TLS{port: port, roots: roots}
	var wg sync.WaitGroup
	t.Cleanup(func() { listener.Close(); wg.Wait() })
	go func() {
		for {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			f.connections.Add(1)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				reader := bufio.NewReader(conn)
				if _, err := io.WriteString(conn, "220 synthetic195 ESMTP\r\n"); err != nil {
					return
				}
				authed := false
				for {
					line, e := reader.ReadString('\n')
					if e != nil {
						return
					}
					line = strings.TrimSpace(line)
					switch {
					case strings.HasPrefix(line, "EHLO "):
						io.WriteString(conn, "250-synthetic195\r\n250 AUTH PLAIN\r\n")
					case strings.HasPrefix(line, "AUTH PLAIN "):
						raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "AUTH PLAIN "))
						if string(raw) != "\x00sender195@example.invalid\x00synthetic195-tls-password" {
							t.Error("SMTP credentials differ from protected account")
							return
						}
						authed = true
						io.WriteString(conn, "235 authenticated\r\n")
					case strings.HasPrefix(line, "MAIL FROM:"):
						if !authed || !strings.Contains(line, "sender195@example.invalid") {
							t.Error("SMTP envelope sender or authentication invalid")
							return
						}
						io.WriteString(conn, "250 sender\r\n")
					case strings.HasPrefix(line, "RCPT TO:"):
						if !allowedRecipients[strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "RCPT TO:")), "<>")] {
							t.Error("original recipient changed")
							return
						}
						io.WriteString(conn, "250 recipient\r\n")
					case line == "DATA":
						f.data.Add(1)
						io.WriteString(conn, "354 message\r\n")
						body, e := textproto.NewReader(reader).ReadDotBytes()
						if e != nil {
							return
						}
						f.mu.Lock()
						f.messages = append(f.messages, string(body))
						f.mu.Unlock()
						if lost {
							return
						}
						io.WriteString(conn, "250 accepted\r\n")
					case line == "QUIT":
						io.WriteString(conn, "221 goodbye\r\n")
						return
					default:
						t.Error("unexpected synthetic SMTP command")
						return
					}
				}
			}()
		}
	}()
	return f
}
func mail195SaveTLS(t *testing.T, f channel168Fixture, state map[string]any, peer *mail195TLS) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": "smtp", "status": 1, "config": map[string]string{"host": "127.0.0.1", "port": peer.port, "username": "sender195@example.invalid", "password": "synthetic195-tls-password"}, "expected_fingerprint": state["fingerprint"]})
	return mail195Data(t, mail195Request(f, "PATCH", "/smtp", string(body)))
}

func TestMailAccounts195ActualProtectedAccountTLSAndUnknownDATAResponseNeverRetries(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(strconv.FormatBool(lost), func(t *testing.T) {
			peer := mail195LocalTLS(t, lost)
			f := channel168Local(t)
			master := make([]byte, 32)
			key := sha256.Sum256(append([]byte("checkout-signing:"), master...))
			f.s.CheckoutKey = key[:]
			state := mail195Data(t, mail195Request(f, "GET", "/smtp", ""))
			mail195SaveTLS(t, f, state, peer)
			if peer.connections.Load() != 0 {
				t.Fatal("saving account connected to SMTP")
			}
			resolver := &mailaccount195.Verification{Store: mailaccount195.FromMaster(f.p, master), RootCAs: peer.roots}
			service, e := enrollment.New(f.p, f.s.Store, master, resolver)
			if e != nil {
				t.Fatal(e)
			}
			_, e = service.Issue(t.Context(), enrollment.VerificationRequest{Purpose: "recover", Email: "recipient195@example.test", Source: "192.0.2.195"})
			if !lost && e != nil {
				t.Fatal("protected account did not reach authenticated trusted TLS SMTP", e)
			}
			if lost && !errors.Is(e, enrollment.ErrUnavailable) {
				t.Fatal("lost DATA response declared success", e)
			}
			if peer.connections.Load() != 1 || peer.data.Load() != 1 {
				t.Fatal("original SMTP send missing or retried", peer.connections.Load(), peer.data.Load())
			}
			var challengeState string
			if e = f.p.QueryRow(t.Context(), `SELECT state FROM enrollment_challenges`).Scan(&challengeState); e != nil || (!lost && challengeState != "sent") || (lost && challengeState != "failed") {
				t.Fatal("challenge delivery state", challengeState, e)
			}
		})
	}
}

func TestMailAccounts195PreparedAccountRemainsOriginalAfterLaterConsoleRotation(t *testing.T) {
	first, second := mail195LocalTLS(t, false), mail195LocalTLS(t, false)
	f := channel168Local(t)
	master := make([]byte, 32)
	key := sha256.Sum256(append([]byte("checkout-signing:"), master...))
	f.s.CheckoutKey = key[:]
	state := mail195Data(t, mail195Request(f, "GET", "/smtp", ""))
	state = mail195SaveTLS(t, f, state, first)
	resolver := &mailaccount195.Verification{Store: mailaccount195.FromMaster(f.p, master), RootCAs: first.roots}
	tx, e := f.p.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(t.Context())
	bound, e := resolver.PrepareVerification(t.Context(), tx)
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(t.Context()); e != nil {
		t.Fatal(e)
	}
	mail195SaveTLS(t, f, state, second)
	if e = bound.SendVerification(context.Background(), "recipient195@example.test", "recover", "1234567"); e != nil {
		t.Fatal("original account snapshot changed after admission", e)
	}
	if first.data.Load() != 1 || second.connections.Load() != 0 {
		t.Fatal("admitted message used rotated account", first.data.Load(), second.connections.Load())
	}
}
