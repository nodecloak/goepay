package enrollment

import (
	"context"
	"errors"
	"regexp"
	"strconv"

	"github.com/jackc/pgx/v5"
	"goepay/internal/payment"
	"goepay/internal/verifycode134"
)

// Phone-verified enrollment (original user/reg.php and findpwd.php, VerifyCode
// type 1). The code lifecycle + anti-replay lives in verifycode134 on
// verification_codes134; here VerifyTx CONSUMES the code once inside the SAME
// transaction as the account create / password reset, so a code can be spent
// only if that business write commits and never twice. The whole flow is gated
// on the SMS gateway being enabled (sms_gateway_head134.status=1); when it is
// off (the default) RegisterByPhone/RecoverByPhone refuse with ErrClosed and the
// email-only paths and tests are untouched. No default password is ever set.

type RegisterPhoneInput struct {
	Code, Phone, Source, Username, Name, Password, InviteCode, PromotionToken string
}
type RecoverPhoneInput struct{ Code, Phone, Source, Password string }

var phonePattern = regexp.MustCompile(`^[0-9]{5,20}$`)

func normalizePhone(phone, source string) (string, string, error) {
	if !phonePattern.MatchString(phone) {
		return "", "", ErrInvalid
	}
	ip := parseSourceIP(source)
	if ip == "" {
		return "", "", ErrInvalid
	}
	return phone, ip, nil
}

func parseSourceIP(source string) string {
	if source == "" {
		return ""
	}
	// normalize() already validated email-path IPs; reuse the same net parse.
	_, ip, e := normalize("a@b.co", source)
	if e != nil {
		return ""
	}
	return ip
}

// RegisterByPhone mirrors Register but establishes the identity with a phone
// code (reg scene, type 1). phone_hash is UNIQUE in enrollment_phones134, so a
// phone already registered is refused with ErrConflict exactly as reg.php does
// ("该手机号码已被注册").
func (s *Service) RegisterByPhone(ctx context.Context, in RegisterPhoneInput) (Registered, error) {
	var out Registered
	phone, source, e := normalizePhone(in.Phone, in.Source)
	if e != nil || !usernamePattern.MatchString(in.Username) || len(in.Name) == 0 || len(in.Name) > 100 || len(in.Password) < 12 || len(in.Password) > 1024 || in.Password == phone || len(in.InviteCode) > 64 || len(in.PromotionToken) > 128 || len(in.Code) < 4 || len(in.Code) > 8 {
		return out, ErrInvalid
	}
	outer, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer outer.Rollback(ctx)
	smsOn, e := smsGatewayOn(ctx, outer)
	if e != nil {
		return out, e
	}
	if !smsOn {
		return out, ErrClosed
	}
	open, required, e := policy(ctx, outer)
	if e != nil {
		return out, e
	}
	if !open {
		return out, ErrClosed
	}
	// This is the phone registration path; "email" (verifytype=0) refuses
	// before any account is created. No restriction ("") or "phone" proceeds.
	vt, e := verifyType(ctx, outer)
	if e != nil {
		return out, e
	}
	if vt == "email" {
		return out, ErrClosed
	}
	payStatus, e := registrationPayStatus(ctx, outer)
	if e != nil {
		return out, e
	}
	// Source verify-attempt budget, committed on rejection like the email path.
	if e = limit(ctx, outer, s.digest("verify-source-sms", source), 900, 100); e != nil {
		return out, commitRejection(ctx, outer, e)
	}
	codes := verifycode134.New(s.pool)
	// Business work + code consume in ONE inner transaction.
	tx, e := outer.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	// Consume FIRST so the inner tx holds only the errcount increment if the
	// code is wrong; a wrong code is then committed (errcount persists) and the
	// business work below never runs.
	if e = codes.VerifyTx(ctx, tx, "reg", 1, phone, in.Code, 0); e != nil {
		if errors.Is(e, verifycode134.ErrWrong) {
			if c := tx.Commit(ctx); c != nil {
				return out, c
			}
			return out, commitRejection(ctx, outer, ErrCode)
		}
		if errors.Is(e, verifycode134.ErrNoCode) || errors.Is(e, verifycode134.ErrExpired) {
			return out, commitRejection(ctx, outer, ErrCode)
		}
		return out, e
	}
	hash, e := passwordHash(in.Password)
	if e != nil {
		return out, e
	}
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
	// The phone claim is one-to-one: a UNIQUE phone_hash collision is "already
	// registered", surfaced as ErrConflict like the original.
	if _, e = tx.Exec(ctx, "INSERT INTO enrollment_phones134(phone,phone_hash,merchant_id,username) VALUES($1,$2,$3,$4)", phone, s.digest("phone", phone), out.MerchantID, in.Username); e != nil {
		return Registered{}, mapConflict(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE merchants SET profile=profile||jsonb_build_object('phone',$2::text) WHERE id=$1", out.MerchantID, phone); e != nil {
		return Registered{}, e
	}
	if required && in.InviteCode == "" {
		return Registered{}, ErrInvalid
	}
	if in.InviteCode != "" {
		var id, max int64
		var enabled bool
		e = tx.QueryRow(ctx, "SELECT id,COALESCE((body->>'max_uses')::bigint,0),COALESCE((body->>'enabled')::boolean,false) AND body->>'archived' IS DISTINCT FROM 'true' FROM backoffice_records WHERE resource='invitation-codes' AND body->>'code'=$1 FOR UPDATE", in.InviteCode).Scan(&id, &max, &enabled)
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
		if _, e = s.store.BindReferralAtRegistrationTx(ctx, tx, out.MerchantID, in.PromotionToken, "sms:"+phone); e != nil {
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

// RecoverByPhone mirrors Recover: a phone code (find scene, type 1) resets the
// password of the account whose verified phone matches, once. Consuming the code
// is atomic with the reset. An unknown phone still consumes the code and derives
// a hash (no enumeration, no implicit login, no MFA removal).
func (s *Service) RecoverByPhone(ctx context.Context, in RecoverPhoneInput) error {
	phone, source, e := normalizePhone(in.Phone, in.Source)
	if e != nil || len(in.Password) < 12 || len(in.Password) > 1024 || in.Password == phone || len(in.Code) < 4 || len(in.Code) > 8 {
		return ErrInvalid
	}
	outer, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer outer.Rollback(ctx)
	smsOn, e := smsGatewayOn(ctx, outer)
	if e != nil {
		return e
	}
	if !smsOn {
		return ErrClosed
	}
	if e = limit(ctx, outer, s.digest("verify-source-sms", source), 900, 100); e != nil {
		return commitRejection(ctx, outer, e)
	}
	codes := verifycode134.New(s.pool)
	tx, e := outer.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = codes.VerifyTx(ctx, tx, "find", 1, phone, in.Code, 0); e != nil {
		if errors.Is(e, verifycode134.ErrWrong) {
			if c := tx.Commit(ctx); c != nil {
				return c
			}
			return commitRejection(ctx, outer, ErrCode)
		}
		if errors.Is(e, verifycode134.ErrNoCode) || errors.Is(e, verifycode134.ErrExpired) {
			return commitRejection(ctx, outer, ErrCode)
		}
		return e
	}
	hash, e := passwordHash(in.Password)
	if e != nil {
		return e
	}
	var merchantID, authVersion int64
	var username string
	e = tx.QueryRow(ctx, `SELECT p.merchant_id,u.username,u.auth_version FROM enrollment_phones134 p
	 JOIN console_users u ON u.username=p.username AND u.role='merchant' AND u.subject_id=p.merchant_id
	 JOIN merchants m ON m.id=p.merchant_id
	 WHERE p.phone_hash=$1 AND u.enabled AND m.enabled FOR UPDATE OF u`, s.digest("phone", phone)).Scan(&merchantID, &username, &authVersion)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if e == nil {
		if _, e = tx.Exec(ctx, "UPDATE console_users SET password_hash=$2,auth_version=auth_version+1 WHERE username=$1 AND role='merchant'", username, hash); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "DELETE FROM console_sessions WHERE role='merchant' AND subject_id=$1", merchantID); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation) VALUES('public',$1,'','enrollment',$1,'password-recovery')", merchantID); e != nil {
			return e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	return outer.Commit(ctx)
}
