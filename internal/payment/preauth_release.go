package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
)

type AuthorizationRelease struct {
	OrderID             int64  `json:"order_id,string"`
	TradeNo             string `json:"trade_no"`
	ChannelID           int64  `json:"channel_id,string"`
	RequestNo           string `json:"request_no"`
	AuthNo              string `json:"auth_no"`
	Amount              int64  `json:"amount,string"`
	State               string `json:"state"`
	Sent                bool   `json:"sent"`
	OriginalAmount      int64  `json:"original_amount,string,omitempty"`
	SourceRevision      int64  `json:"source_revision,string,omitempty"`
	remainingSourceID   *int64
	remainingSourceHash []byte
}

const authorizationReleaseColumns = "r.order_id,o.trade_no,r.channel_id,r.request_no,r.auth_no,r.amount,r.state,r.sent,EXISTS(SELECT 1 FROM payment_authorizations original WHERE original.order_id=r.order_id AND (original.channel_id,original.auth_no,original.amount)=(r.channel_id,r.auth_no,r.amount)) AND r.request_no=('UA'||r.order_id::text),r.remaining_source_id,r.remaining_source_revision,r.remaining_source_hash"

func scanAuthorizationRelease(row pgx.Row) (AuthorizationRelease, error) {
	var r AuthorizationRelease
	var original bool
	e := row.Scan(&r.OrderID, &r.TradeNo, &r.ChannelID, &r.RequestNo, &r.AuthNo, &r.Amount, &r.State, &r.Sent, &original, &r.remainingSourceID, &r.SourceRevision, &r.remainingSourceHash)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	if e == nil && !original {
		e = ErrConflict
	}
	return r, e
}
func (s *Store) AuthorizationRelease(ctx context.Context, merchantID int64, tradeNo string) (AuthorizationRelease, error) {
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return AuthorizationRelease{}, e
	}
	defer tx.Rollback(ctx)
	r, e := scanAuthorizationRelease(tx.QueryRow(ctx, "SELECT "+authorizationReleaseColumns+" FROM payment_authorization_releases r JOIN orders o ON o.id=r.order_id WHERE o.trade_no=$1 AND o.merchant_id=$2", tradeNo, merchantID))
	if e == nil {
		r, e = s.attachPreauthRemaining(ctx, tx, r)
	}
	if e == nil {
		e = tx.Commit(ctx)
	}
	return r, e
}
func (s *Store) BeginAuthorizationRelease(ctx context.Context, merchantID int64, tradeNo string) (AuthorizationRelease, bool, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return AuthorizationRelease{}, false, e
	}
	defer tx.Rollback(ctx)
	var id int64
	var state string
	if e = tx.QueryRow(ctx, "SELECT id,status FROM orders WHERE trade_no=$1 AND merchant_id=$2 FOR UPDATE", tradeNo, merchantID).Scan(&id, &state); e != nil {
		return AuthorizationRelease{}, false, ErrNotFound
	}
	if _, e = s.authenticatedOrderIDCharge104(ctx, tx, id); e != nil {
		return AuthorizationRelease{}, false, e
	}
	old, e := scanAuthorizationRelease(tx.QueryRow(ctx, "SELECT "+authorizationReleaseColumns+" FROM payment_authorization_releases r JOIN orders o ON o.id=r.order_id WHERE r.order_id=$1", id))
	if e == nil {
		old, e = s.attachPreauthRemaining(ctx, tx, old)
		if e != nil {
			return old, false, e
		}
		if old.SourceRevision > 0 {
			return old, false, nil
		}
		// A pending request that has provably never crossed the send boundary may
		// receive its first send after new terminal capture evidence. Unknown or
		// previously sent requests never acquire another send opportunity.
		if old.State == "reversal_pending" && !old.Sent && state != "paid" {
			if e = requireFundsAction(ctx, tx, "authorization-release", tradeNo); e != nil {
				return old, false, e
			}
			if _, e = tx.Exec(ctx, "UPDATE payment_authorizations SET release_state='checking',updated_at=now() WHERE order_id=$1 AND capture_state<>'succeeded'", id); e != nil {
				return old, false, e
			}
			tag, e := tx.Exec(ctx, "UPDATE payment_authorization_releases SET state='checking',updated_at=now() WHERE order_id=$1 AND state='reversal_pending' AND sent=false", id)
			if e != nil {
				return old, false, e
			}
			if tag.RowsAffected() != 1 {
				return old, false, ErrState
			}
			if e = tx.Commit(ctx); e != nil {
				return old, false, e
			}
			old.State = "checking"
			return old, true, nil
		}
		return old, false, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return old, false, e
	}
	a, e := scanPreauthorization(tx.QueryRow(ctx, "SELECT "+preauthorizationColumns+" FROM payment_authorizations a JOIN orders o ON o.id=a.order_id WHERE a.order_id=$1 FOR UPDATE OF a", id))
	if e != nil {
		return old, false, e
	}
	if state == "paid" || a.CaptureState == "succeeded" || a.ReleaseState != "none" {
		return old, false, ErrState
	}
	if e = requireFundsAction(ctx, tx, "authorization-release", tradeNo); e != nil {
		return old, false, e
	}
	requestNo := fmt.Sprintf("UA%d", id)
	if _, e = tx.Exec(ctx, "INSERT INTO payment_authorization_releases(order_id,channel_id,request_no,auth_no,amount) VALUES($1,$2,$3,$4,$5)", id, a.ChannelID, requestNo, a.AuthNo, a.Amount); e != nil {
		return old, false, e
	}
	if _, e = tx.Exec(ctx, "UPDATE payment_authorizations SET release_state='checking',updated_at=now() WHERE order_id=$1", id); e != nil {
		return old, false, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO jobs(event_key,kind,payload,run_at) VALUES($1,'authorization.release.reconcile',jsonb_build_object('trade_no',$2::text),now()+interval '30 seconds') ON CONFLICT(event_key) DO NOTHING", "authorization-release:"+tradeNo, tradeNo); e != nil {
		return old, false, e
	}
	r, e := scanAuthorizationRelease(tx.QueryRow(ctx, "SELECT "+authorizationReleaseColumns+" FROM payment_authorization_releases r JOIN orders o ON o.id=r.order_id WHERE r.order_id=$1", id))
	if e != nil {
		return r, false, e
	}
	e = tx.Commit(ctx)
	return r, e == nil, e
}
func (s *Store) MarkAuthorizationReleaseSent(ctx context.Context, tradeNo string, guards ...func(context.Context, pgx.Tx) error) error {
	if len(guards) > 1 || len(guards) == 1 && guards[0] == nil {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var id int64
	var status string
	if e = tx.QueryRow(ctx, "SELECT id,status FROM orders WHERE trade_no=$1 FOR UPDATE", tradeNo).Scan(&id, &status); e != nil {
		return e
	}
	var capture, release string
	if e = tx.QueryRow(ctx, "SELECT capture_state,release_state FROM payment_authorizations WHERE order_id=$1 FOR UPDATE", id).Scan(&capture, &release); e != nil {
		return e
	}
	if status == "paid" || (capture != "none" && capture != "failed") || release != "checking" {
		return ErrState
	}
	r, e := s.readAuthorizationReleaseTx(ctx, tx, id)
	if e != nil {
		return e
	}
	if r.SourceRevision > 0 {
		return ErrState
	}
	if len(guards) == 1 {
		if e = guards[0](ctx, tx); e != nil {
			return e
		}
	}
	tag, e := tx.Exec(ctx, "UPDATE payment_authorization_releases SET sent=true,state='processing',updated_at=now() WHERE order_id=$1 AND sent=false AND state='checking'", id)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrState
	}
	if _, e = tx.Exec(ctx, "UPDATE payment_authorizations SET release_state='processing',updated_at=now() WHERE order_id=$1", id); e != nil {
		return e
	}
	if len(guards) == 1 {
		if e = guards[0](ctx, tx); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) ResolveAuthorizationRelease(ctx context.Context, tradeNo, state string) error {
	if state != "unknown" && state != "reversal_pending" && state != "failed" && state != "succeeded" {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var id int64
	var orderStatus string
	if e = tx.QueryRow(ctx, "SELECT id,status FROM orders WHERE trade_no=$1 FOR UPDATE", tradeNo).Scan(&id, &orderStatus); e != nil {
		return e
	}
	if _, e = s.readAuthorizationReleaseTx(ctx, tx, id); e != nil {
		return e
	}
	var previous string
	var sent bool
	if e = tx.QueryRow(ctx, "SELECT state,sent FROM payment_authorization_releases WHERE order_id=$1 FOR UPDATE", id).Scan(&previous, &sent); e != nil {
		return e
	}
	if previous == "succeeded" {
		return tx.Commit(ctx)
	}
	if state == "succeeded" && !sent {
		return ErrState
	}
	if _, e = tx.Exec(ctx, "UPDATE payment_authorization_releases SET state=$2,updated_at=now() WHERE order_id=$1", id, state); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE payment_authorizations SET release_state=$2,updated_at=now() WHERE order_id=$1", id, state); e != nil {
		return e
	}
	if state == "succeeded" && orderStatus != "paid" {
		if _, e = tx.Exec(ctx, "UPDATE orders SET status='closed' WHERE id=$1", id); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
