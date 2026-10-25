package backoffice

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestInvitationUXConcurrentReplayAndBoundedClear(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	const requestID = "0fc684fa-f7ec-4bd8-8f11-a9732ca50eb1"
	var wg sync.WaitGroup
	results := make(chan InvitationBatch, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, e := s.GenerateInvitations(ctx, admin, requestID, 1000)
			if e != nil {
				t.Error(e)
				return
			}
			results <- out
		}()
	}
	wg.Wait()
	close(results)
	first := ""
	fresh := 0
	for out := range results {
		if len(out.Items) != 1000 {
			t.Fatalf("wrong batch length %d", len(out.Items))
		}
		if !out.Replayed {
			fresh++
		}
		if first == "" {
			first = out.Items[0]["code"]
		}
		if out.Items[0]["code"] != first {
			t.Fatal("replayed a different batch")
		}
	}
	if fresh != 1 {
		t.Fatalf("fresh batches %d", fresh)
	}
	if _, e := s.GenerateInvitations(ctx, admin, "0fc684fa-f7ec-4bd8-8f11-a9732ca50eb2", 1); e != nil {
		t.Fatal(e)
	}
	clearID := "0fc684fa-f7ec-4bd8-8f11-a9732ca50eb3"
	out, e := s.ClearInvitations(ctx, admin, clearID, "all", true)
	if e != nil || out.Archived != 1000 || out.Remaining != 1 {
		t.Fatalf("bounded clear %+v %v", out, e)
	}
	out, e = s.ClearInvitations(ctx, admin, clearID, "all", true)
	if e != nil || out.Archived != 1000 || out.Remaining != 1 || !out.Replayed {
		t.Fatalf("clear replay %+v %v", out, e)
	}
	if _, e = s.ClearInvitations(ctx, admin, clearID, "used", true); !errors.Is(e, ErrConflict) {
		t.Fatalf("changed clear scope %v", e)
	}
	_, total, e := s.Invitations(ctx, admin, Query{}, "")
	if e != nil || total != 1 {
		t.Fatalf("replay cleared next page %d %v", total, e)
	}
	if _, e = s.GenerateInvitations(ctx, Principal{Role: "merchant", SubjectID: 101}, requestID, 1); !errors.Is(e, ErrForbidden) {
		t.Fatalf("merchant generated invites %v", e)
	}
}

func TestInvitationUXAuditFailureRollsBackGenerationAndArchive(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	batch, e := s.GenerateInvitations(ctx, admin, "4ebc88c5-0842-4380-8fed-a971dce45001", 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `CREATE FUNCTION fail_invitation_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource IN ('invitation-codes','invitation-batches') THEN RAISE EXCEPTION 'synthetic audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER test_invitation_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION fail_invitation_audit()`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.GenerateInvitations(ctx, admin, "4ebc88c5-0842-4380-8fed-a971dce45002", 3); e == nil {
		t.Fatal("generation ignored failed audit")
	}
	if _, e = s.ClearInvitations(ctx, admin, "4ebc88c5-0842-4380-8fed-a971dce45003", "all", true); e == nil {
		t.Fatal("archive ignored failed audit")
	}
	var active, all int
	if e = s.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE body->>'enabled'='true' AND body->>'archived' IS DISTINCT FROM 'true') FROM backoffice_records WHERE resource='invitation-codes'`).Scan(&all, &active); e != nil || all != 1 || active != 1 {
		t.Fatalf("audit rollback all=%d active=%d %v", all, active, e)
	}
	if _, e = s.pool.Exec(ctx, `DROP TRIGGER test_invitation_audit ON backoffice_audit`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Update(ctx, admin, "invitation-codes", mustInvitationID(t, batch.Items[0]["id"]), input(`{"code":"REASSIGNED"}`)); !errors.Is(e, ErrForbidden) {
		t.Fatalf("existing invite identity changed %v", e)
	}
}
func mustInvitationID(t *testing.T, raw string) int64 {
	t.Helper()
	id, e := invitationID(raw)
	if e != nil {
		t.Fatal(e)
	}
	return id
}
