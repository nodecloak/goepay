package merchantnotice

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/businesssms231"
)

type paidEventSource struct {
	OrderID, MerchantID, ChannelID                   int64
	TradeNo, OutTradeNo, Name, Type, ProviderTradeNo string
	Amount, MerchantAmount, PlatformFee              int64
	PaidAt                                           time.Time
}
type eventRecord struct {
	JobID                               int64
	ID, MerchantID, OrderID, ProfileID  int64
	LoginAuditID                        int64
	SettlementID                        int64
	TimerID                             int64
	DomainAuditID                       int64
	SupplementID                        int64
	Scene                               string
	ProfileHash, Cipher, BodyHash, Hash []byte
	SnapshotState, Transaction          string
	CreatedAt                           time.Time
}
type eventData struct {
	TelegramStamp231 string                   `json:",omitempty"`
	SMS231           *businesssms231.Snapshot `json:",omitempty"`
	Supplement       *supplementSource231     `json:",omitempty"`
	VoiceManaged230  bool                     `json:",omitempty"`
	VoiceRevision230 int64                    `json:",omitempty"`
	Version          int
	Source           paidEventSource
	Input            Input
	Routes           []Transport
	Login            *loginEventSource
	Settlement       *settlementEventSource
	Timer            *timerSource
	Domain           *domainEventSource
}

func eventAAD(r eventRecord) []byte {
	raw, _ := json.Marshal([]any{"notice-event80/v1", r.ID, r.MerchantID, r.OrderID, r.LoginAuditID, r.SettlementID, r.TimerID, r.DomainAuditID, r.JobID, r.Scene, r.ProfileID, hex.EncodeToString(r.ProfileHash), r.SnapshotState, r.Transaction, r.CreatedAt.UTC().Format(time.RFC3339Nano), hex.EncodeToString(r.BodyHash)})
	if r.SupplementID > 0 {
		extra, _ := json.Marshal(r.SupplementID)
		raw = append(raw, extra...)
	}
	return raw
}
func (s *Store) eventProof(r eventRecord) []byte {
	sum := sha256.Sum256(r.Cipher)
	return s.digest("source-event", append(eventAAD(r), sum[:]...))
}
func eventAudit(r eventRecord) []byte {
	raw, _ := json.Marshal(map[string]string{"event_hash": hex.EncodeToString(r.Hash), "order_id": fmt.Sprint(r.OrderID), "login_audit_id": fmt.Sprint(r.LoginAuditID), "settlement_id": fmt.Sprint(r.SettlementID), "timer_id": fmt.Sprint(r.TimerID), "domain_audit_id": fmt.Sprint(r.DomainAuditID), "scene": r.Scene, "job_id": fmt.Sprint(r.JobID)})
	return raw
}
func paidSource(ctx context.Context, tx pgx.Tx, id int64) (paidEventSource, error) {
	var p paidEventSource
	err := tx.QueryRow(ctx, `SELECT o.id,o.merchant_id,o.trade_no,o.out_trade_no,o.name,o.type,o.amount,o.merchant_amount,o.platform_fee,o.paid_at,v.channel_id,v.provider_trade_no FROM orders o JOIN verified_payment_receipts v ON v.order_id=o.id WHERE o.id=$1 AND o.status='paid'`, id).Scan(&p.OrderID, &p.MerchantID, &p.TradeNo, &p.OutTradeNo, &p.Name, &p.Type, &p.Amount, &p.MerchantAmount, &p.PlatformFee, &p.PaidAt, &p.ChannelID, &p.ProviderTradeNo)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	p.PaidAt = p.PaidAt.UTC()
	return p, err
}

// CapturePaid is an internal domain publisher. Its caller has just verified
// the provider receipt. Both SQL and application checks require the original
// paid journal in THIS transaction; historical payments are never backfilled.
func (s *Store) CapturePaid(ctx context.Context, tx pgx.Tx, orderID, channelID int64, providerTradeNo string, eligible bool) error {
	if !s.available() || tx == nil || orderID <= 0 || channelID <= 0 || providerTradeNo == "" {
		return ErrInvalid
	}
	p, err := paidSource(ctx, tx, orderID)
	if err != nil {
		return err
	}
	if p.ChannelID != channelID || p.ProviderTradeNo != providerTradeNo {
		return ErrConflict
	}
	var fresh bool
	if err = tx.QueryRow(ctx, `SELECT ($2::bigint=0 OR EXISTS(SELECT 1 FROM journals WHERE event_key='payment:'||$1 AND transaction_id=pg_current_xact_id())) AND ($3::bigint=0 OR EXISTS(SELECT 1 FROM journals WHERE event_key='payment-fee:'||$1 AND transaction_id=pg_current_xact_id()))`, p.TradeNo, p.MerchantAmount, p.PlatformFee).Scan(&fresh); err != nil {
		return err
	}
	if !fresh {
		return ErrConflict
	}
	method := map[string]string{"wxpay": "微信支付", "alipay": "支付宝", "qqpay": "QQ钱包", "bank": "银行卡"}[p.Type]
	if method == "" {
		method = p.Type
	}
	in := Input{Scene: "order", At: p.PaidAt, Params: map[string]string{"name": p.Name, "money": money(p.Amount), "type": method, "out_trade_no": p.OutTradeNo, "trade_no": p.TradeNo, "time": p.PaidAt.In(time.FixedZone("UTC+8", 28800)).Format("2006-01-02 15:04:05")}}
	return s.appendEvent(ctx, tx, eventRecord{MerchantID: p.MerchantID, OrderID: p.OrderID, Scene: "order"}, eventData{Version: 1, Source: p, Input: in}, eligible)
}

func (s *Store) readEvent(ctx context.Context, tx pgx.Tx, id int64) (eventRecord, eventData, error) {
	var r eventRecord
	var data eventData
	err := tx.QueryRow(ctx, `SELECT id,merchant_id,COALESCE(order_id,0),COALESCE(login_audit_id,0),COALESCE(settlement_id,0),COALESCE(timer_id,0),COALESCE(domain_audit_id,0),COALESCE(job_id,0),scene,COALESCE(profile_id,0),profile_hash,snapshot_state,payload_cipher,body_hash,event_hash,source_transaction::text,created_at,COALESCE(supplement_id,0) FROM merchant_notice_events WHERE id=$1`, id).Scan(&r.ID, &r.MerchantID, &r.OrderID, &r.LoginAuditID, &r.SettlementID, &r.TimerID, &r.DomainAuditID, &r.JobID, &r.Scene, &r.ProfileID, &r.ProfileHash, &r.SnapshotState, &r.Cipher, &r.BodyHash, &r.Hash, &r.Transaction, &r.CreatedAt, &r.SupplementID)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, data, ErrNotFound
	}
	if err != nil {
		return r, data, err
	}
	if !hmac.Equal(r.Hash, s.eventProof(r)) || len(r.Cipher) < s.aead.NonceSize()+s.aead.Overhead() {
		return r, data, ErrConflict
	}
	n := s.aead.NonceSize()
	raw, err := s.aead.Open(nil, r.Cipher[:n], r.Cipher[n:], eventAAD(r))
	if err != nil || !hmac.Equal(r.BodyHash, s.digest("event-body", raw)) || json.Unmarshal(raw, &data) != nil || (data.Version != 1 && data.Version != 2 && data.Version != 3 && data.Version != 4) {
		return r, data, ErrConflict
	}
	if data.Version == 2 && (!data.VoiceManaged230 || data.VoiceRevision230 <= 0 || r.Scene != "order") || data.Version == 1 && (data.VoiceManaged230 || data.VoiceRevision230 != 0) {
		return r, data, ErrConflict
	}
	if data.Version == 3 && (data.SMS231 == nil || !data.SMS231.Valid() || r.Scene != "balance" && r.Scene != "group" && r.Scene != "complain") || data.Version < 3 && data.SMS231 != nil {
		return r, data, ErrConflict
	}
	if data.Version == 4 && len(data.TelegramStamp231) != 64 || data.Version < 4 && data.TelegramStamp231 != "" {
		return r, data, ErrConflict
	}
	if data.SMS231 != nil && (!data.SMS231.Valid() || r.Scene != "balance" && r.Scene != "group" && r.Scene != "complain") {
		return r, data, ErrConflict
	}
	if err = s.verifyEventSource(ctx, tx, r, data); err != nil {
		return r, data, err
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT count(*)=1 FROM backoffice_audit WHERE resource='merchant-notice-events' AND resource_id=$1 AND operation='create' AND (actor_role,actor_id,actor_name)=('system',0,'merchant-notice') AND changes=$2::jsonb`, id, eventAudit(r)).Scan(&valid); err != nil {
		return r, data, err
	}
	if !valid {
		return r, data, ErrConflict
	}
	if _, err = renderSource231(data.Input); err != nil {
		return r, data, ErrConflict
	}
	if len(data.Routes) > 0 {
		if r.JobID <= 0 {
			return r, data, ErrConflict
		}
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE id=$1 AND kind='merchant.notice' AND event_key='merchant-notice:'||$2::bigint::text AND payload=jsonb_build_object('event_id',$2::bigint::text))`, r.JobID, r.ID).Scan(&valid); err != nil {
			return r, data, err
		}
		if !valid {
			return r, data, ErrConflict
		}
	} else if r.JobID != 0 {
		return r, data, ErrConflict
	}
	seen := map[Transport]bool{}
	for _, kind := range data.Routes {
		if !validTransport(kind) || seen[kind] || (r.SnapshotState != "captured" && !(r.SnapshotState == "direct" && r.Scene == "wxtransfer" && kind == Wechat && data.Supplement != nil)) {
			return r, data, ErrConflict
		}
		seen[kind] = true
	}
	return r, data, nil
}
