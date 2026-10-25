package enrollment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"math/big"
	"net"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

var (
	ErrInvalid     = errors.New("invalid enrollment input")
	ErrCode        = errors.New("invalid or expired verification")
	ErrClosed      = errors.New("registration is closed")
	ErrLimited     = errors.New("verification rate limit exceeded")
	ErrUnavailable = errors.New("verification delivery unavailable")
	ErrConflict    = errors.New("registration unavailable")
)

type Sender interface {
	SendVerification(context.Context, string, string, string) error
}
type SenderFunc func(context.Context, string, string, string) error

func (f SenderFunc) SendVerification(ctx context.Context, email, purpose, code string) error {
	return f(ctx, email, purpose, code)
}

type Service struct {
	pool   *pgxpool.Pool
	store  *payment.Store
	key    []byte
	sender Sender
}
type VerificationRequest struct{ Purpose, Email, Source string }
type Challenge struct {
	ID        string    `json:"challenge_id"`
	ExpiresAt time.Time `json:"expires_at"`
}
type RegisterInput struct{ ChallengeID, Code, Email, Source, Username, Name, Password, InviteCode, PromotionToken string }
type RecoverInput struct{ ChallengeID, Code, Email, Source, Password string }
type Registered struct {
	PayStatus  string `json:"pay"`
	MerchantID int64  `json:"merchant_id,string"`
	Username   string `json:"username"`
}
type Capabilities struct {
	ReviewRequired   bool `json:"review_required"`
	RegistrationOpen bool `json:"registration_open"`
	EmailEnabled     bool `json:"email_enabled"`
	InviteRequired   bool `json:"invite_required"`
	ExpiresSeconds   int  `json:"expires_seconds"`
}

func New(pool *pgxpool.Pool, store *payment.Store, key []byte, sender Sender) (*Service, error) {
	if pool == nil || store == nil || len(key) != 32 {
		return nil, ErrInvalid
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("goepay/public-enrollment/v1"))
	return &Service{pool: pool, store: store, key: mac.Sum(nil), sender: sender}, nil
}
func (s *Service) Capabilities(ctx context.Context) (Capabilities, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Capabilities{}, e
	}
	defer tx.Rollback(ctx)
	open, invite, e := policy(ctx, tx)
	if e != nil {
		return Capabilities{}, e
	}
	pay, e := registrationPayStatus(ctx, tx)
	return Capabilities{RegistrationOpen: open, InviteRequired: invite, EmailEnabled: s.sender != nil, ExpiresSeconds: 600, ReviewRequired: pay == 2}, e
}

func (s *Service) digest(parts ...string) []byte {
	m := hmac.New(sha256.New, s.key)
	for _, v := range parts {
		m.Write([]byte(v))
		m.Write([]byte{0})
	}
	return m.Sum(nil)
}
func normalize(email, source string) (string, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	a, e := mail.ParseAddress(email)
	ip := net.ParseIP(source)
	if e != nil || a.Address != email || a.Name != "" || len(email) > 254 || ip == nil {
		return "", "", ErrInvalid
	}
	return email, ip.String(), nil
}
func token() (string, error) {
	b := make([]byte, 32)
	_, e := rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b), e
}
func policy(ctx context.Context, tx pgx.Tx) (bool, bool, error) {
	rows, e := tx.Query(ctx, `SELECT body->>'key',COALESCE(body->>'value','') FROM backoffice_records
	 WHERE resource='settings' AND body->>'key' IN ('registration_open','registration_invite_required') ORDER BY id FOR SHARE`)
	if e != nil {
		return false, false, e
	}
	defer rows.Close()
	open, invite := false, false
	for rows.Next() {
		var k, v string
		if e = rows.Scan(&k, &v); e != nil {
			return false, false, e
		}
		if v != "true" && v != "false" {
			return false, false, ErrInvalid
		}
		if k == "registration_open" {
			open = v == "true"
		} else {
			invite = v == "true"
		}
	}
	return open, invite, rows.Err()
}
func limit(ctx context.Context, tx pgx.Tx, key []byte, seconds, max int) error {
	var n int
	e := tx.QueryRow(ctx, `INSERT INTO enrollment_limits(key_hash,window_start,attempts) VALUES($1,statement_timestamp(),1)
	 ON CONFLICT(key_hash) DO UPDATE SET
	 attempts=CASE WHEN enrollment_limits.window_start<=statement_timestamp()-($2::integer*interval '1 second') THEN 1 ELSE LEAST(enrollment_limits.attempts+1,$3::integer+1) END,
	 window_start=CASE WHEN enrollment_limits.window_start<=statement_timestamp()-($2::integer*interval '1 second') THEN statement_timestamp() ELSE enrollment_limits.window_start END
	 RETURNING attempts`, key, seconds, max).Scan(&n)
	if e != nil {
		return e
	}
	if n > max {
		return ErrLimited
	}
	return nil
}
func commitRejection(ctx context.Context, tx pgx.Tx, e error) error {
	if c := tx.Commit(ctx); c != nil {
		return c
	}
	return e
}

func (s *Service) Issue(ctx context.Context, in VerificationRequest) (Challenge, error) {
	var out Challenge
	email, source, e := normalize(in.Email, in.Source)
	if e != nil || in.Purpose != "register" && in.Purpose != "recover" {
		return out, ErrInvalid
	}
	if s.sender == nil {
		return out, ErrUnavailable
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if in.Purpose == "register" {
		open, _, e := policy(ctx, tx)
		if e != nil {
			return out, e
		}
		if !open {
			return out, ErrClosed
		}
	}
	// All replicas lock source first, then mailbox. Both budgets include failed
	// delivery attempts, so transport failure cannot enable a send storm.
	if e = limit(ctx, tx, s.digest("send-source", source), 86400, 11); e != nil {
		return out, commitRejection(ctx, tx, e)
	}
	if e = limit(ctx, tx, s.digest("send-email", email), 86400, 7); e != nil {
		return out, commitRejection(ctx, tx, e)
	}
	eh := s.digest("email", email)
	var recent bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM enrollment_challenges WHERE email_hash=$1 AND created_at>statement_timestamp()-interval '60 seconds')", eh).Scan(&recent); e != nil {
		return out, e
	}
	if recent {
		return out, commitRejection(ctx, tx, ErrLimited)
	}
	out.ID, e = token()
	if e != nil {
		return out, e
	}
	n, e := rand.Int(rand.Reader, big.NewInt(9000000))
	if e != nil {
		return out, e
	}
	code := strconv.FormatInt(n.Int64()+1000000, 10)
	var target *int64
	var targetVersion *int64
	if in.Purpose == "recover" {
		var id, version int64
		e = tx.QueryRow(ctx, "SELECT e.merchant_id,u.auth_version FROM enrollment_emails e JOIN console_users u ON u.username=e.username AND u.role='merchant' AND u.subject_id=e.merchant_id WHERE e.email_hash=$1", eh).Scan(&id, &version)
		if e == nil {
			target = &id
			targetVersion = &version
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return out, e
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE enrollment_challenges SET state='failed' WHERE email_hash=$1 AND purpose=$2 AND consumed_at IS NULL AND state IN ('sending','sent')", eh, in.Purpose); e != nil {
		return out, e
	}
	e = tx.QueryRow(ctx, `INSERT INTO enrollment_challenges(id,purpose,email_hash,source_hash,code_hash,target_merchant_id,target_auth_version,state,expires_at)
	 VALUES($1,$2,$3,$4,$5,$6,$7,'sending',statement_timestamp()+interval '10 minutes') RETURNING expires_at`, out.ID, in.Purpose, eh, s.digest("source", source), s.digest("code", out.ID, in.Purpose, email, code), target, targetVersion).Scan(&out.ExpiresAt)
	if e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	deliveryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	e = s.sender.SendVerification(deliveryCtx, email, in.Purpose, code)
	cancel()
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer finishCancel()
	if e != nil {
		s.pool.Exec(finishCtx, "UPDATE enrollment_challenges SET state='failed' WHERE id=$1 AND state='sending'", out.ID)
		return Challenge{}, ErrUnavailable
	}
	tag, e := s.pool.Exec(finishCtx, "UPDATE enrollment_challenges SET state='sent' WHERE id=$1 AND state='sending' AND expires_at>statement_timestamp()", out.ID)
	if e != nil || tag.RowsAffected() != 1 {
		return Challenge{}, ErrUnavailable
	}
	return out, nil
}

type recoveryTarget struct{ MerchantID, AuthVersion int64 }

func (s *Service) verify(ctx context.Context, tx pgx.Tx, purpose, id, email, source, code string) (*recoveryTarget, error) {
	if e := limit(ctx, tx, s.digest("verify-source", source), 900, 100); e != nil {
		return nil, e
	}
	if e := limit(ctx, tx, s.digest("verify-email", email), 900, 30); e != nil {
		return nil, e
	}
	if len(id) != 43 {
		return nil, ErrCode
	}
	var eh, sh, ch []byte
	var storedPurpose, state string
	var attempts int
	var valid bool
	var target *int64
	var targetVersion *int64
	e := tx.QueryRow(ctx, `SELECT purpose,email_hash,source_hash,code_hash,state,attempts,consumed_at IS NULL,target_merchant_id,target_auth_version FROM enrollment_challenges WHERE id=$1 FOR UPDATE`, id).Scan(&storedPurpose, &eh, &sh, &ch, &state, &attempts, &valid, &target, &targetVersion)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, ErrCode
	}
	if e != nil {
		return nil, e
	}
	if !valid || state != "sent" || attempts >= 5 {
		return nil, ErrCode
	}
	// statement_timestamp belongs to the start of the locking statement, which
	// can precede a long row-lock wait. Check the database wall clock in a new
	// statement only after ownership of the challenge row has been acquired.
	if e = tx.QueryRow(ctx, "SELECT expires_at>clock_timestamp() FROM enrollment_challenges WHERE id=$1", id).Scan(&valid); e != nil {
		return nil, e
	}
	if !valid {
		return nil, ErrCode
	}
	if storedPurpose != purpose || !hmac.Equal(eh, s.digest("email", email)) || !hmac.Equal(sh, s.digest("source", source)) || !hmac.Equal(ch, s.digest("code", id, purpose, email, code)) {
		_, e = tx.Exec(ctx, "UPDATE enrollment_challenges SET attempts=attempts+1 WHERE id=$1", id)
		if e != nil {
			return nil, e
		}
		return nil, ErrCode
	}
	if target != nil && targetVersion != nil {
		return &recoveryTarget{*target, *targetVersion}, nil
	}
	return nil, nil
}

func consumeProof(ctx context.Context, tx pgx.Tx, id string) error {
	tag, e := tx.Exec(ctx, `UPDATE enrollment_challenges SET consumed_at=clock_timestamp()
	 WHERE id=$1 AND state='sent' AND consumed_at IS NULL AND attempts<5 AND expires_at>clock_timestamp()`, id)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrCode
	}
	return nil
}

// A late expiry must undo business work, but retain the outer transaction's
// verification limits. An invalid code before this point is already committed
// by commitRejection; no failed-attempt count is undone to undo business writes.
func rejectExpiredWork(ctx context.Context, work, outer pgx.Tx, e error) error {
	if rollbackErr := work.Rollback(ctx); rollbackErr != nil {
		return rollbackErr
	}
	return commitRejection(ctx, outer, e)
}

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.@-]{3,100}$`)
var passwordSlots = make(chan struct{}, 2)

func passwordHash(password string) (string, error) {
	select {
	case passwordSlots <- struct{}{}:
		defer func() { <-passwordSlots }()
	default:
		return "", ErrLimited
	}
	h, e := identity.HashPassword(password)
	if e != nil {
		return "", ErrInvalid
	}
	return h, nil
}
func mapConflict(e error) error {
	var p *pgconn.PgError
	if errors.As(e, &p) && p.Code == "23505" {
		return ErrConflict
	}
	return e
}
func (s *Service) Register(ctx context.Context, in RegisterInput) (Registered, error) {
	var out Registered
	email, source, e := normalize(in.Email, in.Source)
	if e != nil || !usernamePattern.MatchString(in.Username) || len(in.Name) == 0 || len(in.Name) > 100 || len(in.Password) < 12 || len(in.Password) > 1024 || in.Password == email || len(in.InviteCode) > 64 || len(in.PromotionToken) > 128 {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	open, required, e := policy(ctx, tx)
	if e != nil {
		return out, e
	}
	if !open {
		return out, ErrClosed
	}
	payStatus, e := registrationPayStatus(ctx, tx)
	if e != nil {
		return out, e
	}
	if _, e = s.verify(ctx, tx, "register", in.ChallengeID, email, source, in.Code); e != nil {
		if errors.Is(e, ErrCode) || errors.Is(e, ErrLimited) {
			return out, commitRejection(ctx, tx, e)
		}
		return out, e
	}
	outer := tx
	tx, e = outer.Begin(ctx)
	if e != nil {
		return Registered{}, e
	}
	defer tx.Rollback(ctx)
	hash, e := passwordHash(in.Password)
	if e != nil {
		return out, e
	}
	// console_subject_seq is shared with administrator creation. Skip explicit
	// legacy merchant IDs without recycling an existing financial identity.
	for {
		if e = tx.QueryRow(ctx, "SELECT nextval('console_subject_seq')").Scan(&out.MerchantID); e != nil {
			return out, e
		}
		var exists bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM merchants WHERE id=$1)", out.MerchantID).Scan(&exists); e != nil {
			return out, e
		}
		if !exists {
			break
		}
	}
	key, e := token()
	if e != nil {
		return out, e
	}
	if e = s.store.CreateMerchantInTx(ctx, tx, payment.Merchant{ID: out.MerchantID, Name: in.Name, APIKey: key}); e != nil {
		return Registered{}, mapConflict(e)
	}
	if payStatus == 2 {
		if _, e = tx.Exec(ctx, "UPDATE merchants SET pay=2 WHERE id=$1", out.MerchantID); e != nil {
			return Registered{}, e
		}
	}
	out.PayStatus = strconv.Itoa(payStatus)
	if _, e = tx.Exec(ctx, "INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES($1,'merchant',$2,$3,$4)", in.Username, out.MerchantID, in.Name, hash); e != nil {
		return Registered{}, mapConflict(e)
	}
	if _, e = tx.Exec(ctx, "INSERT INTO enrollment_emails(email,email_hash,merchant_id,username) VALUES($1,$2,$3,$4)", email, s.digest("email", email), out.MerchantID, in.Username); e != nil {
		return Registered{}, mapConflict(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE merchants SET profile=profile||jsonb_build_object('email',$2::text) WHERE id=$1", out.MerchantID, email); e != nil {
		return Registered{}, e
	}
	if required && in.InviteCode == "" {
		return Registered{}, ErrInvalid
	}
	if in.InviteCode != "" {
		var id, max int64
		var enabled bool
		e = tx.QueryRow(ctx, "SELECT id,COALESCE((body->>'max_uses')::bigint,0),COALESCE((body->>'enabled')::boolean,false) FROM backoffice_records WHERE resource='invitation-codes' AND body->>'code'=$1 FOR UPDATE", in.InviteCode).Scan(&id, &max, &enabled)
		if errors.Is(e, pgx.ErrNoRows) || e == nil && (!enabled || max < 1 || max > 1000000) {
			return Registered{}, ErrInvalid
		}
		if e != nil {
			return Registered{}, e
		}
		var used int64
		if e = tx.QueryRow(ctx, "SELECT count(*) FROM enrollment_invitation_uses WHERE invitation_id=$1", id).Scan(&used); e != nil {
			return Registered{}, e
		}
		if used >= max {
			return Registered{}, ErrInvalid
		}
		if _, e = tx.Exec(ctx, "INSERT INTO enrollment_invitation_uses(invitation_id,merchant_id) VALUES($1,$2)", id, out.MerchantID); e != nil {
			return Registered{}, e
		}
	}
	if e = consumeProof(ctx, tx, in.ChallengeID); e != nil {
		if errors.Is(e, ErrCode) {
			return Registered{}, rejectExpiredWork(ctx, tx, outer, e)
		}
		return Registered{}, e
	}
	var registrationAuditID int64
	if out.PayStatus == "2" && s.store.AdminBusinessSourceReady(ctx, tx) {
		e = tx.QueryRow(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes,business_notice_scene,business_notice_transaction) VALUES('public',$1,'','enrollment',$1,'register',jsonb_build_object('pay',$2::text),'regaudit',pg_current_xact_id()) RETURNING id`, out.MerchantID, out.PayStatus).Scan(&registrationAuditID)
	} else {
		_, e = tx.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('public',$1,'','enrollment',$1,'register',jsonb_build_object('pay',$2::text))", out.MerchantID, out.PayStatus)
	}
	if e != nil {
		return Registered{}, e
	}
	if in.PromotionToken != "" {
		if _, e = s.store.BindReferralAtRegistrationTx(ctx, tx, out.MerchantID, in.PromotionToken, in.ChallengeID); e != nil {
			if errors.Is(e, payment.ErrInvalid) || errors.Is(e, payment.ErrNotFound) || errors.Is(e, payment.ErrConflict) || errors.Is(e, payment.ErrState) {
				return Registered{}, ErrInvalid
			}
			return Registered{}, e
		}
	}
	out.Username = in.Username
	if registrationAuditID > 0 {
		s.store.RecordAdminBusinessRegistration(ctx, tx, registrationAuditID)
	}
	if e = tx.Commit(ctx); e != nil {
		return Registered{}, e
	}
	if e = outer.Commit(ctx); e != nil {
		return Registered{}, e
	}
	return out, nil
}
func (s *Service) Recover(ctx context.Context, in RecoverInput) error {
	email, source, e := normalize(in.Email, in.Source)
	if e != nil || len(in.Password) < 12 || len(in.Password) > 1024 || in.Password == email {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	target, e := s.verify(ctx, tx, "recover", in.ChallengeID, email, source, in.Code)
	if e != nil {
		if errors.Is(e, ErrCode) || errors.Is(e, ErrLimited) {
			return commitRejection(ctx, tx, e)
		}
		return e
	}
	outer := tx
	tx, e = outer.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	// Derive even for unknown identities. No registration/profile field is used
	// as authorization, and there is never an implicit login or MFA removal.
	hash, e := passwordHash(in.Password)
	if e != nil {
		return e
	}
	if target != nil {
		var username string
		e = tx.QueryRow(ctx, `SELECT u.username FROM enrollment_emails e JOIN console_users u ON u.username=e.username AND u.role='merchant' AND u.subject_id=e.merchant_id JOIN merchants m ON m.id=e.merchant_id
		 WHERE e.email_hash=$1 AND e.merchant_id=$2 AND u.auth_version=$3 AND u.enabled AND m.enabled FOR UPDATE OF u`, s.digest("email", email), target.MerchantID, target.AuthVersion).Scan(&username)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if e == nil {
			if _, e = tx.Exec(ctx, "UPDATE console_users SET password_hash=$2,auth_version=auth_version+1 WHERE username=$1 AND role='merchant'", username, hash); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, "DELETE FROM console_sessions WHERE role='merchant' AND subject_id=$1", target.MerchantID); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation) VALUES('public',$1,'','enrollment',$1,'password-recovery')", target.MerchantID); e != nil {
				return e
			}
		}
	}
	if e = consumeProof(ctx, tx, in.ChallengeID); e != nil {
		if errors.Is(e, ErrCode) {
			return rejectExpiredWork(ctx, tx, outer, e)
		}
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	return outer.Commit(ctx)
}
