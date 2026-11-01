package backoffice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func init() {
	d := definitions["merchants"]
	f := selectField("creation_mode", "新增方式", "account", "api")
	f.Required = false
	f.CreateOnly = true
	f.WriteOnly = true
	f.AdminOnly = true
	f.Options = []Option{{Label: "商户账号", Value: "account"}, {Label: "仅API（高级）", Value: "api"}}
	d.fields = append(d.fields, f)
	definitions["merchants"] = d
	merchantCreateSetupFields["creation_mode"] = true
}

// Only the new account form opts into generated identities. Existing API-only
// integrations retain their explicit ID/name contract and cannot gain a login
// merely by changing contact metadata on an existing merchant.
func merchantCreationDefinition(d definition, in map[string]json.RawMessage) (definition, bool, error) {
	raw, exists := in["creation_mode"]
	if !exists {
		return d, false, nil
	}
	var mode string
	if json.Unmarshal(raw, &mode) != nil {
		return d, false, ErrInvalid
	}
	if mode == "api" {
		return d, false, nil
	}
	if mode != "account" {
		return d, false, ErrInvalid
	}
	d.fields = append([]Field(nil), d.fields...)
	for i := range d.fields {
		if d.fields[i].Name == "id" || d.fields[i].Name == "name" {
			d.fields[i].Required = false
		}
	}
	return d, true, nil
}

func (s *Service) prepareAccountMerchant(ctx context.Context, v map[string]any) error {
	phone, _ := v["phone"].(string)
	email, _ := v["email"].(string)
	primary := phone
	if primary == "" {
		primary = email
	}
	password, _ := v["login_password"].(string)
	if primary == "" || len(primary) > 100 || len(password) < 12 || len(password) > 1024 {
		return ErrInvalid
	}
	if username, _ := v["login_username"].(string); username != "" && username != primary {
		return ErrInvalid
	}
	v["login_username"] = primary
	if name, _ := v["name"].(string); name == "" {
		v["name"] = primary
	}
	if raw := v["id"]; raw == nil || raw == "" {
		// Share registration's monotonic sequence. Explicit historical IDs may
		// occupy a sequence value; skip those without recycling an account identity.
		for attempt := 0; attempt < 128; attempt++ {
			var id int64
			var exists bool
			if e := s.pool.QueryRow(ctx, "SELECT nextval('console_subject_seq')").Scan(&id); e != nil {
				return e
			}
			if id <= 0 {
				return ErrConflict
			}
			if e := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM merchants WHERE id=$1)", id).Scan(&exists); e != nil {
				return e
			}
			if !exists {
				v["id"] = fmt.Sprint(id)
				return nil
			}
		}
		return ErrConflict
	}
	if strings.TrimSpace(fmt.Sprint(v["id"])) != fmt.Sprint(v["id"]) {
		return ErrInvalid
	}
	return nil
}
