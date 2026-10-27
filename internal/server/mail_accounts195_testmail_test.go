package server

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

func TestMailAccounts195TestEmailFinalExpiryRollsBackAdmissionBeforeAnySMTPConnection(t *testing.T) {
	peer := mail195LocalTLS(t, false)
	f := channel168Local(t)
	f.s.Mail195TestEnabled = true
	f.s.Mail195SMTPRoots = peer.roots
	state := mail195Data(t, mail195Request(f, "GET", "/smtp", ""))
	state = mail195SaveTLS(t, f, state, peer)
	if _, e := f.p.Exec(t.Context(), `CREATE SEQUENCE mail195_test_final_probe;CREATE FUNCTION mail195_test_delay()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM nextval('mail195_test_final_probe');PERFORM pg_sleep(3);RETURN NEW;END$$;CREATE TRIGGER mail195_test_delay_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW WHEN(NEW.resource='mail-test195')EXECUTE FUNCTION mail195_test_delay();UPDATE console_sessions SET expires_at=clock_timestamp()+interval '2 seconds' WHERE role='admin'`); e != nil {
		t.Fatal(e)
	}
	if w := mail195Request(f, "POST", "/smtp/test", mail195TestBody(t, state, "19500000-0000-4000-8000-000000000003", "recipient195@example.test")); w.Code != 403 {
		t.Fatal("expired original test confirmation admitted", w.Code)
	}
	var rows int
	var entered bool
	if e := f.p.QueryRow(t.Context(), `SELECT is_called FROM mail195_test_final_probe`).Scan(&entered); e != nil || !entered {
		t.Fatal("test mail last waiting write not reached", entered, e)
	}
	if e := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM mail_test_requests195)+(SELECT count(*)FROM mail_test_outcomes195)+(SELECT count(*)FROM backoffice_audit WHERE resource='mail-test195')`).Scan(&rows); e != nil || rows != 0 || peer.connections.Load() != 0 {
		t.Fatal("expired confirmation reached SMTP or left declaration", rows, peer.connections.Load(), e)
	}
}

func TestMailAccounts195TestEmailDailyWindowBudgetSurvivesAccountRotationAndReplay(t *testing.T) {
	peer := mail195LocalTLS(t, false)
	f := channel168Local(t)
	f.s.Mail195TestEnabled = true
	f.s.Mail195SMTPRoots = peer.roots
	state := mail195Data(t, mail195Request(f, "GET", "/smtp", ""))
	state = mail195SaveTLS(t, f, state, peer)
	var original string
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("19500000-0000-4000-8000-%012d", 100+i)
		body := mail195TestBody(t, state, id, "recipient195@example.test")
		if i == 1 {
			original = body
		}
		result := mail195Data(t, mail195Request(f, "POST", "/smtp/test", body))
		if result["status"] != "accepted" {
			t.Fatal("local synthetic SMTP did not accept", result)
		}
	}
	state = mail195SaveTLS(t, f, state, peer)
	if w := mail195Request(f, "POST", "/smtp/test", mail195TestBody(t, state, "19500000-0000-4000-8000-000000000106", "recipient195@example.test")); w.Code != 429 {
		t.Fatal("new account revision reset original test budget", w.Code)
	}
	if result := mail195Data(t, mail195Request(f, "POST", "/smtp/test", original)); result["status"] != "accepted" {
		t.Fatal("replay lost original accepted result after account rotation", result)
	}
	if peer.connections.Load() != 5 || peer.data.Load() != 5 {
		t.Fatal("rate rejection or replay resubmitted mail", peer.connections.Load(), peer.data.Load())
	}
}

func mail195TestBody(t *testing.T, state map[string]any, id, to string) string {
	t.Helper()
	raw, e := json.Marshal(map[string]any{"request_id": id, "to": to, "expected_fingerprint": state["fingerprint"]})
	if e != nil {
		t.Fatal(e)
	}
	return string(raw)
}
func TestMailAccounts195TestEmailDefaultOffAndActualTLSConcurrentOneSubmission(t *testing.T) {
	peer := mail195LocalTLS(t, false)
	f := channel168Local(t)
	state := mail195Data(t, mail195Request(f, "GET", "/smtp", ""))
	state = mail195SaveTLS(t, f, state, peer)
	const id = "19500000-0000-4000-8000-000000000001"
	body := mail195TestBody(t, state, id, "recipient195@example.test")
	if w := mail195Request(f, "POST", "/smtp/test", body); w.Code != 503 {
		t.Fatal("test email default-off gate missing", w.Code)
	}
	if peer.connections.Load() != 0 {
		t.Fatal("default-off test email connected")
	}
	f.s.Mail195TestEnabled = true
	f.s.Mail195SMTPRoots = peer.roots
	state = mail195Data(t, mail195Request(f, "GET", "/smtp", ""))
	if state["test_available"] != true {
		t.Fatal("explicit local test mail capability not exposed")
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := mail195Request(f, "POST", "/smtp/test", body)
			if w.Code != 200 {
				t.Error("original test request unavailable", w.Code)
			}
		}()
	}
	wg.Wait()
	result := mail195Data(t, mail195Request(f, "GET", "/smtp/tests/"+id, ""))
	if result["status"] != "accepted" || result["request_id"] != id || peer.connections.Load() != 1 || peer.data.Load() != 1 {
		t.Fatal("concurrent test email missing or duplicated", result, peer.connections.Load(), peer.data.Load())
	}
	if w := mail195Request(f, "POST", "/smtp/test", mail195TestBody(t, state, id, "different195@example.test")); w.Code != 409 {
		t.Fatal("test request recipient was rebound", w.Code)
	}
	var requests, outcomes, effects int
	if e := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM mail_test_requests195),(SELECT count(*)FROM mail_test_outcomes195),(SELECT count(*)FROM orders)+(SELECT count(*)FROM journals)+(SELECT count(*)FROM jobs)+(SELECT count(*)FROM enrollment_challenges)`).Scan(&requests, &outcomes, &effects); e != nil || requests != 1 || outcomes != 1 || effects != 0 {
		t.Fatal("test mail state or financial effects", requests, outcomes, effects, e)
	}
}

func TestMailAccounts195TestEmailUnknownDataReplyStaysUnknownAndNeverResends(t *testing.T) {
	peer := mail195LocalTLS(t, true)
	f := channel168Local(t)
	f.s.Mail195TestEnabled = true
	f.s.Mail195SMTPRoots = peer.roots
	state := mail195Data(t, mail195Request(f, "GET", "/smtp", ""))
	state = mail195SaveTLS(t, f, state, peer)
	const id = "19500000-0000-4000-8000-000000000002"
	body := mail195TestBody(t, state, id, "recipient195@example.test")
	for i := 0; i < 2; i++ {
		result := mail195Data(t, mail195Request(f, "POST", "/smtp/test", body))
		if result["status"] != "unknown" {
			t.Fatal("uncertain SMTP DATA advertised definite delivery", result)
		}
	}
	if peer.connections.Load() != 1 || peer.data.Load() != 1 {
		t.Fatal("unknown original test email resubmitted", peer.connections.Load(), peer.data.Load())
	}
}
