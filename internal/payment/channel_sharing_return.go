package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

type ChannelSharingReturnView struct {
	ID               int64     `json:"id,string"`
	SharingID        int64     `json:"sharing_id,string"`
	OrderID          int64     `json:"order_id,string"`
	MerchantID       int64     `json:"merchant_id,string"`
	ReceiverID       int64     `json:"receiver_id,string"`
	ExternalNo       string    `json:"external_no"`
	OutNo            string    `json:"out_no"`
	Amount           int64     `json:"amount,string"`
	State            string    `json:"state"`
	ProviderReturnNo string    `json:"provider_return_no"`
	CreatedAt        time.Time `json:"created_at"`
}
type ChannelSharingReturnPage struct {
	Items      []ChannelSharingReturnView `json:"items"`
	NextCursor int64                      `json:"next_cursor,string"`
}
type sharingReturnRecord struct {
	ChannelSharingReturnView
	RequestHash       []byte
	FailureProof      []byte
	Actor             TransferBatchActor
	Original          sharingOrderRecord
	LastObservationID int64
	PaidJournalID     int64
}

const sharingReturnColumns = "id,sharing_id,order_id,merchant_id,receiver_id,external_no,out_no,amount,state,provider_return_no,created_at,request_hash,actor_role,actor_id,actor_name,actor_class,COALESCE(last_observation_id,0),COALESCE(paid_journal_id,0),failure_proof"

func (s *Store) sharingReturnFingerprint(r sharingReturnRecord) ([]byte, error) {
	raw, e := json.Marshal([]any{r.ID, r.SharingID, r.OrderID, r.MerchantID, r.ReceiverID, r.ExternalNo, r.OutNo, r.Amount, hex.EncodeToString(r.Original.RequestHash), r.Original.ProviderTradeNo, r.Original.ProviderOrderNo})
	if e != nil {
		return nil, e
	}
	return s.sharingDigest("sharing-return", raw)
}
func (s *Store) sharingReturnObservationFingerprint(r sharingReturnRecord, result adapter.ChannelSharingReturnResult) ([]byte, error) {
	raw, e := json.Marshal([]any{r.ID, hex.EncodeToString(r.RequestHash), r.Original.OutNo, result})
	if e != nil {
		return nil, e
	}
	return s.sharingDigest("sharing-return-observation", raw)
}
func (s *Store) scanSharingReturn(ctx context.Context, tx referralReader, row pgx.Row) (sharingReturnRecord, error) {
	var r sharingReturnRecord
	e := row.Scan(&r.ID, &r.SharingID, &r.OrderID, &r.MerchantID, &r.ReceiverID, &r.ExternalNo, &r.OutNo, &r.Amount, &r.State, &r.ProviderReturnNo, &r.CreatedAt, &r.RequestHash, &r.Actor.Role, &r.Actor.ID, &r.Actor.Name, &r.Actor.Class, &r.LastObservationID, &r.PaidJournalID, &r.FailureProof)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	r.Original, e = s.scanSharingOrder(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingOrderColumns+" FROM channel_sharing_orders WHERE id=$1", r.SharingID))
	if e != nil {
		return r, e
	}
	if r.OrderID != r.Original.OrderID || r.MerchantID != r.Original.MerchantID || !r.Original.Completed || !r.Original.FundsReleased || r.Amount <= 0 {
		return r, ErrConflict
	}
	found := false
	for _, item := range r.Original.Items {
		if item.ReceiverID == r.ReceiverID {
			found = item.State == "succeeded" && r.Amount <= item.Amount
			break
		}
	}
	if !found {
		return r, ErrConflict
	}
	hash, e := s.sharingReturnFingerprint(r)
	if e != nil || !hmac.Equal(hash, r.RequestHash) {
		return r, ErrConflict
	}
	if r.LastObservationID == 0 {
		if r.ProviderReturnNo != "" || r.PaidJournalID != 0 || r.State != "claimed" && r.State != "unknown" && r.State != "failed" {
			return r, ErrConflict
		}
		if r.State == "failed" {
			proof, e := s.sharingReturnFailureFingerprint(r)
			if e != nil || !hmac.Equal(proof, r.FailureProof) {
				return r, ErrConflict
			}
		} else if len(r.FailureProof) != 0 {
			return r, ErrConflict
		}
	} else {
		if len(r.FailureProof) != 0 {
			return r, ErrConflict
		}
		result := adapter.ChannelSharingReturnResult{OrderNo: r.Original.OutNo, ReturnNo: r.OutNo}
		var parent int64
		var proof []byte
		if e = tx.QueryRow(ctx, "SELECT return_id,provider_return_no,state,amount,fingerprint FROM channel_sharing_return_observations WHERE id=$1", r.LastObservationID).Scan(&parent, &result.ProviderReturnNo, &result.State, &result.Amount, &proof); e != nil {
			return r, e
		}
		if parent != r.ID || result.ProviderReturnNo != r.ProviderReturnNo || result.State != r.State || result.Amount != r.Amount || result.State != "pending" && result.State != "succeeded" && result.State != "failed" || (r.State == "succeeded") != (r.PaidJournalID != 0) {
			return r, ErrConflict
		}
		if e = validateSharingReturnResult(r, result); e != nil {
			return r, e
		}
		hash, e = s.sharingReturnObservationFingerprint(r, result)
		if e != nil || !hmac.Equal(hash, proof) {
			return r, ErrConflict
		}
	}
	return r, nil
}

func (s *Store) sharingReturnFailureFingerprint(r sharingReturnRecord) ([]byte, error) {
	raw, e := json.Marshal([]any{r.ID, hex.EncodeToString(r.RequestHash), "failed"})
	if e != nil {
		return nil, e
	}
	return s.sharingDigest("sharing-return-failure", raw)
}
func (s *Store) readSharingReturn(ctx context.Context, mid int64, no string, actor TransferBatchActor) (sharingReturnRecord, error) {
	if !actor.valid(mid) || !channelSharingExternalNo.MatchString(no) {
		return sharingReturnRecord{}, ErrInvalid
	}
	return s.readSharingReturnWhere(ctx, "merchant_id=$1 AND external_no=$2", mid, no)
}

// Only durable internal reconciliation may call by ID. Public owner checks are
// performed before a lookup and cannot be bypassed using a system actor.
func (s *Store) readSharingReturnID(ctx context.Context, id int64) (sharingReturnRecord, error) {
	if id <= 0 {
		return sharingReturnRecord{}, ErrInvalid
	}
	return s.readSharingReturnWhere(ctx, "id=$1", id)
}
func (s *Store) readSharingReturnWhere(ctx context.Context, where string, args ...any) (sharingReturnRecord, error) {
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return sharingReturnRecord{}, e
	}
	defer tx.Rollback(ctx)
	r, e := s.scanSharingReturn(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingReturnColumns+" FROM channel_sharing_returns WHERE "+where, args...))
	if e != nil {
		return r, e
	}
	return r, tx.Commit(ctx)
}
func (s *Store) ChannelSharingReturn(ctx context.Context, mid int64, no string, actor TransferBatchActor) (ChannelSharingReturnView, error) {
	r, e := s.readSharingReturn(ctx, mid, no, actor)
	return r.ChannelSharingReturnView, e
}
func (s *Store) ListChannelSharingReturns(ctx context.Context, mid, cursor int64, limit int, actor TransferBatchActor) (ChannelSharingReturnPage, error) {
	page := ChannelSharingReturnPage{Items: []ChannelSharingReturnView{}}
	if !actor.valid(mid) || cursor < 0 || limit < 1 || limit > 100 {
		return page, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return page, e
	}
	defer tx.Rollback(ctx)
	ids, next, e := sharingPageIDs(ctx, tx, "SELECT id FROM channel_sharing_returns WHERE merchant_id=$1 AND id>$2 ORDER BY id LIMIT $3", mid, cursor, limit)
	if e != nil {
		return page, e
	}
	for _, id := range ids {
		r, e := s.scanSharingReturn(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingReturnColumns+" FROM channel_sharing_returns WHERE id=$1 AND merchant_id=$2", id, mid))
		if e != nil {
			return page, e
		}
		page.Items = append(page.Items, r.ChannelSharingReturnView)
	}
	page.NextCursor = next
	return page, tx.Commit(ctx)
}
func (g ProviderFlow) sharingReturnProvider(ctx context.Context, q fundsReader, r sharingReturnRecord) (adapter.ChannelSharingProvider, error) {
	p, e := g.sharingOrderProvider(ctx, q, r.Original)
	if e != nil {
		return nil, e
	}
	if !p.ChannelSharingCapabilities().ReturnSupported {
		return nil, adapter.ErrUnsupported
	}
	return p, nil
}
func (s *Store) sharingReturnRequest(ctx context.Context, tx pgx.Tx, r sharingReturnRecord) (adapter.ChannelSharingReturnRequest, error) {
	request := adapter.ChannelSharingReturnRequest{OrderNo: r.Original.OutNo, ProviderOrderNo: r.Original.ProviderOrderNo, ProviderTradeNo: r.Original.ProviderTradeNo, ReturnNo: r.OutNo, Amount: r.Amount, Description: "原渠道分账回退"}
	original, e := s.sharingOrderRequest(ctx, tx, r.Original, false)
	if e != nil {
		return request, e
	}
	for i, item := range r.Original.Payment.Items {
		if item.ReceiverID == r.ReceiverID {
			request.ReceiverType = original.Receivers[i].Type
			request.ReceiverAccount = original.Receivers[i].Account
			break
		}
	}
	switch r.Original.Payment.Product {
	case "wxpay_v3_direct", "wxpay_v2_single_direct":
		if request.ReceiverType != "MERCHANT_ID" {
			return request, adapter.ErrUnsupported
		}
		accountLimit := 32
		if r.Original.Payment.Product == "wxpay_v2_single_direct" {
			accountLimit = 64
		}
		if !sharingSafeField(request.ProviderOrderNo, 64, true) || !sharingSafeField(request.ReceiverAccount, accountLimit, true) {
			return request, ErrInvalid
		}
	case "alipay_direct":
		if request.ReceiverType != "userId" || !sharingCanonicalAlipayUID.MatchString(request.ReceiverAccount) {
			return request, adapter.ErrUnsupported
		}
	default:
		return request, adapter.ErrUnsupported
	}
	return request, nil
}
func (s *Store) readSharingReturnRequest(ctx context.Context, r sharingReturnRecord) (adapter.ChannelSharingReturnRequest, error) {
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return adapter.ChannelSharingReturnRequest{}, e
	}
	defer tx.Rollback(ctx)
	request, e := s.sharingReturnRequest(ctx, tx, r)
	if e != nil {
		return request, e
	}
	return request, tx.Commit(ctx)
}
func validateSharingReturnResult(r sharingReturnRecord, result adapter.ChannelSharingReturnResult) error {
	if result.OrderNo != r.Original.OutNo || result.ReturnNo != r.OutNo || result.Amount != r.Amount || !sharingSafeField(result.ProviderReturnNo, 64, r.Original.Payment.Provider == "wxpay") || r.ProviderReturnNo != "" && result.ProviderReturnNo != r.ProviderReturnNo {
		return adapter.ErrAuthentication
	}
	if result.State != "pending" && result.State != "succeeded" && result.State != "failed" {
		return adapter.ErrAuthentication
	}
	return nil
}

// Source-budget and refund callers must authenticate retained provider facts,
// in addition to the SQL evidence that enforces the journal shape.
func (s *Store) verifyOrderSharingReturnEvidence(ctx context.Context, tx referralReader, orderID int64) error {
	var sharingID int64
	e := tx.QueryRow(ctx, "SELECT id FROM channel_sharing_orders WHERE order_id=$1", orderID).Scan(&sharingID)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if _, e = s.scanSharingOrder(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingOrderColumns+" FROM channel_sharing_orders WHERE id=$1", sharingID)); e != nil {
		return e
	}
	var exists bool
	if e = tx.QueryRow(ctx, "SELECT to_regclass('channel_sharing_returns') IS NOT NULL").Scan(&exists); e != nil {
		return e
	}
	if !exists {
		return nil
	}
	for cursor := int64(0); ; {
		ids, e := sharingEvidenceIDsPage(ctx, tx, "SELECT id FROM channel_sharing_returns WHERE order_id=$1 AND id>$2 ORDER BY id LIMIT 100", orderID, cursor)
		if e != nil {
			return e
		}
		for _, id := range ids {
			if _, e = s.scanSharingReturn(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingReturnColumns+" FROM channel_sharing_returns WHERE id=$1", id)); e != nil {
				return e
			}
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	return nil
}

func sharingEvidenceIDsPage(ctx context.Context, tx referralReader, query string, args ...any) ([]int64, error) {
	rows, e := tx.Query(ctx, query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ids := make([]int64, 0, 100)
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
