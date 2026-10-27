// Package mailer provides bounded verification-message submission. SMTP DATA
// acceptance is not proof of inbox delivery. It never retries uncertain sends.
package mailer

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrSMTP = errors.New("verification email submission unavailable")

type SMTPConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"`
	Username string `json:"-"`
	Password string `json:"-"`
	From     string `json:"from"`
	FromName string `json:"from_name,omitempty"`
	ReplyTo  string `json:"reply_to,omitempty"`
	// RootCAs is an explicit trust hook for synthetic local TLS tests/private PKI.
	// Nil uses system trust. Hostname verification is always required.
	RootCAs *x509.CertPool `json:"-"`
}
type SMTP struct {
	host, address, security, username, password, from string
	fromName, replyTo                                 string
	roots                                             *x509.CertPool
}

// NewSMTP performs no DNS lookup, connection, credential discovery or file I/O.
func NewSMTP(c SMTPConfig) (*SMTP, error) {
	if !validHost(c.Host) || c.Port < 1 || c.Port > 65535 || (c.Security != "implicit" && c.Security != "starttls") || !safeText(c.Username, 254) || !safeText(c.Password, 1024) || !mailbox(c.From) {
		return nil, ErrSMTP
	}
	if (c.FromName != "" && !safeText(c.FromName, 256)) || (c.ReplyTo != "" && !mailbox(c.ReplyTo)) {
		return nil, ErrSMTP
	}
	var roots *x509.CertPool
	if c.RootCAs != nil {
		roots = c.RootCAs.Clone()
	}
	return &SMTP{host: c.Host, address: net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), security: c.Security, username: c.Username, password: c.Password, from: c.From, fromName: c.FromName, replyTo: c.ReplyTo, roots: roots}, nil
}
func (s *SMTP) headerFrom() string {
	if s.fromName == "" {
		return s.from
	}
	return (&mail.Address{Name: s.fromName, Address: s.from}).String()
}
func safeText(s string, max int) bool {
	if len(s) == 0 || len(s) > max || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func validHost(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	if net.ParseIP(s) != nil {
		return true
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}
func mailbox(s string) bool {
	if len(s) == 0 || len(s) > 254 {
		return false
	}
	for _, r := range s {
		if r < 33 || r > 126 || strings.ContainsRune("<>\"()\\,;", r) {
			return false
		}
	}
	a, e := mail.ParseAddress(s)
	return e == nil && a.Name == "" && a.Address == s
}
func verificationMessage(from, to, purpose, code string) []byte {
	action := "注册"
	if purpose == "recover" {
		action = "重置密码"
	}
	if purpose == "contact-old" {
		action = "验证原邮箱"
	}
	if purpose == "contact-new" {
		action = "绑定新邮箱"
	}
	body := fmt.Sprintf("您的验证码是：%s，用于 GOEPAY %s。验证码10分钟内有效，请勿泄露；如非本人操作请忽略。\r\n", code, action)
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	var lines strings.Builder
	for len(encoded) > 76 {
		lines.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	lines.WriteString(encoded + "\r\n")
	return []byte("From: " + from + "\r\nTo: " + to + "\r\nSubject: " + mime.QEncoding.Encode("UTF-8", "GOEPAY - 验证码获取") + "\r\nDate: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n" + lines.String())
}

// readBudgetConn bounds all inbound protocol bytes, including the TLS
// handshake. A malicious multiline greeting/reply cannot allocate indefinitely.
type readBudgetConn struct {
	net.Conn
	left int
}

func (c *readBudgetConn) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, ErrSMTP
	}
	if len(p) > c.left {
		p = p[:c.left]
	}
	n, e := c.Conn.Read(p)
	c.left -= n
	return n, e
}

func (s *SMTP) SendVerification(ctx context.Context, to, purpose, code string) error {
	if s == nil || ctx == nil || ctx.Err() != nil || !mailbox(to) || (purpose != "register" && purpose != "recover" && purpose != "contact-old" && purpose != "contact-new") || len(code) != 7 || code[0] < '1' || code[0] > '9' {
		return ErrSMTP
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return ErrSMTP
		}
	}
	return s.send(ctx, to, verificationMessage(s.headerFrom(), to, purpose, code))
}

func (s *SMTP) send(ctx context.Context, to string, message []byte) error {
	if s != nil && s.replyTo != "" {
		message = append([]byte("Reply-To: "+s.replyTo+"\r\n"), message...)
	}
	if s == nil || ctx == nil || ctx.Err() != nil || !mailbox(to) || len(message) == 0 || len(message) > 4096 {
		return ErrSMTP
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, e := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", s.address)
	if e != nil {
		return ErrSMTP
	}
	defer raw.Close()
	deadline, _ := ctx.Deadline()
	if raw.SetDeadline(deadline) != nil {
		return ErrSMTP
	}
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()
	var conn net.Conn = &readBudgetConn{Conn: raw, left: 64 << 10}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: s.host, RootCAs: s.roots}
	if s.security == "implicit" {
		secure := tls.Client(conn, cfg)
		if secure.HandshakeContext(ctx) != nil {
			return ErrSMTP
		}
		conn = secure
	}
	client, e := smtp.NewClient(conn, s.host)
	if e != nil {
		return ErrSMTP
	}
	defer client.Close()
	if s.security == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return ErrSMTP
		}
		if client.StartTLS(cfg) != nil {
			return ErrSMTP
		}
	}
	state, ok := client.TLSConnectionState()
	if !ok || !state.HandshakeComplete || len(state.VerifiedChains) == 0 {
		return ErrSMTP
	}
	advertised, mechanisms := client.Extension("AUTH")
	if !advertised {
		return ErrSMTP
	}
	var auth smtp.Auth
	for _, mechanism := range strings.Fields(mechanisms) {
		if mechanism == "PLAIN" {
			auth = smtp.PlainAuth("", s.username, s.password, s.host)
			break
		}
	}
	if auth == nil {
		for _, mechanism := range strings.Fields(mechanisms) {
			if mechanism == "LOGIN" {
				auth = &loginAuth{host: s.host, username: s.username, password: s.password}
				break
			}
		}
	}
	if auth == nil || client.Auth(auth) != nil {
		return ErrSMTP
	}
	if client.Mail(s.from) != nil || client.Rcpt(to) != nil {
		return ErrSMTP
	}
	writer, e := client.Data()
	if e != nil {
		return ErrSMTP
	}
	if _, e = writer.Write(message); e != nil {
		return ErrSMTP
	}
	if writer.Close() != nil {
		return ErrSMTP
	}
	// DATA's final250 already accepted the submission. A lost QUIT reply must
	// never turn an accepted message into an invitation to resend its code.
	return nil
}

type loginAuth struct {
	host, username, password string
	step                     int
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS || server.Name != a.host {
		return "", nil, ErrSMTP
	}
	return "LOGIN", nil, nil
}
func (a *loginAuth) Next(challenge []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	a.step++
	if a.step == 1 && strings.EqualFold(string(challenge), "Username:") {
		return []byte(a.username), nil
	}
	if a.step == 2 && strings.EqualFold(string(challenge), "Password:") {
		return []byte(a.password), nil
	}
	return nil, ErrSMTP
}

var _ interface {
	SendVerification(context.Context, string, string, string) error
} = (*SMTP)(nil)
var _ io.Reader = (*readBudgetConn)(nil)
