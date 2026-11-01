package payment

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

func setGroupSettlementRails(t *testing.T, s *Store, id int64, body string) {
	t.Helper()
	if _, err := s.pool.Exec(t.Context(), `UPDATE backoffice_records SET body=body||$2::jsonb WHERE resource='groups' AND id=$1`, id, body); err != nil {
		t.Fatal(err)
	}
}

func TestGroupSettlementTypesRestrictNewRecipientAndInitialSetup(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := t.Context()
	setGroupSettlementRails(t, s, 0, `{"settle_alipay":"0","settle_bank":"0","settle_wxpay":"1"}`)
	for _, in := range []SettlementRecipientInput{{Type: "alipay", Account: "local@example.test", Name: "Synthetic"}, {Type: "bank", Account: "6222000011112222", Name: "Synthetic"}} {
		if _, err := s.SetSettlementRecipient(ctx, 681, in, nil, settlementOwner); !errors.Is(err, ErrState) {
			t.Errorf("closed group rail accepted new recipient: %s %v", in.Type, err)
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = s.SetInitialSettlementRecipientTx(ctx, tx, 682, SettlementRecipientInput{Type: "bank", Account: "6222000011112222", Name: "Synthetic"}, SettlementRecipientActor{Role: "admin", Class: "operator", ID: 1, Name: "synthetic"}); !errors.Is(err, ErrState) {
		t.Error("merchant initial setup bypassed group rail", err)
	}
}

func TestGroupSettlementTypesQuoteRequestReplayAndInheritance(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := t.Context()
	in := SettlementRecipientInput{Type: "alipay", Account: "original@example.test", Name: "Synthetic"}
	cfg, err := s.SetSettlementRecipient(ctx, 681, in, nil, settlementOwner)
	if err != nil {
		t.Fatal(err)
	}
	original, err := s.RequestFundsWithSettlementRecipient(ctx, "settlements", 681, 0, "group-rail-original", 100, "merchant:681", nil, &cfg.ConfigID)
	if err != nil {
		t.Fatal(err)
	}
	setGroupSettlementRails(t, s, 0, `{"settle_alipay":"0"}`)
	if quote, err := s.SettlementQuote(ctx, 681, 100); err != nil || quote.CanApply {
		t.Error("quote advertised closed group rail", quote, err)
	}
	if _, err := s.RequestFunds(ctx, "settlements", 681, 0, "group-rail-new", 100, "merchant:681"); !errors.Is(err, ErrState) {
		t.Error("new application bypassed group rail", err)
	}
	if replay, err := s.RequestFundsWithSettlementRecipient(ctx, "settlements", 681, 0, "group-rail-original", 100, "merchant:681", nil, &cfg.ConfigID); err != nil || replay.ID != original.ID {
		t.Fatal("group closure blocked original request replay", replay, err)
	}
	if view, err := s.SettlementRecipient(ctx, 681); err != nil || view.ConfigID != cfg.ConfigID {
		t.Fatal("group closure hid historical recipient", view, err)
	}
	for _, value := range []string{"", "_", "1"} {
		setGroupSettlementRails(t, s, 0, `{"settle_alipay":"`+value+`"}`)
		if quote, err := s.SettlementQuote(ctx, 681, 100); err != nil || !quote.CanApply {
			t.Fatal("inheritance/enable failed", value, quote, err)
		}
	}
	transferSetting(t, s, "settle_alipay", "0")
	if quote, err := s.SettlementQuote(ctx, 681, 100); err != nil || quote.CanApply {
		t.Error("group enable overrode platform closure", quote, err)
	}
	setGroupSettlementRails(t, s, 0, `{"settle_alipay":"0","settle_wxpay":"0","settle_bank":"0"}`)
	if _, err := s.RequestFunds(ctx, "settlements", 682, 0, "missing-recipient-closed-rails", 1, "merchant:682"); !errors.Is(err, ErrState) {
		t.Fatal("missing recipient bypassed every supported rail closure", err)
	}
	setGroupSettlementRails(t, s, 0, `{"settle_alipay":null}`)
	if _, err := s.SettlementQuote(ctx, 681, 100); !errors.Is(err, ErrInvalid) {
		t.Fatal("corrupt rail treated as inherited", err)
	}
}

func TestGroupSettlementTypesMembershipExpiryDuringConfirmationRollsBack(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := t.Context()
	if _, err := s.SetSettlementRecipient(ctx, 681, SettlementRecipientInput{Type: "alipay", Account: "local@example.test", Name: "Synthetic"}, nil, settlementOwner); err != nil {
		t.Fatal(err)
	}
	setGroupSettlementRails(t, s, 0, `{"settle_alipay":"0"}`)
	var vip int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('groups','{"name":"temporary rail","settle_alipay":"1"}')RETURNING id`).Scan(&vip); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at)VALUES(681,$1,clock_timestamp()+interval '1 second')`, vip); err != nil {
		t.Fatal(err)
	}
	guarded := WithFundsActionGuard(ctx, func(c context.Context, tx pgx.Tx, kind, key string) error {
		_, err := tx.Exec(c, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM expires_at-clock_timestamp()))::double precision+0.05)FROM memberships WHERE merchant_id=681`)
		return err
	})
	if _, err := s.RequestFunds(guarded, "settlements", 681, 0, "expired-rail", 100, "merchant:681"); !errors.Is(err, ErrConflict) {
		t.Error("expired membership rail crossed funds confirmation", err)
	}
}

func TestGroupSettlementTypesFrozenPayoutKeepsOriginalRecipientAndRecovery(t *testing.T) {
	s, g, p, request := payoutFixture(t)
	ctx := t.Context()
	p.unknown = true
	setGroupSettlementRails(t, s, 0, `{"settle_wxpay":"0"}`)
	out, err := g.PrepareSettlementPayout(ctx, request.ID, 7, settlementAdmin, nil)
	if err != nil || out.Recipient == nil || out.Recipient.Type != "wxpay" {
		t.Fatal("current group changed frozen payout", out, err)
	}
	_, _ = g.ExecuteSettlementPayout(ctx, request.ID, settlementAdmin, nil)
	g.AllowLive = false
	if recovered, err := g.QuerySettlementPayout(ctx, request.ID, settlementAdmin); err != nil || recovered.State != "succeeded" || p.transfers.Load() != 1 || p.query.Load() != 1 {
		t.Fatal("group closure blocked original receipt recovery", recovered, err)
	}
}

func TestGroupSettlementTypesInitialSetupCanProvisionInactiveMerchant(t *testing.T) {
	s := testStore(t)
	err := s.CreateMerchantAtomic(t.Context(), Merchant{ID: 685, Name: "Inactive synthetic", APIKey: "local-test-only"}, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE merchants SET enabled=false WHERE id=685`); err != nil {
			return err
		}
		_, err := s.SetInitialSettlementRecipientTx(ctx, tx, 685, SettlementRecipientInput{Type: "bank", Account: "6222000011112222", Name: "Synthetic"}, SettlementRecipientActor{Role: "admin", Class: "operator", ID: 1, Name: "synthetic"})
		return err
	})
	if err != nil {
		t.Fatal("group validation must not activate merchant or forbid initial inactive setup", err)
	}
	merchant, err := s.Merchant(t.Context(), 685)
	if err != nil || merchant.Enabled {
		t.Fatal("initial recipient activated merchant", merchant, err)
	}
}

func TestGroupSettlementTypesMissingGlobalInsertedDuringConfirmationRejectsNewRequest(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := t.Context()
	if _, err := s.SetSettlementRecipient(ctx, 681, SettlementRecipientInput{Type: "alipay", Account: "local@example.test", Name: "Synthetic"}, nil, settlementOwner); err != nil {
		t.Fatal(err)
	}
	guarded := WithFundsActionGuard(ctx, func(c context.Context, _ pgx.Tx, _, _ string) error {
		_, err := s.pool.Exec(c, `INSERT INTO backoffice_records(resource,body)VALUES('settings','{"key":"settle_alipay","value":"0"}')`)
		return err
	})
	if _, err := s.RequestFunds(guarded, "settlements", 681, 0, "first-global-close", 100, "merchant:681"); !errors.Is(err, ErrState) {
		t.Fatal("first platform closure crossed confirmation", err)
	}
	if balance, err := s.Balance(ctx, 681); err != nil || balance.Available != 10000 || balance.Reserved != 0 {
		t.Fatal("closed rail changed ledger", balance, err)
	}
}
