package contract_test

import (
	"context"
	"testing"

	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestMerchant174FullIDAndCrossOwnerFingerprintAreExact(t *testing.T) {
	const mid int64 = 9007199254740993
	f := signatureFixture(t, mid)
	ctx := context.Background()
	if e := f.s.Store.CreateMerchant(ctx, payment.Merchant{ID: mid, Name: "Exact merchant174", APIKey: "synthetic-exact-key174", SignatureAlgorithm: "MD5"}); e != nil {
		t.Fatal(e)
	}
	w := f.request("GET", "api-switches", "", f.csrf)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	large := credentialData162(t, w)
	if large["merchant_id"] != "9007199254740993" {
		t.Fatal("owner ID rounded", large["merchant_id"])
	}
	w = f.request("PATCH", "api-switches", apiSwitchBody174(large["fingerprint"].(string), 0, 0, 0), f.csrf)
	if w.Code != 200 {
		t.Fatal("full owner policy update failed", w.Code)
	}
	large = credentialData162(t, w)
	if _, e := f.p.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('merchant2','merchant',2,'Other Merchant','unused-synthetic')`); e != nil {
		t.Fatal(e)
	}
	other := f
	var e error
	other.token, other.csrf, e = f.s.Sessions.Create(identity.User{ID: 2, Role: "merchant", Username: "merchant2", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	if w = other.request("PATCH", "api-switches", apiSwitchBody174(large["fingerprint"].(string), 1, 1, 1), other.csrf); w.Code != 409 {
		t.Fatal("cross-owner fingerprint authorized policy", w.Code)
	}
	if w = f.request("GET", "api-switches?merchant_id=2", "", f.csrf); w.Code != 400 {
		t.Fatal("query owner override accepted", w.Code)
	}
}
func TestMerchant174CredentialRotationKeepsPersonalPolicyAndInvalidatesOldCAS(t *testing.T) {
	f := signatureFixture(t, 2)
	ctx := context.Background()
	before := merchant174Set(t, f, 1, 0, 1)
	m, e := f.s.Store.Merchant(ctx, 2)
	if e != nil {
		t.Fatal(e)
	}
	oldKey := m.APIKey
	m.APIKey = "synthetic-rotated-key174"
	var profile []byte
	if e = f.p.QueryRow(ctx, `SELECT profile FROM merchants WHERE id=2`).Scan(&profile); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Store.ReplaceMerchantAtomic(ctx, m, profile, nil); e != nil {
		t.Fatal(e)
	}
	after := apiSwitchState174(t, f.request("GET", "api-switches", "", f.csrf))
	if after["revision"] != before["revision"] || after["refund"] != float64(0) || after["transfer"] != float64(1) || after["keylogin"] != float64(1) || after["fingerprint"] == before["fingerprint"] {
		t.Fatal("key rotation changed/lost personal policy or CAS facts", after)
	}
	if w := f.request("PATCH", "api-switches", apiSwitchBody174(before["fingerprint"].(string), 1, 1, 1), f.csrf); w.Code != 409 {
		t.Fatal("old pre-rotation policy confirmation accepted", w.Code)
	}
	if w := merchant174KeyLogin(f, "2", oldKey, ""); w.Code != 401 {
		t.Fatal("old rotated login key remained valid", w.Code)
	}
	if w := merchant174KeyLogin(f, "2", m.APIKey, ""); w.Code != 200 {
		t.Fatal("current rotated key could not use unchanged preference", w.Code)
	}
}
func TestMerchant174LostOrForgedPersonalPolicyNeverReturnsToLegacyDefaults(t *testing.T) {
	for _, change := range []string{"row", "mac", "schema-and-anchor"} {
		t.Run(change, func(t *testing.T) {
			f, p, o := merchant174Runtime(t)
			ctx := context.Background()
			merchant174Set(t, f, 0, 0, 0)
			switch change {
			case "row":
				if _, e := f.p.Exec(ctx, `DELETE FROM merchant_api_switches`); e == nil {
					t.Fatal("personal policy was deletable")
				}
				if _, e := f.p.Exec(ctx, `ALTER TABLE merchant_api_switches DISABLE TRIGGER merchant_api_switches_no_delete;DELETE FROM merchant_api_switches;ALTER TABLE merchant_api_switches ENABLE TRIGGER merchant_api_switches_no_delete`); e != nil {
					t.Fatal(e)
				}
			case "mac":
				if _, e := f.p.Exec(ctx, `UPDATE merchant_api_switches SET keylogin=1,refund=1,transfer=1`); e != nil {
					t.Fatal(e)
				}
			case "schema-and-anchor":
				if _, e := f.p.Exec(ctx, `DROP TABLE merchant_api_switches;DELETE FROM schema_migrations WHERE name='000101_merchant_api_switches.up.sql'`); e != nil {
					t.Fatal(e)
				}
			}
			if w := f.request("GET", "api-switches", "", f.csrf); w.Code != 409 {
				t.Fatal("missing/forged personal policy claimed valid legacy default", change, w.Code)
			}
			if out := merchant174Invoke(t, f, "/api/pay/refund", compat.Params{"out_trade_no": o.OutTradeNo, "out_refund_no": "after-policy-loss174", "money": "1.00"}); out["code"] != float64(-1) {
				t.Fatal("lost personal policy admitted fresh refund", out)
			}
			if w := merchant174KeyLogin(f, "2", "contract-rsa-secret", ""); w.Code == 200 || len(w.Result().Cookies()) != 0 {
				t.Fatal("lost policy reopened key login", change, w.Code)
			}
			var intents int
			if e := f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM refunds)+(SELECT count(*) FROM transfers)`).Scan(&intents); e != nil || intents != 0 || p.refundWrites.Load() != 0 || p.transferWrites.Load() != 0 {
				t.Fatal("lost personal policy reserved/sent funds", intents, e)
			}
		})
	}
}
