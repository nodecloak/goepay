// Package verifycode134 issues and verifies SMS/email verification codes on the
// verification_codes134 table, enforcing the exact limits of
// epay_release/includes/lib/VerifyCode.php:
//   - at most one send per recipient per 60 seconds,
//   - at most 3 sends per phone per 24h (SMS) / 7 per email,
//   - at most 6 sends per IP per 24h (SMS) / 11 per email,
//   - a code expires 3600s after issue, is void once status>0 (consumed),
//     and is refused after 5 wrong attempts; a wrong attempt increments errcount.
//
// The issuance is persisted BEFORE the SMS/email send (the caller sends after a
// successful Issue), so the append-only row is the record of what was issued.
package verifycode134

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrTooSoon  = errors.New("两次发送之间需要相隔60秒")
	ErrRecipLim = errors.New("该接收方发送次数过多")
	ErrIPLim    = errors.New("你今天发送次数过多，请稍后再试")
	ErrNoCode   = errors.New("请重新获取验证码")
	ErrExpired  = errors.New("验证码已失效，请重新获取")
	ErrWrong    = errors.New("验证码不正确")
	ErrSource   = errors.New("verification code source cannot be confirmed")
)

// Scene is reg/login/find/bind. Type is 0 email, 1 phone.
type Issue struct {
	UID       int64
	Scene     string
	Type      int
	Recipient string
	IP        string
}

type Limits struct {
	RecipientPerDay int
	IPPerDay        int
}

// SMSLimits / EmailLimits match VerifyCode.php constants.
var SMSLimits = Limits{RecipientPerDay: 3, IPPerDay: 6}
var EmailLimits = Limits{RecipientPerDay: 7, IPPerDay: 11}

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func randomCode(digits int) (string, error) {
	// phone: 6 digits (111111..999999 in the original); email: 7 digits.
	lo, hi := int64(111111), int64(999999)
	if digits == 7 {
		lo, hi = 1111111, 9999999
	}
	n, e := rand.Int(rand.Reader, big.NewInt(hi-lo+1))
	if e != nil {
		return "", e
	}
	v := lo + n.Int64()
	return bigToDec(v), nil
}
func bigToDec(v int64) string { return new(big.Int).SetInt64(v).String() }

func limitsFor(typ int) (Limits, int) {
	if typ == 1 {
		return SMSLimits, 6
	}
	return EmailLimits, 7
}

func validScene(s string) bool {
	switch s {
	case "reg", "login", "find", "bind":
		return true
	}
	return false
}

// Issue checks the per-recipient 60s gap and the daily recipient/IP caps, then
// inserts a fresh code and returns it for the caller to send. It never sends.
func (s *Store) Issue(ctx context.Context, in Issue) (string, error) {
	if s == nil || s.pool == nil {
		return "", ErrSource
	}
	if !validScene(in.Scene) || (in.Type != 0 && in.Type != 1) || in.Recipient == "" || len(in.Recipient) > 254 {
		return "", ErrSource
	}
	limits, digits := limitsFor(in.Type)
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return "", ErrSource
	}
	defer tx.Rollback(ctx)
	// Serialize issuance for this recipient so concurrent sends cannot both pass
	// the 60s/day checks (advisory lock keyed on recipient hash).
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "verifycode134:"+in.Recipient); e != nil {
		return "", ErrSource
	}
	var within60, recipDay, ipDay int
	if e = tx.QueryRow(ctx,
		`SELECT
		  (SELECT count(*) FROM verification_codes134 WHERE recipient=$1 AND created_at>clock_timestamp()-interval '60 seconds'),
		  (SELECT count(*) FROM verification_codes134 WHERE recipient=$1 AND created_at>clock_timestamp()-interval '24 hours'),
		  (SELECT count(*) FROM verification_codes134 WHERE ip=$2 AND ip<>'' AND created_at>clock_timestamp()-interval '24 hours')`,
		in.Recipient, in.IP).Scan(&within60, &recipDay, &ipDay); e != nil {
		return "", ErrSource
	}
	if within60 > 0 {
		return "", ErrTooSoon
	}
	if recipDay >= limits.RecipientPerDay {
		return "", ErrRecipLim
	}
	if in.IP != "" && ipDay >= limits.IPPerDay {
		return "", ErrIPLim
	}
	code, e := randomCode(digits)
	if e != nil {
		return "", ErrSource
	}
	if _, e = tx.Exec(ctx,
		`INSERT INTO verification_codes134(uid,scene,type,recipient,code,ip)VALUES($1,$2,$3,$4,$5,$6)`,
		in.UID, in.Scene, in.Type, in.Recipient, code, in.IP); e != nil {
		return "", ErrSource
	}
	if e = tx.Commit(ctx); e != nil {
		return "", ErrSource
	}
	return code, nil
}

// IssueWithSender matches the original order in VerifyCode::send_code: it checks
// the 60s gap and daily caps and generates the code while holding the
// per-recipient lock, calls send(code) (the SMS provider), and persists the row
// ONLY when send returns nil — so a failed send neither stores a live code nor
// counts against the caps, exactly as the original inserts after a successful
// send_sms. send runs inside the lock so concurrent issuances serialize.
func (s *Store) IssueWithSender(ctx context.Context, in Issue, send func(code string) error) error {
	if s == nil || s.pool == nil {
		return ErrSource
	}
	if !validScene(in.Scene) || (in.Type != 0 && in.Type != 1) || in.Recipient == "" || len(in.Recipient) > 254 {
		return ErrSource
	}
	limits, digits := limitsFor(in.Type)
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return ErrSource
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "verifycode134:"+in.Recipient); e != nil {
		return ErrSource
	}
	var within60, recipDay, ipDay int
	if e = tx.QueryRow(ctx,
		`SELECT
		  (SELECT count(*) FROM verification_codes134 WHERE recipient=$1 AND created_at>clock_timestamp()-interval '60 seconds'),
		  (SELECT count(*) FROM verification_codes134 WHERE recipient=$1 AND created_at>clock_timestamp()-interval '24 hours'),
		  (SELECT count(*) FROM verification_codes134 WHERE ip=$2 AND ip<>'' AND created_at>clock_timestamp()-interval '24 hours')`,
		in.Recipient, in.IP).Scan(&within60, &recipDay, &ipDay); e != nil {
		return ErrSource
	}
	if within60 > 0 {
		return ErrTooSoon
	}
	if recipDay >= limits.RecipientPerDay {
		return ErrRecipLim
	}
	if in.IP != "" && ipDay >= limits.IPPerDay {
		return ErrIPLim
	}
	code, e := randomCode(digits)
	if e != nil {
		return ErrSource
	}
	if e = send(code); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx,
		`INSERT INTO verification_codes134(uid,scene,type,recipient,code,ip)VALUES($1,$2,$3,$4,$5,$6)`,
		in.UID, in.Scene, in.Type, in.Recipient, code, in.IP); e != nil {
		return ErrSource
	}
	return tx.Commit(ctx)
}

// Verify checks the latest live code for (scene,type,recipient[,uid]): expiry
// 3600s, status 0, errcount<5, exact match. A wrong code increments errcount; a
// correct code consumes it (status=1). Returns nil on success.
func (s *Store) Verify(ctx context.Context, scene string, typ int, recipient, code string, uid int64) error {
	if s == nil || s.pool == nil {
		return ErrSource
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return ErrSource
	}
	defer tx.Rollback(ctx)
	e = s.VerifyTx(ctx, tx, scene, typ, recipient, code, uid)
	if e != nil {
		// A wrong code still persists its errcount increment, committed here;
		// every other failure rolls back (no state to keep).
		if errors.Is(e, ErrWrong) {
			if c := tx.Commit(ctx); c != nil {
				return ErrSource
			}
		}
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return ErrSource
	}
	return nil
}

// VerifyTx runs the same check as Verify but inside the CALLER's transaction, so
// consuming the code (status 0->1) is atomic with the caller's account create or
// password reset — a code cannot be spent unless that business write commits,
// and the write cannot commit twice on the same code. The caller owns the tx:
// VerifyTx never commits or rolls back.
//
// Consume-once / anti-replay is unchanged from Verify: the latest live code for
// (scene,type,recipient[,uid]) is taken FOR UPDATE; a consumed (status>0),
// expired (>3600s) or over-tried (errcount>=5) code is refused; a wrong code
// returns ErrWrong and increments errcount — the caller commits that increment
// (as Verify does) even though it then aborts its business work, so repeated
// wrong attempts still exhaust the 5-try cap.
func (s *Store) VerifyTx(ctx context.Context, tx pgx.Tx, scene string, typ int, recipient, code string, uid int64) error {
	if s == nil || tx == nil {
		return ErrSource
	}
	if !validScene(scene) || (typ != 0 && typ != 1) {
		return ErrSource
	}
	var id int64
	var stored string
	var status, errcount int
	var expired bool
	q := `SELECT id,code,status,errcount,created_at<clock_timestamp()-interval '3600 seconds'
	      FROM verification_codes134 WHERE scene=$1 AND type=$2 AND recipient=$3`
	args := []any{scene, typ, recipient}
	if uid > 0 {
		q += ` AND uid=$4`
		args = append(args, uid)
	}
	q += ` ORDER BY id DESC LIMIT 1 FOR UPDATE`
	e := tx.QueryRow(ctx, q, args...).Scan(&id, &stored, &status, &errcount, &expired)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrNoCode
	}
	if e != nil {
		return ErrSource
	}
	if expired || status > 0 || errcount >= 5 {
		return ErrExpired
	}
	if stored != code {
		if _, e = tx.Exec(ctx, `UPDATE verification_codes134 SET errcount=errcount+1 WHERE id=$1`, id); e != nil {
			return ErrSource
		}
		return ErrWrong
	}
	if _, e = tx.Exec(ctx, `UPDATE verification_codes134 SET status=1 WHERE id=$1`, id); e != nil {
		return ErrSource
	}
	return nil
}
