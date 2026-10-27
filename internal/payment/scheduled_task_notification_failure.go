package payment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/mail"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"goepay/internal/platform/jobs"
)

type scheduledNotificationFailure struct {
	ID, JobID, OrderID, MerchantID, PreviousID int64
	Attempt, Count                             int
	TokenHash, PreviousHash, Proof             []byte
	Kind                                       string
	At                                         time.Time
}
type scheduledNotificationStop struct {
	ID, MerchantID, Revision, TriggerFailureID, RiskRecordID int64
	FailureIDs                                               []int64
	SiteName                                                 string
	MailState                                                string
	MailCipher                                               []byte
	At                                                       time.Time
}
type scheduledNotificationSettings struct {
	Enabled, Notice bool
	Count           int
}

func (s *Store) verifyScheduledNotificationBudget(ctx context.Context, tx pgx.Tx, j jobs.Job) error {
	if j.Kind != "payment.notify" {
		return nil
	}
	var automatic bool
	if e := tx.QueryRow(ctx, `SELECT kind='payment.notify' AND event_key='notify:'||(payload->>'trade_no') AND NOT EXISTS(SELECT 1 FROM notification_resend_requests WHERE job_id=jobs.id) AND NOT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='jobs' AND resource_id=jobs.id AND operation='retry' AND actor_role='admin') FROM jobs WHERE id=$1`, j.ID).Scan(&automatic); e != nil {
		return e
	}
	if !automatic {
		return nil
	}
	head, e := s.scheduledNotificationHead(ctx, tx, j.ID)
	if e != nil {
		return e
	}
	if head.Count >= 5 {
		return ErrState
	}
	return nil
}
func ValidateScheduledNotificationSetting(key, value string) error {
	switch key {
	case "auto_check_notify", "check_notify_notice":
		if value != "0" && value != "1" {
			return ErrInvalid
		}
	case "check_notify_count":
		n, e := strconv.Atoi(value)
		if e != nil || n < 0 || n > 1000 || strconv.Itoa(n) != value {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
func scheduledNotificationSettingsTx(ctx context.Context, tx pgx.Tx) (scheduledNotificationSettings, error) {
	var out scheduledNotificationSettings
	rows, e := tx.Query(ctx, `SELECT body->>'key',body->'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=ANY($1::text[]) ORDER BY body->>'key',id FOR SHARE`, []string{"auto_check_notify", "check_notify_count", "check_notify_notice"})
	if e != nil {
		return out, e
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var key, value string
		var raw []byte
		if e = rows.Scan(&key, &raw); e != nil {
			return out, e
		}
		if seen[key] || json.Unmarshal(raw, &value) != nil || ValidateScheduledNotificationSetting(key, value) != nil {
			return out, ErrState
		}
		seen[key] = true
		switch key {
		case "auto_check_notify":
			out.Enabled = value == "1"
		case "check_notify_notice":
			out.Notice = value == "1"
		case "check_notify_count":
			out.Count, _ = strconv.Atoi(value)
		}
	}
	return out, rows.Err()
}
func (s *Store) scheduledNotificationFailureProof(f scheduledNotificationFailure) ([]byte, error) {
	return s.referralDigest("scheduler171/notification-failure", []any{f.ID, f.JobID, f.OrderID, f.MerchantID, f.Attempt, f.TokenHash, f.Kind, f.PreviousID, f.PreviousHash, f.Count, f.At.UTC().Format(time.RFC3339Nano)})
}
func (s *Store) readScheduledNotificationFailure(ctx context.Context, q referralReader, id int64) (scheduledNotificationFailure, error) {
	var f scheduledNotificationFailure
	e := q.QueryRow(ctx, `SELECT id,job_id,order_id,merchant_id,attempt,token_hash,delivery_kind,COALESCE(previous_id,0),previous_hash,failure_count,observed_at,proof_hash FROM scheduled_notification_failures WHERE id=$1`, id).Scan(&f.ID, &f.JobID, &f.OrderID, &f.MerchantID, &f.Attempt, &f.TokenHash, &f.Kind, &f.PreviousID, &f.PreviousHash, &f.Count, &f.At, &f.Proof)
	if e != nil {
		return f, ErrConflict
	}
	f.At = f.At.UTC()
	proof, e := s.scheduledNotificationFailureProof(f)
	if e != nil || !hmac.Equal(proof, f.Proof) || f.Count < 1 || f.Count > 5 || len(f.TokenHash) != 32 || f.PreviousID >= f.ID || f.PreviousID < 0 {
		return f, ErrConflict
	}
	if f.PreviousID == 0 {
		if f.Count != 1 || f.PreviousHash != nil {
			return f, ErrConflict
		}
	} else {
		previous, e := s.readScheduledNotificationFailure(ctx, q, f.PreviousID)
		if e != nil || previous.JobID != f.JobID || previous.OrderID != f.OrderID || previous.MerchantID != f.MerchantID || previous.Count+1 != f.Count || !hmac.Equal(previous.Proof, f.PreviousHash) || previous.At.After(f.At) {
			return f, ErrConflict
		}
	}
	return f, nil
}
func (s *Store) scheduledNotificationHead(ctx context.Context, q referralReader, job int64) (scheduledNotificationFailure, error) {
	var id int64
	var out scheduledNotificationFailure
	if e := q.QueryRow(ctx, `SELECT COALESCE(scheduled_notification_failure_head,0)FROM jobs WHERE id=$1`, job).Scan(&id); e != nil {
		return out, e
	}
	var latest, origin int64
	var originProof string
	if e := q.QueryRow(ctx, `SELECT COALESCE((SELECT id FROM scheduled_notification_failures WHERE job_id=$1 ORDER BY id DESC LIMIT 1),0),COALESCE((SELECT resource_id FROM backoffice_audit WHERE resource='scheduled-notification-failures' AND actor_role='system' AND actor_id=0 AND actor_name='scheduled-notify' AND operation='observed' AND changes->>'job_id'=$2 ORDER BY resource_id DESC LIMIT 1),0),COALESCE((SELECT changes->>'failure_hash' FROM backoffice_audit WHERE resource='scheduled-notification-failures' AND actor_role='system' AND actor_id=0 AND actor_name='scheduled-notify' AND operation='observed' AND changes->>'job_id'=$2 ORDER BY resource_id DESC LIMIT 1),'')`, job, fmt.Sprint(job)).Scan(&latest, &origin, &originProof); e != nil {
		return out, e
	}
	if id != latest || latest != origin {
		return out, ErrConflict
	}
	if id == 0 {
		var orphan bool
		if e := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM scheduled_notification_failures WHERE job_id=$1)`, job).Scan(&orphan); e != nil {
			return out, e
		}
		if orphan {
			return out, ErrConflict
		}
		return out, nil
	}
	f, e := s.readScheduledNotificationFailure(ctx, q, id)
	if e != nil || f.JobID != job || originProof != hex.EncodeToString(f.Proof) {
		return out, ErrConflict
	}
	return f, nil
}
func (s *Store) ObserveNotificationFailure(ctx context.Context, tx pgx.Tx, j jobs.Job, kind string, originalDelay time.Duration) (time.Duration, bool, error) {
	if j.Kind != "payment.notify" || (kind != "ack" && kind != "transport" && kind != "read") {
		return 0, false, ErrInvalid
	}
	ready, e := scheduledAdmissionSchema(ctx, tx)
	if e != nil || !ready {
		return originalDelay, false, e
	}
	p, configured, e := s.scheduledPolicy(ctx, tx, "notify", false)
	if e != nil || !configured {
		return originalDelay, false, e
	}
	var explicit bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM notification_resend_requests WHERE job_id=$1) OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='jobs' AND resource_id=$1 AND operation='retry' AND actor_role='admin')`, j.ID).Scan(&explicit); e != nil {
		return 0, false, e
	}
	if explicit {
		return originalDelay, false, nil
	}
	if j.Attempts > 1 {
		if e = s.verifyScheduledJobClaimTx(ctx, tx, j); e != nil {
			return 0, false, e
		}
	}
	var payload struct {
		TradeNo    string `json:"trade_no"`
		MerchantID *int64 `json:"merchant_id"`
	}
	if decodeScheduledJSON(j.Payload, &payload) != nil || payload.TradeNo == "" || j.EventKey != "notify:"+payload.TradeNo {
		return 0, false, ErrConflict
	}
	var orderID, merchantID int64
	if e = tx.QueryRow(ctx, `SELECT id,merchant_id FROM orders WHERE trade_no=$1 AND status='paid' AND notify_url<>''`, payload.TradeNo).Scan(&orderID, &merchantID); e != nil {
		return 0, false, ErrState
	}
	if payload.MerchantID != nil && *payload.MerchantID != merchantID {
		return 0, false, ErrConflict
	}
	previous, e := s.scheduledNotificationHead(ctx, tx, j.ID)
	if e != nil {
		return 0, false, e
	}
	if previous.Count >= 5 {
		return 0, false, ErrState
	}
	now, e := scheduledDBNow(ctx, tx)
	if e != nil {
		return 0, false, e
	}
	token := sha256.Sum256([]byte(j.Token))
	f := scheduledNotificationFailure{JobID: j.ID, OrderID: orderID, MerchantID: merchantID, Attempt: j.Attempts, Count: previous.Count + 1, PreviousID: previous.ID, PreviousHash: previous.Proof, TokenHash: token[:], Kind: kind, At: now}
	if e = tx.QueryRow(ctx, `SELECT nextval('scheduled_notification_failures_id_seq')`).Scan(&f.ID); e != nil {
		return 0, false, e
	}
	f.Proof, e = s.scheduledNotificationFailureProof(f)
	if e != nil {
		return 0, false, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO scheduled_notification_failures(id,job_id,order_id,merchant_id,attempt,token_hash,delivery_kind,previous_id,previous_hash,failure_count,observed_at,proof_hash)VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8::bigint,0),$9,$10,$11,$12)`, f.ID, f.JobID, f.OrderID, f.MerchantID, f.Attempt, f.TokenHash, f.Kind, f.PreviousID, f.PreviousHash, f.Count, f.At, f.Proof); e != nil {
		return 0, false, e
	}
	if _, e = tx.Exec(ctx, `UPDATE jobs SET scheduled_notification_failure_head=$2 WHERE id=$1`, j.ID, f.ID); e != nil {
		return 0, false, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'scheduled-notify','scheduled-notification-failures',$1,'observed',jsonb_build_object('job_id',$2::text,'failure_hash',$3::text))`, f.ID, fmt.Sprint(j.ID), hex.EncodeToString(f.Proof)); e != nil {
		return 0, false, e
	}
	// Source Pro performs this risk action on an automatic retry, not on the
	// independent first delivery. No proof/admission failure enters this method.
	if j.Attempts > 1 && p.Enabled {
		settings, e := scheduledNotificationSettingsTx(ctx, tx)
		if e != nil {
			return 0, false, e
		}
		if settings.Enabled && settings.Count > 0 {
			if e = s.closePaymentAfterNotificationFailures(ctx, tx, f, settings); e != nil {
				return 0, false, e
			}
		}
	}
	delays := []time.Duration{time.Minute, 9 * time.Minute, 40 * time.Minute, time.Hour, 0}
	return delays[f.Count-1], f.Count >= 5, nil
}
func (s *Store) closePaymentAfterNotificationFailures(ctx context.Context, tx pgx.Tx, trigger scheduledNotificationFailure, settings scheduledNotificationSettings) error {
	var pay int
	var revision int64
	var profile []byte
	if e := tx.QueryRow(ctx, `SELECT pay,config_revision,profile FROM merchants WHERE id=$1 FOR UPDATE`, trigger.MerchantID).Scan(&pay, &revision, &profile); e != nil {
		return e
	}
	if pay != 1 {
		return nil
	}
	if revision == math.MaxInt64 {
		return ErrState
	}
	rows, e := tx.Query(ctx, `SELECT o.id,COALESCE(j.id,0),COALESCE(j.attempts,0),COALESCE(j.state,'') FROM orders o LEFT JOIN jobs j ON j.kind='payment.notify' AND j.event_key='notify:'||o.trade_no WHERE o.merchant_id=$1 AND o.status='paid' AND o.paid_at>=(date_trunc('day',clock_timestamp() AT TIME ZONE 'Asia/Shanghai')-interval '3 days')AT TIME ZONE 'Asia/Shanghai' ORDER BY o.trade_no DESC LIMIT $2`, trigger.MerchantID, settings.Count)
	if e != nil {
		return e
	}
	type candidate struct {
		order, job int64
		attempt    int
		state      string
	}
	candidates := []candidate{}
	for rows.Next() {
		var c candidate
		if e = rows.Scan(&c.order, &c.job, &c.attempt, &c.state); e != nil {
			rows.Close()
			return e
		}
		candidates = append(candidates, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if len(candidates) < settings.Count {
		return nil
	}
	ids := []int64{}
	for _, c := range candidates {
		if c.job == 0 || (c.state != "pending" && c.state != "running") {
			return nil
		}
		f, e := s.scheduledNotificationHead(ctx, tx, c.job)
		if e != nil {
			return e
		}
		if f.ID == 0 || f.OrderID != c.order || f.MerchantID != trigger.MerchantID || f.Attempt != c.attempt {
			return nil
		}
		ids = append(ids, f.ID)
	}
	stop := scheduledNotificationStop{MerchantID: trigger.MerchantID, Revision: revision, TriggerFailureID: trigger.ID, FailureIDs: ids, SiteName: "GOEPAY", MailState: "disabled"}
	var sites int
	if e = tx.QueryRow(ctx, `SELECT count(*),COALESCE(max(NULLIF(body->>'value','')),'GOEPAY')FROM backoffice_records WHERE resource='settings' AND body->>'key'='site_name'`).Scan(&sites, &stop.SiteName); e != nil {
		return e
	}
	if sites > 1 || len(stop.SiteName) > 128 {
		return ErrState
	}
	if e = tx.QueryRow(ctx, `SELECT nextval('scheduled_notification_stops_id_seq'),clock_timestamp()`).Scan(&stop.ID, &stop.At); e != nil {
		return e
	}
	stop.At = stop.At.UTC()
	if settings.Notice {
		stop.MailState = "recipient_missing"
		var fields map[string]json.RawMessage
		var email string
		if json.Unmarshal(profile, &fields) == nil && json.Unmarshal(fields["email"], &email) == nil && email != "" {
			if address, parseErr := mail.ParseAddress(email); parseErr == nil && address.Address == email && address.Name == "" && len(email) <= 254 {
				nonce := make([]byte, s.aead.NonceSize())
				if _, e = rand.Read(nonce); e != nil {
					return e
				}
				stop.MailCipher = s.aead.Seal(nonce, nonce, []byte(email), []byte(fmt.Sprintf("scheduler171/notify-stop/%d", stop.ID)))
				stop.MailState = "pending"
			}
		}
	}
	if e = tx.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('risk-records',jsonb_build_object('decision','deny','reason',$1::text,'source','notification-failure','merchant_id',$2::text)) RETURNING id`, fmt.Sprintf("连续%d个订单回调通知失败，已关闭新支付权限", settings.Count), fmt.Sprint(stop.MerchantID)).Scan(&stop.RiskRecordID); e != nil {
		return e
	}
	raw, e := json.Marshal(stop)
	if e != nil {
		return e
	}
	proof, e := s.referralDigest("scheduler171/notification-stop", stop)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO scheduled_notification_stops(id,merchant_id,original_revision,trigger_failure_id,risk_record_id,facts,created_at,proof_hash)VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,$8)`, stop.ID, stop.MerchantID, stop.Revision, stop.TriggerFailureID, stop.RiskRecordID, string(raw), stop.At, proof); e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, `UPDATE merchants SET pay=0,config_revision=config_revision+1 WHERE id=$1 AND config_revision=$2 AND pay=1`, stop.MerchantID, stop.Revision)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'scheduled-notify','scheduled-notification-stops',$1,'close-pay',jsonb_build_object('merchant_id',$2::text,'risk_record_id',$3::text,'failure_count',$4::integer,'notice_state',$5::text))`, stop.ID, fmt.Sprint(stop.MerchantID), fmt.Sprint(stop.RiskRecordID), len(stop.FailureIDs), stop.MailState); e != nil {
		return e
	}
	if stop.MailState == "pending" {
		if _, e = tx.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload)VALUES($1,'merchant.payment-disabled-notice',jsonb_build_object('stop_id',$2::text))`, fmt.Sprintf("scheduler-notify-stop:%d", stop.ID), fmt.Sprint(stop.ID)); e != nil {
			return e
		}
	}
	return nil
}

var _ jobs.NotificationRetryPolicy = (*Store)(nil)
