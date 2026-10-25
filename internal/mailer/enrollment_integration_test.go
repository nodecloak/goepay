package mailer

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"goepay/internal/enrollment"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func smtpEnrollmentPool(t *testing.T) (*pgxpool.Pool, *payment.Store) {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("exclusive local PostgreSQL required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal("test database unavailable")
	}
	nonce := make([]byte, 12)
	if _, e = rand.Read(nonce); e != nil {
		t.Fatal(e)
	}
	schema := "smtp_" + hex.EncodeToString(nonce)
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		pool.Close()
		_, e := admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
		if e != nil {
			t.Error("owned test schema cleanup failed")
		}
	})
	if e = db.Migrate(ctx, pool); e != nil {
		t.Fatal(e)
	}
	store, e := payment.NewWithKey(pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('settings','{"key":"registration_open","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	return pool, store
}
func publicSMTPRequest(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "https://console.example.invalid/console/v1/public/"+path, strings.NewReader(string(raw)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://console.example.invalid")
	r.RemoteAddr = "192.0.2.81:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func receivedVerification(t *testing.T, f *smtpFixture) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.messages) == 0 {
		t.Fatal("no synthetic SMTP message")
	}
	msg, e := mail.ReadMessage(strings.NewReader(f.messages[len(f.messages)-1]))
	if e != nil {
		t.Fatal("invalid synthetic MIME")
	}
	body, e := io.ReadAll(base64.NewDecoder(base64.StdEncoding, msg.Body))
	if e != nil {
		t.Fatal("invalid synthetic encoding")
	}
	code := regexp.MustCompile(`验证码是：([1-9][0-9]{6})`).FindStringSubmatch(string(body))
	if len(code) != 2 {
		t.Fatal("synthetic message has no verification code")
	}
	return code[1]
}
func issueSMTPChallenge(t *testing.T, h http.Handler, f *smtpFixture, purpose, email string) (string, string) {
	t.Helper()
	w := publicSMTPRequest(t, h, "POST", "verification-code", map[string]string{"purpose": purpose, "email": email})
	if w.Code != 202 {
		t.Fatalf("verification request status=%d", w.Code)
	}
	code := receivedVerification(t, f)
	var out struct {
		Data enrollment.Challenge `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.ID == "" {
		t.Fatal("missing opaque challenge")
	}
	if strings.Contains(w.Body.String(), code) || strings.Contains(w.Body.String(), email) || len(w.Result().Cookies()) > 0 {
		t.Fatal("verification response exposed secret or session")
	}
	return out.Data.ID, code
}

func TestSMTPPublicEnrollmentRegistrationRecoveryAndDefaultClosed(t *testing.T) {
	pool, store := smtpEnrollmentPool(t)
	f := localSMTP(t, "starttls", "")
	sender, e := NewSMTP(f.config)
	if e != nil {
		t.Fatal(e)
	}
	service, e := enrollment.New(pool, store, make([]byte, 32), sender)
	if e != nil {
		t.Fatal(e)
	}
	h := enrollment.NewHandler(service, "https://console.example.invalid")
	w := publicSMTPRequest(t, h, "GET", "capabilities", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"email_enabled":true`) {
		t.Fatal("configured sender not available")
	}
	challenge, code := issueSMTPChallenge(t, h, f, "register", "new@example.test")
	w = publicSMTPRequest(t, h, "POST", "register", map[string]string{"challenge_id": challenge, "code": code, "email": "new@example.test", "username": "smtp-new", "name": "SMTP local", "password": "synthetic-first-password"})
	if w.Code != 201 || len(w.Result().Cookies()) > 0 || strings.Contains(w.Body.String(), code) || strings.Contains(w.Body.String(), "api_key") {
		t.Fatalf("registration did not complete safely status=%d", w.Code)
	}
	var result struct {
		Data enrollment.Registered `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Data.MerchantID <= 0 {
		t.Fatal("no registered merchant")
	}
	var accountBalance int64
	if e = pool.QueryRow(context.Background(), "SELECT COALESCE(sum(balance),0) FROM accounts WHERE owner_id=$1", result.Data.MerchantID).Scan(&accountBalance); e != nil || accountBalance != 0 {
		t.Fatal("registration changed financial balance")
	}
	sessions := identity.NewPersistentSessions(pool, true)
	old, _, e := sessions.Create(identity.User{ID: result.Data.MerchantID, Username: "smtp-new", Role: "merchant", AuthVersion: 1})
	if e != nil {
		t.Fatal("session fixture unavailable")
	}
	if _, e = pool.Exec(context.Background(), "UPDATE enrollment_challenges SET created_at=created_at-interval '61 seconds'"); e != nil {
		t.Fatal(e)
	}
	challenge, code = issueSMTPChallenge(t, h, f, "recover", "new@example.test")
	request := map[string]string{"challenge_id": challenge, "code": code, "email": "new@example.test", "new_password": "synthetic-replacement-password"}
	w = publicSMTPRequest(t, h, "POST", "recover-password", request)
	if w.Code != 200 || len(w.Result().Cookies()) > 0 || strings.Contains(w.Body.String(), code) {
		t.Fatal("recovery did not complete safely")
	}
	if _, valid := sessions.Get(old, "merchant"); valid {
		t.Fatal("recovery retained original session")
	}
	var hash string
	var version int64
	if e = pool.QueryRow(context.Background(), "SELECT password_hash,auth_version FROM console_users WHERE username='smtp-new' AND role='merchant'").Scan(&hash, &version); e != nil || version != 2 || !identity.CheckPassword(hash, "synthetic-replacement-password") {
		t.Fatal("recovery credential/version not updated")
	}
	if replay := publicSMTPRequest(t, h, "POST", "recover-password", request); replay.Code == 200 {
		t.Fatal("recovery proof reused")
	}
	unknownID, unknownCode := issueSMTPChallenge(t, h, f, "recover", "unknown@example.test")
	unknown := publicSMTPRequest(t, h, "POST", "recover-password", map[string]string{"challenge_id": unknownID, "code": unknownCode, "email": "unknown@example.test", "new_password": "synthetic-replacement-password"})
	if unknown.Code != 200 || unknown.Body.String() != w.Body.String() {
		t.Fatal("unknown recovery reveals identity")
	}
	closed, _ := enrollment.New(pool, store, make([]byte, 32), nil)
	closedHTTP := enrollment.NewHandler(closed, "https://console.example.invalid")
	w = publicSMTPRequest(t, closedHTTP, "GET", "capabilities", nil)
	if !strings.Contains(w.Body.String(), `"email_enabled":false`) {
		t.Fatal("unconfigured sender advertised")
	}
	w = publicSMTPRequest(t, closedHTTP, "POST", "verification-code", map[string]string{"purpose": "recover", "email": "closed@example.test"})
	if w.Code != 503 {
		t.Fatal("nil sender pretended to send")
	}
	if f.connections.Load() != 3 || f.dataAttempts.Load() != 3 {
		t.Fatal("unexpected duplicate SMTP submission")
	}
}

func TestSMTPPublicEnrollmentFailedSubmission503AndUnusableProof(t *testing.T) {
	pool, store := smtpEnrollmentPool(t)
	f := localSMTP(t, "implicit", "lost-final")
	sender, e := NewSMTP(f.config)
	if e != nil {
		t.Fatal(e)
	}
	service, _ := enrollment.New(pool, store, make([]byte, 32), sender)
	h := enrollment.NewHandler(service, "https://console.example.invalid")
	w := publicSMTPRequest(t, h, "POST", "verification-code", map[string]string{"purpose": "register", "email": "failed@example.test"})
	if w.Code != 503 {
		t.Fatal("uncertain SMTP submission reported success")
	}
	code := receivedVerification(t, f)
	if strings.Contains(w.Body.String(), code) || strings.Contains(w.Body.String(), "synthetic-password") {
		t.Fatal("HTTP leaked submission secret")
	}
	var id, state string
	if e = pool.QueryRow(context.Background(), "SELECT id,state FROM enrollment_challenges").Scan(&id, &state); e != nil || state != "failed" {
		t.Fatal("uncertain challenge can be consumed")
	}
	w = publicSMTPRequest(t, h, "POST", "register", map[string]string{"challenge_id": id, "code": code, "email": "failed@example.test", "username": "must-not-exist", "name": "SMTP local", "password": "synthetic-first-password"})
	if w.Code == 201 {
		t.Fatal("failed delivery proof registered merchant")
	}
	if f.connections.Load() != 1 || f.dataAttempts.Load() != 1 {
		t.Fatal("uncertain submission retried")
	}
}
