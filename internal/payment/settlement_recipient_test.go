package payment

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
)

var settlementOwner = SettlementRecipientActor{Role: "merchant", ID: 681, Name: "merchant"}

func TestSettlementRecipientEncryptedSnapshotSurvivesConfigChangeAndReplay(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	zero := int64(0)
	in := SettlementRecipientInput{Type: "alipay", Account: "private-settlement-account", Name: "Private Settlement Name"}
	cfg, e := s.SetSettlementRecipient(ctx, 681, in, &zero, settlementOwner)
	if e != nil || cfg.ConfigID <= 0 || cfg.State != "configured" {
		t.Fatal(cfg, e)
	}
	same, e := s.SetSettlementRecipient(ctx, 681, in, &cfg.ConfigID, settlementOwner)
	if e != nil || same.ConfigID != cfg.ConfigID {
		t.Fatal(same, e)
	}
	v, e := s.RequestFundsWithSettlementRecipient(ctx, "settlements", 681, 0, "recipient-original", 100, "merchant:681", nil, &cfg.ConfigID)
	if e != nil || v.SettlementRecipient == nil || v.SettlementRecipient.ConfigID != cfg.ConfigID || v.SettlementRecipient.State != "recorded" {
		t.Fatal(v, e)
	}
	changed := in
	changed.Account = "replacement-recipient"
	next, e := s.SetSettlementRecipient(ctx, 681, changed, &cfg.ConfigID, settlementOwner)
	if e != nil || next.ConfigID == cfg.ConfigID {
		t.Fatal(next, e)
	}
	if _, e = s.SetSettlementRecipient(ctx, 681, in, &cfg.ConfigID, settlementOwner); !errors.Is(e, ErrConflict) {
		t.Fatal("stale config edit", e)
	}
	replay, e := s.RequestFunds(ctx, "settlements", 681, 0, "recipient-original", 100, "merchant:681")
	if e != nil || replay.SettlementRecipient.ConfigID != cfg.ConfigID {
		t.Fatal("implicit retry changed recipient", replay, e)
	}
	if _, e = s.RequestFundsWithSettlementRecipient(ctx, "settlements", 681, 0, "recipient-original", 100, "merchant:681", nil, &next.ConfigID); !errors.Is(e, ErrConflict) {
		t.Fatal("same external number changed recipient", e)
	}
	if _, e = s.RequestFundsWithSettlementRecipient(ctx, "settlements", 681, 0, "stale-config-new", 100, "merchant:681", nil, &cfg.ConfigID); !errors.Is(e, ErrConflict) {
		t.Fatal("stale config accepted new request", e)
	}
	var cipher []byte
	if e = s.pool.QueryRow(ctx, "SELECT recipient_cipher FROM settlement_recipients WHERE request_id=$1", v.ID).Scan(&cipher); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(cipher), in.Account) || strings.Contains(string(cipher), in.Name) {
		t.Fatal("plaintext snapshot")
	}
	if _, e = s.decryptSettlementRecipient("request", v.ID+1, cipher); e == nil {
		t.Fatal("cipher portable across requests")
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), in.Account) || strings.Contains(string(raw), in.Name) {
		t.Fatal("PII response", string(raw))
	}
	var audits string
	if e = s.pool.QueryRow(ctx, "SELECT COALESCE(string_agg(changes::text,''),'') FROM backoffice_audit").Scan(&audits); e != nil || strings.Contains(audits, in.Account) || strings.Contains(audits, in.Name) {
		t.Fatal("PII audit", audits, e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE settlement_recipients SET config_id=$2 WHERE request_id=$1", v.ID, next.ConfigID); e == nil {
		t.Fatal("request recipient mutable")
	}
	if _, e = s.pool.Exec(ctx, "UPDATE settlement_recipient_configs SET app_id='changed' WHERE id=$1", cfg.ConfigID); e == nil {
		t.Fatal("configuration mutable")
	}
}
func TestSettlementRecipientMissingCompatibilityValidationAndAuditRollback(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	missing, e := s.RequestFunds(ctx, "settlements", 681, 0, "no-recipient", 100, "merchant:681")
	if e != nil || missing.SettlementRecipient == nil || missing.SettlementRecipient.State != "missing" {
		t.Fatal(missing, e)
	}
	for _, in := range []SettlementRecipientInput{{Type: "qqpay", Account: "123"}, {Type: "wxpay", Account: "openid", Name: "name"}, {Type: "bank", Account: "not-bank", Name: "name"}, {Type: "alipay", Account: "recipient", Name: "bad\nname"}} {
		if _, e = s.SetSettlementRecipient(ctx, 681, in, nil, settlementOwner); e == nil {
			t.Fatal("invalid recipient accepted", in)
		}
	}
	if _, e = s.SetSettlementRecipient(ctx, 682, SettlementRecipientInput{Type: "alipay", Account: "recipient", Name: "name"}, nil, settlementOwner); !errors.Is(e, ErrInvalid) {
		t.Fatal("foreign config", e)
	}
	if _, e = s.pool.Exec(ctx, `CREATE FUNCTION reject_recipient_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='settlement-recipient' THEN RAISE EXCEPTION 'test audit rejected'; END IF; RETURN NEW; END $$;CREATE TRIGGER reject_recipient_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_recipient_audit()`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SetSettlementRecipient(ctx, 681, SettlementRecipientInput{Type: "alipay", Account: "recipient", Name: "name"}, nil, settlementOwner); e == nil {
		t.Fatal("audit failed open")
	}
	cfg, e := s.SettlementRecipient(ctx, 681)
	if e != nil || cfg.State != "missing" {
		t.Fatal(cfg, e)
	}
	var count int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM settlement_recipient_configs").Scan(&count); e != nil || count != 0 {
		t.Fatal("failed audit persisted recipient", count, e)
	}
}

func TestSettlementRecipientSnapshotFailureRollsBackDedicatedMoneyAndGuardConsumption(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	cfg, e := s.SetSettlementRecipient(ctx, 681, SettlementRecipientInput{Type: "alipay", Account: "synthetic-recipient", Name: "Synthetic Name"}, nil, settlementOwner)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `CREATE TABLE funding_test_guard(id integer);CREATE FUNCTION funding_test_snapshot_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test snapshot rejected';END $$;CREATE TRIGGER funding_test_snapshot BEFORE INSERT ON settlement_recipients FOR EACH ROW EXECUTE FUNCTION funding_test_snapshot_failure()`); e != nil {
		t.Fatal(e)
	}
	guarded := WithFundsActionGuard(ctx, func(c context.Context, tx pgx.Tx, kind, key string) error {
		_, err := tx.Exec(c, "INSERT INTO funding_test_guard VALUES(1)")
		return err
	})
	if _, e = s.RequestFundsWithSettlementRecipient(guarded, "settlements", 681, 0, "snapshot-fail", 100, "merchant:681", nil, &cfg.ConfigID); e == nil {
		t.Fatal("snapshot failed open")
	}
	var requests, funding, guards int
	if e = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM ledger_requests),(SELECT count(*) FROM settlement_fundings),(SELECT count(*) FROM funding_test_guard)").Scan(&requests, &funding, &guards); e != nil || requests+funding+guards != 0 {
		t.Fatal(requests, funding, guards, e)
	}
	balance, _ := s.Balance(ctx, 681)
	if balance.Available != 10000 || balance.Reserved != 0 {
		t.Fatal(balance)
	}
}
