package merchantnotice

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type DeliveryView struct {
	Transport   Transport  `json:"transport"`
	State       string     `json:"state"`
	AttemptedAt *time.Time `json:"attempted_at,omitempty"`
}
type EventView struct {
	ID            int64          `json:"id,string"`
	Scene         string         `json:"scene"`
	TradeNo       string         `json:"trade_no,omitempty"`
	At            time.Time      `json:"at"`
	SnapshotState string         `json:"snapshot_state"`
	State         string         `json:"state"`
	JobState      string         `json:"job_state,omitempty"`
	Deliveries    []DeliveryView `json:"deliveries"`
}

func (s *Store) Events(ctx context.Context, mid, before int64, limit int, actor Actor) ([]EventView, error) {
	if !actor.valid(mid, false) {
		return nil, ErrForbidden
	}
	if !s.available() {
		return nil, ErrState
	}
	if ctx == nil || before < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, _, err = noticeMerchant(ctx, tx, mid, false); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id FROM merchant_notice_events WHERE merchant_id=$1 AND ($2::bigint=0 OR id<$2) ORDER BY id DESC LIMIT $3`, mid, before, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result := make([]EventView, 0, len(ids))
	rank := map[string]int{"not_scheduled": 0, "accepted": 1, "not_sent": 2, "queued": 3, "blocked": 4, "unknown": 5}
	for _, id := range ids {
		event, data, err := s.readEvent(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if event.MerchantID != mid {
			return nil, ErrConflict
		}
		profile, err := s.verifyEventProfile(ctx, tx, event)
		if err != nil {
			return nil, err
		}
		view := EventView{ID: id, Scene: event.Scene, At: data.Input.At, SnapshotState: event.SnapshotState, State: "not_scheduled", Deliveries: make([]DeliveryView, 0, len(data.Routes))}
		if event.OrderID > 0 {
			view.TradeNo = data.Source.TradeNo
		}
		if data.Supplement != nil && data.Input.Scene == "complain" {
			view.TradeNo = data.Input.Params["trade_no"]
		}
		if event.JobID > 0 {
			if err = tx.QueryRow(ctx, `SELECT state FROM jobs WHERE id=$1`, event.JobID).Scan(&view.JobState); err != nil {
				return nil, ErrConflict
			}
		}
		for _, kind := range data.Routes {
			claim, state, readErr := s.readClaim(ctx, tx, id, kind)
			delivery := DeliveryView{Transport: kind, State: state}
			if errors.Is(readErr, ErrNotFound) {
				var orphan bool
				if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='merchant-notice-deliveries' AND operation='attempt' AND changes->>'event_id'=$1 AND changes->>'transport'=$2)`, fmt.Sprint(id), kind).Scan(&orphan); err != nil {
					return nil, err
				}
				if orphan {
					return nil, ErrConflict
				}
				delivery.State = "queued"
				if view.JobState == "dead" {
					delivery.State = "blocked"
				}
			} else if readErr != nil {
				return nil, readErr
			} else {
				recipient := recipientFor(profile.Targets, kind)
				if kind == Wechat && data.Supplement != nil && data.Input.Scene == "wxtransfer" {
					recipient = data.Supplement.Facts.Recipient
				}
				if kind == SMS && data.SMS231 != nil {
					recipient = data.SMS231.Phone
				}
				if claim.JobID != event.JobID || claim.ProfileID != event.ProfileID || !hmac.Equal(claim.EventHash, event.Hash) || !hmac.Equal(claim.ProfileHash, event.ProfileHash) || !hmac.Equal(claim.RecipientHash, s.digest("recipient", []byte(recipient))) {
					return nil, ErrConflict
				}
				at := claim.CreatedAt
				delivery.AttemptedAt = &at
			}
			if rank[delivery.State] > rank[view.State] {
				view.State = delivery.State
			}
			view.Deliveries = append(view.Deliveries, delivery)
		}
		result = append(result, view)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
