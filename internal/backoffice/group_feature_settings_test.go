package backoffice

import (
	"context"
	"encoding/json"
	"testing"
)

func TestGroupFeatureSettingsUseEffectiveMerchantGroupAndStrictOverrides(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	keys := []string{"recharge", "onecode", "subchannel_open", "user_profitsharing", "user_transfer_batch"}
	for _, key := range keys {
		seedSetting(t, s, key, "0")
	}
	set := func(value string) {
		t.Helper()
		fields := map[string]string{}
		for _, key := range keys {
			fields[key] = value
		}
		raw, _ := json.Marshal(fields)
		if _, err := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=body||$1::jsonb WHERE resource='groups' AND id=0`, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"", "_", "0", "1"} {
		set(value)
		for _, key := range keys {
			on, err := s.MerchantFeatureVisibleForMerchant(ctx, 101, key)
			if err != nil || on != (value == "1") {
				t.Fatalf("group override %s=%q on=%v err=%v", key, value, on, err)
			}
			if global, err := s.MerchantFeatureVisible(ctx, key); err != nil || global {
				t.Fatal("group override changed global reader", key, global, err)
			}
		}
	}
	set("1")
	var gid int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"feature restricted","enabled":true,"recharge":"0"}') RETURNING id`).Scan(&gid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(101,$1,clock_timestamp()+interval '1 hour')`, gid); err != nil {
		t.Fatal(err)
	}
	if on, err := s.MerchantFeatureVisibleForMerchant(ctx, 101, "recharge"); err != nil || on {
		t.Fatal("active membership did not apply feature override", on, err)
	}
	if on, err := s.MerchantFeatureVisibleForMerchant(ctx, 102, "recharge"); err != nil || !on {
		t.Fatal("another merchant inherited someone else's membership", on, err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE memberships SET expires_at=clock_timestamp()-interval '1 second' WHERE merchant_id=101`); err != nil {
		t.Fatal(err)
	}
	if on, err := s.MerchantFeatureVisibleForMerchant(ctx, 101, "recharge"); err != nil || !on {
		t.Fatal("expired membership did not restore base group", on, err)
	}
	for _, value := range []string{"malformed", "true"} {
		set(value)
		if on, err := s.MerchantFeatureVisibleForMerchant(ctx, 101, "recharge"); err == nil || on {
			t.Fatal("malformed group feature did not fail closed", on, err)
		}
	}
}

func TestGroupFeatureSettingsSchemaAndValidation(t *testing.T) {
	s := setup(t)
	schema, err := s.Schema(admin, "groups")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"recharge", "onecode", "subchannel_open", "user_profitsharing", "user_transfer_batch"} {
		found := false
		for _, field := range schema.Fields {
			if field.Name == key {
				found = field.Type == "select" && !field.Required
			}
		}
		if !found {
			t.Fatal("group override absent from editable schema", key)
		}
	}
	if err := validateGroupFeatureSettings(map[string]any{"recharge": "0", "onecode": "_"}); err != nil {
		t.Fatal("valid group feature overrides refused", err)
	}
	for _, value := range []any{true, nil, "true", "2", map[string]any{}} {
		if err := validateGroupFeatureSettings(map[string]any{"recharge": value}); err == nil {
			t.Fatal("invalid group feature saved", value)
		}
	}
	created, err := s.Create(t.Context(), admin, "groups", input(`{"name":"Synthetic group features","recharge":"0","onecode":"1","subchannel_open":"_","user_profitsharing":"0","user_transfer_batch":"1"}`))
	if err != nil {
		t.Fatal("editable feature fields could not be persisted through group creation", err)
	}
	updated, err := s.Update(t.Context(), admin, "groups", created.ID, input(`{"recharge":"_"}`))
	if err != nil || updated.Fields["recharge"] != "_" || updated.Fields["onecode"] != "1" || updated.Fields["user_transfer_batch"] != "1" {
		t.Fatal("group update did not preserve independent feature settings", updated.Fields, err)
	}
	if _, err := s.Update(t.Context(), admin, "groups", created.ID, input(`{"recharge":"true"}`)); err == nil {
		t.Fatal("resource update bypassed group feature validation")
	}
}

func TestGroupFeatureSettingsMalformedValuesFailClosedWithoutHidingAccount(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	for _, bad := range []string{"", "true", "bad"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM backoffice_records WHERE resource='settings' AND body->>'key'='recharge'`); err != nil {
			// Settings are mutable through the repository resource. This test only
			// owns its isolated schema, never the immutable financial audit rows.
			t.Fatal(err)
		}
		seedSetting(t, s, "recharge", bad)
		if on, err := s.MerchantFeatureVisible(ctx, "recharge"); err != nil || !on {
			t.Fatal("legacy global reader contract changed", on, err)
		}
		if on, err := s.MerchantFeatureVisibleForMerchant(ctx, 101, "recharge"); err == nil || on {
			t.Fatal("invalid effective global value did not refuse feature", on, err)
		}
		menu, err := s.MerchantFeaturesForMerchant(ctx, 101)
		if err != nil || menu["recharge"] || !menu["onecode"] || !menu["user_totp_open"] {
			t.Fatal("bad business flag should close only its feature, preserving account/security access", menu, err)
		}
	}
	if _, err := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"onecode":"1","recharge":true}'::jsonb WHERE resource='groups' AND id=0; DELETE FROM backoffice_records WHERE resource='settings' AND body->>'key'='recharge'`); err != nil {
		t.Fatal(err)
	}
	menu, err := s.MerchantFeaturesForMerchant(ctx, 101)
	if err != nil || menu["recharge"] || !menu["onecode"] || !menu["user_totp_open"] {
		t.Fatal("malformed group feature broke unrelated menu entries", menu, err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"enabled":false}'::jsonb WHERE resource='groups' AND id=0`); err != nil {
		t.Fatal(err)
	}
	menu, err = s.MerchantFeaturesForMerchant(ctx, 101)
	if err != nil || menu["recharge"] || menu["onecode"] || !menu["user_totp_open"] {
		t.Fatal("unavailable group should not hide old transaction/account recovery", menu, err)
	}
}
