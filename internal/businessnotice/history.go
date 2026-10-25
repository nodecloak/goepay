package businessnotice

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

type EventView struct {
	ID         int64     `json:"id,string"`
	MerchantID int64     `json:"merchant_id,string"`
	SourceID   int64     `json:"source_id,string"`
	JobID      int64     `json:"job_id,string"`
	Scene      string    `json:"scene"`
	Mode       string    `json:"mode"`
	State      string    `json:"state"`
	Transport  string    `json:"transport"`
	At         time.Time `json:"at"`
}

func (s *Store) Events(ctx context.Context, before int64, limit int) ([]EventView, error) {
	if !s.available() || ctx == nil || before < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id FROM admin_business_notice_events WHERE ($1::bigint=0 OR id<$1) ORDER BY id DESC LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	out := make([]EventView, 0, len(ids))
	for _, id := range ids {
		r, _, err := s.readEvent(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		kind, err := ModeFor(r.Scene, r.Mode)
		if err != nil {
			return nil, ErrConflict
		}
		state := "not_scheduled"
		if r.JobID > 0 {
			state = "queued"
		}
		if _, status, claimErr := s.readClaim(ctx, tx, r); claimErr == nil {
			state = status
		} else if !errors.Is(claimErr, ErrNotFound) {
			return nil, claimErr
		}
		source := r.AuditID
		if source == 0 {
			source = r.RequestEventID
		}
		out = append(out, EventView{ID: r.ID, MerchantID: r.MerchantID, SourceID: source, JobID: r.JobID, Scene: r.Scene, Mode: r.Mode, Transport: string(kind), State: state, At: r.At})
	}
	return out, tx.Commit(ctx)
}
