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

type ChannelSharingOrderItem struct {
	ReceiverID       int64  `json:"receiver_id,string"`
	Amount           int64  `json:"amount,string"`
	State            string `json:"state"`
	ProviderDetailNo string `json:"provider_detail_no,omitempty"`
	FailureCode      string `json:"failure_code,omitempty"`
}
type ChannelSharingOrderView struct {
	ID              int64                     `json:"id,string"`
	OrderID         int64                     `json:"order_id,string"`
	MerchantID      int64                     `json:"merchant_id,string"`
	ExternalNo      string                    `json:"external_no"`
	OutNo           string                    `json:"out_no"`
	ProviderOrderNo string                    `json:"provider_order_no"`
	State           string                    `json:"state"`
	Completed       bool                      `json:"completed"`
	FundsReleased   bool                      `json:"funds_released"`
	ReleasedAmount  int64                     `json:"released_amount,string"`
	Items           []ChannelSharingOrderItem `json:"items"`
	CreatedAt       time.Time                 `json:"created_at"`
}

type sharingOrderRecord struct {
	ChannelSharingOrderView
	ProviderTradeNo   string
	RequestHash       []byte
	AutoFinish        bool
	Actor             TransferBatchActor
	Payment           sharingPaymentIntent
	FundingAccount    int64
	LastObservationID int64
	FailureProof      []byte
}

const sharingOrderColumns = "id,order_id,merchant_id,external_no,out_no,provider_trade_no,request_hash,provider_order_no,state,completed,auto_finish,actor_role,actor_id,actor_name,actor_class,created_at,COALESCE(last_observation_id,0),failure_proof"

func (s *Store) scanSharingOrder(ctx context.Context, tx referralReader, row pgx.Row) (sharingOrderRecord, error) {
	var r sharingOrderRecord
	e := row.Scan(&r.ID, &r.OrderID, &r.MerchantID, &r.ExternalNo, &r.OutNo, &r.ProviderTradeNo, &r.RequestHash, &r.ProviderOrderNo, &r.State, &r.Completed, &r.AutoFinish, &r.Actor.Role, &r.Actor.ID, &r.Actor.Name, &r.Actor.Class, &r.CreatedAt, &r.LastObservationID, &r.FailureProof)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	rows, e := tx.Query(ctx, "SELECT receiver_id,amount,state,provider_detail_no,failure_code FROM channel_sharing_order_items WHERE sharing_id=$1 ORDER BY position", r.ID)
	if e != nil {
		return r, e
	}
	for rows.Next() {
		var item ChannelSharingOrderItem
		if e = rows.Scan(&item.ReceiverID, &item.Amount, &item.State, &item.ProviderDetailNo, &item.FailureCode); e != nil {
			rows.Close()
			return r, e
		}
		r.Items = append(r.Items, item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return r, e
	}
	r.Payment, e = scanSharingPayment(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingPaymentColumns+" FROM provider_attempt_sharing WHERE order_id=$1", r.OrderID))
	if e != nil {
		return r, e
	}
	if e = s.verifySharingPayment(ctx, tx, r.Payment); e != nil {
		return r, e
	}
	if e = tx.QueryRow(ctx, "SELECT account_id FROM channel_sharing_fundings WHERE order_id=$1", r.OrderID).Scan(&r.FundingAccount); e != nil {
		return r, e
	}
	h, e := s.sharingOrderFingerprint(r)
	if e != nil || !hmac.Equal(h, r.RequestHash) {
		return r, ErrConflict
	}
	if e = s.verifySharingObservations(ctx, tx, r); e != nil {
		return r, e
	}
	if e = s.verifySharingOrderFailure(r); e != nil {
		return r, e
	}
	var completion sharingCompletionEvidence
	var proofHash []byte
	e = tx.QueryRow(ctx, "SELECT proof_kind,expected_unsplit,observed_remaining,release_amount,proof_hash FROM channel_sharing_completions WHERE order_id=$1", r.OrderID).Scan(&completion.Kind, &completion.ExpectedUnsplit, &completion.ObservedRemaining, &completion.ReleaseAmount, &proofHash)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return r, e
	}
	if e == nil {
		h, e := s.sharingCompletionFingerprint(r, completion)
		if e != nil || !hmac.Equal(h, proofHash) {
			return r, ErrConflict
		}
		r.FundsReleased = true
		r.ReleasedAmount = completion.ReleaseAmount
	}
	return r, nil
}
func (s *Store) sharingOrderFingerprint(r sharingOrderRecord) ([]byte, error) {
	raw, e := json.Marshal([]any{r.ID, r.OrderID, r.MerchantID, r.ExternalNo, r.OutNo, r.ProviderTradeNo, hex.EncodeToString(r.Payment.RequestHash), r.AutoFinish, r.FundingAccount})
	if e != nil {
		return nil, e
	}
	return s.sharingDigest("sharing-order", raw)
}
func (s *Store) ChannelSharingOrder(ctx context.Context, mid int64, no string, actor TransferBatchActor) (ChannelSharingOrderView, error) {
	r, e := s.readSharingOrder(ctx, mid, no, actor)
	return r.ChannelSharingOrderView, e
}
func (s *Store) readSharingOrder(ctx context.Context, mid int64, no string, actor TransferBatchActor) (sharingOrderRecord, error) {
	if !actor.valid(mid) || !channelSharingExternalNo.MatchString(no) {
		return sharingOrderRecord{}, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return sharingOrderRecord{}, e
	}
	defer tx.Rollback(ctx)
	r, e := s.scanSharingOrder(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingOrderColumns+" FROM channel_sharing_orders WHERE merchant_id=$1 AND external_no=$2", mid, no))
	if e != nil {
		return r, e
	}
	return r, tx.Commit(ctx)
}

// readSharingOrderID is internal to durable reconciliation. Public reads always
// use readSharingOrder and validate the merchant actor before any lookup.
func (s *Store) readSharingOrderID(ctx context.Context, id int64) (sharingOrderRecord, error) {
	if id <= 0 {
		return sharingOrderRecord{}, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return sharingOrderRecord{}, e
	}
	defer tx.Rollback(ctx)
	r, e := s.scanSharingOrder(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingOrderColumns+" FROM channel_sharing_orders WHERE id=$1", id))
	if e != nil {
		return r, e
	}
	return r, tx.Commit(ctx)
}
func (g ProviderFlow) sharingOrderProvider(ctx context.Context, q fundsReader, r sharingOrderRecord) (adapter.ChannelSharingProvider, error) {
	orderID := r.OrderID
	if orderID == 0 {
		orderID = r.Payment.OrderID
	}
	original, e := scanOrder(q.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1 AND merchant_id=$2", orderID, r.MerchantID))
	if e != nil {
		return nil, e
	}
	g, _, e = g.ForPaymentIntentTx(ctx, q, original.MerchantID, original.OutTradeNo, r.Payment.ChannelID)
	if e != nil {
		return nil, e
	}
	p, b, e := g.sharingChannel(r.MerchantID, r.Payment.ChannelID)
	if e != nil {
		return nil, e
	}
	h, e := g.Store.sharingDigest("account", []byte(b.AccountScope))
	if e != nil {
		return nil, e
	}
	if b.Provider != r.Payment.Provider || b.AppID != r.Payment.AppID || b.Product != r.Payment.Product || !hmac.Equal(h, r.Payment.AccountHash) {
		return nil, ErrConflict
	}
	if e = g.Store.checkProviderAccount(ctx, q, r.Payment.AttemptID, g.Providers[r.Payment.ChannelID]); e != nil {
		return nil, e
	}
	return p, nil
}
func (s *Store) sharingOrderRequest(ctx context.Context, tx pgx.Tx, r sharingOrderRecord, requireBound bool) (adapter.ChannelSharingRequest, error) {
	request := adapter.ChannelSharingRequest{OrderNo: r.OutNo, ProviderTradeNo: r.ProviderTradeNo, TotalAmount: r.Payment.GrossAmount, UnfreezeUnsplit: r.AutoFinish}
	if !r.Payment.Declared || len(r.Payment.Items) == 0 {
		return request, ErrState
	}
	for _, item := range r.Payment.Items {
		row, e := scanSharingReceiver(tx.QueryRow(ctx, "SELECT "+sharingReceiverColumns+" FROM channel_sharing_receivers WHERE id=$1 AND merchant_id=$2", item.ReceiverID, r.MerchantID))
		if e != nil {
			return request, e
		}
		if requireBound && row.State != "bound" {
			return request, ErrState
		}
		if row.Binding.ChannelID != r.Payment.ChannelID || row.Binding.Provider != r.Payment.Provider || row.Binding.AppID != r.Payment.AppID || row.Binding.Product != r.Payment.Product || !hmac.Equal(row.AccountHash, r.Payment.AccountHash) {
			return request, ErrConflict
		}
		input, e := s.openSharingReceiverRecord(row)
		if e != nil {
			return request, e
		}
		request.Receivers = append(request.Receivers, adapter.SharingAllocation{SharingReceiver: input, Amount: item.Amount, Description: "订单渠道分账"})
	}
	return request, nil
}
func (s *Store) readSharingOrderRequest(ctx context.Context, r sharingOrderRecord) (adapter.ChannelSharingRequest, error) {
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return adapter.ChannelSharingRequest{}, e
	}
	defer tx.Rollback(ctx)
	request, e := s.sharingOrderRequest(ctx, tx, r, false)
	if e != nil {
		return request, e
	}
	return request, tx.Commit(ctx)
}
