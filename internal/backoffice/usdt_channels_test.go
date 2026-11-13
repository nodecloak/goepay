package backoffice

import (
	"errors"
	"testing"
)

func TestUSDTGroupChannelReferencesAreTypedAndCannotUseCNYRolls(t *testing.T) {
	s := setup(t)
	ctx := t.Context()
	if _, err := s.pool.Exec(ctx, `INSERT INTO crypto_channels(id,name,network,revision,fee_bps) VALUES(9001,'USDT channel','tron',1,50)`); err != nil {
		t.Fatal(err)
	}
	// A configured closed channel may be selected before enabling collections.
	group, err := s.Create(ctx, admin, "groups", input(`{"name":"USDT group","info":{"usdt":{"channel":"9001","type":"channel","rate":"99.5"}}}`))
	if err != nil {
		t.Fatal("registered crypto channel rejected", err)
	}
	for _, policy := range []string{`{"channel":"9002","type":"channel"}`, `{"channel":"9001","type":"roll"}`, `{"channel":"-1","type":"channel"}`, `{"channel":"-2","type":"channel"}`, `{"channel":"-3","type":"roll"}`, `{"channel":"-4","type":"channel"}`} {
		if _, err = s.Update(ctx, admin, "groups", group.ID, input(`{"info":{"usdt":`+policy+`}}`)); !errors.Is(err, ErrInvalid) {
			t.Fatal("unsupported USDT routing accepted", policy, err)
		}
	}
	for _, policy := range []string{`{}`, `{"channel":"0","type":"channel"}`, `{"channel":"-5","type":"channel"}`} {
		if _, err = s.Update(ctx, admin, "groups", group.ID, input(`{"info":{"usdt":`+policy+`}}`)); err != nil {
			t.Fatal("valid USDT routing rejected", policy, err)
		}
	}
	if _, err = s.pool.Exec(ctx, `UPDATE crypto_channels SET archived=true WHERE id=9001`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, admin, "groups", group.ID, input(`{"info":{"usdt":{"channel":"9001","type":"channel"}}}`)); !errors.Is(err, ErrInvalid) {
		t.Fatal("archived channel selected", err)
	}
}

func TestUSDTPaymentMethodCanBeConfiguredButDefaultsClosed(t *testing.T) {
	s := setup(t)
	page, err := s.List(t.Context(), admin, "pay-types", Query{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range page.Items {
		if row.Fields["type"] == "usdt" {
			found = true
			if row.ID != 0 || row.Fields["enabled"] != false {
				t.Fatal("USDT default enabled", row)
			}
		}
	}
	if !found {
		t.Fatal("USDT absent from payment methods")
	}
	row, err := s.Create(t.Context(), admin, "pay-types", input(`{"name":"USDT","type":"usdt","enabled":true}`))
	if err != nil {
		t.Fatal("USDT pay method not editable", err)
	}
	if _, err = s.Update(t.Context(), admin, "pay-types", row.ID, input(`{"enabled":false}`)); err != nil {
		t.Fatal(err)
	}
}
