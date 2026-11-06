// Package refundevent owns authenticated platform refund event subscriptions
// and once-only outbound deliveries. It never calls an upstream refund API.
package refundevent

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/platform/jobs"
)

var ErrSource = errors.New("refund event source cannot be confirmed")
var ErrConflict = errors.New("refund event subscription changed")
var ErrInvalid = errors.New("invalid refund event subscription")

const JobKind = "merchant.refund-event"
const NoticeJobKind = "merchant.refund-event-closed-notice"
const ComplaintJobKind = "merchant.complaint-event"

type Store struct {
	pool *pgxpool.Pool
	key  []byte
}

func New(pool *pgxpool.Pool, key []byte) *Store {
	return &Store{pool: pool, key: append([]byte(nil), key...)}
}

func (s *Store) sign(domain string, raw []byte) []byte {
	h := hmac.New(sha256.New, s.key)
	h.Write([]byte("goepay/refund-event201/" + domain + "\x00"))
	h.Write(raw)
	return h.Sum(nil)
}
func encode(value any) ([]byte, error) {
	var out bytes.Buffer
	e := json.NewEncoder(&out)
	e.SetEscapeHTML(false)
	if err := e.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out.Bytes(), []byte{'\n'}), nil
}
func (s *Store) seal(domain string, value any) ([]byte, []byte, error) {
	if len(s.key) != 32 {
		return nil, nil, ErrSource
	}
	raw, err := encode(value)
	if err != nil {
		return nil, nil, err
	}
	return raw, s.sign(domain, raw), nil
}
func (s *Store) open(domain string, raw, mac []byte, value any) error {
	if len(s.key) != 32 || len(mac) != 32 || !hmac.Equal(s.sign(domain, raw), mac) || json.Unmarshal(raw, value) != nil {
		return ErrSource
	}
	return nil
}
func text(raw string, limit int) bool {
	return utf8.ValidString(raw) && len(raw) <= limit && strings.IndexFunc(raw, unicode.IsControl) < 0
}
func eventKey(id int64) string  { return fmt.Sprintf("merchant-refund-event201:%d", id) }
func noticeKey(id int64) string { return fmt.Sprintf("merchant-refund-event-closed201:%d", id) }
func complaintEventKey(id int64) string {
	return fmt.Sprintf("merchant-complaint-event201:%d", id)
}
func jobPayload(id int64) []byte {
	raw, _ := json.Marshal(map[string]string{"event_id": fmt.Sprint(id)})
	return raw
}

// Ready never interprets a partially installed or removed migration as a fresh
// installation. Older databases without any 201 source remain historical.
func Ready(ctx context.Context, tx pgx.Tx) (bool, error) {
	var origins, events, heads, revisions, receipts bool
	err := tx.QueryRow(ctx, `SELECT to_regclass(current_schema()||'.merchant_refund_origins201') IS NOT NULL,to_regclass(current_schema()||'.merchant_refund_events201') IS NOT NULL,to_regclass(current_schema()||'.merchant_event_subscription_heads201') IS NOT NULL,to_regclass(current_schema()||'.merchant_event_subscription_revisions201') IS NOT NULL,to_regclass(current_schema()||'.schema_migrations') IS NOT NULL`).Scan(&origins, &events, &heads, &revisions, &receipts)
	if err != nil {
		return false, err
	}
	if origins && events && heads && revisions {
		return true, nil
	}
	if origins || events || heads || revisions {
		return false, ErrSource
	}
	if receipts {
		var installed bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='000113_merchant_refund_events201.up.sql')`).Scan(&installed); err != nil {
			return false, err
		}
		if installed {
			return false, ErrSource
		}
	}
	return false, nil
}

func lockMerchant(ctx context.Context, tx pgx.Tx, mid int64) error {
	var id int64
	if err := tx.QueryRow(ctx, `SELECT id FROM merchants WHERE id=$1 FOR UPDATE`, mid).Scan(&id); err != nil {
		return err
	}
	return nil
}

func checkJob(ctx context.Context, tx pgx.Tx, j jobs.Job, kind string, id int64) error {
	key := eventKey(id)
	if kind == NoticeJobKind {
		key = noticeKey(id)
	} else if kind == ComplaintJobKind {
		key = complaintEventKey(id)
	}
	if j.ID <= 0 || j.Token == "" || j.Kind != kind || j.EventKey != key || j.Attempts <= 0 {
		return ErrSource
	}
	var supplied map[string]string
	if json.Unmarshal(j.Payload, &supplied) != nil || len(supplied) != 1 || supplied["event_id"] != fmt.Sprint(id) {
		return ErrSource
	}
	var live bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE id=$1 AND kind=$2 AND event_key=$3 AND payload=$4::jsonb AND state='running' AND lease_token=$5 AND attempts=$6 AND lease_until>clock_timestamp())`, j.ID, kind, key, jobPayload(id), j.Token, j.Attempts).Scan(&live)
	if err != nil {
		return err
	}
	if !live {
		return jobs.ErrLeaseLost
	}
	return nil
}

func audit(ctx context.Context, tx pgx.Tx, mid int64, operation string, proof []byte) error {
	_, err := tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('system',0,'refund-event201','merchant-refund-events201',$1,$2,jsonb_build_object('proof',$3::text))`, mid, operation, hex.EncodeToString(proof))
	return err
}

func deliveryAudit(ctx context.Context, tx pgx.Tx, mid, eventID, claimID int64, operation string, proof []byte) error {
	_, err := tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('system',0,'refund-event201','merchant-refund-events201',$1,$2,jsonb_build_object('proof',$3::text,'event_id',$4::text,'claim_id',$5::text))`, mid, operation, hex.EncodeToString(proof), fmt.Sprint(eventID), fmt.Sprint(claimID))
	return err
}

func deliveryWitness(ctx context.Context, tx pgx.Tx, mid, eventID, claimID int64, operation string, proof []byte) error {
	var valid bool
	err := tx.QueryRow(ctx, `SELECT count(*)=1 FROM backoffice_audit WHERE resource='merchant-refund-events201' AND resource_id=$1 AND operation=$2 AND actor_role='system' AND actor_id=0 AND actor_name='refund-event201' AND changes=jsonb_build_object('proof',$3::text,'event_id',$4::text,'claim_id',$5::text)`, mid, operation, hex.EncodeToString(proof), fmt.Sprint(eventID), fmt.Sprint(claimID)).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrSource
	}
	return nil
}

func hasDeliveryWitness(ctx context.Context, tx pgx.Tx, mid, eventID, claimID int64, operation string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='merchant-refund-events201' AND resource_id=$1 AND operation=$2 AND changes->>'event_id'=$3 AND ($4::bigint=0 OR changes->>'claim_id'=$4::text))`, mid, operation, fmt.Sprint(eventID), claimID).Scan(&exists)
	return exists, err
}
