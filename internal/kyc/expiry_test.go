package kyc

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"goepay/internal/identity"
)

type expiryProvider233 struct{ calls atomic.Int64 }

func (p *expiryProvider233) Initialize(context.Context, Snapshot, Submission, Attempt) (InitResult, error) {
	p.calls.Add(1)
	return InitResult{}, ErrUnavailable
}
func (p *expiryProvider233) Query(context.Context, Snapshot, Attempt) (QueryResult, error) {
	p.calls.Add(1)
	return QueryResult{}, ErrUnavailable
}
func (p *expiryProvider233) Recognize(context.Context, Snapshot, string, []byte) (OCRResult, error) {
	p.calls.Add(1)
	return OCRResult{}, ErrUnavailable
}

func expiryPool233(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("synthetic PostgreSQL required")
	}
	ctx := t.Context()
	base, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	id, e := randomID()
	if e != nil {
		base.Close()
		t.Fatal(e)
	}
	schema := "kyc233_" + id
	if _, e = base.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); e != nil {
		base.Close()
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.ConnConfig.RuntimeParams["application_name"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		pool.Close()
		defer base.Close()
		var count int
		if err := base.QueryRow(context.Background(), "SELECT count(*) FROM pg_stat_activity WHERE application_name=$1", schema).Scan(&count); err != nil || count != 0 {
			t.Errorf("owned schema retained: connections=%d error=%v", count, err)
			return
		}
		if _, err := base.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	if e = db.Migrate(ctx, pool); e != nil {
		t.Fatal(e)
	}
	return pool
}

func TestKYC233ExpiredVerifiedProofAllowsFreshExplicitSubmission(t *testing.T) {
	p := expiryPool233(t)
	ctx := t.Context()
	if _, e := p.Exec(ctx, `INSERT INTO merchants(id,name,api_key,profile)VALUES(1,'Synthetic expiry',decode('00','hex'),'{"bind_mobile":"13800138000"}');INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class)VALUES('admin233','admin',233,'Synthetic Admin','unused','super'),('merchant233','merchant',1,'Synthetic Merchant','unused','operator')`); e != nil {
		t.Fatal(e)
	}
	sessions := identity.NewPersistentSessions(p, false)
	adminUser := identity.User{ID: 233, Role: "admin", Username: "admin233", AdminClass: "super", AuthVersion: 1}
	merchantUser := identity.User{ID: 1, Role: "merchant", Username: "merchant233", AuthVersion: 1}
	adminToken, adminCSRF, e := sessions.Create(adminUser)
	if e != nil {
		t.Fatal(e)
	}
	merchantToken, merchantCSRF, e := sessions.Create(merchantUser)
	if e != nil {
		t.Fatal(e)
	}
	provider := &expiryProvider233{}
	s, e := New(p, make([]byte, 32), provider, nil, "https://synthetic.example.invalid")
	if e != nil {
		t.Fatal(e)
	}
	edits := map[string]CredentialEdit{}
	for _, k := range CredentialKeys() {
		edits[k] = CredentialEdit{Action: "keep"}
	}
	edits["cert_appcode"] = CredentialEdit{Action: "replace", Value: "synthetic-expiry-appcode"}
	if _, e = s.UpdateConfig(ctx, Actor{User: adminUser, Token: adminToken, CSRF: adminCSRF}, ConfigInput{Mode: 2, ChannelID: "0", Money: "0.00", Credentials: edits}); e != nil {
		t.Fatal(e)
	}
	merchant := Actor{User: merchantUser, Token: merchantToken, CSRF: merchantCSRF}
	input := Submission{ExpectedRevision: 1, Name: "本地测试", Number: "11010519491231002X", Consent: true}
	first, e := s.Submit(ctx, merchant, input)
	if e != nil {
		t.Fatal(e)
	}
	// A valid historical checkpoint models a crash between proof persistence
	// and finalization. Encrypt and authenticate it using the domain writer.
	tx, e := p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	row, e := s.readAttempt(ctx, tx, first.AttemptID, 1, true)
	if e != nil {
		tx.Rollback(ctx)
		t.Fatal(e)
	}
	old := time.Now().UTC().Truncate(time.Microsecond).Add(-25 * time.Hour)
	row.Status = "verified"
	row.Data.VerifiedAt = old
	row.Data.OperationStartedAt = old.Add(-time.Minute)
	row.Expires = old.Add(29 * time.Minute)
	if e = s.writeAttempt(ctx, tx, &row, false); e != nil {
		tx.Rollback(ctx)
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	info, e := s.Info(ctx, merchant)
	if e != nil || info.Status != "expired" {
		t.Fatal("expired proof not actionable", info.Status, e)
	}
	next, e := s.Submit(ctx, merchant, input)
	if e != nil || next.Status != "ready" || next.AttemptID == first.AttemptID {
		t.Fatal("fresh explicit submission blocked", next.Status, e)
	}
	var oldStatus string
	var charges int
	if e = p.QueryRow(ctx, "SELECT status,(SELECT count(*) FROM journals WHERE event_key LIKE 'certification233:%') FROM kyc_attempts233 WHERE id=$1", first.AttemptID).Scan(&oldStatus, &charges); e != nil || oldStatus != "expired" || charges != 0 || provider.calls.Load() != 0 {
		t.Fatal(fmt.Sprint("old proof repeated or charged: ", oldStatus, " ", charges), e)
	}
}
