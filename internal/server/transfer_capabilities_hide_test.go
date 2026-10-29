package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
)

// multiTransferProvider offers both the wxpay and qqpay transfer rails on one
// channel, so the transfer_wxpay / transfer_qqpay switches can be seen to filter
// the offered types in the capabilities response.
type multiTransferProvider struct{ *channel168Provider }

func (*multiTransferProvider) TransferTypes() []string                        { return []string{"wxpay", "qqpay"} }
func (*multiTransferProvider) ValidateTransfer(adapter.TransferRequest) error { return nil }
func (*multiTransferProvider) Transfer(context.Context, adapter.TransferRequest) (adapter.TransferResult, error) {
	return adapter.TransferResult{}, nil
}
func (*multiTransferProvider) QueryTransfer(context.Context, adapter.TransferRequest) (adapter.TransferResult, error) {
	return adapter.TransferResult{}, nil
}

// transfer_wxpay=0 removes the wxpay rail from the transfer form's offered types
// (the server filters the channel capabilities); the enabled qqpay rail stays.
func TestTransferCapabilitiesHidesDisabledTransferRail(t *testing.T) {
	f := channel168Local(t)
	f.s.ProviderFlow.Providers[7] = &multiTransferProvider{channel168Provider: &channel168Provider{app: "app168-seven"}}
	ctx := context.Background()
	if _, e := f.p.Exec(ctx, `UPDATE merchants SET profile=profile||'{"transfer_api_enabled":true,"transfer_rate":"0"}'::jsonb WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.p.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('tmerchant1','merchant',1,'Synthetic','unused')`); e != nil {
		t.Fatal(e)
	}
	token, _, e := f.s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "tmerchant1", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	caps := func() map[string]any {
		r := httptest.NewRequest("GET", "/console/v1/merchant/transfers/capabilities", nil)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		var out struct {
			Data map[string]any `json:"data"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatalf("capabilities HTTP %d %s", w.Code, w.Body)
		}
		return out.Data
	}
	types := func() map[string]bool {
		got := map[string]bool{}
		for _, c := range caps()["channels"].([]any) {
			for _, tp := range c.(map[string]any)["types"].([]any) {
				got[tp.(string)] = true
			}
		}
		return got
	}

	// Default (no switch rows): both rails offered.
	if base := types(); !base["wxpay"] || !base["qqpay"] {
		t.Fatalf("default should offer both rails: %v", base)
	}
	// transfer_wxpay=0 → wxpay hidden, qqpay still offered.
	if _, e := f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key','transfer_wxpay','value','0'))`); e != nil {
		t.Fatal(e)
	}
	got := types()
	if got["wxpay"] {
		t.Fatalf("transfer_wxpay=0 should hide wxpay: %v", got)
	}
	if !got["qqpay"] {
		t.Fatalf("transfer_qqpay still on should offer qqpay: %v", got)
	}
}
