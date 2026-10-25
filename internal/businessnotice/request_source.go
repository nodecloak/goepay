package businessnotice

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/ledger"
	"goepay/internal/merchantnotice"
)

func (s *Store) readRequestSource(ctx context.Context, tx pgx.Tx, id int64) (requestSource, error) {
	var a requestSource
	if s.requests == nil {
		return a, ErrState
	}
	err := tx.QueryRow(ctx, `SELECT id,request_id,actor,created_at,business_notice_transaction::text FROM ledger_request_events WHERE id=$1 AND status='pending' AND business_notice_transaction IS NOT NULL`, id).Scan(&a.ID, &a.RequestID, &a.Actor, &a.At, &a.Transaction)
	if err != nil {
		return a, err
	}
	a.At = a.At.UTC()
	a.Facts, err = s.requests.BusinessNoticeRequestFacts(ctx, tx, a.RequestID)
	if err != nil {
		return a, err
	}
	if a.Facts.ID != a.RequestID || a.Facts.MerchantID <= 0 || a.Actor != fmt.Sprintf("merchant:%d", a.Facts.MerchantID) || len(a.Facts.OriginalHash) != 32 || a.Transaction != a.Facts.Transaction {
		return a, ErrConflict
	}
	return a, nil
}
func (s *Store) CaptureRequest(ctx context.Context, tx pgx.Tx, id int64) error {
	if !s.available() || tx == nil || id <= 0 {
		return ErrInvalid
	}
	a, err := s.readRequestSource(ctx, tx, id)
	if err != nil {
		return err
	}
	var current string
	if err = tx.QueryRow(ctx, `SELECT pg_current_xact_id()::text`).Scan(&current); err != nil {
		return err
	}
	if current != a.Transaction {
		return ErrConflict
	}
	params := map[string]string{"uid": fmt.Sprint(a.Facts.MerchantID)}
	scene := ""
	switch a.Facts.Kind {
	case "internal-transfers":
		scene = "internal_transfer"
		params["touid"] = fmt.Sprint(a.Facts.RecipientID)
		params["money"] = ledger.Format(a.Facts.Gross)
	case "settlements":
		scene = "apply"
		params["type"] = a.Facts.DisplayType
		params["realmoney"] = ledger.Format(a.Facts.Net)
	default:
		return ErrState
	}
	return s.appendEvent(ctx, tx, eventRecord{MerchantID: a.Facts.MerchantID, RequestEventID: a.ID, Scene: scene, Transaction: a.Transaction}, eventData{Request: &a, Input: merchantnotice.Input{Params: params}})
}
