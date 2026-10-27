package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"goepay/internal/payment"
)

func TestMerchant174KeyLoginRealStrictDTOAndDisposableLargeIDFixture(t *testing.T) {
	ctx := context.Background()
	var captured json.RawMessage
	for _, mid := range []int64{2, 9007199254740993} {
		var f signatureFixture163
		var token, schema string
		ran := t.Run(fmt.Sprint(mid), func(t *testing.T) {
			f = signatureFixture(t, mid)
			schema = f.p.Config().ConnConfig.RuntimeParams["search_path"]
			if mid != 2 {
				if e := f.s.Store.CreateMerchant(ctx, payment.Merchant{ID: mid, Name: "Synthetic strict DTO merchant174", APIKey: "synthetic-disposable-big-key174", SignatureAlgorithm: "MD5"}); e != nil {
					t.Fatal(e)
				}
			}
			var adminClass string
			if e := f.p.QueryRow(ctx, `SELECT admin_class FROM console_users WHERE username='merchant'`).Scan(&adminClass); e != nil || adminClass != "operator" {
				t.Fatal("fixture must exercise real schema16 merchant default", adminClass, e)
			}
			merchant, e := f.s.Store.Merchant(ctx, mid)
			if e != nil {
				t.Fatal(e)
			}
			w := merchant174KeyLogin(f, fmt.Sprint(mid), merchant.APIKey, "")
			if w.Code != 200 {
				t.Fatal("real valid login", w.Code)
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatal("original persistent cookie missing")
			}
			token = cookies[0].Value
			var response map[string]json.RawMessage
			if json.Unmarshal(w.Body.Bytes(), &response) != nil || len(response) != 3 || response["code"] == nil || response["message"] == nil || response["data"] == nil {
				t.Fatal("strict login envelope")
			}
			var code int
			var message string
			if json.Unmarshal(response["code"], &code) != nil || code != 0 || json.Unmarshal(response["message"], &message) != nil {
				t.Fatal("strict successful login envelope types")
			}
			var data map[string]json.RawMessage
			if json.Unmarshal(response["data"], &data) != nil || len(data) != 2 || data["user"] == nil || data["csrf_token"] == nil {
				t.Fatal("strict login data")
			}
			var user map[string]json.RawMessage
			if json.Unmarshal(data["user"], &user) != nil || len(user) != 3 || user["id"] == nil || user["name"] == nil || user["role"] == nil {
				keys := []string{}
				for k := range user {
					keys = append(keys, k)
				}
				t.Fatalf("actual merchant login user must contain only id/name/role; got keys %v", keys)
			}
			var id, name, role, csrf string
			if json.Unmarshal(user["id"], &id) != nil || id != fmt.Sprint(mid) || json.Unmarshal(user["name"], &name) != nil || name == "" || json.Unmarshal(user["role"], &role) != nil || role != "merchant" || json.Unmarshal(data["csrf_token"], &csrf) != nil || !regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`).MatchString(csrf) {
				t.Fatal("actual strict merchant identity/CSRF")
			}
			if mid > 9007199254740991 {
				captured = append(json.RawMessage(nil), w.Body.Bytes()...)
			}
		})
		if schema == "" {
			continue
		}
		// The subtest's existing fixture cleanup closes its pool and drops schema.
		if _, ok := f.s.Sessions.Get(token, "merchant"); ok {
			t.Fatal("exported synthetic session remained usable after cleanup")
		}
		admin, e := pgxpool.New(ctx, os.Getenv("GOEPAY_TEST_DATABASE_URL"))
		if e != nil {
			t.Fatal(e)
		}
		var exists bool
		e = admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)`, schema).Scan(&exists)
		admin.Close()
		if e != nil || exists {
			t.Fatal("synthetic fixture schema was not disposed", e)
		}
		if !ran {
			continue
		}
	}
	if t.Failed() {
		return
	}
	if target := os.Getenv("GOEPAY_MERCHANT174_KEY_DTO_FIXTURE"); target != "" {
		if len(captured) == 0 {
			t.Fatal("actual large-ID response unavailable")
		}
		out := map[string]any{"synthetic_only": true, "source": "actual local Go HTTP handler plus PostgreSQL", "producer": "TestMerchant174KeyLoginRealStrictDTOAndDisposableLargeIDFixture", "merchant_id": "9007199254740993", "schema_disposed": true, "session_invalid_after_cleanup": true, "cookie_or_bearer_exported": false, "response": captured}
		raw, e := json.MarshalIndent(out, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(target, append(raw, '\n'), 0600); e != nil {
			t.Fatal(e)
		}
	}
}
