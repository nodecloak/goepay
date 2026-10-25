package payment

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestReferralOwnerTokenIsIssuedOnceEncryptedAndRevisionBound(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 901, Name: "local promoter", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	actor := TransferBatchActor{Role: "merchant", ID: 901, Name: "local-promoter"}
	first, e := s.RotateReferralToken(ctx, 901, 0, actor)
	if e != nil || len(first.Token) != 46 || !strings.HasPrefix(first.Token, "rp_") || first.Version != 1 || !first.Enabled {
		t.Fatal("owner could not create a strong one-time referral token", first.ReferralTokenView, e)
	}
	read, e := s.ReferralToken(ctx, 901, actor)
	if e != nil || read.Version != 1 || read.Masked == first.Token {
		t.Fatal(read, e)
	}
	raw, _ := json.Marshal(read)
	if strings.Contains(string(raw), first.Token) {
		t.Fatal("token read disclosed bearer material")
	}
	var cipher []byte
	var audit string
	if e = s.pool.QueryRow(ctx, "SELECT token_cipher FROM referral_tokens WHERE id=$1", first.ID).Scan(&cipher); e != nil || strings.Contains(string(cipher), first.Token) {
		t.Fatal("token was not encrypted", e)
	}
	if e = s.pool.QueryRow(ctx, "SELECT COALESCE(string_agg(changes::text,''),'') FROM backoffice_audit WHERE resource='referral-tokens'").Scan(&audit); e != nil || strings.Contains(audit, first.Token) {
		t.Fatal("audit exposed bearer material", e)
	}
	if _, e = s.RotateReferralToken(ctx, 901, 0, actor); !errors.Is(e, ErrConflict) {
		t.Fatal("stale issue revision regenerated a token", e)
	}
	next, e := s.RotateReferralToken(ctx, 901, 1, actor)
	if e != nil || next.Token == first.Token || next.Version != 2 {
		t.Fatal(next.ReferralTokenView, e)
	}
	revoked, e := s.RevokeReferralToken(ctx, 901, 2, actor)
	if e != nil || revoked.Version != 3 || revoked.Enabled {
		t.Fatal(revoked, e)
	}
}

func TestReferralExpiredTokenCannotBindFreshVerifiedRegistration(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 901, Name: "local promoter", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	actor := TransferBatchActor{Role: "merchant", ID: 901, Name: "local-promoter"}
	issued, e := s.changeReferralToken(ctx, 901, 0, actor, true, time.Microsecond)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	challenge := referralRegistrationBirth(t, s, tx, 902)
	if _, e = s.BindReferralAtRegistrationTx(ctx, tx, 902, issued.Token, challenge); !errors.Is(e, ErrInvalid) {
		t.Fatal("expired promotion token accepted a new verified registration", e)
	}
}
