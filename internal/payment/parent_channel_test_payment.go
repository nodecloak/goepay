package payment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type ParentPaymentTestInput struct {
	RequestID, Name, Expected string
	Amount                    int64
}
type ParentPaymentTest struct {
	Version                                                                                int
	ActorID, ChannelID, MerchantID, Amount                                                 int64
	ActorName, RequestID, OutTradeNo, Name, Kind, Expected, Origin, CreatedAt, ScopeMarker string
}

func (t ParentPaymentTest) Marker() string { return t.ScopeMarker }
func (s *Store) parentPaymentTestMarker(actor, mid int64, key, no, name string, amount int64) (string, error) {
	proof, e := s.referralDigest("parentchannel180/test-marker", []any{actor, mid, key, no, name, amount})
	if e != nil {
		return "", e
	}
	return fmt.Sprintf("parent-test:%d:%s:%x", actor, key, proof), nil
}
func validParentTestName(s string) bool {
	if len(s) == 0 || len(s) > 256 || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (s *Store) parentTestSchema(ctx context.Context, q fundsReader) error {
	ready, e := s.parentPaymentSchema(ctx, q)
	if e != nil {
		return e
	}
	var table bool
	if e = q.QueryRow(ctx, `SELECT to_regclass(current_schema()||'.runtime_parent_payment_tests') IS NOT NULL`).Scan(&table); e != nil {
		return e
	}
	if !ready || !table {
		return ErrConflict
	}
	return nil
}
func (s *Store) readParentTest(ctx context.Context, q fundsReader, actor ParentChannelActor, key string) (ParentPaymentTest, error) {
	var out ParentPaymentTest
	if e := s.parentTestSchema(ctx, q); e != nil {
		return out, e
	}
	var channel, mid int64
	var no string
	var raw, proof []byte
	e := q.QueryRow(ctx, `SELECT channel_id,merchant_id,out_trade_no,facts,proof_hash FROM runtime_parent_payment_tests WHERE actor_id=$1 AND request_key=$2`, actor.User.ID, key).Scan(&channel, &mid, &no, &raw, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		var originated bool
		if e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='runtime-parent-payment-tests' AND changes->>'request_id'=$1 AND changes->>'actor_id'=$2) OR EXISTS(SELECT 1 FROM orders WHERE param LIKE $3)`, key, fmt.Sprint(actor.User.ID), fmt.Sprintf("parent-test:%d:%s:%%", actor.User.ID, key)).Scan(&originated); e != nil {
			return out, e
		}
		if originated {
			return out, ErrConflict
		}
		return out, ErrNotFound
	}
	if e != nil {
		return out, e
	}
	if len(raw) > 32768 || parentFactsJSON(raw, &out) != nil {
		return out, ErrConflict
	}
	want, e := s.referralDigest("parentchannel180/payment-test", out)
	if e != nil || !hmac.Equal(want, proof) || out.Version != 1 || out.ActorID != actor.User.ID || out.ActorName != actor.User.Username || out.RequestID != key || out.ChannelID != channel || out.MerchantID != mid || out.OutTradeNo != no || !parentRequestID(key) || out.Amount <= 0 {
		return out, ErrConflict
	}
	marker, e := s.parentPaymentTestMarker(out.ActorID, out.MerchantID, out.RequestID, out.OutTradeNo, out.Name, out.Amount)
	if e != nil || !hmac.Equal([]byte(marker), []byte(out.ScopeMarker)) {
		return out, ErrConflict
	}
	var audits int
	if e = q.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='runtime-parent-payment-tests' AND operation='runtime-parent-payment-test' AND resource_id=$1 AND changes->>'request_id'=$2 AND changes->>'actor_id'=$3 AND changes->>'out_trade_no'=$4`, channel, key, fmt.Sprint(actor.User.ID), no).Scan(&audits); e != nil {
		return out, e
	}
	if audits != 1 {
		return out, ErrConflict
	}
	return out, nil
}
func (s *Store) parentTestMerchant(ctx context.Context, q fundsReader) (int64, error) {
	var raw string
	e := q.QueryRow(ctx, `SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='test_pay_uid' ORDER BY id DESC LIMIT 1`).Scan(&raw)
	if e != nil {
		return 0, ErrState
	}
	mid, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || mid <= 0 || fmt.Sprint(mid) != raw {
		return 0, ErrState
	}
	return mid, nil
}
func (g *ProviderFlow) checkParentTestTarget(ctx context.Context, tx pgx.Tx, actor ParentChannelActor, t ParentPaymentTest, operator func(int64, int64) bool) error {
	if !g.paymentWritesEnabled() {
		return ErrLiveDisabled
	}
	if e := actor.check(ctx, tx); e != nil {
		return e
	}
	mid, e := g.Store.parentTestMerchant(ctx, tx)
	if e != nil || mid != t.MerchantID {
		return ErrState
	}
	var enabled bool
	if e = tx.QueryRow(ctx, `SELECT enabled FROM merchants WHERE id=$1 FOR SHARE`, mid).Scan(&enabled); e != nil || !enabled {
		return ErrState
	}
	if e = parentChannelLock(ctx, tx, t.ChannelID); e != nil {
		return e
	}
	h, e := g.Store.readParentHead(ctx, tx, t.ChannelID)
	if e != nil {
		return e
	}
	a, e := g.Store.readParentAccount(ctx, tx, h.id, h.version)
	if e != nil {
		return e
	}
	if h.kind != t.Kind {
		return ErrConflict
	}
	factory := g.ParentFactories[h.source]
	if factory == nil || factory.PayType() != h.kind {
		return ErrConflict
	}
	p, _, e := g.Store.runtimePolicyTx(ctx, tx, h.fee, false)
	if e != nil {
		return e
	}
	view, e := g.Store.parentView(h, a, factory, p)
	if e != nil {
		return e
	}
	if !hmac.Equal([]byte(t.Expected), []byte(view.Fingerprint)) {
		return ErrConflict
	}
	return g.Store.checkParentRuntimeWriteTx(ctx, tx, &parentExecutionBinding{account: a, merchant: mid, live: func() bool { return g.paymentWritesEnabled() }, authorized: func() bool { return operator != nil && operator(mid, h.source) }}, mid, t.ChannelID)
}
func (g *ProviderFlow) CreateParentPaymentTest(ctx context.Context, actor ParentChannelActor, channel int64, in ParentPaymentTestInput, origin string, operator func(int64, int64) bool) (ParentPaymentTest, bool, error) {
	var out ParentPaymentTest
	in.Name = strings.TrimSpace(in.Name)
	if g == nil || g.Store == nil || channel <= 0 || !parentRequestID(in.RequestID) || !validParentTestName(in.Name) || in.Amount <= 0 || len(in.Expected) != 64 || !validProviderOrigin(origin, false) || operator == nil {
		return out, false, ErrInvalid
	}
	tx, e := g.Store.pool.Begin(ctx)
	if e != nil {
		return out, false, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("goepay/parent-test/%d/%s", actor.User.ID, in.RequestID)); e != nil {
		return out, false, e
	}
	if e = actor.check(ctx, tx); e != nil {
		return out, false, e
	}
	out, e = g.Store.readParentTest(ctx, tx, actor, in.RequestID)
	if e == nil {
		if out.ChannelID != channel || out.Name != in.Name || out.Amount != in.Amount || out.Expected != in.Expected {
			return out, false, ErrConflict
		}
		if e = actor.check(ctx, tx); e != nil {
			return out, false, e
		}
		return out, false, tx.Commit(ctx)
	}
	if !errors.Is(e, ErrNotFound) {
		return out, false, e
	}
	mid, e := g.Store.parentTestMerchant(ctx, tx)
	if e != nil {
		return out, false, e
	}
	var created time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&created); e != nil {
		return out, false, e
	}
	nonce := make([]byte, 16)
	if _, e = rand.Read(nonce); e != nil {
		return out, false, e
	}
	head, e := g.Store.readParentHead(ctx, tx, channel)
	if e != nil {
		return out, false, e
	}
	out = ParentPaymentTest{Version: 1, ActorID: actor.User.ID, ActorName: actor.User.Username, ChannelID: channel, MerchantID: mid, RequestID: in.RequestID, OutTradeNo: "PT" + hex.EncodeToString(nonce), Name: in.Name, Kind: head.kind, Amount: in.Amount, Expected: in.Expected, Origin: strings.TrimRight(origin, "/"), CreatedAt: created.UTC().Format(time.RFC3339Nano)}
	out.ScopeMarker, e = g.Store.parentPaymentTestMarker(out.ActorID, out.MerchantID, out.RequestID, out.OutTradeNo, out.Name, out.Amount)
	if e != nil {
		return out, false, e
	}
	if e = g.checkParentTestTarget(ctx, tx, actor, out, operator); e != nil {
		return out, false, e
	}
	proof, e := g.Store.referralDigest("parentchannel180/payment-test", out)
	if e != nil {
		return out, false, e
	}
	raw, e := json.Marshal(out)
	if e != nil {
		return out, false, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO runtime_parent_payment_tests(actor_id,request_key,channel_id,merchant_id,out_trade_no,facts,proof_hash)VALUES($1,$2,$3,$4,$5,$6::jsonb,$7)`, out.ActorID, out.RequestID, out.ChannelID, out.MerchantID, out.OutTradeNo, string(raw), proof); e != nil {
		return out, false, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('admin',$1,$2,'runtime-parent-payment-tests',$3,'runtime-parent-payment-test',jsonb_build_object('request_id',$4::text,'actor_id',$5::text,'out_trade_no',$6::text))`, actor.User.ID, actor.User.Username, channel, out.RequestID, fmt.Sprint(actor.User.ID), out.OutTradeNo); e != nil {
		return out, false, e
	}
	if e = g.checkParentTestTarget(ctx, tx, actor, out, operator); e != nil {
		return out, false, e
	}
	return out, true, tx.Commit(ctx)
}
func (s *Store) ReadParentPaymentTest(ctx context.Context, actor ParentChannelActor, channel int64, key string) (ParentPaymentTest, error) {
	var out ParentPaymentTest
	if !parentRequestID(key) || channel <= 0 {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = actor.check(ctx, tx); e != nil {
		return out, e
	}
	out, e = s.readParentTest(ctx, tx, actor, key)
	if e != nil {
		return out, e
	}
	if out.ChannelID != channel {
		return out, ErrNotFound
	}
	if e = actor.check(ctx, tx); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

type parentTestContextKey struct{}
type parentTestFirstSendCheck func(context.Context, pgx.Tx, Order) error

func (g *ProviderFlow) WithParentPaymentTest(ctx context.Context, actor ParentChannelActor, t ParentPaymentTest, operator func(int64, int64) bool) context.Context {
	return context.WithValue(ctx, parentTestContextKey{}, parentTestFirstSendCheck(func(ctx context.Context, tx pgx.Tx, o Order) error {
		if o.MerchantID != t.MerchantID || o.OutTradeNo != t.OutTradeNo || o.Amount != t.Amount || o.Name != t.Name || o.Param != t.Marker() {
			return ErrConflict
		}
		original, e := g.Store.readParentTest(ctx, tx, actor, t.RequestID)
		if e != nil {
			return e
		}
		if original.OutTradeNo != t.OutTradeNo {
			return ErrConflict
		}
		return g.checkParentTestTarget(ctx, tx, actor, t, operator)
	}))
}
func (s *Store) parentTestFirstSend(ctx context.Context, tx pgx.Tx, o Order) error {
	if check, ok := ctx.Value(parentTestContextKey{}).(parentTestFirstSendCheck); ok {
		return check(ctx, tx, o)
	}
	// A durable keyed purpose marker remains authenticated by the original
	// order/fee birth proof, even if the console registry is later lost. Ordinary
	// merchant APIs cannot replace the test's original administrative admission.
	parts := strings.Split(o.Param, ":")
	if len(parts) != 4 || parts[0] != "parent-test" || !parentRequestID(parts[2]) {
		return nil
	}
	actor, e := strconv.ParseInt(parts[1], 10, 64)
	if e != nil || actor <= 0 || fmt.Sprint(actor) != parts[1] {
		return nil
	}
	marker, e := s.parentPaymentTestMarker(actor, o.MerchantID, parts[2], o.OutTradeNo, o.Name, o.Amount)
	if e != nil {
		return e
	}
	if hmac.Equal([]byte(marker), []byte(o.Param)) {
		return ErrConflict
	}
	return nil
}
