package payment

import (
	"errors"
	"goepay/internal/identity"
	"testing"
)

var createSetupAdmin = identity.User{ID: 1, Role: "admin", Username: "local-admin"}

func TestInitialMerchantFeeModeChargesTheBuyerOnNewOrders(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	feeMerchant(t, s, 10421, 600, "platform")
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetInitialMerchantBuyerFeeModeTx(ctx, tx, 10421, 1, createSetupAdmin); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 10421, OutTradeNo: "initial-mode104", Amount: 100, Name: "Synthetic goods"})
	if e != nil {
		t.Fatal(e)
	}
	c, e := s.AuthenticatedOrderCharge104(ctx, o)
	if e != nil || c != 106 || o.MerchantAmount != 100 || o.PlatformFee != 6 || o.FeePolicy != "buyer_additive" {
		t.Fatalf("订单加费 not applied: %+v charge=%d %v", o, c, e)
	}
	tx, e = s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if e = s.SetInitialMerchantBuyerFeeModeTx(ctx, tx, 10421, 1, createSetupAdmin); !errors.Is(e, ErrConflict) {
		t.Fatal("initial mode replaced an existing policy", e)
	}
}

func TestInitialMerchantFeeModeBalanceWritesNothingAndRefusesBadInput(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	feeMerchant(t, s, 10422, 600, "platform")
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	for _, bad := range []struct {
		mode  int
		actor identity.User
	}{
		{2, createSetupAdmin},
		{-1, createSetupAdmin},
		{1, identity.User{ID: 10422, Role: "merchant", Username: "m"}},
		{1, identity.User{Role: "admin", Username: "anonymous"}},
	} {
		if e = s.SetInitialMerchantBuyerFeeModeTx(ctx, tx, 10422, bad.mode, bad.actor); !errors.Is(e, ErrInvalid) {
			t.Fatalf("mode %d by %+v accepted: %v", bad.mode, bad.actor, e)
		}
	}
	if e = s.SetInitialMerchantBuyerFeeModeTx(ctx, tx, 10422, 0, createSetupAdmin); e != nil {
		t.Fatal(e)
	}
	var rows int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM customer_charge_policies104 WHERE kind='merchant' AND owner_id=10422`).Scan(&rows); e != nil || rows != 0 {
		t.Fatal("余额扣费 wrote a policy", rows, e)
	}
}

func TestInitialSettlementRecipientRecordsOnlyTheFirstVersion(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	for _, id := range []int64{10423, 10424} {
		if e := s.CreateMerchant(ctx, Merchant{ID: id, Name: "recipient", APIKey: "test-only-recipient-key"}); e != nil {
			t.Fatal(e)
		}
	}
	admin := SettlementRecipientActor{Role: "admin", Class: "operator", ID: 1, Name: "local-admin"}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.SetInitialSettlementRecipientTx(ctx, tx, 10423, SettlementRecipientInput{Type: "bank", Account: "6222000011112222", Name: "本地收款人"}, admin)
	if e != nil || v.State != "configured" || v.Type != "bank" || v.AccountMasked != "***2222" {
		t.Fatal(v, e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	current, e := s.SettlementRecipient(ctx, 10423)
	if e != nil || current.ConfigID != v.ConfigID || current.Type != "bank" {
		t.Fatal("recipient not current", current, e)
	}
	var audits int
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='settlement-recipient' AND resource_id=10423 AND actor_role='admin' AND operation='configure'`).Scan(&audits); e != nil || audits != 1 {
		t.Fatal("recipient audit", audits, e)
	}
	tx, e = s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = s.SetInitialSettlementRecipientTx(ctx, tx, 10423, SettlementRecipientInput{Type: "alipay", Account: "other@example.invalid", Name: "其他"}, admin); !errors.Is(e, ErrConflict) {
		t.Fatal("initial recipient replaced an existing version", e)
	}
	for _, bad := range []struct {
		in    SettlementRecipientInput
		actor SettlementRecipientActor
	}{
		{SettlementRecipientInput{Type: "wxpay", Account: "openid-local", Name: "本地"}, admin},
		{SettlementRecipientInput{Type: "qqpay", Account: "10001", Name: "本地"}, admin},
		{SettlementRecipientInput{Type: "alipay", Account: "a@example.invalid", Name: "本地"}, SettlementRecipientActor{Role: "merchant", ID: 10424, Name: "m"}},
		{SettlementRecipientInput{Type: "alipay", Account: "a@example.invalid", Name: "本地"}, SettlementRecipientActor{Role: "admin", ID: 1, Name: "legacy"}},
	} {
		if _, e = s.SetInitialSettlementRecipientTx(ctx, tx, 10424, bad.in, bad.actor); !errors.Is(e, ErrInvalid) {
			t.Fatalf("%+v by %+v accepted: %v", bad.in, bad.actor, e)
		}
	}
}
