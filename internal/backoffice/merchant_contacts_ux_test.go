package backoffice

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
)

func TestMerchantContactsUXOriginalFieldsPersistAndSearchWithoutChangingDomainApproval(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	_, err := s.Create(ctx, admin, "merchants", input(`{"id":"11901","name":"本地联系人商户","api_key":"merchant11901-synthetic-api-key","signature_algorithm":"MD5","phone":"13000011901","email":"merchant11901@example.invalid","qq":"1000011901","url":"shop11901.example.invalid"}`))
	if err != nil {
		t.Fatalf("original merchant contact fields rejected: %v", err)
	}
	for _, view := range []string{"get", "list"} {
		var fields map[string]any
		if view == "get" {
			item, e := s.Get(ctx, admin, "merchants", 11901)
			if e != nil {
				t.Fatal(e)
			}
			fields = item.Fields
		} else {
			page, e := s.List(ctx, admin, "merchants", Query{Page: 1, Limit: 20, Search: "shop11901"})
			if e != nil || page.Total != 1 {
				t.Fatalf("contact list projection missing: %+v %v", page, e)
			}
			fields = page.Items[0].Fields
		}
		for key, value := range map[string]string{"phone": "13000011901", "qq": "1000011901", "url": "shop11901.example.invalid"} {
			if fields[key] != value {
				t.Errorf("%s contact %s: %#v", view, key, fields[key])
			}
		}
	}
	for column, needle := range map[string]string{"phone": "13000011901", "qq": "1000011901", "url": "shop11901.example.invalid"} {
		f, search, e := ParseMerchantDirectoryQuery(url.Values{"column": {column}, "q": {needle}})
		if e != nil {
			t.Fatal(column, e)
		}
		page, e := s.List(ctx, admin, "merchants", Query{Page: 1, Limit: 20, Search: search, Merchant: f})
		if e != nil || page.Total != 1 || page.Items[0].ID != 11901 {
			t.Fatalf("original %s search absent: %+v %v", column, page, e)
		}
	}
	if _, err = s.Create(ctx, admin, "domains", input(`{"merchant_id":"11901","domain":"approved11901.example.invalid","approved":true,"enabled":true}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, admin, "merchants", 11901, input(`{"phone":"","qq":"","url":"https://new11901.example.invalid/path"}`)); err != nil {
		t.Fatal(err)
	}
	item, err := s.Get(ctx, admin, "merchants", 11901)
	if err != nil || item.Fields["phone"] != "" || item.Fields["qq"] != "" || item.Fields["url"] != "https://new11901.example.invalid/path" {
		t.Fatal(item, err)
	}
	var domains, logins int
	if err = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM backoffice_records WHERE resource='domains' AND owner_id=11901 AND body->>'domain'='approved11901.example.invalid' AND body->>'approved'='true'),(SELECT count(*) FROM console_users WHERE role='merchant' AND subject_id=11901)`).Scan(&domains, &logins); err != nil || domains != 1 || logins != 0 {
		t.Fatalf("contact edit changed authorization or login: %d %d %v", domains, logins, err)
	}
}

func TestMerchantContactsUXRejectUnsafeExplicitWritesAndPreservePrivateLegacyFields(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	for key, values := range map[string][]any{"phone": {"bad\r\nphone", "<script>", json.Number("13000011901")}, "qq": {"01", "123<script>", json.Number("1000011901")}, "url": {"javascript:alert(1)", "https://user:password@example.invalid", "https://site.invalid/\r\nheader", "//other.invalid"}} {
		for _, value := range values {
			raw, _ := json.Marshal(map[string]any{key: value})
			if _, err := s.Update(ctx, admin, "merchants", 101, input(string(raw))); !errors.Is(err, ErrInvalid) {
				t.Fatalf("unsafe %s=%#v accepted: %v", key, value, err)
			}
		}
	}
	if _, err := s.pool.Exec(ctx, `UPDATE merchants SET profile='{"phone":{"token":"nested-private-119"},"qq":{"secret":"nested-private-119"},"url":{"password":"nested-private-119"},"private_metadata":{"keep":"original"}}' WHERE id=101`); err != nil {
		t.Fatal(err)
	}
	item, err := s.Get(ctx, admin, "merchants", 101)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"phone", "qq", "url", "private_metadata"} {
		if _, ok := item.Fields[key]; ok {
			t.Fatalf("private legacy shape exported: %s", key)
		}
	}
	if _, err = s.Update(ctx, admin, "merchants", 101, input(`{"name":"保留私有原始资料"}`)); err != nil {
		t.Fatal(err)
	}
	var kept bool
	if err = s.pool.QueryRow(ctx, `SELECT profile->'private_metadata'->>'keep'='original' FROM merchants WHERE id=101`).Scan(&kept); err != nil || !kept {
		t.Fatal("contact projection destroyed private metadata", err)
	}
}

func TestMerchantContactsUXCredentialFailureRollsBackContactAndOrdinaryEditKeepsIdentity(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, admin, "merchants", input(`{"id":"11902","name":"本地绑定账号","api_key":"merchant11902-synthetic-api-key","signature_algorithm":"MD5","phone":"13000011902","login_username":"contact11902","login_password":"synthetic-contact-password-11902"}`)); err != nil {
		t.Fatal(err)
	}
	var epoch int64
	if err := s.pool.QueryRow(ctx, `SELECT auth_version FROM console_users WHERE role='merchant' AND subject_id=11902`).Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(ctx, admin, "merchants", 11902, input(`{"phone":"13000011903","qq":"1000011902"}`)); err != nil {
		t.Fatal(err)
	}
	var next int64
	if err := s.pool.QueryRow(ctx, `SELECT auth_version FROM console_users WHERE role='merchant' AND subject_id=11902`).Scan(&next); err != nil || next != epoch {
		t.Fatal("contact edit revoked or replaced authenticated identity", next, epoch, err)
	}
	if _, err := s.Update(ctx, admin, "merchants", 11902, input(`{"phone":"13000011904","login_username":"different11902"}`)); !errors.Is(err, ErrForbidden) {
		t.Fatal("existing identity renamed via contact form", err)
	}
	item, err := s.Get(ctx, admin, "merchants", 11902)
	if err != nil || item.Fields["phone"] != "13000011903" {
		t.Fatal("failed credential mutation persisted contact", item, err)
	}
	if _, err = s.Create(ctx, admin, "merchants", input(`{"id":"11903","name":"本地账号冲突","api_key":"merchant11903-synthetic-api-key","signature_algorithm":"MD5","phone":"13000011903","login_username":"contact11902","login_password":"synthetic-contact-password-11903"}`)); !errors.Is(err, ErrConflict) {
		t.Fatal("duplicate identity accepted", err)
	}
	var orphan bool
	if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM merchants WHERE id=11903)`).Scan(&orphan); err != nil || orphan {
		t.Fatal("failed new credential left contact merchant", err)
	}
	if _, err = s.Update(ctx, Principal{Role: "merchant", SubjectID: 11902, Username: "contact11902"}, "merchants", 11902, input(`{"phone":"13000011904"}`)); !errors.Is(err, ErrForbidden) {
		t.Fatal("merchant used administrator contact editor", err)
	}
}
