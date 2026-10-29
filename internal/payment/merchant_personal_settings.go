package payment

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"goepay/internal/ledger"
)

var personalQQPattern = regexp.MustCompile(`^[1-9][0-9]{4,19}$`)
var personalAmountPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]{1,2})?$`)

type MerchantPersonalSettings struct {
	MerchantID  int64  `json:"merchant_id,string"`
	QQ          string `json:"qq"`
	Minimum     string `json:"pay_minmoney"`
	Maximum     string `json:"pay_maxmoney"`
	RemainMoney string `json:"remain_money"`
	BoundsValid bool   `json:"bounds_valid"`
	Fingerprint string `json:"fingerprint"`
	CanUpdate   bool   `json:"can_update"`
}

// validRemainMoney accepts the original remain_money forms: "" (none),
// "N%" with 0<N<100, or a fixed yuan amount. Shared by read-side state
// validation and the merchant-side update.
func validRemainMoney(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	if len(s) > 20 {
		return false
	}
	numeric := strings.TrimSpace(strings.TrimSuffix(s, "%"))
	n, e := ledger.Parse(numeric)
	if e != nil || n < 0 {
		return false
	}
	if strings.HasSuffix(s, "%") {
		return n > 0 && n < 10000
	}
	return true
}

func personalAmount(raw string, strict bool) (string, int64, error) {
	if raw == "" {
		return "", 0, nil
	}
	if len(raw) > 21 || strict && !personalAmountPattern.MatchString(raw) {
		return "", 0, ErrInvalid
	}
	n, e := ledger.Parse(raw)
	if e != nil {
		return "", 0, ErrInvalid
	}
	return ledger.Format(n), n, nil
}

func (s *Store) personalSettingsTx(ctx context.Context, tx pgx.Tx, a MerchantRSAActor, write bool) (MerchantPersonalSettings, error) {
	var out MerchantPersonalSettings
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	var raw []byte
	var revision int64
	var enabled bool
	e := tx.QueryRow(ctx, "SELECT id,profile,config_revision,enabled FROM merchants WHERE id=$1"+lock, a.User.ID).Scan(&out.MerchantID, &raw, &revision, &enabled)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, identity.ErrOriginalMerchantSession
	}
	if e != nil {
		return out, e
	}
	if !enabled || revision <= 0 {
		return out, identity.ErrOriginalMerchantSession
	}
	if e = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); e != nil {
		return out, e
	}
	var profile map[string]json.RawMessage
	if json.Unmarshal(raw, &profile) != nil || profile == nil {
		return out, ErrState
	}
	text := func(key string) (string, error) {
		v, ok := profile[key]
		if !ok || string(v) == "null" {
			return "", nil
		}
		var s string
		if json.Unmarshal(v, &s) != nil {
			return "", ErrState
		}
		return s, nil
	}
	if out.QQ, e = text("qq"); e != nil {
		return out, e
	}
	if out.QQ != "" && !personalQQPattern.MatchString(out.QQ) {
		return out, ErrState
	}
	minimum, e := text("pay_minmoney")
	if e != nil {
		return out, e
	}
	maximum, e := text("pay_maxmoney")
	if e != nil {
		return out, e
	}
	var min, max int64
	if out.Minimum, min, e = personalAmount(minimum, false); e != nil {
		return out, ErrState
	}
	if out.Maximum, max, e = personalAmount(maximum, false); e != nil {
		return out, ErrState
	}
	out.BoundsValid = max == 0 || min <= max
	if out.RemainMoney, e = text("remain_money"); e != nil {
		return out, e
	}
	if !validRemainMoney(out.RemainMoney) {
		return out, ErrState
	}
	// 结算留存 joins the fingerprint only when set, so merchants without one
	// keep the fingerprint they had before the field existed.
	fact := []any{out.MerchantID, revision, enabled, profile["qq"], profile["pay_minmoney"], profile["pay_maxmoney"]}
	if out.RemainMoney != "" {
		fact = append(fact, out.RemainMoney)
	}
	digest, e := s.referralDigest("merchant-personal172/v1", fact)
	if e != nil {
		return out, e
	}
	out.Fingerprint = hex.EncodeToString(digest)
	out.CanUpdate = true
	return out, nil
}

func (s *Store) MerchantPersonalSettings(ctx context.Context, a MerchantRSAActor) (MerchantPersonalSettings, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return MerchantPersonalSettings{}, e
	}
	defer tx.Rollback(ctx)
	out, e := s.personalSettingsTx(ctx, tx, a, false)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

// UpdateMerchantPersonalSettings writes the merchant's own settings. A nil
// remainMoney keeps the stored 结算留存 (callers of the earlier four-field
// contract never touch it).
func (s *Store) UpdateMerchantPersonalSettings(ctx context.Context, a MerchantRSAActor, qq, minimum, maximum string, remainMoney *string, expected string) (MerchantPersonalSettings, error) {
	var empty MerchantPersonalSettings
	qq = strings.TrimSpace(qq)
	minimum = strings.TrimSpace(minimum)
	maximum = strings.TrimSpace(maximum)
	if qq != "" && !personalQQPattern.MatchString(qq) {
		return empty, ErrInvalid
	}
	if remainMoney != nil {
		trimmed := strings.TrimSpace(*remainMoney)
		if !validRemainMoney(trimmed) {
			return empty, ErrInvalid
		}
		remainMoney = &trimmed
	}
	minText, min, e := personalAmount(minimum, true)
	if e != nil {
		return empty, e
	}
	maxText, max, e := personalAmount(maximum, true)
	if e != nil {
		return empty, e
	}
	if max > 0 && min > max {
		return empty, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	state, e := s.personalSettingsTx(ctx, tx, a, true)
	if e != nil {
		return empty, e
	}
	if len(expected) != 64 || subtle.ConstantTimeCompare([]byte(expected), []byte(state.Fingerprint)) != 1 {
		return empty, ErrConflict
	}
	fields := `{"fields":["qq","pay_minmoney","pay_maxmoney"]}`
	if remainMoney == nil {
		_, e = tx.Exec(ctx, `UPDATE merchants SET profile=profile||jsonb_build_object('qq',$2::text,'pay_minmoney',$3::text,'pay_maxmoney',$4::text),config_revision=config_revision+1 WHERE id=$1`, a.User.ID, qq, minText, maxText)
	} else {
		fields = `{"fields":["qq","pay_minmoney","pay_maxmoney","remain_money"]}`
		_, e = tx.Exec(ctx, `UPDATE merchants SET profile=profile||jsonb_build_object('qq',$2::text,'pay_minmoney',$3::text,'pay_maxmoney',$4::text,'remain_money',$5::text),config_revision=config_revision+1 WHERE id=$1`, a.User.ID, qq, minText, maxText, *remainMoney)
	}
	if e != nil {
		return empty, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('merchant',$1,$2,'merchants',$1,'personal-settings-update',$3::jsonb)`, a.User.ID, a.User.Username, fields); e != nil {
		return empty, e
	}
	state, e = s.personalSettingsTx(ctx, tx, a, true)
	if e != nil {
		return empty, e
	}
	if e = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); e != nil {
		return empty, e
	}
	if e = tx.Commit(ctx); e != nil {
		return empty, e
	}
	return state, nil
}
