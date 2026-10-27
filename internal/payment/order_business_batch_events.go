package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"io"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type orderBusinessBatchEventFacts struct {
	State              string `json:"state"`
	Code               string `json:"code,omitempty"`
	OrderStatus        string `json:"order_status,omitempty"`
	OperationID        int64  `json:"operation_id,omitempty"`
	JobID              int64  `json:"job_id,omitempty"`
	AuthorizationEpoch int64  `json:"authorization_epoch,omitempty"`
	AuthVersion        int64  `json:"auth_version,omitempty"`
}
type orderBusinessBatchEvent struct {
	Number          int64
	Position        int
	Kind            string
	Facts           orderBusinessBatchEventFacts
	Previous, Proof []byte
	CreatedAt       time.Time
}
type OrderBusinessBatchCounts struct {
	Total     int `json:"total"`
	Processed int `json:"processed"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Unknown   int `json:"unknown"`
	Cancelled int `json:"cancelled"`
}

func strictOrderBusinessBatchJSON(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return ErrConflict
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ErrConflict
	}
	return nil
}
func batchItem185Terminal(state string) bool {
	switch state {
	case "succeeded", "unpaid", "unsupported", "failed", "queued", "cancelled":
		return true
	}
	return false
}
func (s *Store) orderBusinessBatchEventProof(r orderBusinessBatchRecord, e orderBusinessBatchEvent) ([]byte, error) {
	return s.referralDigest("order-business-batch185/event", []any{r.ID, r.SourceHash, e.Number, e.Position, e.Kind, e.Facts, e.Previous, e.CreatedAt})
}
func (s *Store) loadOrderBusinessBatchEvents(ctx context.Context, tx referralReader, r *orderBusinessBatchRecord) error {
	r.lastHash = append([]byte(nil), r.SourceHash...)
	rows, e := tx.Query(ctx, `SELECT event_no,position,event_type,facts,previous_hash,proof_hash,created_at FROM order_business_batch_events WHERE batch_id=$1 ORDER BY event_no`, r.ID)
	if e != nil {
		return e
	}
	for rows.Next() {
		var ev orderBusinessBatchEvent
		var raw []byte
		if e = rows.Scan(&ev.Number, &ev.Position, &ev.Kind, &raw, &ev.Previous, &ev.Proof, &ev.CreatedAt); e != nil {
			break
		}
		if ev.Number != r.eventCount+1 || r.eventCount >= 401 || ev.Position < 0 || ev.Position > len(r.Items) || strictOrderBusinessBatchJSON(raw, &ev.Facts) != nil || !hmac.Equal(ev.Previous, r.lastHash) {
			e = ErrConflict
			break
		}
		proof, err := s.orderBusinessBatchEventProof(*r, ev)
		if err != nil {
			e = err
			break
		}
		if !hmac.Equal(proof, ev.Proof) {
			e = ErrConflict
			break
		}
		if ev.Kind == "authorize" {
			if ev.Position != 0 || r.authorized || ev.Number != 1 || ev.Facts.State != "authorized" || ev.Facts.AuthorizationEpoch <= 0 || ev.Facts.AuthVersion <= 0 {
				e = ErrConflict
				break
			}
			r.authorized = true
			r.authorizationEpoch = ev.Facts.AuthorizationEpoch
			r.authorizationVersion = ev.Facts.AuthVersion
			r.eventCount = ev.Number
			r.lastHash = ev.Proof
			continue
		}
		if ev.Position == 0 {
			e = ErrConflict
			break
		}
		item := &r.Items[ev.Position-1]
		switch ev.Kind {
		case "claim":
			if item.State != "unsent" || ev.Facts.State != "running" {
				e = ErrConflict
			} else {
				item.State = "running"
			}
		case "result":
			if (item.State != "running" && item.State != "unknown") || (!batchItem185Terminal(ev.Facts.State) && ev.Facts.State != "unknown") {
				e = ErrConflict
			} else {
				item.State = ev.Facts.State
				item.Code = ev.Facts.Code
				item.OrderStatus = ev.Facts.OrderStatus
			}
		case "cancel":
			if item.State != "unsent" || ev.Facts.State != "cancelled" {
				e = ErrConflict
			} else {
				item.State = "cancelled"
			}
		default:
			e = ErrConflict
		}
		if e != nil {
			break
		}
		r.eventCount = ev.Number
		r.lastHash = ev.Proof
	}
	rows.Close()
	if e != nil {
		return e
	}
	if e = rows.Err(); e != nil {
		return e
	}
	var anchors int64
	var valid bool
	if e = tx.QueryRow(ctx, `SELECT count(*),COALESCE(bool_and(e.batch_id IS NOT NULL AND a.changes->>'proof_hash'=encode(e.proof_hash,'hex')),true) FROM backoffice_audit a LEFT JOIN order_business_batch_events e ON e.batch_id=a.resource_id AND a.changes->>'event_no'=e.event_no::text WHERE a.actor_role='admin' AND a.actor_id=$1 AND a.actor_name=$2 AND a.resource='order-business-batches' AND a.resource_id=$3 AND a.operation='event'`, r.ActorID, r.ActorName, r.ID).Scan(&anchors, &valid); e != nil {
		return e
	}
	if anchors != r.eventCount || !valid {
		return ErrConflict
	}
	r.State = "prepared"
	r.Counts = OrderBusinessBatchCounts{Total: len(r.Items)}
	for _, item := range r.Items {
		switch item.State {
		case "succeeded", "queued":
			r.Counts.Succeeded++
		case "unpaid", "unsupported", "failed":
			r.Counts.Failed++
		case "cancelled":
			r.Counts.Cancelled++
		case "unknown":
			r.Counts.Unknown++
		}
		if batchItem185Terminal(item.State) {
			r.Counts.Processed++
		}
	}
	if r.Counts.Processed == r.Counts.Total {
		r.State = "completed"
		if r.Counts.Cancelled == r.Counts.Total {
			r.State = "cancelled"
		}
	} else if r.Counts.Unknown > 0 {
		r.State = "attention"
	} else if r.eventCount > 0 {
		r.State = "running"
	}
	r.Fingerprint = hex.EncodeToString(r.lastHash)
	return nil
}
func (s *Store) appendOrderBusinessBatchEvent(ctx context.Context, tx pgx.Tx, r orderBusinessBatchRecord, position int, kind string, facts orderBusinessBatchEventFacts) error {
	ev := orderBusinessBatchEvent{Number: r.eventCount + 1, Position: position, Kind: kind, Facts: facts, Previous: r.lastHash}
	if e := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&ev.CreatedAt); e != nil {
		return e
	}
	proof, e := s.orderBusinessBatchEventProof(r, ev)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(facts)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO order_business_batch_events(batch_id,event_no,position,event_type,facts,previous_hash,proof_hash,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6,$7,$8)`, r.ID, ev.Number, position, kind, string(raw), ev.Previous, proof, ev.CreatedAt); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'order-business-batches',$3,'event',jsonb_build_object('event_no',$4::text,'proof_hash',encode($5::bytea,'hex'),'position',$6::int,'event_type',$7::text))`, r.ActorID, r.ActorName, r.ID, strconv.FormatInt(ev.Number, 10), proof, position, kind)
	return e
}
