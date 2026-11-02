package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"goepay/internal/identity"
	"goepay/internal/migration"
	"goepay/internal/payment"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func migrationUpload234(t *testing.T, extra string) *http.Request {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	if e := w.WriteField("source", "epay_pro"); e != nil {
		t.Fatal(e)
	}
	if e := w.WriteField("prefix", "pay_"); e != nil {
		t.Fatal(e)
	}
	f, e := w.CreateFormFile("backup", "synthetic.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Write([]byte("INSERT INTO `pay_type` (`id`,`name`) VALUES (1,'alipay');")); e != nil {
		t.Fatal(e)
	}
	if extra != "" {
		if e = w.WriteField(extra, "untrusted"); e != nil {
			t.Fatal(e)
		}
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", "/console/v1/admin/migration/epay/prepare", &b)
	r.Header.Set("Content-Type", w.FormDataContentType())
	return r
}

const syntheticSQL234 = `CREATE TABLE pay_user (uid bigint,money decimal(10,2),addtime datetime,username varchar(128),` + "`key`" + ` varchar(32));
INSERT INTO pay_user VALUES(9001,12.50,'2026-10-01 00:00:00','Synthetic Recipient','synthetic-source-key-never-echoed');
CREATE TABLE pay_type (id int,name varchar(32)); INSERT INTO pay_type VALUES(1,'alipay');
CREATE TABLE pay_order (trade_no varchar(32),uid int,out_trade_no varchar(64),name varchar(64),type int,money decimal(10,2),realmoney decimal(10,2),status int,addtime datetime,endtime datetime,tid int,version int);
INSERT INTO pay_order VALUES('2340000000000000001',9001,'legacy-client-234','Synthetic Product',1,1.00,1.00,1,'2026-10-01 01:00:00','2026-10-01 01:01:00',0,0);`

func migrationHTTP234(t *testing.T) (*Server, *pgxpool.Pool, string, string) {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	root, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	var entropy [12]byte
	if _, e = rand.Read(entropy[:]); e != nil {
		t.Fatal(e)
	}
	schema := "wizardhttp234_" + hex.EncodeToString(entropy[:])
	if _, e = root.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	c, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	c.ConnConfig.RuntimeParams["search_path"] = schema
	p, e := pgxpool.NewWithConfig(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Close(); root.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); root.Close() })
	if e = db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('wizard234','admin',234,'Synthetic Admin','unused-hash','super')`); e != nil {
		t.Fatal(e)
	}
	s := &Server{Pool: p, CheckoutKey: bytes.Repeat([]byte{42}, 32), Sessions: identity.NewPersistentSessions(p, false)}
	s.MigrationWizard234, e = migration.NewWizard234(p, bytes.Repeat([]byte{23}, 32))
	if e != nil {
		t.Fatal(e)
	}
	token, csrf, e := s.Sessions.Create(identity.User{ID: 234, Username: "wizard234", Role: "admin", AdminClass: "super", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	return s, p, token, csrf
}

func callWizard234(s *Server, token, csrf, method, path string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "/console/v1/admin/migration/epay"+path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func prepareWizard234(t *testing.T, s *Server, token, csrf string) migration.WizardView234 {
	t.Helper()
	return prepareWizardDump234(t, s, token, csrf, syntheticSQL234)
}

func prepareWizardDump234(t *testing.T, s *Server, token, csrf, raw string) migration.WizardView234 {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	w.WriteField("source", "epay_pro")
	w.WriteField("prefix", "pay_")
	part, e := w.CreateFormFile("backup", "ignored-private-filename.sql")
	if e != nil {
		t.Fatal(e)
	}
	part.Write([]byte(raw))
	w.Close()
	r := httptest.NewRequest("POST", "/console/v1/admin/migration/epay/prepare", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	r.Header.Set("X-CSRF-Token", csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	out := httptest.NewRecorder()
	s.Handler().ServeHTTP(out, r)
	if strings.Contains(out.Body.String(), "synthetic-source-key") || strings.Contains(out.Body.String(), "Synthetic Recipient") {
		t.Fatal("private backup leaked")
	}
	return kyc233View[migration.WizardView234](t, out)
}

func TestMigration234HTTPBackupOverDefaultBodyLimit(t *testing.T) {
	s, _, token, csrf := migrationHTTP234(t)
	// Use small valid comment tokens to exceed the default HTTP body limit
	// without exceeding any SQL token, row or column limit.
	raw := strings.Repeat("-- synthetic padding for large backup\n", 40000) + syntheticSQL234
	if len(raw) <= 1<<20 {
		t.Fatal("large backup regression fixture is too small")
	}
	v := prepareWizardDump234(t, s, token, csrf, raw)
	if v.Status != "prepared" || len(v.Issues) != 0 || v.Merchants != 1 || v.Orders != 1 {
		t.Fatal("valid backup above the ordinary request limit was not prepared", v.Status, v.Issues)
	}
}

func TestMigration234HTTPCompleteApplyReplayAndNoJobs(t *testing.T) {
	s, p, token, csrf := migrationHTTP234(t)
	caps := kyc233View[migration.Capabilities234](t, callWizard234(s, token, csrf, "GET", "", nil))
	if !caps.TargetReady {
		t.Fatal("fresh target rejected", caps.Issues)
	}
	v := prepareWizard234(t, s, token, csrf)
	if v.Status != "prepared" || len(v.Issues) != 0 || v.Merchants != 1 || v.Orders != 1 {
		t.Fatal("prepare failed", v.Status, v.Issues)
	}
	in := migration.ApplyInput234{ExpectedSHA256: v.SHA256, SourceFrozen: true, ArchiveAcknowledged: true, Confirmation: "MIGRATE"}
	for i := 0; i < 2; i++ {
		out := kyc233View[migration.WizardView234](t, callWizard234(s, token, csrf, "POST", "/"+v.ID+"/apply", in))
		if out.Status != "completed" || out.Report == nil {
			t.Fatal("result not completed", out.Status)
		}
	}
	var balance, journals, jobs int64
	var enabled bool
	if e := p.QueryRow(t.Context(), `SELECT (SELECT balance FROM accounts WHERE owner_id=9001 AND kind='available'),(SELECT count(*) FROM journals),(SELECT count(*) FROM jobs),(SELECT enabled FROM merchants WHERE id=9001)`).Scan(&balance, &journals, &jobs, &enabled); e != nil || balance != 1250 || journals != 1 || jobs != 0 || enabled {
		t.Fatal("migration duplicated funds, created jobs or activated merchant", balance, journals, jobs, enabled, e)
	}
	got := kyc233View[migration.WizardView234](t, callWizard234(s, token, csrf, "GET", "/"+v.ID, nil))
	if got.Status != "completed" {
		t.Fatal("status unreadable")
	}
	store, e := payment.NewWithKey(p, bytes.Repeat([]byte{23}, 32))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = store.Merchant(t.Context(), 9001); e != nil {
		t.Fatal("imported merchant cannot be read using the business master key")
	}
	check, e := migration.CheckRefundHistory(t.Context(), p, bytes.Repeat([]byte{23}, 32))
	if e != nil || check.InvalidLegacyRefundHistory != 0 {
		t.Fatal("imported history does not reconcile with the business master key")
	}
}

func TestMigration234HTTPConfirmationCSRFAndTargetRace(t *testing.T) {
	s, p, token, csrf := migrationHTTP234(t)
	v := prepareWizard234(t, s, token, csrf)
	if got := callWizard234(s, token, "", "POST", "/"+v.ID+"/apply", map[string]any{}).Code; got != 403 {
		t.Fatal("CSRF allowed", got)
	}
	if got := callWizard234(s, token, csrf, "POST", "/"+v.ID+"/apply", map[string]any{"expected_sha256": v.SHA256, "source_frozen": true, "archive_acknowledged": true, "confirmation": "MIGRATE", "extra": "unsafe"}).Code; got != 400 {
		t.Fatal("extra field allowed", got)
	}
	bad := migration.ApplyInput234{ExpectedSHA256: v.SHA256, Confirmation: "MIGRATE"}
	if got := callWizard234(s, token, csrf, "POST", "/"+v.ID+"/apply", bad).Code; got < 400 {
		t.Fatal("unacknowledged import allowed")
	}
	if _, e := p.Exec(t.Context(), `INSERT INTO merchants(id,name,api_key,enabled) VALUES(9999,'Existing',decode('00','hex'),false)`); e != nil {
		t.Fatal(e)
	}
	bad.SourceFrozen = true
	bad.ArchiveAcknowledged = true
	if got := callWizard234(s, token, csrf, "POST", "/"+v.ID+"/apply", bad).Code; got != 409 {
		t.Fatal("target race permitted", got)
	}
	var n int
	if e := p.QueryRow(t.Context(), `SELECT count(*) FROM merchants WHERE id=9001`).Scan(&n); e != nil || n != 0 {
		t.Fatal("source data written to occupied target", n, e)
	}
}

func TestMigration234HTTPAdmissionAndPermissionsBeforeBody(t *testing.T) {
	s, p, token, csrf := migrationHTTP234(t)
	migrationUploadAdmission234 <- struct{}{}
	defer func() {
		for len(migrationUploadAdmission234) > 0 {
			<-migrationUploadAdmission234
		}
	}()
	for _, restricted := range []bool{false, true} {
		if restricted {
			if _, e := p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('wizardoperator234','admin',235,'Synthetic Operator','unused-hash','operator')`); e != nil {
				t.Fatal(e)
			}
			var e error
			token, csrf, e = s.Sessions.Create(identity.User{ID: 235, Username: "wizardoperator234", Role: "admin", AdminClass: "operator", AuthVersion: 1})
			if e != nil {
				t.Fatal(e)
			}
		}
		body := &rejectedOCRBody233{}
		r := httptest.NewRequest("POST", "/console/v1/admin/migration/epay/prepare", body)
		r.Header.Set("X-CSRF-Token", csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		want := 429
		if restricted {
			want = 403
		}
		if w.Code != want || body.read {
			t.Fatal("restricted upload consumed input", w.Code, want, body.read)
		}
	}
}

func TestMigration234HTTPRequiresExplicitBusinessKeyBinding(t *testing.T) {
	s, _, token, csrf := migrationHTTP234(t)
	s.MigrationWizard234 = nil
	if got := callWizard234(s, token, csrf, "GET", "", nil).Code; got != 503 {
		t.Fatal("silently fell back to checkout signing key", got)
	}
}

func TestMigration234MultipartPreservesBytesAndRejectsAmbiguity(t *testing.T) {
	raw, source, prefix, e := readMigrationDump234(migrationUpload234(t, ""))
	if e != nil || source != "epay_pro" || prefix != "pay_" || !strings.HasPrefix(string(raw), "INSERT INTO") {
		t.Fatal("valid backup rejected", e)
	}
	for _, field := range []string{"source", "prefix", "backup", "database_url", "token"} {
		if _, _, _, e = readMigrationDump234(migrationUpload234(t, field)); e == nil {
			t.Fatal("ambiguous upload accepted", field)
		}
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader("not a multipart file"))
	r.Header.Set("Content-Type", "application/json")
	if _, _, _, e = readMigrationDump234(r); e == nil {
		t.Fatal("non-multipart accepted")
	}
	blockedBody := &rejectedOCRBody233{}
	r = httptest.NewRequest("POST", "/", blockedBody)
	r.ContentLength = (64 << 20) + (129 << 10)
	if _, _, _, e = readMigrationDump234(r); e == nil || blockedBody.read {
		t.Fatal("oversized declared request read body")
	}
}

func TestMigration234ErrorsNeverEchoSourceOrDatabaseDetails(t *testing.T) {
	for _, e := range []error{migration.ErrWizardBusy234, migration.ErrWizardCapacity234, migration.ErrWizardInvalid234, migration.ErrWizardTarget234, migration.ErrWizardConflict234, errors.New("postgres://private-synthetic-password@internal/schema: source-secret")} {
		w := httptest.NewRecorder()
		migrationError234(w, e)
		if w.Code < 400 || strings.Contains(w.Body.String(), "private-synthetic-password") || strings.Contains(w.Body.String(), "source-secret") {
			t.Fatal("migration error leaked details")
		}
		if errors.Is(e, migration.ErrWizardCapacity234) && (w.Code != 429 || w.Header().Get("Retry-After") == "") {
			t.Fatal("capacity response missing bounded retry hint")
		}
	}
}
