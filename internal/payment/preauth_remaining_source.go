package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type remainingEvidenceReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type preauthRemainingSource struct {
	ID, OrderID, Revision int64
	PreviousID            *int64
	PreviousHash          []byte
	ObservationID         string
	Terms                 preauthRemainingReleaseTerms
	CreatedAt             time.Time
	TransactionID         string
	Proof                 []byte
	observation           PreauthorizationObservation
}
type preauthRemainingClaim struct {
	OrderID, SourceID, Revision int64
	CreatedAt                   time.Time
	TransactionID               string
	Proof                       []byte
}

func (s *Store) remainingSourceProof(v preauthRemainingSource) []byte {
	raw, _ := json.Marshal([]any{v.ID, v.OrderID, v.Revision, v.PreviousID, v.PreviousHash, v.ObservationID, v.Terms, v.CreatedAt.UTC().Format(time.RFC3339Nano), v.TransactionID, v.observation.sourceProof, v.observation.resultProof})
	return s.oauthHash("preauth-remaining75/source", string(raw))
}
func (s *Store) remainingClaimProof(c preauthRemainingClaim, v preauthRemainingSource) []byte {
	raw, _ := json.Marshal([]any{c.OrderID, c.SourceID, c.Revision, c.CreatedAt.UTC().Format(time.RFC3339Nano), c.TransactionID, v.Proof})
	return s.oauthHash("preauth-remaining75/claim", string(raw))
}
func remainingAudit(ctx context.Context, tx pgx.Tx, v preauthRemainingSource, op string, proof []byte) error {
	a := v.observation.original
	_, e := tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES($1,$2,$3,'authorization-remaining-sources',$4,$5,jsonb_build_object('proof',$6::text,'order_id',$7::text,'revision',$8::text,'observation_id',$9::text))`, a.actorRole, a.actorID, a.actorUsername, v.ID, op, hex.EncodeToString(proof), fmt.Sprint(v.OrderID), fmt.Sprint(v.Revision), v.ObservationID)
	return e
}
func remainingAuditValid(ctx context.Context, tx remainingEvidenceReader, v preauthRemainingSource, op string, proof []byte) error {
	a := v.observation.original
	var ok bool
	e := tx.QueryRow(ctx, `SELECT count(*)=1 FROM backoffice_audit WHERE resource='authorization-remaining-sources' AND resource_id=$1 AND operation=$2 AND (actor_role,actor_id,actor_name)=($3,$4,$5) AND changes=jsonb_build_object('proof',$6::text,'order_id',$7::text,'revision',$8::text,'observation_id',$9::text)`, v.ID, op, a.actorRole, a.actorID, a.actorUsername, hex.EncodeToString(proof), fmt.Sprint(v.OrderID), fmt.Sprint(v.Revision), v.ObservationID).Scan(&ok)
	if e != nil {
		return e
	}
	if !ok {
		return ErrConflict
	}
	return nil
}
func (s *Store) readAuthorizationReleaseTx(ctx context.Context, tx remainingEvidenceReader, id int64) (AuthorizationRelease, error) {
	r, e := scanAuthorizationRelease(tx.QueryRow(ctx, "SELECT "+authorizationReleaseColumns+" FROM payment_authorization_releases r JOIN orders o ON o.id=r.order_id WHERE r.order_id=$1", id))
	if e != nil {
		return r, e
	}
	return s.attachPreauthRemaining(ctx, tx, r)
}

// A parent with a remaining head can never fall back to its legacy gross
// amount when a source, observation, audit or claim has been lost.
func (s *Store) remainingSources(ctx context.Context, tx remainingEvidenceReader, r AuthorizationRelease) ([]preauthRemainingSource, error) {
	rows, e := tx.Query(ctx, `SELECT id,order_id,revision,previous_id,previous_hash,observation_id,terms,created_at,transaction_id::text,proof_hash FROM authorization_remaining_sources WHERE order_id=$1 ORDER BY revision LIMIT 101`, r.OrderID)
	if e != nil {
		return nil, e
	}
	all := []preauthRemainingSource{}
	for rows.Next() {
		var v preauthRemainingSource
		var raw []byte
		if e = rows.Scan(&v.ID, &v.OrderID, &v.Revision, &v.PreviousID, &v.PreviousHash, &v.ObservationID, &raw, &v.CreatedAt, &v.TransactionID, &v.Proof); e != nil {
			rows.Close()
			return nil, e
		}
		if json.Unmarshal(raw, &v.Terms) != nil {
			rows.Close()
			return nil, ErrConflict
		}
		all = append(all, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if r.SourceRevision < 0 || r.SourceRevision > 100 || int64(len(all)) != r.SourceRevision {
		return nil, ErrConflict
	}
	if r.SourceRevision == 0 {
		if r.remainingSourceID != nil || len(r.remainingSourceHash) != 0 {
			return nil, ErrConflict
		}
		var orphan bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='authorization-remaining-sources' AND changes->>'order_id'=$1)`, fmt.Sprint(r.OrderID)).Scan(&orphan); e != nil {
			return nil, e
		}
		if orphan {
			return nil, ErrConflict
		}
		return all, nil
	}
	if r.remainingSourceID == nil || len(r.remainingSourceHash) != 32 {
		return nil, ErrConflict
	}
	var auditCount int64
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='authorization-remaining-sources' AND changes->>'order_id'=$1`, fmt.Sprint(r.OrderID)).Scan(&auditCount); e != nil {
		return nil, e
	}
	expectedAudits := r.SourceRevision
	if r.Sent {
		expectedAudits++
	}
	if auditCount != expectedAudits {
		return nil, ErrConflict
	}
	for i := range all {
		v := &all[i]
		if v.Revision != int64(i+1) || v.OrderID != r.OrderID || len(v.Proof) != 32 || v.TransactionID == "" {
			return nil, ErrConflict
		}
		if i == 0 {
			if v.PreviousID != nil || len(v.PreviousHash) != 0 {
				return nil, ErrConflict
			}
		} else if v.PreviousID == nil || *v.PreviousID != all[i-1].ID || !hmac.Equal(v.PreviousHash, all[i-1].Proof) || v.CreatedAt.Before(all[i-1].CreatedAt) {
			return nil, ErrConflict
		}
		q, err := s.scanObservation(tx.QueryRow(ctx, "SELECT "+preauthObservationColumns+" FROM preauth_observations WHERE id=$1", v.ObservationID))
		if err != nil || s.observationOrigin(ctx, tx, q) != nil || s.observationAuditValid(ctx, tx, q) != nil {
			return nil, ErrConflict
		}
		v.observation = q
		expected, err := preauthRemainingTerms(q, PreauthRemainingReleaseInput{ExpectedObservationID: v.ObservationID, ExpectedRemainingCents: v.Terms.Amount, ExpectedSourceRevision: v.Revision - 1}, v.CreatedAt)
		if err != nil || expected != v.Terms || q.OrderID != r.OrderID || q.ChannelID != r.ChannelID || q.AuthNo != r.AuthNo || q.TradeNo != r.TradeNo || q.Amount != r.Amount || v.Terms.RequestNo != r.RequestNo || !hmac.Equal(v.Proof, s.remainingSourceProof(*v)) {
			return nil, ErrConflict
		}
		if err = remainingAuditValid(ctx, tx, *v, "authorize", v.Proof); err != nil {
			return nil, err
		}
	}
	last := all[len(all)-1]
	if last.ID != *r.remainingSourceID || !hmac.Equal(last.Proof, r.remainingSourceHash) {
		return nil, ErrConflict
	}
	return all, nil
}
func (s *Store) attachPreauthRemaining(ctx context.Context, tx remainingEvidenceReader, r AuthorizationRelease) (AuthorizationRelease, error) {
	all, e := s.remainingSources(ctx, tx, r)
	if e != nil {
		return r, e
	}
	var c preauthRemainingClaim
	e = tx.QueryRow(ctx, `SELECT order_id,source_id,source_revision,created_at,transaction_id::text,proof_hash FROM authorization_remaining_claims WHERE order_id=$1`, r.OrderID).Scan(&c.OrderID, &c.SourceID, &c.Revision, &c.CreatedAt, &c.TransactionID, &c.Proof)
	if r.SourceRevision == 0 {
		if !errors.Is(e, pgx.ErrNoRows) {
			return r, ErrConflict
		}
		return r, nil
	}
	v := all[len(all)-1]
	if !r.Sent {
		if !errors.Is(e, pgx.ErrNoRows) {
			return r, ErrConflict
		}
	} else {
		if e != nil || c.OrderID != r.OrderID || c.SourceID != v.ID || c.Revision != v.Revision || c.CreatedAt.Before(v.CreatedAt) || !c.CreatedAt.Before(v.Terms.SendBefore) || c.TransactionID == "" || len(c.Proof) != 32 || !hmac.Equal(c.Proof, s.remainingClaimProof(c, v)) {
			return r, ErrConflict
		}
		if e = remainingAuditValid(ctx, tx, v, "claim", c.Proof); e != nil {
			return r, e
		}
	}
	r.OriginalAmount, r.Amount = r.Amount, v.Terms.Amount
	return r, nil
}

func (s *Store) bindPreauthRemainingRelease(ctx context.Context, mid int64, trade string, in PreauthRemainingReleaseInput, token, role string) (AuthorizationRelease, error) {
	var out AuthorizationRelease
	if len(s.indexKey) != 32 {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	var id int64
	var status string
	if e = tx.QueryRow(ctx, `SELECT id,status FROM orders WHERE merchant_id=$1 AND trade_no=$2 FOR UPDATE`, mid, trade).Scan(&id, &status); e != nil {
		return out, ErrNotFound
	}
	a, e := scanPreauthorization(tx.QueryRow(ctx, "SELECT "+preauthorizationColumns+" FROM payment_authorizations a JOIN orders o ON o.id=a.order_id WHERE a.order_id=$1 FOR UPDATE OF a", id))
	if e != nil {
		return out, e
	}
	if status == "paid" || a.CaptureState == "succeeded" {
		return out, ErrState
	}
	out, e = s.readAuthorizationReleaseTx(ctx, tx, id)
	newParent := false
	if errors.Is(e, ErrNotFound) {
		if a.ReleaseState != "none" {
			return out, ErrState
		}
		var orphan bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM authorization_remaining_sources WHERE order_id=$1) OR EXISTS(SELECT 1 FROM authorization_remaining_claims WHERE order_id=$1) OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='authorization-remaining-sources' AND changes->>'order_id'=$1::bigint::text)`, id).Scan(&orphan); e != nil {
			return out, e
		}
		if orphan {
			return out, ErrConflict
		}
		newParent = true
		out = AuthorizationRelease{OrderID: id, TradeNo: trade, ChannelID: a.ChannelID, RequestNo: fmt.Sprintf("UA%d", id), AuthNo: a.AuthNo, Amount: a.Amount, State: "checking"}
		e = nil
	}
	if e != nil {
		return out, e
	}
	if out.Sent || out.State != "checking" && out.State != "reversal_pending" {
		return out, ErrState
	}
	if out.SourceRevision != in.ExpectedSourceRevision {
		return out, ErrConflict
	}
	q, e := s.observationRow(ctx, tx, "id=$1", in.ExpectedObservationID)
	if e != nil || q.MerchantID != mid || q.TradeNo != trade || q.original.actorRole != role || !hmac.Equal(q.original.sessionHash, lookupSessionHash(token)) || len(token) != 43 || s.observationAlive(ctx, tx, q) != nil || s.observationAuditValid(ctx, tx, q) != nil {
		return out, ErrPreauthObservation
	}
	guard, _ := ctx.Value(fundsActionGuardKey{}).(FundsActionGuard)
	if guard == nil {
		return out, ErrInvalid
	}
	if e = guard(ctx, tx, "authorization-release-remaining", fmt.Sprintf("%s:%d:%s", trade, in.ExpectedSourceRevision, q.ID)); e != nil {
		return out, e
	}
	// Confirmation is consumed against the same pre-intent snapshot shown by
	// the preview. Inserting the parent first would change that digest.
	if newParent {
		if _, e = tx.Exec(ctx, `INSERT INTO payment_authorization_releases(order_id,channel_id,request_no,auth_no,amount)VALUES($1,$2,$3,$4,$5)`, id, a.ChannelID, out.RequestNo, a.AuthNo, a.Amount); e != nil {
			return out, e
		}
	}
	var v preauthRemainingSource
	if e = tx.QueryRow(ctx, `SELECT nextval('authorization_remaining_sources_id_seq'),clock_timestamp(),pg_current_xact_id()::text`).Scan(&v.ID, &v.CreatedAt, &v.TransactionID); e != nil {
		return out, e
	}
	v.Terms, e = preauthRemainingTerms(q, in, v.CreatedAt)
	if e != nil {
		return out, e
	}
	if s.observationAlive(ctx, tx, q) != nil {
		return out, ErrPreauthObservation
	}
	v.OrderID, v.Revision, v.ObservationID, v.observation = id, in.ExpectedSourceRevision+1, q.ID, q
	v.PreviousID, v.PreviousHash = out.remainingSourceID, out.remainingSourceHash
	v.Proof = s.remainingSourceProof(v)
	raw, e := json.Marshal(v.Terms)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO authorization_remaining_sources(id,order_id,revision,previous_id,previous_hash,observation_id,terms,created_at,transaction_id,proof_hash)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::xid8,$10)`, v.ID, id, v.Revision, v.PreviousID, v.PreviousHash, q.ID, raw, v.CreatedAt, v.TransactionID, v.Proof); e != nil {
		return out, e
	}
	if e = remainingAudit(ctx, tx, v, "authorize", v.Proof); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `UPDATE payment_authorization_releases SET remaining_source_id=$2,remaining_source_revision=$3,remaining_source_hash=$4,state='checking',updated_at=now() WHERE order_id=$1`, id, v.ID, v.Revision, v.Proof); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `UPDATE payment_authorizations SET release_state='checking',updated_at=now() WHERE order_id=$1`, id); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload,run_at)VALUES($1,'authorization.release.reconcile',jsonb_build_object('trade_no',$2::text),now()+interval '30 seconds')ON CONFLICT(event_key)DO NOTHING`, "authorization-release:"+trade, trade); e != nil {
		return out, e
	}
	out, e = s.readAuthorizationReleaseTx(ctx, tx, id)
	if e != nil {
		return out, e
	}
	if s.observationAlive(ctx, tx, q) != nil {
		return out, ErrPreauthObservation
	}
	e = tx.Commit(ctx)
	return out, e
}

func (s *Store) markPreauthRemainingSent(ctx context.Context, trade string, revision int64, check func(context.Context) error) (AuthorizationRelease, error) {
	var out AuthorizationRelease
	if check == nil {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	var id int64
	var status, capture, release string
	if e = tx.QueryRow(ctx, `SELECT id,status FROM orders WHERE trade_no=$1 FOR UPDATE`, trade).Scan(&id, &status); e != nil {
		return out, e
	}
	if e = tx.QueryRow(ctx, `SELECT capture_state,release_state FROM payment_authorizations WHERE order_id=$1 FOR UPDATE`, id).Scan(&capture, &release); e != nil {
		return out, e
	}
	out, e = s.readAuthorizationReleaseTx(ctx, tx, id)
	if e != nil {
		return out, e
	}
	if out.SourceRevision != revision {
		return out, ErrConflict
	}
	if revision <= 0 || out.Sent || out.State != "checking" || release != "checking" || status == "paid" || capture != "none" && capture != "failed" {
		return out, ErrState
	}
	// Read the raw original amount for historical source authentication.
	raw := out
	raw.Amount = out.OriginalAmount
	all, e := s.remainingSources(ctx, tx, raw)
	if e != nil {
		return out, e
	}
	v := all[len(all)-1]
	if s.observationAlive(ctx, tx, v.observation) != nil || check(ctx) != nil {
		return out, ErrPreauthObservation
	}
	c := preauthRemainingClaim{OrderID: id, SourceID: v.ID, Revision: v.Revision}
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp(),pg_current_xact_id()::text`).Scan(&c.CreatedAt, &c.TransactionID); e != nil {
		return out, e
	}
	if !c.CreatedAt.Before(v.Terms.SendBefore) {
		return out, ErrState
	}
	c.Proof = s.remainingClaimProof(c, v)
	if _, e = tx.Exec(ctx, `INSERT INTO authorization_remaining_claims(order_id,source_id,source_revision,created_at,transaction_id,proof_hash)VALUES($1,$2,$3,$4,$5::xid8,$6)`, id, c.SourceID, c.Revision, c.CreatedAt, c.TransactionID, c.Proof); e != nil {
		return out, e
	}
	if e = remainingAudit(ctx, tx, v, "claim", c.Proof); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `UPDATE payment_authorization_releases SET sent=true,state='processing',updated_at=now() WHERE order_id=$1`, id); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `UPDATE payment_authorizations SET release_state='processing',updated_at=now() WHERE order_id=$1`, id); e != nil {
		return out, e
	}
	if s.observationAlive(ctx, tx, v.observation) != nil || check(ctx) != nil {
		return out, ErrPreauthObservation
	}
	out, e = s.readAuthorizationReleaseTx(ctx, tx, id)
	if e != nil {
		return out, e
	}
	e = tx.Commit(ctx)
	return out, e
}
