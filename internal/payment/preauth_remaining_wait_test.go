package payment

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"
)

func TestPreauthRemaining75ClaimAuditWaitRollsBackExpiredSendAuthority(t *testing.T) {
	s, g, o, token, _ := observationFixture68(t)
	ctx := context.Background()
	q, e := g.ObservePreauthorization(ctx, PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("9", 64)}, token, "merchant", observationCheck68, observationCheckTx68)
	if e != nil {
		t.Fatal(e)
	}
	// Shorten a synthetic observation while preserving all original MACs. The
	// event clock, not wall-clock elapsed assertions, proves the awaited stage.
	if e = s.pool.QueryRow(ctx, `SELECT clock_timestamp()+interval '20 seconds'`).Scan(&q.expiresAt); e != nil {
		t.Fatal(e)
	}
	q.sourceProof = s.observationSource(q)
	q.resultProof = s.observationResult(q)
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SET LOCAL session_replication_role=replica"); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE preauth_observations SET expires_at=$2,source_proof=$3,result_proof=$4 WHERE id=$1`, q.ID, q.expiresAt, q.sourceProof, q.resultProof); e != nil {
		t.Fatal(e)
	}
	for _, op := range []string{"claimed", "verified"} {
		proof := s.oauthHash("preauth-observation-event-v1", q.ID+"/"+hex.EncodeToString(q.sourceProof)+"/"+op)
		if _, e = tx.Exec(ctx, `UPDATE preauth_observation_events SET proof=$3 WHERE observation_id=$1 AND operation=$2`, q.ID, op, proof); e != nil {
			t.Fatal(e)
		}
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.bindPreauthRemainingRelease(remainingSourceGuard75(t, s), o.MerchantID, o.TradeNo, PreauthRemainingReleaseInput{ExpectedObservationID: q.ID, ExpectedRemainingCents: 450}, token, "merchant"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `CREATE SEQUENCE remaining_claim_wait75;
 CREATE FUNCTION remaining_claim_delay75() RETURNS trigger LANGUAGE plpgsql AS $$ DECLARE deadline timestamptz;BEGIN
 IF NEW.resource='authorization-remaining-sources' AND NEW.operation='claim' THEN
 PERFORM nextval('remaining_claim_wait75'); SELECT expires_at INTO deadline FROM preauth_observations WHERE id=NEW.changes->>'observation_id';
 PERFORM pg_sleep(GREATEST(0,extract(epoch FROM deadline-clock_timestamp())+0.05)); END IF;RETURN NEW;END $$;
 CREATE TRIGGER remaining_claim_wait75 BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION remaining_claim_delay75()`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.markPreauthRemainingSent(ctx, o.TradeNo, 1, observationCheckTx68); e == nil {
		t.Fatal("expired claim crossed send boundary")
	}
	var entered bool
	var claims int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT is_called FROM remaining_claim_wait75),(SELECT count(*) FROM authorization_remaining_claims)`).Scan(&entered, &claims); e != nil || !entered || claims != 0 {
		t.Fatal("claim audit not reached or failed to roll back", entered, claims, e)
	}
	r, e := s.AuthorizationRelease(ctx, o.MerchantID, o.TradeNo)
	if e != nil || r.Sent || r.State != "checking" || r.Amount != 450 {
		t.Fatal("expired history lost original unsent source", r, e)
	}
}
