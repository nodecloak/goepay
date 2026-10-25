package backoffice

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
)

func TestSettlementModeUXOriginalGroupOptionsAndBothMode(t *testing.T) {
	s := setup(t)
	schema, err := s.Schema(admin, "groups")
	if err != nil {
		t.Fatal(err)
	}
	options := map[string]string{}
	for _, field := range schema.Fields {
		if field.Name == "settle_open" {
			for _, option := range field.Options {
				options[option.Value] = option.Label
			}
		}
	}
	for value, label := range map[string]string{"1": "只开启每日自动结算", "2": "只开启手动申请结算", "3": "开启自动+手动结算"} {
		if options[value] != label {
			t.Errorf("original mode %s got %q want %q", value, options[value], label)
		}
	}
	if _, err = s.Create(context.Background(), admin, "groups", input(`{"name":"原自动和手动结算组","settle_open":"3"}`)); err != nil {
		t.Fatalf("original valid both mode rejected: %v", err)
	}
}

func TestSettlementModeUXSettingsAndQuoteExposeOwnerScopedAccess(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	for _, value := range []string{"4", "01", "true", "_"} {
		raw, _ := json.Marshal(map[string]string{"key": "settle_open", "value": value})
		if _, err := s.Create(ctx, admin, "settings", input(string(raw))); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid setting %q accepted: %v", value, err)
		}
	}
	if _, err := s.Create(ctx, admin, "settings", input(`{"key":"settle_open","value":"1"}`)); err != nil {
		t.Fatal(err)
	}
	invoke := func(p Principal, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.ServeWorkflows(w, httptest.NewRequest("GET", path, nil), p, []string{"settlement-policy"})
		return w
	}
	w := invoke(Principal{Role: "merchant", SubjectID: 101, Username: "merchant"}, "/settlement-policy?amount=1.00")
	var out struct {
		Data struct {
			Access struct {
				Mode   string `json:"mode"`
				Manual bool   `json:"manual_allowed"`
				Auto   bool   `json:"automatic_allowed"`
				Group  string `json:"group_id"`
			} `json:"settlement_access"`
			Can bool `json:"can_apply"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || out.Data.Access.Mode != "1" || out.Data.Access.Manual || !out.Data.Access.Auto || out.Data.Access.Group != "0" || out.Data.Can {
		t.Fatalf("quote does not explain original mode: %d %s %v", w.Code, w.Body.String(), err)
	}
	if w = invoke(Principal{Role: "merchant", SubjectID: 101, Username: "merchant"}, "/settlement-policy?merchant_id=102&amount=1.00"); w.Code != 403 {
		t.Fatal("foreign settlement mode leaked", w.Code)
	}
	if w = invoke(admin, "/settlement-policy?amount=1.00"); w.Code != 422 {
		t.Fatal("admin quote accepted missing owner", w.Code)
	}
	if w = invoke(admin, "/settlement-policy?merchant_id=101&amount=1.00"); w.Code != 200 {
		t.Fatal("admin scoped quote failed", w.Code)
	}
}
