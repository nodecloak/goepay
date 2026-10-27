package contract_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"goepay/internal/identity"
)

func TestGlobalAccess179ExportDisposedActualHTTPFixture(t *testing.T) {
	target := os.Getenv("GOEPAY_GLOBALACCESS179_UI_FIXTURE")
	if target == "" {
		t.Skip("explicit synthetic frontend fixture export only")
	}
	out := map[string]any{"synthetic_only": true, "source": "actual local Go HTTP plus PostgreSQL", "cookie_or_bearer_exported": false}
	var schema, token string
	var sessions *identity.Sessions
	if !t.Run("disposed-source", func(t *testing.T) {
		c := adminCredentialsFixture162(t)
		if e := c.pool.QueryRow(context.Background(), `SELECT current_schema()`).Scan(&schema); e != nil {
			t.Fatal(e)
		}
		token, sessions = c.token, c.s.Sessions
		public := func() map[string]any {
			t.Helper()
			w := httptest.NewRecorder()
			c.s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/console/v1/public/merchant-access", nil))
			if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("actual public fixture response", w.Code)
			}
			return credentialData162(t, w)
		}
		state := platformState179(t, platformAdmin179(c, "GET", "settings/merchant-access", "", c.csrf))
		out["admin_default"], out["public_default"] = state, public()
		state = platformState179(t, platformAdmin179(c, "PATCH", "settings/merchant-access", platformPatch179(state["fingerprint"].(string), "1", "0"), c.csrf))
		out["admin_closed"], out["public_closed"] = state, public()
		state = platformState179(t, platformAdmin179(c, "PATCH", "settings/merchant-access", platformPatch179(state["fingerprint"].(string), "0", "0"), c.csrf))
		out["admin_key_open_refund_closed"], out["public_key_open_refund_closed"] = state, public()
		state = platformState179(t, platformAdmin179(c, "PATCH", "settings/merchant-access", platformPatch179(state["fingerprint"].(string), "1", "1"), c.csrf))
		out["admin_key_closed_refund_open"], out["public_key_closed_refund_open"] = state, public()
	}) {
		return
	}
	if _, ok := sessions.Get(token, "admin"); ok {
		t.Fatal("synthetic source session remains usable")
	}
	p, e := pgxpool.New(context.Background(), os.Getenv("GOEPAY_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	var exists bool
	if e = p.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)`, schema).Scan(&exists); e != nil || exists {
		t.Fatal("source schema was not disposed", e)
	}
	out["schema_disposed"], out["session_invalid_after_cleanup"] = true, true
	raw, e := json.MarshalIndent(out, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(target, append(raw, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
}
