package payment

import (
	"context"
	"crypto/hmac"
	"fmt"
)

// Archive stops future admission without deleting immutable account or route
// evidence. Historical queries and refunds still restore their original version.
func (s *Store) ArchiveSubchannel(ctx context.Context, a SubchannelActor, id int64, expected string) (PaymentSubchannel, error) {
	if a.User.Role != "admin" || a.User.AdminClass != "super" {
		return PaymentSubchannel{}, ErrInvalid
	}
	return s.stopSubchannel(ctx, a, id, expected, true)
}
func (s *Store) StopSubchannel(ctx context.Context, a SubchannelActor, id int64, expected string) (PaymentSubchannel, error) {
	return s.stopSubchannel(ctx, a, id, expected, false)
}
func (s *Store) stopSubchannel(ctx context.Context, a SubchannelActor, id int64, expected string, archive bool) (PaymentSubchannel, error) {
	var out PaymentSubchannel
	if id <= 0 || len(expected) != 64 || a.Delegated {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = a.lockMerchant(ctx, tx); e != nil {
		return out, e
	}
	old, e := s.readSubchannel(ctx, tx, tx.QueryRow(ctx, `SELECT `+childColumns+` FROM payment_subchannels WHERE id=$1 AND ($2::boolean OR merchant_id=$3)`, id, a.User.Role == "admin" && a.User.AdminClass == "super", a.User.ID))
	if e != nil {
		return out, e
	}
	if !a.allows(old.MerchantID) {
		return out, ErrNotFound
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("goepay/route/channel/%d", old.ParentID)); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("goepay/route/subchannel/%d", id)); e != nil {
		return out, e
	}
	out, e = s.readSubchannel(ctx, tx, tx.QueryRow(ctx, `SELECT `+childColumns+` FROM payment_subchannels WHERE id=$1 FOR UPDATE`, id))
	if e != nil {
		return out, e
	}
	if out.ParentID != old.ParentID || out.Archived || !hmac.Equal([]byte(out.Fingerprint), []byte(expected)) {
		return out, ErrConflict
	}
	if e = a.check(ctx, tx); e != nil {
		return out, e
	}
	out.Enabled = false
	out.Archived = archive
	out.Revision++
	out.proof, e = s.childProof(out)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `UPDATE payment_subchannels SET enabled=false,archived=$4,revision=$2,proof_hash=$3,updated_at=clock_timestamp() WHERE id=$1`, id, out.Revision, out.proof, archive); e != nil {
		return out, e
	}
	operation := "payment-subchannel-stop"
	if archive {
		operation = "payment-subchannel-archive"
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'payment-subchannels',$4,$5,jsonb_build_object('revision',$6::text,'archived',$7::boolean))`, a.User.Role, a.User.ID, a.User.Username, id, operation, fmt.Sprint(out.Revision), archive); e != nil {
		return out, e
	}
	out, e = s.readSubchannel(ctx, tx, tx.QueryRow(ctx, `SELECT `+childColumns+` FROM payment_subchannels WHERE id=$1`, id))
	if e != nil {
		return out, e
	}
	if e = a.check(ctx, tx); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
