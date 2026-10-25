package payment

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"testing"
)

// The payment package cannot import enrollment. This fixture constructs its
// complete original registration transaction with all database guards enabled;
// the enrollment package separately tests actual OTP verification and rollback.
func referralBoundMerchants(t *testing.T, s *Store, promoterID, invitedID int64) ReferralRelation {
	t.Helper()
	ctx := context.Background()
	if _, e := s.Merchant(ctx, promoterID); errors.Is(e, ErrNotFound) {
		if e = s.CreateMerchant(ctx, Merchant{ID: promoterID, Name: "local referral promoter", APIKey: "local-fixture-only"}); e != nil {
			t.Fatal(e)
		}
	} else if e != nil {
		t.Fatal(e)
	}
	actor := TransferBatchActor{Role: "merchant", ID: promoterID, Name: "local-referral-promoter"}
	current, e := s.ReferralToken(ctx, promoterID, actor)
	if e != nil && !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	issued, e := s.RotateReferralToken(ctx, promoterID, current.Version, actor)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	challenge := referralRegistrationBirth(t, s, tx, invitedID)
	r, e := s.BindReferralAtRegistrationTx(ctx, tx, invitedID, issued.Token, challenge)
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	return r
}
func referralRegistrationBirth(t *testing.T, s *Store, tx pgx.Tx, mid int64) string {
	t.Helper()
	ctx := context.Background()
	entropy := make([]byte, 32)
	if _, e := rand.Read(entropy); e != nil {
		t.Fatal(e)
	}
	challenge := base64.RawURLEncoding.EncodeToString(entropy)
	hash := sha256.Sum256([]byte(fmt.Sprintf("local-referral-registration:%d", mid)))
	if _, e := tx.Exec(ctx, "INSERT INTO enrollment_challenges(id,purpose,email_hash,source_hash,code_hash,state,expires_at) VALUES($1,'register',$2,$2,$2,'sent',clock_timestamp()+interval '10 minutes')", challenge, hash[:]); e != nil {
		t.Fatal(e)
	}
	if e := s.CreateMerchantInTx(ctx, tx, Merchant{ID: mid, Name: "local referred merchant", APIKey: "local-fixture-only"}); e != nil {
		t.Fatal(e)
	}
	username := fmt.Sprintf("referral-%d", mid)
	email := username + "@example.test"
	password, e := identity.HashPassword("local-referral-fixture-password")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES($1,'merchant',$2,'Local Referral',$3)", username, mid, password); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "INSERT INTO enrollment_emails(email,email_hash,merchant_id,username) VALUES($1,$2,$3,$4)", email, hash[:], mid, username); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation) VALUES('public',$1,'','enrollment',$1,'register')", mid); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE enrollment_challenges SET consumed_at=clock_timestamp() WHERE id=$1 AND consumed_at IS NULL", challenge); e != nil {
		t.Fatal(e)
	}
	return challenge
}
func TestReferralRelationBornWithCompleteOriginalRegistration(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	r := referralBoundMerchants(t, s, 901, 902)
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	read, e := s.ReferralRelationTx(ctx, tx, 902)
	if e != nil || read.ID != r.ID || read.PromoterID != 901 || read.InvitedID != 902 || read.TokenVersion != 1 || len(read.ProofHash) != 32 {
		t.Fatal(read, e)
	}
	if _, e = s.ReferralRelationTx(ctx, tx, 901); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE referral_relations SET promoter_id=902 WHERE id=$1", r.ID); e == nil {
		t.Fatal("referral relation was mutable")
	}
}
