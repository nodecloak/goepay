package merchantnotice

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var businessZone = time.FixedZone("UTC+8", 8*60*60)

// DailyWindow is a pure calendar calculation. Scheduling itself always reads
// PostgreSQL's clock after locking the merchant; callers cannot select a day.
func DailyWindow(at time.Time, sendHour int) (time.Time, time.Time, bool, error) {
	if at.IsZero() || sendHour < 0 || sendHour > 23 {
		return time.Time{}, time.Time{}, false, ErrInvalid
	}
	local := at.In(businessZone)
	end := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, businessZone)
	return end.AddDate(0, 0, -1), end, local.Hour() >= sendHour, nil
}

type TransferReportReader interface {
	NoticeTransferLines(context.Context, pgx.Tx, int64, time.Time, time.Time) ([]AmountLine, error)
}
type ExpiredMembership struct {
	PurchaseID, MerchantID int64
	GroupName              string
	ExpiresAt              time.Time
	SourceVersion          int
	ProofHash              []byte
}
type MembershipExpiryReader interface {
	NoticeExpiredMembership(context.Context, pgx.Tx, int64, time.Time) (*ExpiredMembership, error)
}
type ScheduledPage struct {
	LastMerchantID   int64
	Scanned, Created int
}
type timerSource struct {
	ID, MerchantID, ProfileID       int64
	Scene, BusinessKey, Transaction string
	ProfileHash, BodyHash, Hash     []byte
	At                              time.Time
}

func timerInputBytes(in Input) []byte {
	in.SiteName = ""
	raw, _ := json.Marshal(in)
	return raw
}
func (s *Store) timerProof(p timerSource) []byte {
	raw, _ := json.Marshal([]any{p.ID, p.MerchantID, p.ProfileID, p.Scene, p.BusinessKey, hex.EncodeToString(p.ProfileHash), hex.EncodeToString(p.BodyHash), p.Transaction, p.At.UTC().Format(time.RFC3339Nano)})
	return s.digest("timer-source", raw)
}
func timerAudit(p timerSource) []byte {
	raw, _ := json.Marshal(map[string]string{"source_hash": hex.EncodeToString(p.Hash), "scene": p.Scene, "business_key": p.BusinessKey})
	return raw
}
func (s *Store) appendTimer(ctx context.Context, tx pgx.Tx, profile profileRecord, in Input, key string) (bool, error) {
	p := timerSource{MerchantID: profile.MerchantID, ProfileID: profile.ID, ProfileHash: profile.ProfileHash, Scene: in.Scene, BusinessKey: key, At: in.At.UTC()}
	p.BodyHash = s.digest("timer-body", timerInputBytes(in))
	if err := tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('merchant_notice_timer_sources','id')),pg_current_xact_id()::text`).Scan(&p.ID, &p.Transaction); err != nil {
		return false, err
	}
	p.Hash = s.timerProof(p)
	var inserted int64
	err := tx.QueryRow(ctx, `INSERT INTO merchant_notice_timer_sources(id,merchant_id,profile_id,profile_hash,scene,business_key,body_hash,source_hash,source_transaction,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::xid8,$10) ON CONFLICT(merchant_id,scene,business_key) DO NOTHING RETURNING id`, p.ID, p.MerchantID, p.ProfileID, p.ProfileHash, p.Scene, p.BusinessKey, p.BodyHash, p.Hash, p.Transaction, p.At).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('system',0,'merchant-notice','merchant-notice-timers',$1,'create',$2)`, p.ID, timerAudit(p)); err != nil {
		return false, err
	}
	err = s.appendEvent(ctx, tx, eventRecord{MerchantID: p.MerchantID, TimerID: p.ID, Scene: p.Scene}, eventData{Version: 1, Timer: &p, Input: in}, true)
	return err == nil, err
}
func (s *Store) verifyTimerSource(ctx context.Context, tx pgx.Tx, r eventRecord, data eventData) error {
	if r.TimerID <= 0 || r.OrderID != 0 || r.LoginAuditID != 0 || r.SettlementID != 0 || data.Timer == nil || data.Login != nil || data.Settlement != nil || data.Source != (paidEventSource{}) {
		return ErrConflict
	}
	var p timerSource
	err := tx.QueryRow(ctx, `SELECT id,merchant_id,profile_id,profile_hash,scene,business_key,body_hash,source_hash,source_transaction::text,created_at FROM merchant_notice_timer_sources WHERE id=$1`, r.TimerID).Scan(&p.ID, &p.MerchantID, &p.ProfileID, &p.ProfileHash, &p.Scene, &p.BusinessKey, &p.BodyHash, &p.Hash, &p.Transaction, &p.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	p.At = p.At.UTC()
	a, _ := json.Marshal(p)
	b, _ := json.Marshal(data.Timer)
	if p.MerchantID != r.MerchantID || p.ProfileID != r.ProfileID || p.Scene != r.Scene || p.Transaction != r.Transaction || !p.At.Equal(data.Input.At) || !hmac.Equal(p.ProfileHash, r.ProfileHash) || !hmac.Equal(p.Hash, s.timerProof(p)) || !hmac.Equal(p.BodyHash, s.digest("timer-body", timerInputBytes(data.Input))) || !hmac.Equal(a, b) {
		return ErrConflict
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT count(*)=1 FROM backoffice_audit WHERE resource='merchant-notice-timers' AND resource_id=$1 AND operation='create' AND (actor_role,actor_id,actor_name)=('system',0,'merchant-notice') AND changes=$2::jsonb`, p.ID, timerAudit(p)).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return ErrConflict
	}
	if p.Scene == "group" {
		id, parseErr := strconv.ParseInt(data.Input.Params["purchase_id"], 10, 64)
		if parseErr != nil || id <= 0 || fmt.Sprint(id) != data.Input.Params["purchase_id"] || p.BusinessKey != "purchase:"+fmt.Sprint(id) {
			return ErrConflict
		}
		var owner int64
		var name string
		var expiry time.Time
		var proof []byte
		if err = tx.QueryRow(ctx, `SELECT p.merchant_id,p.group_name,p.expires_at,CASE WHEN p.source_version=1 THEN s.proof_hash ELSE l.row_hash END FROM membership_purchases p LEFT JOIN membership_referral_sources s ON s.purchase_id=p.id LEFT JOIN membership_referral_legacy_purchases l ON l.purchase_id=p.id WHERE p.id=$1`, id).Scan(&owner, &name, &expiry, &proof); err != nil {
			return ErrConflict
		}
		if owner != r.MerchantID || name != data.Input.Params["group"] || expiry.After(p.At) || expiry.In(businessZone).Format("2006-01-02 15:04:05") != data.Input.Params["endtime"] || hex.EncodeToString(proof) != data.Input.Params["purchase_hash"] {
			return ErrConflict
		}
	}
	return nil
}

func (s *Store) GenerateScheduledPage(ctx context.Context, afterID int64, limit, sendHour int, transfers TransferReportReader) (ScheduledPage, error) {
	out := ScheduledPage{LastMerchantID: afterID}
	if !s.available() || ctx == nil || afterID < 0 || limit < 1 || limit > 200 || sendHour < 0 || sendHour > 23 {
		return out, ErrInvalid
	}
	rows, err := s.pool.Query(ctx, `SELECT id FROM merchants WHERE enabled AND id>$1 ORDER BY id LIMIT $2`, afterID, limit)
	if err != nil {
		return out, err
	}
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return out, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for _, id := range ids {
		var n int
		for attempt := 0; attempt < 3; attempt++ {
			n, err = s.generateScheduledMerchant(ctx, id, sendHour, transfers)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || (pgErr.Code != "40001" && pgErr.Code != "40P01") {
				break
			}
		}
		if err != nil {
			return out, err
		}
		out.Created += n
		out.Scanned++
		out.LastMerchantID = id
	}
	return out, nil
}
func (s *Store) generateScheduledMerchant(ctx context.Context, mid int64, sendHour int, transfers TransferReportReader) (int, error) {
	return s.generateScheduledMerchantScope(ctx, mid, sendHour, transfers, "", "")
}

// GenerateScheduledSceneMerchant publishes one original scene for one frozen
// business day. Delivery remains owned by the existing authenticated worker.
func (s *Store) GenerateScheduledSceneMerchant(ctx context.Context, mid int64, sendHour int, transfers TransferReportReader, scene, expectedDay string) (bool, error) {
	day, err := time.Parse("2006-01-02", expectedDay)
	if !s.available() || ctx == nil || mid <= 0 || sendHour < 0 || sendHour > 23 || (scene != "daily_report" && scene != "balance" && scene != "group") || err != nil || day.Format("2006-01-02") != expectedDay {
		return false, ErrInvalid
	}
	if _, err = s.generateScheduledMerchantScope(ctx, mid, sendHour, transfers, scene, expectedDay); err != nil {
		return false, err
	}
	var recorded bool
	if scene == "group" {
		err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM merchant_notice_timer_sources WHERE merchant_id=$1 AND scene='group' AND created_at>=$2::date AT TIME ZONE 'Asia/Shanghai' AND created_at<($2::date+1)AT TIME ZONE 'Asia/Shanghai')`, mid, expectedDay).Scan(&recorded)
	} else {
		err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM merchant_notice_timer_sources WHERE merchant_id=$1 AND scene=$2 AND business_key=$3)`, mid, scene, expectedDay).Scan(&recorded)
	}
	return recorded, err
}

func (s *Store) generateScheduledMerchantScope(ctx context.Context, mid int64, sendHour int, transfers TransferReportReader, scene, expectedDay string) (int, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	active, _, err := noticeMerchant(ctx, tx, mid, true)
	if errors.Is(err, ErrNotFound) || err == nil && !active {
		return 0, tx.Commit(ctx)
	}
	if err != nil {
		return 0, err
	}
	p, data, err := s.readProfile(ctx, tx, mid)
	if errors.Is(err, ErrNotFound) {
		return 0, tx.Commit(ctx)
	}
	if err != nil {
		return 0, err
	}
	var at time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
		return 0, err
	}
	at = at.UTC()
	if expectedDay != "" && at.In(businessZone).Format("2006-01-02") != expectedDay {
		return 0, ErrState
	}
	created := 0
	allowed := map[string]bool{"group": true, "daily_report": true, "balance": true}
	if scene == "" {
		if gate, ok := transfers.(interface {
			ScheduledNoticeSceneLegacyAllowedTx(context.Context, pgx.Tx, string) (bool, error)
		}); ok {
			for _, current := range []string{"group", "daily_report", "balance"} {
				allowed[current], err = gate.ScheduledNoticeSceneLegacyAllowedTx(ctx, tx, current)
				if err != nil {
					return 0, err
				}
			}
		}
	}
	if reader, ok := transfers.(MembershipExpiryReader); ok && allowed["group"] && (scene == "" || scene == "group") {
		expired, e := reader.NoticeExpiredMembership(ctx, tx, mid, at)
		if e != nil {
			return 0, e
		}
		if expired != nil {
			var firstProfileAt time.Time
			if e = tx.QueryRow(ctx, `SELECT min(created_at) FROM merchant_notice_profiles WHERE merchant_id=$1`, mid).Scan(&firstProfileAt); e != nil {
				return 0, e
			}
			if expired.MerchantID != mid || expired.PurchaseID <= 0 || expired.ExpiresAt.After(at) || len(expired.ProofHash) != 32 {
				return 0, ErrConflict
			}
			if !expired.ExpiresAt.Before(firstProfileAt) {
				input := Input{Scene: "group", At: at, Params: map[string]string{"group": expired.GroupName, "endtime": expired.ExpiresAt.In(businessZone).Format("2006-01-02 15:04:05"), "purchase_id": fmt.Sprint(expired.PurchaseID), "purchase_hash": hex.EncodeToString(expired.ProofHash)}}
				added, e := s.appendTimer(ctx, tx, p, input, "purchase:"+fmt.Sprint(expired.PurchaseID))
				if e != nil {
					return 0, e
				}
				if added {
					created++
				}
			}
		}
	} else if allowed["group"] && (scene == "" || scene == "group") && data.Preferences.Group != 0 {
		return 0, ErrState
	}
	_, _, dailyDue, _ := DailyWindow(at, sendHour)
	if allowed["daily_report"] && (scene == "" || scene == "daily_report") && data.Preferences.DailyReport != 0 && dailyDue {
		added, e := s.generateDaily(ctx, tx, p, at, transfers)
		if e != nil {
			return 0, e
		}
		if added {
			created++
		}
	}
	if allowed["balance"] && (scene == "" || scene == "balance") && data.Preferences.Balance != 0 && data.Preferences.BalanceMoney > 0 {
		added, e := s.generateBalance(ctx, tx, p, at, data.Preferences.BalanceMoney)
		if e != nil {
			return 0, e
		}
		if added {
			created++
		}
	}
	if expectedDay != "" {
		var finalTime time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&finalTime); err != nil {
			return 0, err
		}
		if finalTime.In(businessZone).Format("2006-01-02") != expectedDay {
			return 0, ErrState
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return created, nil
}

func timerRecorded(ctx context.Context, tx pgx.Tx, mid int64, scene, key string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM merchant_notice_timer_sources WHERE merchant_id=$1 AND scene=$2 AND business_key=$3)`, mid, scene, key).Scan(&exists)
	return exists, err
}
func (s *Store) generateDaily(ctx context.Context, tx pgx.Tx, p profileRecord, at time.Time, transfers TransferReportReader) (bool, error) {
	key := at.In(businessZone).Format("2006-01-02")
	exists, err := timerRecorded(ctx, tx, p.MerchantID, "daily_report", key)
	if err != nil || exists {
		return false, err
	}
	report, err := s.dailyReport(ctx, tx, p.MerchantID, at, transfers)
	if err != nil {
		return false, err
	}
	return s.appendTimer(ctx, tx, p, Input{Scene: "daily_report", At: at, Daily: &report}, key)
}
func (s *Store) generateBalance(ctx context.Context, tx pgx.Tx, p profileRecord, at time.Time, threshold int64) (bool, error) {
	key := at.In(businessZone).Format("2006-01-02")
	exists, err := timerRecorded(ctx, tx, p.MerchantID, "balance", key)
	if err != nil || exists {
		return false, err
	}
	var balance int64
	if err = tx.QueryRow(ctx, `SELECT balance FROM accounts WHERE owner_id=$1 AND kind='available' AND scope_id=0`, p.MerchantID).Scan(&balance); err != nil {
		return false, err
	}
	if balance < 0 {
		return false, ErrConflict
	}
	if balance >= threshold {
		return false, nil
	}
	return s.appendTimer(ctx, tx, p, Input{Scene: "balance", At: at, Params: map[string]string{"money": money(balance), "msgmoney": money(threshold), "user": fmt.Sprint(p.MerchantID), "time": at.In(businessZone).Format("2006-01-02 15:04:05")}}, key)
}
