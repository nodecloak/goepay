package server

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
	"os"
	"strings"
	"testing"
)

func TestOrderBatch185ExportDisposedActualHTTPFixture(t *testing.T) {
	target := os.Getenv("GOEPAY_ORDERBATCH185_UI_FIXTURE")
	if target == "" {
		t.Skip("explicit local UI fixture export only")
	}
	out := map[string]any{"synthetic_only": true, "source": "actual local Go HTTP and PostgreSQL", "cookie_or_bearer_exported": false}
	var schema, token, csrf string
	var sessions *identity.Sessions
	if !t.Run("disposed-source", func(t *testing.T) {
		f, _, row := subchannel168BProductLocal(t)
		sessions, token, csrf = f.s.Sessions, f.token, f.csrf
		if e := f.p.QueryRow(context.Background(), `SELECT current_schema()`).Scan(&schema); e != nil {
			t.Fatal(e)
		}
		if v := f.pay(t, "fixture-query185", "1.00", 7); v["code"] != float64(1) {
			t.Fatal(v)
		}
		o, e := f.s.Store.Order(context.Background(), 1, "fixture-query185")
		if e != nil {
			t.Fatal(e)
		}
		prepared := batch185Prepared(t, f, o.TradeNo, "fixture-prepared185")
		out["prepared"] = prepared
		decode := func(wbody []byte) map[string]any {
			t.Helper()
			var v struct{ Data map[string]any }
			if json.Unmarshal(wbody, &v) != nil || v.Data == nil {
				t.Fatal("fixture envelope")
			}
			return v.Data
		}
		w := batch185Request(f, "POST", "/"+prepared["id"].(string)+"/execute", `{"limit":1}`)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		out["completed"] = decode(w.Body.Bytes())
		subchannel168BRotate(t, f, row)
		trades := []map[string]string{}
		for _, no := range []string{"fixture-partial185-a", "fixture-partial185-b"} {
			if v := f.pay(t, no, "1.00", 7); v["code"] != float64(1) {
				t.Fatal(v)
			}
			o, e := f.s.Store.Order(context.Background(), 1, no)
			if e != nil {
				t.Fatal(e)
			}
			trades = append(trades, map[string]string{"trade_no": o.TradeNo})
		}
		body, _ := json.Marshal(map[string]any{"external_no": "fixture-partial185", "action": "query", "reason": "local original progress", "items": trades})
		w = batch185Request(f, "POST", "", string(body))
		if w.Code != 201 {
			t.Fatal(w.Code, w.Body.String())
		}
		p := decode(w.Body.Bytes())
		w = batch185Request(f, "POST", "/"+p["id"].(string)+"/execute", `{"limit":1}`)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		partial := decode(w.Body.Bytes())
		out["partial"] = partial
		cancel, _ := json.Marshal(map[string]any{"expected_fingerprint": partial["fingerprint"]})
		w = batch185Request(f, "POST", "/"+p["id"].(string)+"/cancel", string(cancel))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		out["partly_cancelled"] = decode(w.Body.Bytes())
	}) {
		return
	}
	if _, ok := sessions.Get(token, "admin"); ok {
		t.Fatal("fixture original session remains valid")
	}
	p, e := pgxpool.New(context.Background(), os.Getenv("GOEPAY_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	var remains bool
	if e = p.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)`, schema).Scan(&remains); e != nil || remains {
		t.Fatal("fixture source schema remains", e)
	}
	out["schema_disposed"], out["session_invalid_after_cleanup"] = true, true
	raw, e := json.MarshalIndent(out, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), token) || strings.Contains(string(raw), csrf) {
		t.Fatal("fixture exported usable credentials")
	}
	if e = os.WriteFile(target, append(raw, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
}
