package mailer

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type smtpFixture struct {
	config       SMTPConfig
	mu           sync.Mutex
	messages     []string
	commands     []string
	connections  atomic.Int32
	dataAttempts atomic.Int32
}

func localSMTP(t *testing.T, security, failure string) *smtpFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if failure == "wrong-name" {
		template.DNSNames = []string{"unrelated.example.invalid"}
		template.IPAddresses = nil
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	parsed, _ := x509.ParseCertificate(der)
	roots.AddCert(parsed)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	n, _ := strconv.Atoi(port)
	f := &smtpFixture{config: SMTPConfig{Host: "127.0.0.1", Port: n, Security: security, Username: "synthetic-user", Password: "synthetic-password", From: "sender@example.test", RootCAs: roots}}
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
				f.serve(conn, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, failure)
			}()
		}
	}()
	return f
}
func (f *smtpFixture) serve(raw net.Conn, cfg *tls.Config, failure string) {
	defer raw.Close()
	raw.SetDeadline(time.Now().Add(3 * time.Second))
	var conn net.Conn = raw
	secure := false
	if f.config.Security == "implicit" {
		conn = tls.Server(raw, cfg)
		if conn.(*tls.Conn).Handshake() != nil {
			return
		}
		secure = true
	}
	if failure == "stall" {
		io.Copy(io.Discard, conn)
		return
	}
	if failure == "oversize" {
		io.WriteString(conn, "220 "+strings.Repeat("x", 70<<10)+"\r\n")
		return
	}
	io.WriteString(conn, "220 local synthetic SMTP\r\n")
	reader := bufio.NewReader(conn)
	for {
		line, e := reader.ReadString('\n')
		if e != nil {
			return
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		f.mu.Lock()
		f.commands = append(f.commands, strings.SplitN(line, " ", 2)[0])
		f.mu.Unlock()
		switch {
		case strings.HasPrefix(line, "EHLO "):
			if !secure && failure != "no-starttls" {
				io.WriteString(conn, "250-local\r\n250-STARTTLS\r\n250 AUTH PLAIN LOGIN\r\n")
			} else if failure == "no-auth" {
				io.WriteString(conn, "250 local\r\n")
			} else if failure == "login-only" {
				io.WriteString(conn, "250-local\r\n250 AUTH LOGIN\r\n")
			} else {
				io.WriteString(conn, "250-local\r\n250 AUTH PLAIN LOGIN\r\n")
			}
		case line == "STARTTLS":
			io.WriteString(conn, "220 upgrade\r\n")
			conn = tls.Server(raw, cfg)
			if conn.(*tls.Conn).Handshake() != nil {
				return
			}
			reader = bufio.NewReader(conn)
			secure = true
		case strings.HasPrefix(line, "AUTH "):
			if !secure {
				io.WriteString(conn, "530 TLS required\r\n")
				return
			}
			if failure == "auth" {
				io.WriteString(conn, "535 synthetic-password private-rejection\r\n")
				continue
			}
			if line == "AUTH LOGIN" {
				io.WriteString(conn, "334 VXNlcm5hbWU6\r\n")
				u, _ := reader.ReadString('\n')
				io.WriteString(conn, "334 UGFzc3dvcmQ6\r\n")
				p, _ := reader.ReadString('\n')
				if strings.TrimSpace(u) != base64.StdEncoding.EncodeToString([]byte(f.config.Username)) || strings.TrimSpace(p) != base64.StdEncoding.EncodeToString([]byte(f.config.Password)) {
					return
				}
			} else {
				data, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "AUTH PLAIN "))
				if e != nil || string(data) != "\x00"+f.config.Username+"\x00"+f.config.Password {
					return
				}
			}
			io.WriteString(conn, "235 authenticated\r\n")
		case strings.HasPrefix(line, "MAIL FROM:"):
			io.WriteString(conn, "250 sender accepted\r\n")
		case strings.HasPrefix(line, "RCPT TO:"):
			if failure == "recipient" {
				io.WriteString(conn, "550 private@example.test rejected\r\n")
			} else {
				io.WriteString(conn, "250 recipient accepted\r\n")
			}
		case line == "DATA":
			f.dataAttempts.Add(1)
			io.WriteString(conn, "354 send\r\n")
			var message strings.Builder
			for {
				part, e := reader.ReadString('\n')
				if e != nil {
					return
				}
				if part == ".\r\n" {
					break
				}
				message.WriteString(part)
			}
			f.mu.Lock()
			f.messages = append(f.messages, message.String())
			f.mu.Unlock()
			if failure == "lost-final" {
				return
			}
			if failure == "data" {
				io.WriteString(conn, "554 private-code-1234567 rejected\r\n")
			} else {
				io.WriteString(conn, "250 queued\r\n")
			}
		case line == "QUIT":
			return
		default:
			io.WriteString(conn, "500 unsupported\r\n")
		}
	}
}

func TestSMTPAuthenticatedTLSVerification(t *testing.T) {
	for _, security := range []string{"implicit", "starttls"} {
		t.Run(security, func(t *testing.T) {
			f := localSMTP(t, security, "")
			sender, e := NewSMTP(f.config)
			if e != nil {
				t.Fatal(e)
			}
			if f.connections.Load() != 0 {
				t.Fatal("constructor connected")
			}
			if e = sender.SendVerification(context.Background(), "recipient@example.test", "register", "1234567"); e != nil {
				t.Fatal(e)
			}
			if f.connections.Load() != 1 || f.dataAttempts.Load() != 1 {
				t.Fatal("not exactly one session/DATA")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.messages) != 1 {
				t.Fatal("not accepted")
			}
			msg, e := mail.ReadMessage(strings.NewReader(f.messages[0]))
			if e != nil {
				t.Fatal(e)
			}
			if msg.Header.Get("To") != "recipient@example.test" || msg.Header.Get("From") != "sender@example.test" || msg.Header.Get("Bcc") != "" {
				t.Fatal("wrong envelope headers")
			}
			body, _ := io.ReadAll(base64.NewDecoder(base64.StdEncoding, msg.Body))
			if !strings.Contains(string(body), "1234567") || !strings.Contains(string(body), "注册") {
				t.Fatal("wrong purpose/code")
			}
		})
	}
}
func TestSMTPRejectsInjectionAndUnsupportedInputsWithoutIO(t *testing.T) {
	f := localSMTP(t, "implicit", "")
	sender, e := NewSMTP(f.config)
	if e != nil {
		t.Fatal(e)
	}
	for _, in := range [][3]string{{"x@example.test\r\nBcc: victim@example.test", "register", "1234567"}, {"Name <x@example.test>", "register", "1234567"}, {"a@example.test,b@example.test", "register", "1234567"}, {"x@example.test", "login", "1234567"}, {"x@example.test", "recover", "1234567\r\n"}, {"x@example.test", "register", "0123456"}} {
		if e = sender.SendVerification(context.Background(), in[0], in[1], in[2]); e != ErrSMTP {
			t.Fatal("unsafe input accepted")
		}
	}
	if f.connections.Load() != 0 {
		t.Fatal("invalid input connected")
	}
	raw, _ := json.Marshal(f.config)
	if strings.Contains(string(raw), f.config.Password) || strings.Contains(string(raw), f.config.Username) {
		t.Fatal("credential serialized")
	}
}
func TestSMTPFailClosedRedactedAndNeverRetries(t *testing.T) {
	for _, failure := range []string{"no-starttls", "no-auth", "auth", "recipient", "data", "lost-final", "oversize", "wrong-ca", "wrong-name", "login-only"} {
		t.Run(failure, func(t *testing.T) {
			f := localSMTP(t, "starttls", failure)
			config := f.config
			if failure == "wrong-ca" {
				config.RootCAs = x509.NewCertPool()
			}
			sender, e := NewSMTP(config)
			if e != nil {
				t.Fatal(e)
			}
			e = sender.SendVerification(context.Background(), "recipient@example.test", "recover", "7654321")
			if failure == "login-only" {
				if e != nil {
					t.Fatal(e)
				}
			} else if e != ErrSMTP {
				t.Fatalf("failure was not redacted: %v", e)
			}
			if f.connections.Load() != 1 || f.dataAttempts.Load() > 1 {
				t.Fatal("unexpected retry")
			}
			if failure == "no-starttls" || failure == "wrong-ca" || failure == "wrong-name" || failure == "no-auth" {
				f.mu.Lock()
				defer f.mu.Unlock()
				for _, command := range f.commands {
					if command == "AUTH" || command == "MAIL" || command == "DATA" {
						t.Fatal("sent before verified TLS")
					}
				}
			}
		})
	}
}
func TestSMTPCancellationAndDeadline(t *testing.T) {
	f := localSMTP(t, "implicit", "stall")
	sender, e := NewSMTP(f.config)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	if e = sender.SendVerification(ctx, "recipient@example.test", "recover", "7654321"); e != ErrSMTP || time.Since(start) > time.Second {
		t.Fatal("deadline not bounded")
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if e = sender.SendVerification(ctx, "recipient@example.test", "register", "1234567"); e != ErrSMTP {
		t.Fatal("cancelled send accepted")
	}
	if f.connections.Load() != 1 {
		t.Fatal("cancelled request connected")
	}
}
