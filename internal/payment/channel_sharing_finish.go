package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"time"
)

type ChannelSharingFinishView struct {
	PreviousFailedID int64                    `json:"previous_failed_id,string,omitempty"`
	ID               int64                    `json:"id,string"`
	OrderID          int64                    `json:"order_id,string"`
	MerchantID       int64                    `json:"merchant_id,string"`
	TradeNo          string                   `json:"trade_no"`
	ExternalNo       string                   `json:"external_no"`
	OutNo            string                   `json:"out_no"`
	ProviderOrderNo  string                   `json:"provider_order_no"`
	ProviderTradeNo  string                   `json:"provider_trade_no"`
	State            string                   `json:"state"`
	Source           string                   `json:"source"`
	GrossAmount      int64                    `json:"gross_amount,string"`
	MerchantNet      int64                    `json:"merchant_net,string"`
	SharedAmount     int64                    `json:"shared_amount,string"`
	ExpectedUnsplit  int64                    `json:"expected_unsplit,string"`
	ReleaseAmount    int64                    `json:"release_amount,string"`
	FundsReleased    bool                     `json:"funds_released"`
	CreatedAt        time.Time                `json:"created_at"`
	Sharing          *ChannelSharingOrderView `json:"sharing,omitempty"`
}
type sharingFinishRecord struct {
	ChannelSharingFinishView
	Payment                                              sharingPaymentIntent
	Actor                                                TransferBatchActor
	RequestHash, FailureProof                            []byte
	FundingAccount, OriginalSharingID, LastObservationID int64
}

const sharingFinishColumns = "p.id,p.order_id,p.merchant_id,o.trade_no,p.external_no,p.out_no,p.provider_trade_no,p.provider_order_no,p.gross_amount,p.merchant_net,p.shared_amount,p.expected_unsplit,p.release_amount,p.state,p.created_at,COALESCE(p.previous_finish_id,0),COALESCE(p.original_sharing_id,0),COALESCE(p.last_observation_id,0),p.request_hash,p.failure_proof,p.actor_role,p.actor_id,p.actor_name,p.actor_class"

func (s *Store) sharingFinishFingerprint(r sharingFinishRecord) ([]byte, error) {
	raw, e := json.Marshal([]any{r.ID, r.OrderID, r.MerchantID, r.TradeNo, r.ExternalNo, r.OutNo, r.ProviderTradeNo, r.GrossAmount, r.MerchantNet, r.SharedAmount, r.ExpectedUnsplit, r.ReleaseAmount, r.FundingAccount, r.OriginalSharingID, r.PreviousFailedID, hex.EncodeToString(r.Payment.RequestHash), r.Actor})
	if e != nil {
		return nil, e
	}
	return s.sharingDigest("sharing-finish", raw)
}
func (s *Store) sharingFinishFailureFingerprint(r sharingFinishRecord) ([]byte, error) {
	raw, e := json.Marshal([]any{r.ID, hex.EncodeToString(r.RequestHash), "failed-unaccepted"})
	if e != nil {
		return nil, e
	}
	return s.sharingDigest("sharing-finish-failure", raw)
}
func (s *Store) scanSharingFinish(ctx context.Context, tx referralReader, row pgx.Row) (sharingFinishRecord, error) {
	var r sharingFinishRecord
	e := row.Scan(&r.ID, &r.OrderID, &r.MerchantID, &r.TradeNo, &r.ExternalNo, &r.OutNo, &r.ProviderTradeNo, &r.ProviderOrderNo, &r.GrossAmount, &r.MerchantNet, &r.SharedAmount, &r.ExpectedUnsplit, &r.ReleaseAmount, &r.State, &r.CreatedAt, &r.PreviousFailedID, &r.OriginalSharingID, &r.LastObservationID, &r.RequestHash, &r.FailureProof, &r.Actor.Role, &r.Actor.ID, &r.Actor.Name, &r.Actor.Class)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	r.Source = "finish"
	r.FundsReleased = r.State == "succeeded"
	r.Payment, e = scanSharingPayment(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingPaymentColumns+" FROM provider_attempt_sharing WHERE order_id=$1", r.OrderID))
	if e != nil {
		return r, e
	}
	if e = s.verifySharingPayment(r.Payment); e != nil {
		return r, e
	}
	if e = tx.QueryRow(ctx, "SELECT account_id FROM channel_sharing_fundings WHERE order_id=$1", r.OrderID).Scan(&r.FundingAccount); e != nil {
		return r, e
	}
	h, e := s.sharingFinishFingerprint(r)
	if e != nil || !hmac.Equal(h, r.RequestHash) {
		return r, ErrConflict
	}
	if r.OriginalSharingID > 0 {
		original, e := s.scanSharingOrder(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingOrderColumns+" FROM channel_sharing_orders WHERE id=$1", r.OriginalSharingID))
		if e != nil {
			return r, e
		}
		if original.OrderID != r.OrderID || original.State != "failed" || original.Completed || original.LastObservationID != 0 {
			return r, ErrConflict
		}
	}
	if r.State == "failed" && r.LastObservationID == 0 {
		h, e := s.sharingFinishFailureFingerprint(r)
		if e != nil || !hmac.Equal(h, r.FailureProof) {
			return r, ErrConflict
		}
	} else if len(r.FailureProof) != 0 {
		return r, ErrConflict
	}
	if e = s.verifySharingFinishObservations(ctx, tx, r); e != nil {
		return r, e
	}
	var valid bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM channel_sharing_finish_invariants WHERE finish_id=$1 AND valid IS TRUE) AND EXISTS(SELECT 1 FROM channel_sharing_funding_invariants WHERE order_id=$2 AND valid IS TRUE)", r.ID, r.OrderID).Scan(&valid); e != nil {
		return r, e
	}
	if !valid {
		return r, ErrConflict
	}
	return r, nil
}
func (s *Store) ChannelSharingFinish(ctx context.Context, mid int64, no string, actor TransferBatchActor) (ChannelSharingFinishView, error) {
	if !actor.valid(mid) || !channelSharingExternalNo.MatchString(no) {
		return ChannelSharingFinishView{}, ErrInvalid
	}
	r, e := s.readSharingFinish(ctx, mid, no, 0)
	return r.ChannelSharingFinishView, e
}
func (s *Store) readSharingFinish(ctx context.Context, mid int64, no string, id int64) (sharingFinishRecord, error) {
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return sharingFinishRecord{}, e
	}
	defer tx.Rollback(ctx)
	query := "SELECT " + sharingFinishColumns + " FROM channel_sharing_finishes p JOIN orders o ON o.id=p.order_id WHERE p.merchant_id=$1 AND p.external_no=$2"
	args := []any{mid, no}
	if id > 0 {
		query = "SELECT " + sharingFinishColumns + " FROM channel_sharing_finishes p JOIN orders o ON o.id=p.order_id WHERE p.id=$1"
		args = []any{id}
	}
	r, e := s.scanSharingFinish(ctx, tx, tx.QueryRow(ctx, query, args...))
	if e != nil {
		return r, e
	}
	return r, tx.Commit(ctx)
}
func (s *Store) ChannelSharingFinishes(ctx context.Context, mid int64, limit int, actor TransferBatchActor) ([]ChannelSharingFinishView, error) {
	if !actor.valid(mid) || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	var ids []int64
	if e = tx.QueryRow(ctx, "SELECT COALESCE(array_agg(id ORDER BY id DESC),'{}'::bigint[]) FROM (SELECT id FROM channel_sharing_finishes WHERE merchant_id=$1 ORDER BY id DESC LIMIT $2) p", mid, limit).Scan(&ids); e != nil {
		return nil, e
	}
	views := make([]ChannelSharingFinishView, 0, len(ids))
	for _, id := range ids {
		r, e := s.scanSharingFinish(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingFinishColumns+" FROM channel_sharing_finishes p JOIN orders o ON o.id=p.order_id WHERE p.id=$1", id))
		if e != nil {
			return nil, e
		}
		views = append(views, r.ChannelSharingFinishView)
	}
	return views, tx.Commit(ctx)
}
func sharingFinishFromPS(o Order, p sharingOrderRecord) sharingFinishRecord {
	return sharingFinishRecord{ChannelSharingFinishView: ChannelSharingFinishView{OrderID: o.ID, MerchantID: o.MerchantID, TradeNo: o.TradeNo, ExternalNo: p.ExternalNo, ProviderTradeNo: p.ProviderTradeNo, State: "succeeded", Source: "sharing", GrossAmount: o.Amount, MerchantNet: o.MerchantAmount, SharedAmount: o.MerchantAmount - p.ReleasedAmount, ExpectedUnsplit: o.Amount - (o.MerchantAmount - p.ReleasedAmount), ReleaseAmount: p.ReleasedAmount, FundsReleased: true, Sharing: &p.ChannelSharingOrderView}}
}
func (s *Store) previewSharingFinish(ctx context.Context, tx pgx.Tx, mid int64, trade string, actor TransferBatchActor) (sharingFinishRecord, error) {
	var r sharingFinishRecord
	if !actor.valid(mid) || trade == "" {
		return r, ErrInvalid
	}
	o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE merchant_id=$1 AND trade_no=$2", mid, trade))
	if e != nil {
		return r, e
	}
	r, e = s.scanSharingFinish(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingFinishColumns+" FROM channel_sharing_finishes p JOIN orders o ON o.id=p.order_id WHERE p.order_id=$1 ORDER BY p.id DESC LIMIT 1", o.ID))
	var previous int64
	if e == nil {
		if r.State != "failed" {
			return r, nil
		}
		if e = s.verifySharingFinishHistory(ctx, tx, o.ID, true); e != nil {
			return r, e
		}
		previous = r.ID
	} else if !errors.Is(e, ErrNotFound) {
		return r, e
	}
	r = sharingFinishRecord{ChannelSharingFinishView: ChannelSharingFinishView{PreviousFailedID: previous, OrderID: o.ID, MerchantID: mid, TradeNo: trade, GrossAmount: o.Amount, MerchantNet: o.MerchantAmount, ExpectedUnsplit: o.Amount, ReleaseAmount: o.MerchantAmount, Source: "finish", State: "eligible"}, Actor: actor}
	r.Payment, e = scanSharingPayment(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingPaymentColumns+" FROM provider_attempt_sharing WHERE order_id=$1", o.ID))
	if errors.Is(e, pgx.ErrNoRows) {
		return r, ErrState
	}
	if e != nil {
		return r, e
	}
	if e = s.verifySharingPayment(r.Payment); e != nil {
		return r, e
	}
	var valid, conflicting bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM channel_sharing_funding_invariants WHERE order_id=$1 AND valid IS TRUE),EXISTS(SELECT 1 FROM recharge_intents WHERE order_id=$1) OR EXISTS(SELECT 1 FROM refunds WHERE order_id=$1 AND status<>'failed') OR EXISTS(SELECT 1 FROM order_income_holds WHERE order_id=$1 AND state='held') OR EXISTS(SELECT 1 FROM order_profit_allocations WHERE order_id=$1)", o.ID).Scan(&valid, &conflicting); e != nil {
		return r, e
	}
	if !valid || o.Status != "paid" || !r.Payment.Declared || r.Payment.Purpose != "payment" {
		return r, ErrState
	}
	original, e := s.scanSharingOrder(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingOrderColumns+" FROM channel_sharing_orders WHERE order_id=$1", o.ID))
	if e == nil {
		if original.FundsReleased {
			return sharingFinishFromPS(o, original), nil
		}
		if original.State != "failed" || original.Completed || original.LastObservationID != 0 {
			return r, ErrState
		}
		r.OriginalSharingID = original.ID
	} else if !errors.Is(e, ErrNotFound) {
		return r, e
	}
	if conflicting {
		return r, ErrState
	}
	if !wechatSharingProduct(r.Payment.Product) {
		return r, adapter.ErrUnsupported
	}
	if e = tx.QueryRow(ctx, "SELECT account_id FROM channel_sharing_fundings WHERE order_id=$1", o.ID).Scan(&r.FundingAccount); e != nil {
		return r, e
	}
	if e = tx.QueryRow(ctx, "SELECT provider_trade_no FROM verified_payment_receipts WHERE order_id=$1 AND channel_id=$2", o.ID, r.Payment.ChannelID).Scan(&r.ProviderTradeNo); e != nil {
		return r, e
	}
	return r, nil
}
func (s *Store) PreviewChannelSharingFinishTx(ctx context.Context, tx pgx.Tx, mid int64, trade string, actor TransferBatchActor) (ChannelSharingFinishView, error) {
	r, e := s.previewSharingFinish(ctx, tx, mid, trade, actor)
	return r.ChannelSharingFinishView, e
}
func (g ProviderFlow) PreviewChannelSharingFinish(ctx context.Context, mid int64, trade string, actor TransferBatchActor) (ChannelSharingFinishView, error) {
	if g.Store == nil {
		return ChannelSharingFinishView{}, ErrInvalid
	}
	tx, e := g.Store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return ChannelSharingFinishView{}, e
	}
	defer tx.Rollback(ctx)
	r, e := g.Store.previewSharingFinish(ctx, tx, mid, trade, actor)
	if e != nil {
		return r.ChannelSharingFinishView, e
	}
	if r.Source != "sharing" {
		if _, e = g.sharingFinishProvider(ctx, tx, r); e != nil {
			return r.ChannelSharingFinishView, e
		}
	}
	return r.ChannelSharingFinishView, tx.Commit(ctx)
}
func (g ProviderFlow) sharingFinishProvider(ctx context.Context, q fundsReader, r sharingFinishRecord) (adapter.ChannelSharingFinisher, error) {
	if !wechatSharingProduct(r.Payment.Product) {
		return nil, adapter.ErrUnsupported
	}
	p, e := g.sharingOrderProvider(ctx, q, sharingOrderRecord{Payment: r.Payment, MerchantID: r.MerchantID})
	if e != nil {
		return nil, e
	}
	finisher, ok := p.(adapter.ChannelSharingFinisher)
	if !ok || !p.ChannelSharingCapabilities().FinishSupported {
		return nil, adapter.ErrUnsupported
	}
	return finisher, nil
}
func sharingFinishRequest(r sharingFinishRecord) adapter.ChannelSharingFinishRequest {
	return adapter.ChannelSharingFinishRequest{OrderNo: r.OutNo, ProviderOrderNo: r.ProviderOrderNo, ProviderTradeNo: r.ProviderTradeNo, ExpectedUnsplit: r.ExpectedUnsplit, Description: "原渠道分账完结"}
}
func (s *Store) verifyOrderSharingFinishEvidence(ctx context.Context, tx referralReader, orderID int64) error {
	var exists bool
	if e := tx.QueryRow(ctx, "SELECT to_regclass('channel_sharing_finishes') IS NOT NULL").Scan(&exists); e != nil {
		return e
	}
	if !exists {
		return nil
	}
	return s.verifySharingFinishHistory(ctx, tx, orderID, false)
}

// Each page is read within the caller's original transaction. A terminal failure
// can reopen capacity only after every historical keyed result was authenticated.
func (s *Store) verifySharingFinishHistory(ctx context.Context, tx referralReader, oid int64, requireFailed bool) error {
	var cursor int64
	for {
		var ids []int64
		if e := tx.QueryRow(ctx, "SELECT COALESCE(array_agg(id ORDER BY id),'{}'::bigint[]) FROM (SELECT id FROM channel_sharing_finishes WHERE order_id=$1 AND id>$2 ORDER BY id LIMIT 100) p", oid, cursor).Scan(&ids); e != nil {
			return e
		}
		if len(ids) == 0 {
			return nil
		}
		for _, id := range ids {
			r, e := s.scanSharingFinish(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingFinishColumns+" FROM channel_sharing_finishes p JOIN orders o ON o.id=p.order_id WHERE p.id=$1", id))
			if e != nil {
				return e
			}
			if requireFailed && r.State != "failed" {
				return ErrConflict
			}
			cursor = id
		}
		if len(ids) < 100 {
			return nil
		}
	}
}
