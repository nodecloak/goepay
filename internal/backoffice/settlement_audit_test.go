package backoffice

import (
	"context"
	"errors"
	"goepay/internal/payment"
	"strings"
	"testing"
)

func TestSettlementRecipientAuditRedactsIdentity(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	p := Principal{Role: "merchant", SubjectID: 101, Username: "m1"}
	schema, e := s.Schema(p, "settlement-settings")
	if e != nil || schema.Permissions.Create || schema.Permissions.Update || schema.Permissions.Delete {
		t.Fatal("legacy schema advertises writes", schema.Permissions, e)
	}
	if _, err := s.Create(ctx, p, "settlement-settings", input(`{"account_type":"alipay","account_name":"Private Test Recipient","account_reference":"private-recipient@example.com","enabled":true}`)); !errors.Is(err, ErrForbidden) {
		t.Fatal("legacy plaintext create allowed", err)
	}
	if _, err := s.Update(ctx, p, "settlement-settings", 1, input(`{"account_name":"Replacement Private Recipient"}`)); !errors.Is(err, ErrForbidden) {
		t.Fatal("legacy plaintext update allowed", err)
	}
	if err := s.Delete(ctx, p, "settlement-settings", 1); !errors.Is(err, ErrForbidden) {
		t.Fatal("legacy deletion allowed", err)
	}
	if _, err := s.store.SetSettlementRecipient(ctx, 101, payment.SettlementRecipientInput{Type: "alipay", Account: "private-recipient@example.com", Name: "Private Test Recipient"}, nil, payment.SettlementRecipientActor{Role: "merchant", ID: 101, Name: "m1"}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.pool.Query(ctx, "SELECT changes::text FROM backoffice_audit WHERE resource='settlement-recipient' ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var count int
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(raw, "Private") || strings.Contains(raw, "private-recipient") {
			t.Fatal("recipient identity leaked to immutable audit", raw)
		}
		if !strings.Contains(raw, "config_id") {
			t.Fatal("audit lacks immutable version", raw)
		}
		count++
	}
	if err = rows.Err(); err != nil || count != 1 {
		t.Fatal("audit count", count, err)
	}
}
