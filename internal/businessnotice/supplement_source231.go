package businessnotice

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/merchantnotice"
	"goepay/internal/noticesource"
)

type supplementSource231 = noticesource.Source

func (s *Store) CaptureSupplement231(ctx context.Context, tx pgx.Tx, source noticesource.Source) error {
	if !s.available() || tx == nil || (source.Facts.Kind != "complain" && source.Facts.Kind != "channellimit") {
		return ErrInvalid
	}
	if e := noticesource.Verify(ctx, tx, source); e != nil {
		return e
	}
	var current string
	if e := tx.QueryRow(ctx, "SELECT pg_current_xact_id()::text").Scan(&current); e != nil {
		return e
	}
	if current != source.Transaction {
		return ErrConflict
	}
	scene := source.Facts.Kind
	if scene == "complain" {
		scene = "complain_all"
	}
	changes, _ := json.Marshal(map[string]string{"source_id": fmt.Sprint(source.ID)})
	var audit int64
	e := tx.QueryRow(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes,business_notice_scene,business_notice_transaction) VALUES('system',0,'notification-source231','notification-sources231',$1,'create',$2,$3,pg_current_xact_id()) RETURNING id`, source.ID, changes, scene).Scan(&audit)
	if e != nil {
		return e
	}
	params := source.Facts.Params
	if scene == "complain_all" {
		params = map[string]string{"id": fmt.Sprint(source.Facts.ReferenceID), "type": source.Facts.Params["type"], "trade_no": source.Facts.Params["trade_no"], "title": source.Facts.Params["title"]}
	}
	return s.appendEvent(ctx, tx, eventRecord{MerchantID: source.Facts.MerchantID, AuditID: audit, Scene: scene, Transaction: source.Transaction}, eventData{Supplement231: &source, Input: merchantnotice.Input{Params: params}})
}
func (s *Store) verifySupplement231(ctx context.Context, tx pgx.Tx, r eventRecord, d eventData) (map[string]string, error) {
	p := d.Supplement231
	if p == nil || r.RequestEventID != 0 || r.AuditID <= 0 || d.Audit != nil || d.Request != nil || p.Facts.MerchantID != r.MerchantID || p.Transaction != r.Transaction {
		return nil, ErrConflict
	}
	if e := noticesource.Verify(ctx, tx, *p); e != nil {
		return nil, ErrConflict
	}
	expectedScene := p.Facts.Kind
	if expectedScene == "complain" {
		expectedScene = "complain_all"
	}
	if expectedScene != r.Scene {
		return nil, ErrConflict
	}
	var valid bool
	e := tx.QueryRow(ctx, `SELECT count(*)=1 FROM backoffice_audit WHERE id=$1 AND actor_role='system' AND actor_id=0 AND actor_name='notification-source231' AND resource='notification-sources231' AND resource_id=$2 AND operation='create' AND business_notice_scene=$3 AND business_notice_transaction=$4::xid8 AND changes=jsonb_build_object('source_id',$2::bigint::text)`, r.AuditID, p.ID, r.Scene, r.Transaction).Scan(&valid)
	if e != nil {
		return nil, e
	}
	if !valid {
		return nil, ErrConflict
	}
	if r.Scene == "complain_all" {
		return map[string]string{"id": fmt.Sprint(p.Facts.ReferenceID), "type": p.Facts.Params["type"], "trade_no": p.Facts.Params["trade_no"], "title": p.Facts.Params["title"]}, nil
	}
	return p.Facts.Params, nil
}
