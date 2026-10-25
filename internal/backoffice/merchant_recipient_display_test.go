package backoffice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"goepay/internal/payment"
)

func TestMerchantConsoleRecipientProjectionIsMaskedAndNotWritable(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	configured, err := s.store.SetSettlementRecipient(ctx, 101, payment.SettlementRecipientInput{Type: "alipay", Account: "synthetic-recipient-account-1234", Name: "Synthetic Private Person"}, nil, payment.SettlementRecipientActor{Role: "admin", Class: "operator", Name: "admin", ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	assert := func(it Item, state string) {
		t.Helper()
		raw, err := json.Marshal(it.Fields["settlement_recipient"])
		if err != nil {
			t.Fatal(err)
		}
		var view payment.SettlementRecipientView
		if err = json.Unmarshal(raw, &view); err != nil || view.State != state {
			t.Errorf("recipient projection unavailable: %s %v", raw, err)
		}
		if state == "configured" && (view.ConfigID != configured.ConfigID || view.AccountMasked != "***1234" || view.NameMasked != "***" || view.Type != "alipay") {
			t.Error("masked current recipient differs", view)
		}
		all, _ := json.Marshal(it.Fields)
		for _, secret := range []string{"synthetic-recipient-account", "Synthetic Private Person", "recipient_cipher", "identity_hash", "test-secret-long"} {
			if strings.Contains(string(all), secret) {
				t.Error("merchant view leaked private material", secret)
			}
		}
	}
	it, err := s.Get(ctx, admin, "merchants", 101)
	if err != nil {
		t.Fatal(err)
	}
	assert(it, "configured")
	limitedConfig := s.pool.Config()
	limitedConfig.MaxConns = 1
	limitedPool, err := pgxpool.NewWithConfig(ctx, limitedConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer limitedPool.Close()
	limitedStore, err := payment.NewWithKey(limitedPool, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	limited := New(limitedPool, limitedStore)
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	limitedPage, err := limited.List(readCtx, admin, "merchants", Query{Page: 1, Limit: 20})
	if err != nil || len(limitedPage.Items) != 2 {
		t.Fatal("single connection list failed", err)
	}
	page, err := s.List(ctx, admin, "merchants", Query{Page: 1, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if item.ID == 101 {
			assert(item, "configured")
		} else if item.ID == 102 {
			assert(item, "missing")
		}
	}
	if _, err = s.Update(ctx, admin, "merchants", 101, input(`{"settlement_recipient":{"state":"configured","account_masked":"forged"}}`)); !errors.Is(err, ErrForbidden) {
		t.Error("writable summary", err)
	}
	it, err = s.Update(ctx, admin, "merchants", 101, input(`{"name":"ordinary account edit"}`))
	if err != nil {
		t.Fatal(err)
	}
	assert(it, "configured")
	var polluted bool
	if err = s.pool.QueryRow(ctx, `SELECT profile ? 'settlement_recipient' FROM merchants WHERE id=101`).Scan(&polluted); err != nil || polluted {
		t.Fatal("summary copied into mutable profile", polluted, err)
	}
}
