package server

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func parent180Post(t *testing.T, f channel168Fixture, body map[string]any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(body)
	w := parent180HTTP(f, "POST", "", string(raw))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct{ Data map[string]any }
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	return out.Data
}
func TestParentChannel180ActualHTTPCreateReplayKeepsOneAccountAndRejectsChangedIntent(t *testing.T) {
	f, body := parent180ReadyCreate(t)
	first := parent180Post(t, f, body)
	second := parent180Post(t, f, body)
	if first["id"] != second["id"] || first["fingerprint"] != second["fingerprint"] {
		t.Fatal("same original create intent was recreated")
	}
	for _, table := range []string{"runtime_parent_channels", "runtime_parent_channel_accounts", "runtime_parent_channel_requests"} {
		var n int
		if e := f.p.QueryRow(t.Context(), `SELECT count(*) FROM `+table).Scan(&n); e != nil || n != 1 {
			t.Fatal("replay changed immutable source count", table, n, e)
		}
	}
	body["name"] = "Changed original create intent"
	raw, _ := json.Marshal(body)
	w := parent180HTTP(f, "POST", "", string(raw))
	if w.Code != 409 {
		t.Fatal("changed create intent accepted", w.Code)
	}
	var n int
	f.p.QueryRow(t.Context(), `SELECT count(*) FROM runtime_parent_channels`).Scan(&n)
	if n != 1 {
		t.Fatal("unknown/replayed creation produced another parent")
	}
}
func TestParentChannel180ActualHTTPCreateExpiryAfterLastWriteRollsEverythingBack(t *testing.T) {
	f, body := parent180ReadyCreate(t)
	ctx := t.Context()
	if _, e := f.p.Exec(ctx, `CREATE FUNCTION parent180_delay_create() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM pg_sleep(1.3);RETURN NEW;END$$; CREATE TRIGGER parent180_delay_create AFTER INSERT ON runtime_parent_channel_requests FOR EACH ROW EXECUTE FUNCTION parent180_delay_create(); UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE username='admin168'`); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(body)
	w := parent180HTTP(f, "POST", "", string(raw))
	if w.Code != 403 {
		t.Fatal("expired creator committed after last delayed write", w.Code, w.Body.String())
	}
	for _, table := range []string{"runtime_parent_channels", "runtime_parent_channel_accounts", "runtime_parent_channel_grants", "runtime_parent_channel_requests"} {
		var n int
		if e := f.p.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); e != nil || n != 0 {
			t.Fatal("expired creation left business source", table, n, e)
		}
	}
	var n int
	if e := f.p.QueryRow(ctx, `SELECT count(*) FROM runtime_channel_policies WHERE channel_id NOT IN (7,8)`).Scan(&n); e != nil || n != 0 {
		t.Fatal("expired creation left enabled/fee policy", n, e)
	}
}
func TestParentChannel180ActualHTTPCreateRechecksCSRFStampAfterAdmissionWait(t *testing.T) {
	f, body := parent180ReadyCreate(t)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	gate, e := f.p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer gate.Rollback(context.Background())
	if _, e = gate.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('goepay/route/channel/8',0))`); e != nil {
		t.Fatal(e)
	}
	pid := int(gate.Conn().PgConn().PID())
	raw, _ := json.Marshal(body)
	done := make(chan int, 1)
	go func() { w := parent180HTTP(f, "POST", "", string(raw)); done <- w.Code }()
	for {
		var blocked bool
		if e = f.p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::int=ANY(pg_blocking_pids(pid)))`, pid).Scan(&blocked); e != nil {
			t.Fatal(e)
		}
		if blocked {
			break
		}
		select {
		case status := <-done:
			t.Fatal("create never reached controlled admission wait", status)
		case <-time.After(10 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal("create never reached parent lock")
		}
	}
	if _, e = f.p.Exec(ctx, `UPDATE console_sessions SET csrf=$1 WHERE username='admin168'`, strings.Repeat("d", 43)); e != nil {
		t.Fatal(e)
	}
	if e = gate.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case status := <-done:
		if status != 403 {
			t.Fatal("stale CSRF form committed after wait", status)
		}
	case <-ctx.Done():
		t.Fatal("create did not finish after gate")
	}
	var n int
	f.p.QueryRow(ctx, `SELECT count(*) FROM runtime_parent_channels`).Scan(&n)
	if n != 0 {
		t.Fatal("stale form wrote a parent account")
	}
}
func TestParentChannel180ActualHTTPAccountChangeKeepsOldVersionAndRejectsLegalOldHeadRestore(t *testing.T) {
	f, body := parent180ReadyCreate(t)
	first := parent180Post(t, f, body)
	id := first["id"].(string)
	var oldHead, oldCipher []byte
	if e := f.p.QueryRow(t.Context(), `SELECT to_jsonb(h),a.ciphertext FROM runtime_parent_channels h JOIN runtime_parent_channel_accounts a ON a.channel_id=h.id AND a.version=1 WHERE h.id=$1`, id).Scan(&oldHead, &oldCipher); e != nil {
		t.Fatal(e)
	}
	patch, _ := json.Marshal(map[string]any{"expected_fingerprint": first["fingerprint"], "account": map[string]any{"config": map[string]any{"appmchid": map[string]string{"operation": "replace", "value": "synthetic180-revised-merchant"}}}})
	changed := parent180HTTP(f, "PATCH", "/"+id, string(patch))
	if changed.Code != 200 {
		t.Fatalf("real parent account version change unavailable: HTTP=%d %s", changed.Code, changed.Body.String())
	}
	var out struct{ Data map[string]any }
	if e := json.Unmarshal(changed.Body.Bytes(), &out); e != nil || out.Data["account_version"] != "2" || out.Data["revision"] != "2" {
		t.Fatal("account edit overwrote prior version", e, out.Data)
	}
	var currentOld []byte
	var count int
	f.p.QueryRow(t.Context(), `SELECT ciphertext FROM runtime_parent_channel_accounts WHERE channel_id=$1 AND version=1`, id).Scan(&currentOld)
	f.p.QueryRow(t.Context(), `SELECT count(*) FROM runtime_parent_channel_accounts WHERE channel_id=$1`, id).Scan(&count)
	if !bytes.Equal(oldCipher, currentOld) || count != 2 {
		t.Fatal("old account was modified in place")
	}
	if _, e := f.p.Exec(t.Context(), `WITH original AS (SELECT * FROM jsonb_populate_record(NULL::runtime_parent_channels,$1::jsonb)) UPDATE runtime_parent_channels h SET (name,revision,account_version,archived,proof_hash,created_at,updated_at)=(original.name,original.revision,original.account_version,original.archived,original.proof_hash,original.created_at,original.updated_at) FROM original WHERE h.id=original.id`, string(oldHead)); e != nil {
		t.Fatal("synthetic legal old row restore failed", e)
	}
	listed := parent180HTTP(f, "GET", "", "")
	if listed.Code != 409 {
		t.Fatal("legal old parent head revived despite complete immutable/audit history", listed.Code, listed.Body.String())
	}
}
