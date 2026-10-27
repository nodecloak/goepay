package payment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
)

// Private facts. HTTP callers receive a separate, fixed projection, never these
// commitments, session digests or the keyed order-purpose marker.
type MerchantScanIntent106 struct {
	Version                                                   int
	MerchantID, Amount                                        int64
	RequestID, OutTradeNo, Name, Kind, ClientIP               string
	CodeDigest, SessionDigest, PolicyFingerprint, ScopeMarker string
	CreatedAt, ExpiresAt                                      time.Time
}
type MerchantScanInput106 struct {
	RequestID, Remark, Kind, ClientIP, AuthCode string
	Amount                                      int64
}

func scanRequestID106(key string) bool {
	return len(key) == 36 && key[8] == '-' && key[13] == '-' && key[18] == '-' && key[23] == '-' && parentRequestID(strings.ReplaceAll(key, "-", ""))
}

type merchantScanOrder106 struct {
	Version                                                  int
	MerchantID, OrderID, ChannelID, Amount, Charge, Net, Fee int64
	RequestID, TradeNo, IntentCommitment                     string
}

func (t MerchantScanIntent106) Marker() string { return t.ScopeMarker }
func (s *Store) scanSessionDigest106(a MerchantRSAActor) (string, error) {
	p, e := s.referralDigest("merchant-scan106/session", []any{a.User.Username, a.User.ID, a.User.AuthVersion, a.Token, a.CSRF})
	return hex.EncodeToString(p), e
}
func (s *Store) scanCodeDigest106(mid int64, key, code string) (string, error) {
	p, e := s.referralDigest("merchant-scan106/code", []any{mid, key, code})
	return hex.EncodeToString(p), e
}
func (s *Store) scanMarker106(t MerchantScanIntent106) (string, error) {
	p, e := s.referralDigest("merchant-scan106/marker", []any{t.MerchantID, t.RequestID, t.OutTradeNo, t.Kind, t.Name, t.Amount})
	return "merchant-scan:" + t.RequestID + ":" + hex.EncodeToString(p), e
}
func (s *Store) scanMerchant106(ctx context.Context, tx pgx.Tx, a MerchantRSAActor, write bool) error {
	var enabled bool
	if e := tx.QueryRow(ctx, `SELECT enabled AND pay=1 FROM merchants WHERE id=$1 FOR SHARE`, a.User.ID).Scan(&enabled); e != nil {
		return ErrState
	}
	if e := identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); e != nil {
		return e
	}
	if write && !enabled {
		return ErrState
	}
	return nil
}
func (s *Store) scanAdmission106(ctx context.Context, tx pgx.Tx, a MerchantRSAActor, t MerchantScanIntent106) error {
	if e := s.scanMerchant106(ctx, tx, a, true); e != nil {
		return e
	}
	policy, e := s.merchantScanPolicyTx106(ctx, tx, false)
	if e != nil {
		return e
	}
	view, e := s.scanPolicyView106(policy)
	if e != nil {
		return e
	}
	proof, e := s.referralDigest("merchant-scan106/intent", t)
	if e != nil {
		return e
	}
	closed, e := s.readScanClosure106(ctx, tx, t, proof)
	if e != nil {
		return e
	}
	if closed {
		return ErrState
	}
	session, e := s.scanSessionDigest106(a)
	if e != nil {
		return e
	}
	if view.Open != "1" || !hmac.Equal([]byte(view.Fingerprint), []byte(t.PolicyFingerprint)) || !hmac.Equal([]byte(session), []byte(t.SessionDigest)) {
		return ErrConflict
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return e
	}
	if !now.Before(t.ExpiresAt) {
		return ErrState
	}
	return identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF)
}
func (s *Store) readScanIntent106(ctx context.Context, q fundsReader, mid int64, key string) (MerchantScanIntent106, []byte, error) {
	var out MerchantScanIntent106
	if e := s.merchantScanSchema106(ctx, q); e != nil {
		return out, nil, e
	}
	var raw, proof []byte
	var no string
	e := q.QueryRow(ctx, `SELECT out_trade_no,facts,proof_hash FROM merchant_scan_intents106 WHERE merchant_id=$1 AND request_id=$2::uuid`, mid, key).Scan(&no, &raw, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		var originated bool
		if e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='merchant-scan-intents106' AND actor_id=$1 AND changes->>'request_id'=$2) OR EXISTS(SELECT 1 FROM orders WHERE merchant_id=$1 AND param LIKE $3)`, mid, key, "merchant-scan:"+key+":%").Scan(&originated); e != nil {
			return out, nil, e
		}
		if originated {
			return out, nil, ErrConflict
		}
		return out, nil, ErrNotFound
	}
	if e != nil {
		return out, nil, e
	}
	if len(raw) > 16384 || parentFactsJSON(raw, &out) != nil {
		return out, nil, ErrConflict
	}
	want, e := s.referralDigest("merchant-scan106/intent", out)
	marker, me := s.scanMarker106(out)
	if e != nil || me != nil || !hmac.Equal(want, proof) || out.Version != 1 || out.MerchantID != mid || out.RequestID != key || out.OutTradeNo != no || out.ScopeMarker != marker || !out.ExpiresAt.Equal(out.CreatedAt.Add(5*time.Minute)) {
		return out, nil, ErrConflict
	}
	var audits int
	if e = q.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='merchant-scan-intents106' AND operation='scan-prepare' AND actor_id=$1 AND changes->>'request_id'=$2 AND changes->>'out_trade_no'=$3 AND changes->>'commitment'=$4`, mid, key, no, scanCommitment106(proof)).Scan(&audits); e != nil {
		return out, nil, e
	}
	if audits != 1 {
		return out, nil, ErrConflict
	}
	return out, proof, nil
}

func (s *Store) DeclareMerchantScan106(ctx context.Context, a MerchantRSAActor, in MerchantScanInput106) (MerchantScanIntent106, error) {
	var out MerchantScanIntent106
	if !scanRequestID106(in.RequestID) || in.Amount <= 0 || in.Amount > 99999999 || (in.Kind != "wxpay" && in.Kind != "alipay") || len(in.Remark) > 256 || !utf8.ValidString(in.Remark) || in.AuthCode == "" {
		return out, ErrInvalid
	}
	for _, r := range in.Remark {
		if unicode.IsControl(r) {
			return out, ErrInvalid
		}
	}
	name := in.Remark
	if name == "" {
		name = "付款码收款"
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("merchant-scan106/%d/%s", a.User.ID, in.RequestID)); e != nil {
		return out, e
	}
	if e = s.scanMerchant106(ctx, tx, a, true); e != nil {
		return out, e
	}
	digest, e := s.scanCodeDigest106(a.User.ID, in.RequestID, in.AuthCode)
	if e != nil {
		return out, e
	}
	out, _, e = s.readScanIntent106(ctx, tx, a.User.ID, in.RequestID)
	if e == nil {
		if out.Amount != in.Amount || out.Name != name || out.Kind != in.Kind || !hmac.Equal([]byte(out.CodeDigest), []byte(digest)) {
			return out, ErrConflict
		}
		if e = s.scanAdmission106(ctx, tx, a, out); e != nil {
			return out, e
		}
		return out, tx.Commit(ctx)
	}
	if !errors.Is(e, ErrNotFound) {
		return out, e
	}
	closed, e := s.readScanClosure106(ctx, tx, MerchantScanIntent106{MerchantID: a.User.ID, RequestID: in.RequestID}, nil)
	if e != nil {
		return out, e
	}
	if closed {
		return out, ErrState
	}
	policy, e := s.merchantScanPolicyTx106(ctx, tx, false)
	if e != nil {
		return out, e
	}
	view, e := s.scanPolicyView106(policy)
	if e != nil {
		return out, e
	}
	session, e := s.scanSessionDigest106(a)
	if e != nil {
		return out, e
	}
	nonce := make([]byte, 16)
	if _, e = rand.Read(nonce); e != nil {
		return out, e
	}
	var at time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		return out, e
	}
	out = MerchantScanIntent106{Version: 1, MerchantID: a.User.ID, RequestID: in.RequestID, OutTradeNo: "MS" + hex.EncodeToString(nonce), Name: name, Kind: in.Kind, Amount: in.Amount, ClientIP: in.ClientIP, CodeDigest: digest, SessionDigest: session, PolicyFingerprint: view.Fingerprint, CreatedAt: at.UTC(), ExpiresAt: at.UTC().Add(5 * time.Minute)}
	out.ScopeMarker, e = s.scanMarker106(out)
	if e != nil {
		return out, e
	}
	proof, e := s.referralDigest("merchant-scan106/intent", out)
	if e != nil {
		return out, e
	}
	raw, e := json.Marshal(out)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO merchant_scan_intents106(merchant_id,request_id,out_trade_no,facts,proof_hash)VALUES($1,$2::uuid,$3,$4::jsonb,$5)`, out.MerchantID, out.RequestID, out.OutTradeNo, string(raw), proof); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('merchant',$1,$2,'merchant-scan-intents106',$1,'scan-prepare',jsonb_build_object('request_id',$3::text,'out_trade_no',$4::text,'commitment',$5::text))`, a.User.ID, a.User.Username, out.RequestID, out.OutTradeNo, scanCommitment106(proof)); e != nil {
		return out, e
	}
	if e = s.scanAdmission106(ctx, tx, a, out); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

func (s *Store) scanOrderFacts106(ctx context.Context, q pgx.Tx, t MerchantScanIntent106, o Order, channel int64, proof []byte) (merchantScanOrder106, error) {
	if o.MerchantID != t.MerchantID || o.OutTradeNo != t.OutTradeNo || o.Amount != t.Amount || o.Name != t.Name || o.Type != t.Kind || o.Param != t.Marker() || o.NotifyURL != "" || o.ReturnURL != "" || channel <= 0 {
		return merchantScanOrder106{}, ErrConflict
	}
	c, _, e := s.authenticatedOrderCharge104(ctx, q, o)
	if e != nil {
		return merchantScanOrder106{}, e
	}
	return merchantScanOrder106{Version: 1, MerchantID: t.MerchantID, OrderID: o.ID, ChannelID: channel, Amount: o.Amount, Charge: c, Net: o.MerchantAmount, Fee: o.PlatformFee, RequestID: t.RequestID, TradeNo: o.TradeNo, IntentCommitment: scanCommitment106(proof)}, nil
}
func (s *Store) readScanOrder106(ctx context.Context, q pgx.Tx, t MerchantScanIntent106, o Order, proof []byte) (merchantScanOrder106, error) {
	var facts merchantScanOrder106
	var raw, mac []byte
	var mid int64
	var key string
	e := q.QueryRow(ctx, `SELECT merchant_id,request_id::text,facts,proof_hash FROM merchant_scan_order_sources106 WHERE order_id=$1`, o.ID).Scan(&mid, &key, &raw, &mac)
	if errors.Is(e, pgx.ErrNoRows) {
		return facts, ErrConflict
	}
	if e != nil {
		return facts, e
	}
	if parentFactsJSON(raw, &facts) != nil || len(raw) > 16384 || mid != t.MerchantID || key != t.RequestID {
		return facts, ErrConflict
	}
	expected, e := s.scanOrderFacts106(ctx, q, t, o, facts.ChannelID, proof)
	if e != nil {
		return facts, e
	}
	want, e := s.referralDigest("merchant-scan106/order", expected)
	if e != nil || expected != facts || !hmac.Equal(want, mac) {
		return facts, ErrConflict
	}
	var audits int
	if e = q.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='merchant-scan-orders106' AND resource_id=$1 AND actor_id=$2 AND changes->>'request_id'=$3 AND changes->>'commitment'=$4`, o.ID, t.MerchantID, t.RequestID, scanCommitment106(mac)).Scan(&audits); e != nil {
		return facts, e
	}
	if audits != 1 {
		return facts, ErrConflict
	}
	return facts, nil
}
func (s *Store) BindMerchantScanOrder106(ctx context.Context, a MerchantRSAActor, key string, channel int64) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	t, proof, e := s.readScanIntent106(ctx, tx, a.User.ID, key)
	if e != nil {
		return e
	}
	o, e := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM orders WHERE merchant_id=$1 AND out_trade_no=$2 FOR UPDATE`, t.MerchantID, t.OutTradeNo))
	if e != nil {
		return e
	}
	facts, e := s.scanOrderFacts106(ctx, tx, t, o, channel, proof)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(facts)
	if e != nil {
		return e
	}
	mac, e := s.referralDigest("merchant-scan106/order", facts)
	if e != nil {
		return e
	}
	var exists bool
	var audits int
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM merchant_scan_order_sources106 WHERE order_id=$1),(SELECT count(*)FROM backoffice_audit WHERE resource='merchant-scan-orders106' AND resource_id=$1)`, o.ID).Scan(&exists, &audits); e != nil {
		return e
	}
	if !exists && audits != 0 {
		return ErrConflict
	}
	if !exists {
		if _, e = tx.Exec(ctx, `INSERT INTO merchant_scan_order_sources106(order_id,merchant_id,request_id,facts,proof_hash)VALUES($1,$2,$3::uuid,$4::jsonb,$5)`, o.ID, t.MerchantID, key, string(raw), mac); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('merchant',$1,$2,'merchant-scan-orders106',$3,'scan-bind',jsonb_build_object('request_id',$4::text,'commitment',$5::text))`, a.User.ID, a.User.Username, o.ID, key, scanCommitment106(mac)); e != nil {
			return e
		}
	}
	stored, e := s.readScanOrder106(ctx, tx, t, o, proof)
	if e != nil {
		return e
	}
	if stored != facts {
		return ErrConflict
	}
	if e = s.scanAdmission106(ctx, tx, a, t); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

type MerchantScanState106 struct {
	Intent             MerchantScanIntent106
	Order              Order
	ChannelID, Charge  int64
	State              string
	CanApply, CanQuery bool
}

func (s *Store) ReadMerchantScan106(ctx context.Context, a MerchantRSAActor, key string) (MerchantScanState106, error) {
	var out MerchantScanState106
	if !scanRequestID106(key) {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = s.scanMerchant106(ctx, tx, a, false); e != nil {
		return out, e
	}
	t, proof, e := s.readScanIntent106(ctx, tx, a.User.ID, key)
	if errors.Is(e, ErrNotFound) {
		t = MerchantScanIntent106{MerchantID: a.User.ID, RequestID: key}
		closed, ce := s.readScanClosure106(ctx, tx, t, nil)
		if ce != nil {
			return out, ce
		}
		if !closed {
			return out, ErrNotFound
		}
		out.Intent = t
		out.State = "closed"
		if ce = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); ce != nil {
			return out, ce
		}
		return out, tx.Commit(ctx)
	}
	if e != nil {
		return out, e
	}
	out.Intent = t
	out.State = "unprepared"
	closed, e := s.readScanClosure106(ctx, tx, t, proof)
	if e != nil {
		return out, e
	}
	if closed {
		out.State = "closed"
		if e = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); e != nil {
			return out, e
		}
		return out, tx.Commit(ctx)
	}
	o, e := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM orders WHERE merchant_id=$1 AND out_trade_no=$2`, t.MerchantID, t.OutTradeNo))
	if e != nil && !errors.Is(e, ErrNotFound) {
		return out, e
	}
	if e == nil {
		facts, e := s.readScanOrder106(ctx, tx, t, o, proof)
		if e != nil {
			return out, e
		}
		out.Order = o
		out.ChannelID = facts.ChannelID
		out.Charge = facts.Charge
		out.State = o.Status
		if o.Status == "created" {
			out.State = "prepared"
			out.CanApply = s.scanAdmission106(ctx, tx, a, t) == nil
		}
		var exists bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_attempts WHERE order_id=$1 AND channel_id=$2 AND method='scan')`, o.ID, facts.ChannelID).Scan(&exists); e != nil {
			return out, e
		}
		out.CanQuery = exists
	}
	if e = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

type merchantScanContext106 struct{}
type merchantScanSend106 struct {
	actor   MerchantRSAActor
	intent  MerchantScanIntent106
	allowed func(int64, int64) bool
}

func (s *Store) WithMerchantScan106(ctx context.Context, a MerchantRSAActor, t MerchantScanIntent106, allowed func(int64, int64) bool) context.Context {
	return context.WithValue(ctx, merchantScanContext106{}, merchantScanSend106{a, t, allowed})
}
func (s *Store) merchantScanFirstSend106(ctx context.Context, tx pgx.Tx, o Order) error {
	parts := strings.Split(o.Param, ":")
	if len(parts) != 3 || parts[0] != "merchant-scan" || !scanRequestID106(parts[1]) {
		return nil
	}
	marker, e := s.scanMarker106(MerchantScanIntent106{MerchantID: o.MerchantID, RequestID: parts[1], OutTradeNo: o.OutTradeNo, Kind: o.Type, Name: o.Name, Amount: o.Amount})
	if e != nil {
		return e
	}
	if !hmac.Equal([]byte(marker), []byte(o.Param)) {
		return nil
	} // ordinary caller params remain compatible
	send, ok := ctx.Value(merchantScanContext106{}).(merchantScanSend106)
	if !ok {
		return ErrConflict
	}
	t, proof, e := s.readScanIntent106(ctx, tx, o.MerchantID, parts[1])
	if e != nil {
		return e
	}
	if t != send.intent || send.actor.User.ID != o.MerchantID {
		return ErrConflict
	}
	facts, e := s.readScanOrder106(ctx, tx, t, o, proof)
	if e != nil {
		return e
	}
	if send.allowed == nil || !send.allowed(o.MerchantID, facts.ChannelID) {
		return ErrLiveDisabled
	}
	return s.scanAdmission106(ctx, tx, send.actor, t)
}
func (s *Store) merchantScanBeforeIO106(ctx context.Context, o Order) error {
	if !strings.HasPrefix(o.Param, "merchant-scan:") {
		return nil
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = s.merchantScanFirstSend106(ctx, tx, o); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) CheckMerchantScanCode106(t MerchantScanIntent106, code string) error {
	digest, e := s.scanCodeDigest106(t.MerchantID, t.RequestID, code)
	if e != nil {
		return e
	}
	if !hmac.Equal([]byte(digest), []byte(t.CodeDigest)) {
		return ErrConflict
	}
	return nil
}

// Closing serializes on the same order row as first-send ownership. This only
// abandons a preparation that was never sent; upstream cancellation is separate.
func (s *Store) CloseMerchantScan106(ctx context.Context, a MerchantRSAActor, key string) error {
	if !scanRequestID106(key) {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("merchant-scan106/%d/%s", a.User.ID, key)); e != nil {
		return e
	}
	t, proof, e := s.readScanIntent106(ctx, tx, a.User.ID, key)
	if errors.Is(e, ErrNotFound) {
		t = MerchantScanIntent106{MerchantID: a.User.ID, RequestID: key}
		proof = nil
	} else if e != nil {
		return e
	}
	o, e := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM orders WHERE merchant_id=$1 AND out_trade_no=$2 FOR UPDATE`, a.User.ID, t.OutTradeNo))
	if e != nil && !errors.Is(e, ErrNotFound) {
		return e
	}
	if e = s.scanMerchant106(ctx, tx, a, false); e != nil {
		return e
	}
	closed, e := s.readScanClosure106(ctx, tx, t, proof)
	if e != nil {
		return e
	}
	if closed {
		return tx.Commit(ctx)
	}
	if o.ID != 0 {
		if o.MerchantID != t.MerchantID || o.OutTradeNo != t.OutTradeNo || o.Amount != t.Amount || o.Name != t.Name || o.Type != t.Kind || o.Param != t.Marker() {
			return ErrConflict
		}
		if _, _, e = s.authenticatedOrderCharge104(ctx, tx, o); e != nil {
			return e
		}
		var sent bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_attempts WHERE order_id=$1)`, o.ID).Scan(&sent); e != nil {
			return e
		}
		if sent || (o.Status != "created" && o.Status != "closed") {
			return ErrState
		}
		if _, e = tx.Exec(ctx, `UPDATE orders SET status='closed' WHERE id=$1`, o.ID); e != nil {
			return e
		}
	}
	if e = s.writeScanClosure106(ctx, tx, a, t, proof); e != nil {
		return e
	}
	if e = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
