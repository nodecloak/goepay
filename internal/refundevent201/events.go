package refundevent201

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type Origin struct {
	RefundID, MerchantID, OrderID, Amount int64
	ExternalNo, Entry                     string
}

func (s *Store) RecordOrigin(ctx context.Context, tx pgx.Tx, origin Origin) error {
	if origin.RefundID <= 0 || origin.MerchantID <= 0 || origin.OrderID <= 0 || origin.Amount <= 0 || (origin.Entry != "api" && origin.Entry != "platform") {
		return ErrSource
	}
	raw, mac, err := s.seal("origin", origin)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO merchant_refund_origins201(refund_id,body,mac) VALUES($1,$2,$3)`, origin.RefundID, raw, mac)
	return err
}

func (s *Store) ReadOrigin(ctx context.Context, tx pgx.Tx, expected Origin) (Origin, []byte, error) {
	var origin Origin
	var raw, mac []byte
	err := tx.QueryRow(ctx, `SELECT body,mac FROM merchant_refund_origins201 WHERE refund_id=$1`, expected.RefundID).Scan(&raw, &mac)
	if err != nil {
		return origin, nil, err
	}
	if s.open("origin", raw, mac, &origin) != nil || origin != expected {
		return origin, nil, ErrSource
	}
	return origin, mac, nil
}

// Event freezes successful financial facts, including cumulative refunded money
// at the trusted transition. Delivery never recomputes it from a later balance.
type Event struct {
	ID, RefundID, MerchantID, OrderID int64
	OriginProof, ResultProof          string
	Content                           string
}

func (s *Store) Capture(ctx context.Context, tx pgx.Tx, origin Origin, resultProof []byte, content []byte) error {
	_, proof, err := s.ReadOrigin(ctx, tx, origin)
	if err != nil {
		return err
	}
	if origin.Entry == "api" {
		return nil
	}
	if len(resultProof) != 32 || !json.Valid(content) || len(content) > 12000 {
		return ErrSource
	}
	var priorID int64
	err = tx.QueryRow(ctx, `SELECT id FROM merchant_refund_events201 WHERE refund_id=$1`, origin.RefundID).Scan(&priorID)
	if err == nil {
		return ErrSource
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var id int64
	if err = tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('merchant_refund_events201','id'))`).Scan(&id); err != nil {
		return err
	}
	event := Event{ID: id, RefundID: origin.RefundID, MerchantID: origin.MerchantID, OrderID: origin.OrderID, OriginProof: hex.EncodeToString(proof), ResultProof: hex.EncodeToString(resultProof), Content: string(content)}
	raw, mac, err := s.seal("event", event)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO merchant_refund_events201(id,refund_id,merchant_id,body,mac) OVERRIDING SYSTEM VALUE VALUES($1,$2,$3,$4,$5)`, id, origin.RefundID, origin.MerchantID, raw, mac); err != nil {
		return err
	}
	if err = audit(ctx, tx, origin.MerchantID, "capture", mac); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload) VALUES($1,$2,$3::jsonb)`, eventKey(id), JobKind, jobPayload(id))
	return err
}

func (s *Store) readEvent(ctx context.Context, tx pgx.Tx, id int64) (Event, []byte, error) {
	var event Event
	var refund, mid int64
	var raw, mac []byte
	err := tx.QueryRow(ctx, `SELECT refund_id,merchant_id,body,mac FROM merchant_refund_events201 WHERE id=$1`, id).Scan(&refund, &mid, &raw, &mac)
	if err != nil {
		return event, nil, err
	}
	if s.open("event", raw, mac, &event) != nil || event.ID != id || event.RefundID != refund || event.MerchantID != mid || !json.Valid([]byte(event.Content)) {
		return event, nil, ErrSource
	}
	var proof []byte
	if err = tx.QueryRow(ctx, `SELECT mac FROM merchant_refund_origins201 WHERE refund_id=$1`, refund).Scan(&proof); err != nil {
		return event, nil, err
	}
	if !hmac.Equal([]byte(event.OriginProof), []byte(hex.EncodeToString(proof))) {
		return event, nil, ErrSource
	}
	var witnessed bool
	if err = tx.QueryRow(ctx, `SELECT count(*)=1 FROM backoffice_audit WHERE resource='merchant-refund-events201' AND resource_id=$1 AND operation='capture' AND actor_role='system' AND actor_id=0 AND actor_name='refund-event201' AND changes=jsonb_build_object('proof',$2::text)`, mid, hex.EncodeToString(mac)).Scan(&witnessed); err != nil {
		return event, nil, err
	}
	if !witnessed {
		return event, nil, ErrSource
	}
	return event, mac, nil
}

func payloadEventID(raw []byte) (int64, error) {
	var in map[string]string
	if json.Unmarshal(raw, &in) != nil || len(in) != 1 {
		return 0, ErrSource
	}
	var id int64
	if _, err := fmt.Sscan(in["event_id"], &id); err != nil || id <= 0 || in["event_id"] != fmt.Sprint(id) {
		return 0, ErrSource
	}
	return id, nil
}

type VerifyEvent func(context.Context, pgx.Tx, Event) error
