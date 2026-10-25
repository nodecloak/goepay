package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

type ChannelSharingPaymentItem struct {
	ReceiverID int64 `json:"receiver_id,string"`
	Amount     int64 `json:"amount,string"`
}
type ChannelSharingPaymentView struct {
	AttemptID   int64                       `json:"attempt_id,string"`
	OrderID     int64                       `json:"order_id,string"`
	MerchantID  int64                       `json:"merchant_id,string"`
	ChannelID   int64                       `json:"channel_id,string"`
	Method      string                      `json:"method"`
	Provider    string                      `json:"provider"`
	AppID       string                      `json:"app_id"`
	Product     string                      `json:"product"`
	RuleID      int64                       `json:"rule_id,string"`
	Declared    bool                        `json:"declared"`
	Purpose     string                      `json:"purpose"`
	GrossAmount int64                       `json:"gross_amount,string"`
	MerchantNet int64                       `json:"merchant_net,string"`
	RefundMode  string                      `json:"refund_mode"`
	AutoFinish  bool                        `json:"auto_finish"`
	Items       []ChannelSharingPaymentItem `json:"items"`
	CreatedAt   time.Time                   `json:"created_at"`
}
type sharingPaymentIntent struct {
	ChannelSharingPaymentView
	AccountHash, RequestHash []byte
	Prepared                 bool
	ExpectedRuleID           int64
}

const sharingPaymentColumns = "attempt_id,order_id,merchant_id,channel_id,method,provider,app_id,product,account_hash,request_hash,COALESCE(rule_id,0),declared,gross_amount,merchant_net,refund_mode,auto_finish,created_at,purpose"

func scanSharingPayment(ctx context.Context, tx referralReader, row pgx.Row) (sharingPaymentIntent, error) {
	var d sharingPaymentIntent
	e := row.Scan(&d.AttemptID, &d.OrderID, &d.MerchantID, &d.ChannelID, &d.Method, &d.Provider, &d.AppID, &d.Product, &d.AccountHash, &d.RequestHash, &d.RuleID, &d.Declared, &d.GrossAmount, &d.MerchantNet, &d.RefundMode, &d.AutoFinish, &d.CreatedAt, &d.Purpose)
	if errors.Is(e, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	if e != nil {
		return d, e
	}
	d.Items = make([]ChannelSharingPaymentItem, 0)
	rows, e := tx.Query(ctx, "SELECT receiver_id,amount FROM provider_attempt_sharing_items WHERE attempt_id=$1 ORDER BY position", d.AttemptID)
	if e != nil {
		return d, e
	}
	defer rows.Close()
	for rows.Next() {
		var item ChannelSharingPaymentItem
		if e = rows.Scan(&item.ReceiverID, &item.Amount); e != nil {
			return d, e
		}
		d.Items = append(d.Items, item)
	}
	return d, rows.Err()
}
func (s *Store) sharingPaymentFingerprint(d sharingPaymentIntent) ([]byte, error) {
	// Time and the preparation/CAS flags are not financial terms.
	terms := []any{d.AttemptID, d.OrderID, d.MerchantID, d.ChannelID, d.Method, d.Provider, d.AppID, d.Product, hex.EncodeToString(d.AccountHash), d.RuleID, d.Declared, d.GrossAmount, d.MerchantNet, d.RefundMode, d.AutoFinish, d.Items}
	// Preserve the original payment proof domain for existing schema49 rows.
	// New recharge exclusions bind their distinct immutable purpose explicitly.
	if d.Purpose == "recharge" {
		terms = append(terms, "purpose", d.Purpose)
	} else if d.Purpose != "" && d.Purpose != "payment" {
		return nil, ErrInvalid
	}
	raw, e := json.Marshal(terms)
	if e != nil {
		return nil, e
	}
	return s.sharingDigest("payment-declaration", raw)
}
func (s *Store) verifySharingPayment(d sharingPaymentIntent) error {
	h, e := s.sharingPaymentFingerprint(d)
	if e != nil || !hmac.Equal(h, d.RequestHash) {
		return ErrConflict
	}
	return nil
}
func (s *Store) ChannelSharingPayment(ctx context.Context, mid int64, trade string, actor TransferBatchActor) (ChannelSharingPaymentView, error) {
	if !actor.valid(mid) || trade == "" {
		return ChannelSharingPaymentView{}, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return ChannelSharingPaymentView{}, e
	}
	defer tx.Rollback(ctx)
	d, e := scanSharingPayment(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingPaymentColumns+" FROM provider_attempt_sharing WHERE merchant_id=$1 AND order_id=(SELECT id FROM orders WHERE merchant_id=$1 AND trade_no=$2)", mid, trade))
	if e != nil {
		return ChannelSharingPaymentView{}, e
	}
	if e = s.verifySharingPayment(d); e != nil {
		return ChannelSharingPaymentView{}, e
	}
	return d.ChannelSharingPaymentView, tx.Commit(ctx)
}

// Prepare only determines the wire flag. The attempt transaction verifies the
// current policy under a shared advisory lock and stores all terms before I/O.
func (g ProviderFlow) prepareSharingPayment(ctx context.Context, o Order, channel int64, method, appID, scope string) (sharingPaymentIntent, error) {
	s := g.Store
	account, e := s.sharingDigest("account", []byte(scope))
	if e != nil {
		return sharingPaymentIntent{}, e
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return sharingPaymentIntent{}, e
	}
	defer tx.Rollback(ctx)
	old, e := scanSharingPayment(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingPaymentColumns+" FROM provider_attempt_sharing WHERE order_id=$1", o.ID))
	if e == nil {
		if e = s.verifySharingPayment(old); e != nil {
			return sharingPaymentIntent{}, e
		}
		if old.ChannelID != channel || old.Method != method || old.AppID != appID || !hmac.Equal(old.AccountHash, account) {
			return sharingPaymentIntent{}, ErrConflict
		}
		old.Prepared = true
		return old, tx.Commit(ctx)
	}
	if !errors.Is(e, ErrNotFound) {
		return sharingPaymentIntent{}, e
	}
	// Never infer a historical declaration for an existing external attempt.
	var historical bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM provider_attempts WHERE order_id=$1)", o.ID).Scan(&historical); e != nil {
		return sharingPaymentIntent{}, e
	}
	if historical {
		return sharingPaymentIntent{}, ErrConflict
	}
	d := sharingPaymentIntent{ChannelSharingPaymentView: ChannelSharingPaymentView{OrderID: o.ID, MerchantID: o.MerchantID, ChannelID: channel, Method: method, Provider: o.Type, AppID: appID, GrossAmount: o.Amount, MerchantNet: o.MerchantAmount, Items: make([]ChannelSharingPaymentItem, 0)}, AccountHash: account, Prepared: true}
	d.Purpose, e = sharingPaymentPurpose(ctx, tx, o.ID)
	if e != nil {
		return d, e
	}
	rule, e := scanSharingRule(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingRuleColumns+" FROM channel_sharing_rules WHERE id=(SELECT rule_id FROM channel_sharing_rule_current WHERE merchant_id=$1 AND channel_id=$2)", o.MerchantID, channel))
	if errors.Is(e, ErrNotFound) {
		return d, tx.Commit(ctx)
	}
	if e != nil {
		return d, e
	}
	if e = s.verifySharingRule(rule); e != nil {
		return d, e
	}
	d.RuleID, d.ExpectedRuleID = rule.ID, rule.ID
	if d.Purpose == "recharge" || !rule.Enabled || !slices.Contains(rule.Methods, method) {
		return d, tx.Commit(ctx)
	}
	amounts, total, e := calculateChannelSharing(o.Amount, o.MerchantAmount, rule.MinimumAmount, rule.Items)
	if e != nil {
		return d, e
	}
	if total == 0 {
		return d, tx.Commit(ctx)
	}
	p, b, e := g.sharingChannel(o.MerchantID, channel)
	if e != nil {
		return d, e
	}
	if b.Provider != rule.Provider || b.AppID != rule.AppID || b.Product != rule.Product || !hmac.Equal(account, rule.AccountHash) {
		return d, ErrConflict
	}
	profile, ok := p.(adapter.ChannelSharingPaymentProfile)
	if !ok || !p.ChannelSharingCapabilities().AutoFinishSupported {
		return d, adapter.ErrUnsupported
	}
	mode, e := profile.ChannelSharingRefundMode(method)
	if e != nil {
		return d, e
	}
	if mode != "merchant_funded" {
		return d, adapter.ErrUnsupported
	}
	d.Declared, d.Product, d.RefundMode, d.AutoFinish = true, rule.Product, mode, true
	for i, item := range rule.Items {
		if amounts[i] > 0 {
			d.Items = append(d.Items, ChannelSharingPaymentItem{ReceiverID: item.ReceiverID, Amount: amounts[i]})
		}
	}
	return d, tx.Commit(ctx)
}

func (s *Store) bindSharingPayment(ctx context.Context, tx pgx.Tx, attemptID, orderID, channel int64, method, scope string, created bool, candidate *sharingPaymentIntent) error {
	if scope == "" {
		return nil
	}
	account, e := s.sharingDigest("account", []byte(scope))
	if e != nil {
		return e
	}
	if !created {
		old, e := scanSharingPayment(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingPaymentColumns+" FROM provider_attempt_sharing WHERE attempt_id=$1", attemptID))
		if e != nil {
			return e
		}
		if e = s.verifySharingPayment(old); e != nil {
			return e
		}
		if !hmac.Equal(account, old.AccountHash) || candidate == nil || old.AppID != candidate.AppID {
			return ErrConflict
		}
		if candidate.Prepared {
			copy := *candidate
			copy.AttemptID = attemptID
			h, e := s.sharingPaymentFingerprint(copy)
			if e != nil || !hmac.Equal(h, old.RequestHash) {
				return ErrConflict
			}
		}
		return nil
	}
	o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", orderID))
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock_shared(hashtextextended($1,48))", fmt.Sprintf("channel-sharing-rule:%d:%d", o.MerchantID, channel)); e != nil {
		return e
	}
	var currentID int64
	if e = tx.QueryRow(ctx, "SELECT COALESCE((SELECT rule_id FROM channel_sharing_rule_current WHERE merchant_id=$1 AND channel_id=$2),0)", o.MerchantID, channel).Scan(&currentID); e != nil {
		return e
	}
	d := sharingPaymentIntent{ChannelSharingPaymentView: ChannelSharingPaymentView{AttemptID: attemptID, OrderID: orderID, MerchantID: o.MerchantID, ChannelID: channel, Method: method, Provider: o.Type, GrossAmount: o.Amount, MerchantNet: o.MerchantAmount, Items: make([]ChannelSharingPaymentItem, 0)}, AccountHash: account}
	if candidate == nil {
		return ErrInvalid
	}
	d.AppID = candidate.AppID
	purpose, e := sharingPaymentPurpose(ctx, tx, orderID)
	if e != nil {
		return e
	}
	d.Purpose = purpose
	if candidate.Prepared {
		if candidate.Purpose != purpose || candidate.ExpectedRuleID != currentID || candidate.OrderID != orderID || candidate.ChannelID != channel || candidate.Method != method || candidate.Provider != o.Type || candidate.MerchantID != o.MerchantID || candidate.GrossAmount != o.Amount || candidate.MerchantNet != o.MerchantAmount || !hmac.Equal(candidate.AccountHash, account) {
			return ErrConflict
		}
		d = *candidate
		d.AttemptID = attemptID
	} else {
		// Scan/preauthorization and internal account-aware callers do not silently
		// ignore an applicable policy. Ordinary submission supplies prepared terms.
		d.RuleID = currentID
	}
	if d.Declared {
		ids := make([]int64, len(d.Items))
		for i, item := range d.Items {
			ids[i] = item.ReceiverID
		}
		slices.Sort(ids)
		for _, id := range ids {
			r, e := scanSharingReceiver(tx.QueryRow(ctx, "SELECT "+sharingReceiverColumns+" FROM channel_sharing_receivers WHERE id=$1 FOR SHARE", id))
			if e != nil {
				return e
			}
			if r.State != "bound" {
				return ErrState
			}
			if r.Binding.MerchantID != d.MerchantID || r.Binding.ChannelID != channel || r.Binding.Provider != d.Provider || r.Binding.AppID != d.AppID || r.Binding.Product != d.Product || !hmac.Equal(r.AccountHash, account) {
				return ErrConflict
			}
			if _, e = s.openSharingReceiverRecord(r); e != nil {
				return e
			}
		}
	}
	hash, e := s.sharingPaymentFingerprint(d)
	if e != nil {
		return e
	}
	var rule any
	if d.RuleID > 0 {
		rule = d.RuleID
	}
	_, e = tx.Exec(ctx, `INSERT INTO provider_attempt_sharing(attempt_id,order_id,merchant_id,channel_id,method,provider,app_id,product,account_hash,request_hash,rule_id,declared,gross_amount,merchant_net,refund_mode,auto_finish,receiver_count,purpose) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`, attemptID, orderID, d.MerchantID, channel, method, d.Provider, d.AppID, d.Product, account, hash, rule, d.Declared, d.GrossAmount, d.MerchantNet, d.RefundMode, d.AutoFinish, len(d.Items), d.Purpose)
	if e != nil {
		return e
	}
	for i, item := range d.Items {
		if _, e = tx.Exec(ctx, "INSERT INTO provider_attempt_sharing_items(attempt_id,position,receiver_id,amount) VALUES($1,$2,$3,$4)", attemptID, i+1, item.ReceiverID, item.Amount); e != nil {
			return e
		}
	}
	return nil
}

func sharingPaymentPurpose(ctx context.Context, tx pgx.Tx, orderID int64) (string, error) {
	var recharge bool
	if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM recharge_intents WHERE order_id=$1)", orderID).Scan(&recharge); e != nil {
		return "", e
	}
	if recharge {
		return "recharge", nil
	}
	return "payment", nil
}

func (s *Store) reserveChannelSharingPayment(ctx context.Context, tx pgx.Tx, o Order) error {
	d, e := scanSharingPayment(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingPaymentColumns+" FROM provider_attempt_sharing WHERE order_id=$1", o.ID))
	if errors.Is(e, ErrNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	if e = s.verifySharingPayment(d); e != nil {
		return e
	}
	if !d.Declared {
		return nil
	}
	var receipt bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM verified_payment_receipts WHERE order_id=$1 AND channel_id=$2)", o.ID, d.ChannelID).Scan(&receipt); e != nil {
		return e
	}
	if !receipt {
		return ErrState
	}
	if d.MerchantNet != o.MerchantAmount || d.GrossAmount != o.Amount {
		return ErrConflict
	}
	var account, available int64
	if e = tx.QueryRow(ctx, "INSERT INTO accounts(owner_id,kind,scope_id) VALUES($1,'channel_sharing_reserve',$2) RETURNING id", o.MerchantID, o.ID).Scan(&account); e != nil {
		return e
	}
	if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind='available' AND scope_id=0", o.MerchantID).Scan(&available); e != nil {
		return e
	}
	key := fmt.Sprintf("channel-sharing:%d:reserve", o.ID)
	if e = postByAccountIDs(ctx, tx, key, "original channel sharing payment reserve", available, account, o.MerchantAmount); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO channel_sharing_fundings(order_id,account_id,amount,reserve_journal_id) SELECT $1,$2,$3,id FROM journals WHERE event_key=$4", o.ID, account, o.MerchantAmount, key)
	return e
}
