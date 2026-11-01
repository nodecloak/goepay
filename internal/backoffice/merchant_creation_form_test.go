package backoffice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"goepay/internal/identity"
)

func TestMerchantCreationFormAllocatesIdentityAndBindsContactLogin(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	for _, tc := range []struct{ phone, email, login string }{
		{"13000012201", "", "13000012201"},
		{"", "create12202@example.test", "create12202@example.test"},
		{"+651300012203", "", "+651300012203"},
		{"", "create+12204@example.test", "create+12204@example.test"},
	} {
		raw, _ := json.Marshal(map[string]any{"creation_mode": "account", "phone": tc.phone, "email": tc.email, "login_password": "synthetic-create122-password", "group_id": "0", "fee_mode": "0"})
		created, e := s.Create(ctx, createSetupSuper, "merchants", input(string(raw)))
		if e != nil {
			t.Fatal("reference add-merchant fields must be enough", e)
		}
		if created.ID <= 0 {
			t.Fatal("missing allocated ID")
		}
		m, e := s.store.Merchant(ctx, created.ID)
		if e != nil || m.Name != tc.login || len(m.APIKey) != 32 {
			t.Fatal("merchant identity or generated key invalid", e)
		}
		var username, hash string
		if e = s.pool.QueryRow(ctx, "SELECT username,password_hash FROM console_users WHERE role='merchant' AND subject_id=$1", created.ID).Scan(&username, &hash); e != nil || username != tc.login || !identity.CheckPassword(hash, "synthetic-create122-password") {
			t.Fatal("login binding not committed", e)
		}
		for _, key := range []string{"login_password", "api_key", "creation_mode"} {
			if _, exists := created.Fields[key]; exists {
				t.Fatal("private creation field returned", key)
			}
		}
		if _, e = s.Create(ctx, createSetupSuper, "merchants", input(string(raw))); e == nil {
			t.Fatal("repeated creation must not create another contact login")
		}
		var count int
		if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM console_users WHERE role='merchant' AND username=$1", tc.login).Scan(&count); e != nil || count != 1 {
			t.Fatal("duplicate login", e, count)
		}
	}
}

func TestMerchantCreationFormProtectsRecipientAudit(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	created, e := s.Create(ctx, createSetupSuper, "merchants", input(`{"creation_mode":"account","phone":"13000012205","login_password":"synthetic-create122-password","settlement_type":"alipay","settlement_account":"private12205@example.invalid","settlement_name":"private-recipient-12205"}`))
	if e != nil {
		t.Fatal(e)
	}
	var raw string
	if e = s.pool.QueryRow(ctx, "SELECT changes::text FROM backoffice_audit WHERE resource='merchants' AND resource_id=$1 AND operation='create'", created.ID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{"private12205@example.invalid", "private-recipient-12205", "synthetic-create122-password"} {
		if strings.Contains(raw, secret) {
			t.Fatal("private setup value retained in audit")
		}
	}
}

func TestMerchantCreationFormInvalidDraftCreatesNothing(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	for _, raw := range []string{
		`{"creation_mode":"account","login_password":"synthetic-create122-password"}`,
		`{"creation_mode":"account","phone":"13000012203"}`,
		`{"creation_mode":"account","phone":"13000012203","login_password":"short"}`,
		`{"creation_mode":"account","email":"bad email","login_password":"synthetic-create122-password"}`,
		`{"creation_mode":"account","phone":"13000012203","login_username":"other-account","login_password":"synthetic-create122-password"}`,
	} {
		if _, e := s.Create(ctx, createSetupSuper, "merchants", input(raw)); e == nil {
			t.Fatal("invalid creation draft accepted")
		}
	}
	var count int
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM merchants WHERE name LIKE '130000122%' OR name LIKE '%12202%' ").Scan(&count); e != nil || count != 0 {
		t.Fatal("invalid draft created merchant", fmt.Sprint(count), e)
	}
}
