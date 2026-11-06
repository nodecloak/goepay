package kyc

import (
	"context"
	"crypto/hmac"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"goepay/internal/ledger"
)

type privateAttempt struct {
	Snapshot           Snapshot   `json:"snapshot"`
	Submission         Submission `json:"submission"`
	Provider           Attempt    `json:"provider"`
	SessionHash        []byte     `json:"session_hash"`
	Scan               string     `json:"scan"`
	AuthCodeUsed       bool       `json:"auth_code_used"`
	VerifiedAt         time.Time  `json:"verified_at"`
	OperationStartedAt time.Time  `json:"operation_started_at"`
	ReturnExpiresAt    time.Time  `json:"return_expires_at"`
}
type attemptRow struct {
	ID                 string
	Merchant, Revision int64
	Status             string
	Cipher, Proof      []byte
	Expires, NextQuery time.Time
	Data               privateAttempt
}
type certRecord struct {
	Merchant      int64
	AttemptID     string
	CertType      int
	At            time.Time
	Cipher, Proof []byte
	Submission    Submission
	Mode          int
}
type certPayload struct {
	Submission Submission `json:"submission"`
	Mode       int        `json:"mode"`
}

func (s *Service) merchant(ctx context.Context, tx pgx.Tx, a Actor) error {
	if a.Delegated {
		return identity.ErrOriginalMerchantSession
	}
	var enabled bool
	if e := tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1 FOR NO KEY UPDATE", a.User.ID).Scan(&enabled); e != nil {
		return e
	}
	if !enabled {
		return identity.ErrOriginalMerchantSession
	}
	return identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF)
}
func (s *Service) record(ctx context.Context, tx pgx.Tx, merchant int64) (certRecord, error) {
	var r certRecord
	r.Merchant = merchant
	e := tx.QueryRow(ctx, "SELECT attempt_id,cert_type,certified_at,record_cipher,proof_hash FROM merchant_certifications233 WHERE merchant_id=$1", merchant).Scan(&r.AttemptID, &r.CertType, &r.At, &r.Cipher, &r.Proof)
	if e != nil {
		return r, e
	}
	if !hmac.Equal(r.Proof, s.digest("record", merchant, r.AttemptID, r.CertType, utc(r.At), r.Cipher)) {
		return r, ErrConflict
	}
	var latest string
	var recorded []byte
	if e = tx.QueryRow(ctx, "SELECT attempt_id,proof_hash FROM kyc_certification_events233 WHERE merchant_id=$1 ORDER BY event_id DESC LIMIT 1", merchant).Scan(&latest, &recorded); e != nil {
		return r, e
	}
	if latest != r.AttemptID || !hmac.Equal(recorded, r.Proof) {
		return r, ErrConflict
	}
	var p certPayload
	if e = s.open(fmt.Sprint("record/", merchant, "/", r.AttemptID), r.Cipher, &p); e != nil {
		return r, e
	}
	r.Submission = p.Submission
	r.Mode = p.Mode
	a, e := s.readAttempt(ctx, tx, r.AttemptID, merchant, false)
	if e != nil || a.Status != "passed" || a.Data.Submission.CertType != r.CertType {
		return r, ErrConflict
	}
	return r, nil
}
func (s *Service) readAttempt(ctx context.Context, tx pgx.Tx, id string, merchant int64, lock bool) (attemptRow, error) {
	r := attemptRow{ID: id, Merchant: merchant}
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	e := tx.QueryRow(ctx, "SELECT revision,status,attempt_cipher,proof_hash,expires_at,next_query_at FROM kyc_attempts233 WHERE id=$1 AND merchant_id=$2"+suffix, id, merchant).Scan(&r.Revision, &r.Status, &r.Cipher, &r.Proof, &r.Expires, &r.NextQuery)
	if e != nil {
		return r, e
	}
	if !hmac.Equal(r.Proof, s.digest("attempt", r.ID, r.Merchant, r.Revision, r.Status, r.Cipher, utc(r.Expires), utc(r.NextQuery))) {
		return r, ErrConflict
	}
	var ok bool
	e = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE revision=$2 AND proof_hash=$3 AND status=$4)=1 AND max(revision)=$2 FROM kyc_attempt_events233 WHERE attempt_id=$1`, id, r.Revision, r.Proof, r.Status).Scan(&ok)
	if e != nil {
		return r, e
	}
	if !ok {
		return r, ErrConflict
	}
	if e = s.open(fmt.Sprint("attempt/", id, "/", r.Revision), r.Cipher, &r.Data); e != nil {
		return r, e
	}
	if r.Data.Provider.ID != id {
		return r, ErrConflict
	}
	return r, nil
}
func (s *Service) writeAttempt(ctx context.Context, tx pgx.Tx, r *attemptRow, insert bool) error {
	r.Revision++
	raw, e := s.seal(fmt.Sprint("attempt/", r.ID, "/", r.Revision), r.Data)
	if e != nil {
		return e
	}
	r.Cipher = raw
	r.Proof = s.digest("attempt", r.ID, r.Merchant, r.Revision, r.Status, r.Cipher, utc(r.Expires), utc(r.NextQuery))
	if insert {
		_, e = tx.Exec(ctx, `INSERT INTO kyc_attempts233(id,merchant_id,revision,status,attempt_cipher,expires_at,next_query_at,proof_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, r.ID, r.Merchant, r.Revision, r.Status, r.Cipher, r.Expires, r.NextQuery, r.Proof)
	} else {
		tag, err := tx.Exec(ctx, `UPDATE kyc_attempts233 SET revision=$3,status=$4,attempt_cipher=$5,expires_at=$6,next_query_at=$7,proof_hash=$8 WHERE id=$1 AND merchant_id=$2 AND revision=$9`, r.ID, r.Merchant, r.Revision, r.Status, r.Cipher, r.Expires, r.NextQuery, r.Proof, r.Revision-1)
		e = err
		if e == nil && tag.RowsAffected() != 1 {
			e = ErrConflict
		}
	}
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO kyc_attempt_events233(attempt_id,revision,proof_hash,status) VALUES($1,$2,$3,$4)", r.ID, r.Revision, r.Proof, r.Status)
	return e
}

func (s *Service) info(ctx context.Context, tx pgx.Tx, merchant int64) (Info, error) {
	c, e := s.readConfig(ctx, tx)
	if e != nil {
		return Info{}, e
	}
	out := Info{Revision: c.Revision, OCRConfigRevision: c.Revision, Mode: c.Mode, Corporate: c.Corporate, Money: ledger.Format(c.Fee), OCRType: c.OCRType, Status: "none"}
	var phone string
	if e = tx.QueryRow(ctx, "SELECT COALESCE(NULLIF(profile->>'bind_mobile',''),(SELECT phone FROM enrollment_phones134 WHERE merchant_id=m.id),'') FROM merchants m WHERE id=$1", merchant).Scan(&phone); e != nil {
		return out, e
	}
	out.Phone = mask(phone)
	r, e := s.record(ctx, tx, merchant)
	if e == nil {
		out.Cert = 1
		out.CertType = r.CertType
		out.CardType = r.Submission.CardType
		out.Name = mask(r.Submission.Name)
		out.Number = mask(r.Submission.Number)
		out.CompanyName = mask(r.Submission.CompanyName)
		out.CompanyNumber = mask(r.Submission.CompanyNumber)
		out.Time = utc(r.At)
		out.TypeText = "个人认证"
		if r.CertType == 1 {
			out.TypeText = "企业认证"
		}
		out.Method = modeText(r.Mode)
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	var id string
	e = tx.QueryRow(ctx, "SELECT id FROM kyc_attempts233 WHERE merchant_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1", merchant).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return out, e
	}
	a, e := s.readAttempt(ctx, tx, id, merchant, false)
	if e != nil {
		return out, e
	}
	out.AttemptID = a.ID
	out.Status = a.Status
	out.Scan = a.Data.Scan
	out.ExpiresAt = utc(a.Expires)
	out.AuthCodeUsed = a.Data.AuthCodeUsed
	out.AuthCodeReady = !a.Data.AuthCodeUsed && a.Data.Provider.AuthCode != "" && !a.Data.ReturnExpiresAt.IsZero() && time.Now().Before(a.Data.ReturnExpiresAt)
	if a.NextQuery.After(time.Unix(0, 0)) {
		out.QueryAfter = utc(a.NextQuery)
	}
	if a.Status == "ready" || a.Status == "initializing" || a.Status == "pending" || a.Status == "unknown" || a.Status == "verified" {
		out.Revision = a.Data.Snapshot.Revision
		out.Mode = a.Data.Snapshot.Mode
		out.Money = ledger.Format(a.Data.Snapshot.Fee)
		out.Corporate = a.Data.Snapshot.Corporate
		out.ConfigurationChanged = c.Revision != a.Data.Snapshot.Revision
		if a.Status == "verified" {
			out.ConfigurationChanged = false
		}
		if a.Data.Snapshot.Mode == 2 && a.Status != "verified" {
			out.ConfigurationChanged = out.ConfigurationChanged || phone != a.Data.Submission.Phone
			out.Phone = mask(a.Data.Submission.Phone)
		}
	}
	if !time.Now().Before(a.Expires) && a.Status != "passed" && a.Status != "cancelled" && a.Status != "rejected" && a.Status != "verified" {
		out.Status = "expired"
	}
	if a.Status == "verified" {
		out.ExpiresAt = utc(a.Data.VerifiedAt.Add(24 * time.Hour))
		if time.Now().After(a.Data.VerifiedAt.Add(24 * time.Hour)) {
			out.Status = "expired"
		}
	}
	if out.Status == "pending" {
		out.URL = a.Data.Provider.URL
	}
	return out, nil
}
func modeText(mode int) string {
	switch mode {
	case 1:
		return "支付宝身份验证"
	case 2:
		return "手机号三要素"
	case 3:
		return "支付宝实名信息验证"
	case 4:
		return "微信扫码实名认证"
	case 5:
		return "阿里云金融级实人认证"
	}
	return ""
}

func (s *Service) fresh(ctx context.Context, tx pgx.Tx, r attemptRow) error {
	c, e := s.readConfig(ctx, tx)
	if e != nil {
		return e
	}
	if c.Revision != r.Data.Snapshot.Revision || c.Mode != r.Data.Snapshot.Mode || c.Mode == 0 {
		return ErrConflict
	}
	if c.Mode == 2 {
		var phone string
		e = tx.QueryRow(ctx, "SELECT COALESCE(NULLIF(profile->>'bind_mobile',''),(SELECT phone FROM enrollment_phones134 WHERE merchant_id=m.id),'') FROM merchants m WHERE id=$1", r.Merchant).Scan(&phone)
		if e != nil {
			return e
		}
		if phone == "" || phone != r.Data.Submission.Phone {
			return ErrConflict
		}
	}
	return nil
}
func (s *Service) Info(ctx context.Context, a Actor) (Info, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Info{}, e
	}
	defer tx.Rollback(ctx)
	if e = s.merchant(ctx, tx, a); e != nil {
		return Info{}, e
	}
	out, e := s.info(ctx, tx, a.User.ID)
	if e != nil {
		return out, e
	}
	if e = s.merchant(ctx, tx, a); e != nil {
		return Info{}, e
	}
	return out, nil
}

func (s *Service) Submit(ctx context.Context, a Actor, in Submission) (Info, error) {
	if s.provider == nil || s.base == "" {
		return Info{}, ErrUnavailable
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Info{}, e
	}
	defer tx.Rollback(ctx)
	if e = s.merchant(ctx, tx, a); e != nil {
		return Info{}, e
	}
	c, e := s.readConfig(ctx, tx)
	if e != nil {
		return Info{}, e
	}
	if c.Mode == 0 {
		return Info{}, ErrUnavailable
	}
	if in.ExpectedRevision != c.Revision {
		return Info{}, ErrConflict
	}
	if !validSubmission(in, c.Mode, c.Corporate) {
		return Info{}, ErrInvalid
	}
	old, e := s.record(ctx, tx, a.User.ID)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return Info{}, e
	}
	if e == nil && (old.CertType == 1 || in.CertType != 1) {
		return Info{}, ErrConflict
	}
	// Serialize against all mutations for this merchant before account locks.
	var active string
	e = tx.QueryRow(ctx, "SELECT id FROM kyc_attempts233 WHERE merchant_id=$1 AND status IN ('ready','initializing','pending','unknown','verified')", a.User.ID).Scan(&active)
	if e == nil {
		r, e := s.readAttempt(ctx, tx, active, a.User.ID, true)
		if e != nil {
			return Info{}, e
		}
		activeValid := time.Now().Before(r.Expires)
		if r.Status == "verified" {
			activeValid = r.Data.VerifiedAt.IsZero() || time.Now().Before(r.Data.VerifiedAt.Add(24*time.Hour))
		}
		if activeValid {
			return Info{}, ErrConflict
		}
		r.Status = "expired"
		if e = s.writeAttempt(ctx, tx, &r, false); e != nil {
			return Info{}, e
		}
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return Info{}, e
	}
	if c.Mode == 1 || c.Mode == 3 {
		if s.resolve == nil {
			return Info{}, ErrUnavailable
		}
		channel, e := s.resolve(ctx, tx, c.ChannelID)
		if e != nil || channel.Plugin != "alipay" {
			return Info{}, ErrUnavailable
		}
		c.Alipay = &channel
	}
	if c.Mode == 2 {
		if e = tx.QueryRow(ctx, "SELECT COALESCE(NULLIF(profile->>'bind_mobile',''),(SELECT phone FROM enrollment_phones134 WHERE merchant_id=m.id),'') FROM merchants m WHERE id=$1", a.User.ID).Scan(&in.Phone); e != nil {
			return Info{}, e
		}
		if len(in.Phone) != 11 || in.Phone[0] != '1' {
			return Info{}, ErrInvalid
		}
	}
	if c.Fee > 0 {
		var balance int64
		if e = tx.QueryRow(ctx, "SELECT balance FROM accounts WHERE owner_id=$1 AND kind='available' AND scope_id=0", a.User.ID).Scan(&balance); e != nil {
			return Info{}, e
		}
		if balance < c.Fee {
			return Info{}, ErrInsufficient
		}
	}
	id, e := randomID()
	if e != nil {
		return Info{}, e
	}
	state, e := randomID()
	if e != nil {
		return Info{}, e
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	r := attemptRow{ID: id, Merchant: a.User.ID, Status: "ready", Expires: now.Add(30 * time.Minute), NextQuery: time.Unix(0, 0).UTC()}
	r.Data = privateAttempt{Snapshot: c, Submission: in, SessionHash: s.digest("session", a.Token), Provider: Attempt{ID: id, State: state, CallbackURL: s.base + "/merchant/account/certificate?attempt_id=" + id + "&state=" + state}}
	if c.Mode == 3 {
		r.Data.Provider.CallbackURL = s.base + "/callbacks/certification233/return?attempt_id=" + id
	}
	if e = s.writeAttempt(ctx, tx, &r, true); e != nil {
		return Info{}, e
	}
	if e = s.merchant(ctx, tx, a); e != nil {
		return Info{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Info{}, e
	}
	return s.Info(ctx, a)
}

func (s *Service) finish(ctx context.Context, tx pgx.Tx, r *attemptRow, status string) error {
	if status != "pending" && status != "passed" && status != "rejected" {
		return ErrUnknown
	}
	if status != "passed" {
		r.Status = status
		return s.writeAttempt(ctx, tx, r, false)
	}
	verifiedAt := time.Now().UTC().Truncate(time.Microsecond)
	if r.Data.OperationStartedAt.IsZero() || !r.Data.OperationStartedAt.Before(r.Expires) {
		return ErrConflict
	}
	r.Status = "verified"
	r.Data.VerifiedAt = verifiedAt
	return s.writeAttempt(ctx, tx, r, false)
}

// finalize consumes an already durable provider proof. A fee failure leaves
// that proof intact, allowing the merchant to top up and finish without another
// billable initialization or replaying an OAuth authorization code.
func (s *Service) finalize(ctx context.Context, a Actor, id string) (Info, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Info{}, e
	}
	defer tx.Rollback(ctx)
	if e = s.merchant(ctx, tx, a); e != nil {
		return Info{}, e
	}
	r, e := s.readAttempt(ctx, tx, id, a.User.ID, true)
	if e != nil {
		return Info{}, e
	}
	if r.Status == "passed" {
		return s.info(ctx, tx, a.User.ID)
	}
	if r.Status != "verified" || r.Data.VerifiedAt.IsZero() || r.Data.OperationStartedAt.IsZero() || !r.Data.OperationStartedAt.Before(r.Expires) || time.Now().After(r.Data.VerifiedAt.Add(24*time.Hour)) {
		return Info{}, ErrConflict
	}
	old, e := s.record(ctx, tx, r.Merchant)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return Info{}, e
	}
	if e == nil && (old.CertType == 1 || r.Data.Submission.CertType != 1) {
		return Info{}, ErrConflict
	}
	if r.Data.Snapshot.Fee > 0 {
		if e = postFee(ctx, tx, r.Merchant, r.ID, r.Data.Snapshot.Fee); e != nil {
			if errors.Is(e, ErrInsufficient) {
				tx.Rollback(ctx)
				return s.Info(ctx, a)
			}
			return Info{}, e
		}
	}
	r.Status = "passed"
	if e = s.writeAttempt(ctx, tx, &r, false); e != nil {
		return Info{}, e
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	raw, e := s.seal(fmt.Sprint("record/", r.Merchant, "/", r.ID), certPayload{Submission: r.Data.Submission, Mode: r.Data.Snapshot.Mode})
	if e != nil {
		return Info{}, e
	}
	proof := s.digest("record", r.Merchant, r.ID, r.Data.Submission.CertType, utc(at), raw)
	_, e = tx.Exec(ctx, `INSERT INTO merchant_certifications233(merchant_id,attempt_id,cert_type,certified_at,record_cipher,proof_hash) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(merchant_id) DO UPDATE SET attempt_id=EXCLUDED.attempt_id,cert_type=EXCLUDED.cert_type,certified_at=EXCLUDED.certified_at,record_cipher=EXCLUDED.record_cipher,proof_hash=EXCLUDED.proof_hash`, r.Merchant, r.ID, r.Data.Submission.CertType, at, raw, proof)
	if e != nil {
		return Info{}, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO kyc_certification_events233(merchant_id,attempt_id,certified_at,proof_hash) VALUES($1,$2,$3,$4)", r.Merchant, r.ID, at, proof); e != nil {
		return Info{}, e
	}
	if e = s.merchant(ctx, tx, a); e != nil {
		return Info{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Info{}, ErrUnknown
	}
	return s.Info(ctx, a)
}

func postFee(ctx context.Context, tx pgx.Tx, merchant int64, id string, fee int64) error {
	var from, to int64
	if e := tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind='available' AND scope_id=0", merchant).Scan(&from); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, "INSERT INTO accounts(owner_id,kind,scope_id) VALUES(0,'fees',0) ON CONFLICT DO NOTHING"); e != nil {
		return e
	}
	if e := tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=0 AND kind='fees' AND scope_id=0").Scan(&to); e != nil {
		return e
	}
	rows, e := tx.Query(ctx, "SELECT id FROM accounts WHERE id=ANY($1::bigint[]) ORDER BY id FOR UPDATE", []int64{from, to})
	if e != nil {
		return e
	}
	for rows.Next() {
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	var jid int64
	e = tx.QueryRow(ctx, "INSERT INTO journals(event_key,description) VALUES($1,'实名认证') ON CONFLICT DO NOTHING RETURNING id", "certification233:"+id).Scan(&jid)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrConflict
	}
	if e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, "UPDATE accounts SET balance=balance-$1 WHERE id=$2 AND balance>=$1", fee, from)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrInsufficient
	}
	if _, e = tx.Exec(ctx, "UPDATE accounts SET balance=balance+$1 WHERE id=$2", fee, to); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO ledger_entries(journal_id,account_id,amount) VALUES($1,$2,-$4::bigint),($1,$3,$4::bigint)", jid, from, to, fee)
	return e
}

type Operation struct {
	AttemptID string `json:"attempt_id"`
	State     string `json:"state"`
	AuthCode  string `json:"auth_code"`
}

func (s *Service) claim(ctx context.Context, a Actor, in Operation, initialize bool) (attemptRow, error) {
	if !tokenID.MatchString(in.AttemptID) || len(in.State) > 128 || len(in.AuthCode) > 2048 {
		return attemptRow{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return attemptRow{}, e
	}
	defer tx.Rollback(ctx)
	if e = s.merchant(ctx, tx, a); e != nil {
		return attemptRow{}, e
	}
	r, e := s.readAttempt(ctx, tx, in.AttemptID, a.User.ID, true)
	if e != nil {
		return r, e
	}
	if !initialize && (r.Status == "verified" || r.Status == "passed") {
		return r, nil
	}
	if initialize && !hmac.Equal(r.Data.SessionHash, s.digest("session", a.Token)) {
		return r, ErrConflict
	}
	if !time.Now().Before(r.Expires) {
		return r, ErrConflict
	}
	if initialize {
		if e = s.fresh(ctx, tx, r); e != nil {
			return r, e
		}
		if r.Data.Snapshot.Mode == 1 || r.Data.Snapshot.Mode == 3 {
			if s.resolve == nil || r.Data.Snapshot.Alipay == nil {
				return r, ErrUnavailable
			}
			current, err := s.resolve(ctx, tx, r.Data.Snapshot.ChannelID)
			if err != nil {
				return r, ErrConflict
			}
			left, _ := json.Marshal(current)
			right, _ := json.Marshal(r.Data.Snapshot.Alipay)
			if !hmac.Equal(left, right) {
				return r, ErrConflict
			}
		}
		if in.State != "" || in.AuthCode != "" {
			return r, ErrInvalid
		}
		if r.Status != "ready" {
			return r, ErrConflict
		}
		r.Status = "initializing"
	} else {
		if r.Status == "passed" {
			return r, nil
		}
		if (r.Status != "pending" && r.Status != "unknown") || r.Data.Provider.Token == "" || time.Now().Before(r.NextQuery) {
			return r, ErrConflict
		}
		if r.Data.Snapshot.Mode == 3 {
			if r.Data.AuthCodeUsed {
				return r, ErrInvalid
			}
			if in.State == "" && in.AuthCode == "" {
				if r.Data.Provider.AuthCode == "" || r.Data.ReturnExpiresAt.IsZero() || !time.Now().Before(r.Data.ReturnExpiresAt) {
					return r, ErrInvalid
				}
			} else {
				if r.Data.Provider.AuthCode != "" || !hmac.Equal([]byte(in.State), []byte(r.Data.Provider.State)) || !validCode(in.AuthCode) {
					return r, ErrInvalid
				}
				r.Data.Provider.AuthCode = in.AuthCode
			}
			r.Data.AuthCodeUsed = true
		} else if in.State != "" || in.AuthCode != "" {
			return r, ErrInvalid
		}
		r.NextQuery = time.Now().UTC().Truncate(time.Microsecond).Add(30 * time.Second)
	}
	r.Data.OperationStartedAt = time.Now().UTC().Truncate(time.Microsecond)
	if e = s.writeAttempt(ctx, tx, &r, false); e != nil {
		return r, e
	}
	if e = s.merchant(ctx, tx, a); e != nil {
		return r, e
	}
	if !time.Now().Before(r.Expires) {
		return r, ErrConflict
	}
	if !initialize && r.Data.Snapshot.Mode == 3 && !r.Data.ReturnExpiresAt.IsZero() && !time.Now().Before(r.Data.ReturnExpiresAt) {
		return r, ErrConflict
	}
	return r, tx.Commit(ctx)
}
func safeProviderURL(raw string, mode int) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(raw) > 16384 || strings.Contains(raw, "\\") || u.Port() != "" {
		return false
	}
	h := u.Hostname()
	if u.RawPath != "" {
		return false
	}
	switch mode {
	case 1:
		return h == "openapi.alipay.com" && u.Path == "/gateway.do" && u.Fragment == ""
	case 3:
		return h == "openauth.alipay.com" && u.Path == "/oauth2/publicAppAuthorize.htm" && u.Fragment == ""
	case 4:
		return h == "faceid.qq.com" && (u.Path == "" || u.Path == "/") && u.Fragment == "" || h == "open.weixin.qq.com" && u.Path == "/connect/oauth2/authorize" && (u.Fragment == "" || u.Fragment == "wechat_redirect")
	case 5:
		return h == "picker.antcloudauth.aliyuncs.com" && u.Path == "/gateway.do" && u.Fragment == ""
	}
	return false
}
func (s *Service) GetURL(ctx context.Context, a Actor, in Operation) (Info, error) {
	if s.provider == nil {
		return Info{}, ErrUnavailable
	}
	r, e := s.claim(ctx, a, in, true)
	if e != nil {
		// Reading the existing result never repeats initialization.
		if errors.Is(e, ErrConflict) {
			info, read := s.Info(ctx, a)
			if read == nil && info.AttemptID == in.AttemptID && !info.ConfigurationChanged && (info.Status == "pending" || info.Status == "passed") {
				return info, nil
			}
		}
		return Info{}, e
	}
	result, callErr := s.provider.Initialize(ctx, r.Data.Snapshot, r.Data.Submission, r.Data.Provider)
	if result.Status != "pending" && result.Status != "passed" && result.Status != "rejected" {
		callErr = ErrUnknown
	}
	if len(result.Token) > 16384 {
		callErr = ErrUnknown
	}
	if result.Status == "pending" && (result.Token == "" || !safeProviderURL(result.URL, r.Data.Snapshot.Mode)) {
		callErr = ErrUnknown
	}
	if result.Scan != "" && result.Scan != "alipay" && result.Scan != "wechat" && result.Scan != "phone" {
		callErr = ErrUnknown
	}
	status, e := s.checkpoint(ctx, r, result, callErr, false)
	if e != nil {
		return Info{}, e
	}
	if status == "verified" {
		return s.finalize(ctx, a, r.ID)
	}
	return s.Info(ctx, a)
}
func (s *Service) Query(ctx context.Context, a Actor, in Operation) (Info, error) {
	if s.provider == nil {
		return Info{}, ErrUnavailable
	}
	r, e := s.claim(ctx, a, in, false)
	if e != nil {
		return Info{}, e
	}
	if r.Status == "passed" {
		return s.Info(ctx, a)
	}
	if r.Status == "verified" {
		return s.finalize(ctx, a, r.ID)
	}
	result, callErr := s.provider.Query(ctx, r.Data.Snapshot, r.Data.Provider)
	if result.Status != "pending" && result.Status != "passed" && result.Status != "rejected" {
		callErr = ErrUnknown
	}
	status, e := s.checkpoint(ctx, r, InitResult{Status: result.Status}, callErr, true)
	if e != nil {
		return Info{}, e
	}
	if status == "verified" {
		return s.finalize(ctx, a, r.ID)
	}
	return s.Info(ctx, a)
}

// An authenticated server-side provider result is a private historical fact.
// Preserve it even if the client disconnects or its session expires in flight;
// authorization is checked separately before returning it or charging any fee.
func (s *Service) checkpoint(parent context.Context, claimed attemptRow, result InitResult, callErr error, query bool) (string, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
	defer cancel()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return "", ErrUnknown
	}
	defer tx.Rollback(ctx)
	var id int64
	if e = tx.QueryRow(ctx, "SELECT id FROM merchants WHERE id=$1 FOR NO KEY UPDATE", claimed.Merchant).Scan(&id); e != nil {
		return "", e
	}
	current, e := s.readAttempt(ctx, tx, claimed.ID, claimed.Merchant, true)
	if e != nil {
		return "", e
	}
	if current.Revision != claimed.Revision || current.Status != claimed.Status || (!query && current.Status != "initializing") || (query && current.Status != "pending" && current.Status != "unknown") {
		return "", ErrConflict
	}
	current.Data.Provider.AuthCode = ""
	if callErr != nil {
		current.Status = "unknown"
		e = s.writeAttempt(ctx, tx, &current, false)
	} else {
		if !query {
			current.Data.Provider.Token = result.Token
			current.Data.Provider.URL = result.URL
			current.Data.Scan = result.Scan
		}
		e = s.finish(ctx, tx, &current, result.Status)
	}
	if e != nil {
		return "", e
	}
	if e = tx.Commit(ctx); e != nil {
		return "", ErrUnknown
	}
	return current.Status, nil
}
func (s *Service) Reset(ctx context.Context, a Actor, in Operation) (Info, error) {
	if !tokenID.MatchString(in.AttemptID) || in.State != "" || in.AuthCode != "" {
		return Info{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Info{}, e
	}
	defer tx.Rollback(ctx)
	if e = s.merchant(ctx, tx, a); e != nil {
		return Info{}, e
	}
	r, e := s.readAttempt(ctx, tx, in.AttemptID, a.User.ID, true)
	if e != nil {
		return Info{}, e
	}
	if r.Status == "passed" {
		return Info{}, ErrConflict
	}
	if r.Status != "cancelled" {
		r.Status = "cancelled"
		r.Data.Provider.AuthCode = ""
		if e = s.writeAttempt(ctx, tx, &r, false); e != nil {
			return Info{}, e
		}
	}
	if e = s.merchant(ctx, tx, a); e != nil {
		return Info{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Info{}, e
	}
	return s.Info(ctx, a)
}

// Recognize claims a unique operation before any token/provider I/O. It stores
// neither the input image nor OCR output, including on failure.
func (s *Service) Recognize(ctx context.Context, a Actor, operation, kind, expectedProvider string, expectedRevision int64, image []byte) (OCRResult, error) {
	if s.provider == nil || !tokenID.MatchString(operation) || !validDocument(kind) {
		return OCRResult{}, ErrInvalid
	}
	if _, e := validateImage(image); e != nil {
		return OCRResult{}, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return OCRResult{}, e
	}
	defer tx.Rollback(ctx)
	if a.User.Role == "admin" {
		e = s.admin(ctx, tx, a)
	} else {
		e = s.merchant(ctx, tx, a)
	}
	if e != nil {
		return OCRResult{}, e
	}
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "ocr233/"+a.User.Role+"/"+strconv.FormatInt(a.User.ID, 10)); e != nil {
		return OCRResult{}, e
	}
	var count int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM ocr_operations233 WHERE actor_role=$1 AND actor_id=$2 AND created_at>now()-interval '1 minute'", a.User.Role, a.User.ID).Scan(&count); e != nil {
		return OCRResult{}, e
	}
	if count >= 10 {
		return OCRResult{}, ErrConflict
	}
	c, e := s.readConfig(ctx, tx)
	if e != nil {
		return OCRResult{}, e
	}
	if c.OCRType == "" {
		return OCRResult{}, ErrUnavailable
	}
	if expectedProvider != c.OCRType || expectedRevision != c.Revision {
		return OCRResult{}, ErrConflict
	}
	if c.OCRType == "baidu" && (len(image) > 2<<20 || len(url.QueryEscape(base64.StdEncoding.EncodeToString(image))) > 4<<20) {
		return OCRResult{}, ErrInvalid
	}
	tag, e := tx.Exec(ctx, "INSERT INTO ocr_operations233(operation_id,actor_role,actor_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", operation, a.User.Role, a.User.ID)
	if e != nil {
		return OCRResult{}, e
	}
	if tag.RowsAffected() != 1 {
		return OCRResult{}, ErrConflict
	}
	if a.User.Role == "admin" {
		e = s.admin(ctx, tx, a)
	} else {
		e = s.merchant(ctx, tx, a)
	}
	if e != nil {
		return OCRResult{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return OCRResult{}, ErrUnknown
	}
	out, e := s.provider.Recognize(ctx, c, kind, image)
	if e != nil {
		return OCRResult{}, ErrUnknown
	}
	// Provider output remains data for review, never executable text or a status.
	if len(out.Fields) > 32 {
		return OCRResult{}, ErrUnknown
	}
	for k, v := range out.Fields {
		if len(k) > 64 || len(v) > 8192 {
			return OCRResult{}, ErrUnknown
		}
	}
	check, e := s.pool.Begin(ctx)
	if e != nil {
		return OCRResult{}, ErrUnknown
	}
	defer check.Rollback(ctx)
	if a.User.Role == "admin" {
		e = s.admin(ctx, check, a)
	} else {
		e = s.merchant(ctx, check, a)
	}
	if e != nil {
		return OCRResult{}, e
	}
	current, e := s.readConfig(ctx, check)
	if e != nil {
		return OCRResult{}, e
	}
	if current.Revision != c.Revision {
		return OCRResult{}, ErrConflict
	}
	if a.User.Role == "admin" {
		e = s.admin(ctx, check, a)
	} else {
		e = s.merchant(ctx, check, a)
	}
	if e != nil {
		return OCRResult{}, e
	}
	if out.Fields == nil {
		out.Fields = map[string]string{}
	}
	return out, nil
}
