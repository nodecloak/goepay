package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The returned evidence is for the separate console confirmation digest. It
// grants no send authority and contains no plaintext credentials.
func (s *Store) PreauthRemainingConfirmationSnapshot(ctx context.Context, q remainingEvidenceReader, mid int64, trade string, in PreauthRemainingReleaseInput) (json.RawMessage, error) {
	observed, e := s.scanObservation(q.QueryRow(ctx, "SELECT "+preauthObservationColumns+" FROM preauth_observations WHERE id=$1", in.ExpectedObservationID))
	if e != nil || observed.MerchantID != mid || observed.TradeNo != trade || s.observationOrigin(ctx, q, observed) != nil || s.observationAuditValid(ctx, q, observed) != nil {
		return nil, ErrPreauthObservation
	}
	var at time.Time
	if e = q.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&at); e != nil {
		return nil, e
	}
	terms, e := preauthRemainingTerms(observed, in, at)
	if e != nil {
		return nil, e
	}
	r, e := s.readAuthorizationReleaseTx(ctx, q, observed.OrderID)
	var head any
	if errors.Is(e, ErrNotFound) {
		if in.ExpectedSourceRevision != 0 {
			return nil, ErrConflict
		}
		head = []any{observed.OrderID, fmt.Sprintf("UA%d", observed.OrderID), 0}
	} else {
		if e != nil {
			return nil, e
		}
		if r.Sent || r.SourceRevision != in.ExpectedSourceRevision || r.State != "checking" && r.State != "reversal_pending" {
			return nil, ErrConflict
		}
		head = []any{r.OrderID, r.RequestNo, r.SourceRevision, r.remainingSourceID, r.remainingSourceHash, r.State, r.Sent}
	}
	var valid bool
	e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM orders o JOIN payment_authorizations a ON a.order_id=o.id WHERE o.id=$1 AND o.status<>'paid' AND a.capture_state<>'succeeded' AND a.release_state IN('none','checking','reversal_pending')) AND EXISTS(SELECT 1 FROM merchants m JOIN console_sessions ss ON ss.token_hash=$2 JOIN console_users u ON (u.username,u.role,u.subject_id,u.auth_version)=(ss.username,ss.role,ss.subject_id,ss.auth_version) WHERE m.id=$3 AND m.enabled AND m.config_revision=$4 AND u.enabled AND ss.expires_at>clock_timestamp() AND NOT EXISTS(SELECT 1 FROM console_session_issuers i WHERE i.token_hash=ss.token_hash))`, observed.OrderID, observed.original.sessionHash, mid, observed.merchantRevision).Scan(&valid)
	if e != nil {
		return nil, e
	}
	if !valid {
		return nil, ErrPreauthObservation
	}
	raw, e := json.Marshal([]any{terms, head, observed.sourceProof, observed.resultProof})
	return raw, e
}

var _ remainingEvidenceReader = (pgx.Tx)(nil)
