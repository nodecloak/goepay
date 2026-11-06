package merchantnotice

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"goepay/internal/noticesource"
)

type supplementSource231 = noticesource.Source

// The confirmation's template and channel are authorized at birth. Configuring
// templates later cannot turn an old package into a new sending opportunity.
func (s *Store) WechatTransferReady231(ctx context.Context, tx pgx.Tx) (string, error) {
	var count int
	var raw []byte
	e := tx.QueryRow(ctx, `SELECT count(*),COALESCE(min((body->'value')::text),'"1"') FROM backoffice_records WHERE resource='settings' AND body->>'key'='wxnotice'`).Scan(&count, &raw)
	var enabled string
	if e != nil {
		return "", e
	}
	if count > 1 || json.Unmarshal(raw, &enabled) != nil || (enabled != "0" && enabled != "1") {
		return "", ErrConflict
	}
	if enabled == "0" {
		return "", nil
	}
	cfg, e := s.wechatTemplateSettings(ctx, tx, "wxtransfer")
	if e != nil {
		if e == ErrState {
			return "", nil
		}
		return "", e
	}
	if len(cfg.Fields) == 0 {
		return "", nil
	}
	seen := map[string]bool{}
	for k, v := range cfg.Fields {
		known, e := ValidatePlatformSetting(k, v)
		if !known || e != nil || !templateField80.MatchString(v) || seen[v] {
			return "", ErrConflict
		}
		seen[v] = true
	}
	return confirmationTemplateHash231(cfg), nil
}
func confirmationTemplateHash231(cfg WechatTemplateConfig) string {
	raw, _ := json.Marshal([]any{cfg.ID, cfg.Fields})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// All old renderers retain their contract; only the template-only confirmation
// scene uses RenderWechat.
func renderSource231(in Input) (Message, error) {
	if in.Scene == "wxtransfer" {
		return RenderWechat(in)
	}
	return Render(in)
}
func (s *Store) CaptureSupplement231(ctx context.Context, tx pgx.Tx, source noticesource.Source) error {
	if !s.available() || tx == nil || source.Facts.MerchantID <= 0 || (source.Facts.Kind != "complain" && source.Facts.Kind != "wxtransfer") {
		return ErrInvalid
	}
	if e := noticesource.Verify(ctx, tx, source); e != nil {
		return e
	}
	var current string
	if e := tx.QueryRow(ctx, "SELECT pg_current_xact_id()::text").Scan(&current); e != nil {
		return e
	}
	if current != source.Transaction {
		return ErrConflict
	}
	active, _, e := noticeMerchant(ctx, tx, source.Facts.MerchantID, false)
	if e != nil {
		return e
	}
	in := Input{Scene: source.Facts.Kind, At: source.At, Params: source.Facts.Params}
	return s.appendEvent(ctx, tx, eventRecord{MerchantID: source.Facts.MerchantID, Scene: source.Facts.Kind, SupplementID: source.ID}, eventData{Version: 1, Supplement: &source, Input: in}, active)
}
func (s *Store) verifySupplement231(ctx context.Context, tx pgx.Tx, r eventRecord, d eventData) error {
	if r.SupplementID <= 0 || d.Supplement == nil || r.OrderID != 0 || r.LoginAuditID != 0 || r.SettlementID != 0 || r.TimerID != 0 || r.DomainAuditID != 0 || d.Domain != nil || d.Timer != nil || d.Login != nil || d.Settlement != nil || d.Source != (paidEventSource{}) {
		return ErrConflict
	}
	p := d.Supplement
	if p.ID != r.SupplementID || p.Transaction != r.Transaction || p.Facts.MerchantID != r.MerchantID || p.Facts.Kind != r.Scene || !p.At.Equal(d.Input.At) {
		return ErrConflict
	}
	a, _ := json.Marshal(p.Facts.Params)
	b, _ := json.Marshal(d.Input.Params)
	if !hmac.Equal(a, b) {
		return ErrConflict
	}
	if e := noticesource.Verify(ctx, tx, *p); e != nil {
		return ErrConflict
	}
	return nil
}
