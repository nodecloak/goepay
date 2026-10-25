package payment

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

type sharingCompletionEvidence struct {
	Kind              string
	ExpectedUnsplit   int64
	ObservedRemaining *int64
	ReleaseAmount     int64
}

func sharingResultActorValid(actor TransferBatchActor, mid int64) bool {
	return actor.valid(mid) || actor.Role == "system" && actor.ID == 0 && actor.Name == "channel-sharing-worker" && actor.Class == ""
}

func sharingFailureCode(state, raw string) string {
	if state != "failed" {
		return ""
	}
	// Free-form provider reasons can contain the full receiver account or name.
	// Keep only explicit non-personal categories outside encrypted evidence.
	switch raw {
	case "NO_AUTH", "NO_RELATION", "ACCOUNT_ABNORMAL", "RECEIVER_ACCOUNT_ABNORMAL":
		return raw
	}
	return "PROVIDER_DECLINED"
}

func (s *Store) sharingCompletionFingerprint(r sharingOrderRecord, proof sharingCompletionEvidence) ([]byte, error) {
	if !r.Completed || !r.AutoFinish || r.LastObservationID <= 0 {
		return nil, ErrState
	}
	success := int64(0)
	for _, item := range r.Items {
		if item.State == "succeeded" {
			success += item.Amount
		} else if item.State != "failed" {
			return nil, ErrState
		}
	}
	if proof.ExpectedUnsplit != r.Payment.GrossAmount-success || proof.ReleaseAmount != r.Payment.MerchantNet-success {
		return nil, ErrConflict
	}
	switch proof.Kind {
	case "wechat_remaining_zero":
		if !wechatSharingProduct(r.Payment.Product) || proof.ObservedRemaining == nil || *proof.ObservedRemaining != 0 {
			return nil, ErrInvalid
		}
	case "alipay_royalty_finish":
		if r.Payment.Product != "alipay_direct" || proof.ObservedRemaining != nil {
			return nil, ErrInvalid
		}
	default:
		return nil, ErrInvalid
	}
	raw, e := json.Marshal([]any{r.ID, r.OrderID, hex.EncodeToString(r.RequestHash), r.ProviderOrderNo, r.ProviderTradeNo, r.LastObservationID, proof})
	if e != nil {
		return nil, e
	}
	return s.sharingDigest("sharing-completion", raw)
}

func normalizedSharingObservation(r sharingOrderRecord, request adapter.ChannelSharingRequest, result adapter.ChannelSharingResult) ([]ChannelSharingOrderItem, error) {
	if result.OrderNo != r.OutNo || result.ProviderTradeNo != r.ProviderTradeNo || !sharingSafeField(result.ProviderOrderNo, 64, r.Payment.Provider != "alipay") || r.ProviderOrderNo != "" && r.ProviderOrderNo != result.ProviderOrderNo || len(result.Receivers) != len(request.Receivers) || len(request.Receivers) != len(r.Payment.Items) {
		return nil, adapter.ErrAuthentication
	}
	if result.State != "pending" && result.State != "succeeded" && result.State != "failed" && result.State != "partial" {
		return nil, adapter.ErrAuthentication
	}
	lookup := map[string]int{}
	for i, input := range request.Receivers {
		lookup[input.Type+"\x00"+input.Account] = i
	}
	if len(lookup) != len(request.Receivers) {
		return nil, ErrConflict
	}
	seen := map[int]bool{}
	items := make([]ChannelSharingOrderItem, len(request.Receivers))
	success, failed := 0, 0
	for _, got := range result.Receivers {
		index, ok := lookup[got.Type+"\x00"+got.Account]
		if !ok || seen[index] || got.Amount != request.Receivers[index].Amount || !sharingSafeField(got.ProviderDetailNo, 64, false) {
			return nil, adapter.ErrAuthentication
		}
		seen[index] = true
		switch got.State {
		case "succeeded":
			if got.ProviderDetailNo == "" {
				return nil, adapter.ErrAuthentication
			}
			success++
		case "failed":
			failed++
		case "pending":
		default:
			return nil, adapter.ErrAuthentication
		}
		items[index] = ChannelSharingOrderItem{ReceiverID: r.Payment.Items[index].ReceiverID, Amount: got.Amount, State: got.State, ProviderDetailNo: got.ProviderDetailNo, FailureCode: sharingFailureCode(got.State, got.FailureCode)}
	}
	if result.State != "pending" {
		if success+failed != len(items) {
			return nil, adapter.ErrAuthentication
		}
		expected := "partial"
		if failed == 0 {
			expected = "succeeded"
		} else if success == 0 {
			expected = "failed"
		}
		if result.State != expected {
			return nil, adapter.ErrAuthentication
		}
	}
	return items, nil
}
func (s *Store) applySharingObservation(ctx context.Context, original sharingOrderRecord, request adapter.ChannelSharingRequest, result adapter.ChannelSharingResult, source string, actor TransferBatchActor) error {
	if !sharingResultActorValid(actor, original.MerchantID) || source != "submit" && source != "query" {
		return ErrInvalid
	}
	// Alipay's signed settle.query response identifies the original request and
	// its receiver details, but its documented schema has no settle_no field.
	// Preserve a previously authenticated acceptance number when available.
	if original.Payment.Provider == "alipay" && result.ProviderOrderNo == "" {
		result.ProviderOrderNo = original.ProviderOrderNo
	}
	items, e := normalizedSharingObservation(original, request, result)
	if e != nil {
		return e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT id FROM orders WHERE id=$1 FOR UPDATE", original.OrderID); e != nil {
		return e
	}
	current, e := s.scanSharingOrder(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingOrderColumns+" FROM channel_sharing_orders WHERE id=$1 AND merchant_id=$2 FOR UPDATE", original.ID, original.MerchantID))
	if e != nil {
		return e
	}
	if current.Completed {
		return tx.Commit(ctx)
	}
	if current.State == "unsent" || current.ProviderOrderNo != "" && current.ProviderOrderNo != result.ProviderOrderNo {
		return ErrConflict
	}
	if len(current.Items) != len(items) {
		return ErrConflict
	}
	for i, item := range items {
		old := current.Items[i]
		if old.ReceiverID != item.ReceiverID || old.Amount != item.Amount {
			return ErrConflict
		}
		if old.State == "succeeded" || old.State == "failed" {
			if item.State != "pending" && (item.State != old.State || item.ProviderDetailNo != old.ProviderDetailNo) {
				return ErrConflict
			}
			if item.State == "pending" && result.State != "pending" {
				return ErrConflict
			}
		}
	}
	raw, e := json.Marshal(items)
	if e != nil {
		return e
	}
	fact, e := json.Marshal([]any{current.ID, current.OutNo, current.ProviderTradeNo, result.ProviderOrderNo, result.State, items})
	if e != nil {
		return e
	}
	hash, e := s.sharingDigest("sharing-observation", fact)
	if e != nil {
		return e
	}
	var observation int64
	e = tx.QueryRow(ctx, `INSERT INTO channel_sharing_observations(sharing_id,fingerprint,source,provider_order_no,state,items,actor_role,actor_id,actor_name) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(sharing_id,fingerprint) DO NOTHING RETURNING id`, current.ID, hash, source, result.ProviderOrderNo, result.State, raw, actor.Role, actor.ID, actor.Name).Scan(&observation)
	if errors.Is(e, pgx.ErrNoRows) {
		if e = tx.QueryRow(ctx, "SELECT id FROM channel_sharing_observations WHERE sharing_id=$1 AND fingerprint=$2", current.ID, hash).Scan(&observation); e != nil {
			return e
		}
	} else if e != nil {
		return e
	}
	var clearing int64
	for i, item := range items {
		if current.Items[i].State == "succeeded" || current.Items[i].State == "failed" {
			continue
		}
		var journal any
		if item.State == "succeeded" {
			if clearing == 0 {
				if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=0 AND kind='clearing' AND scope_id=0").Scan(&clearing); e != nil {
					return e
				}
			}
			key := fmt.Sprintf("channel-sharing:%d:receiver:%d:paid", current.ID, item.ReceiverID)
			if e = postByAccountIDs(ctx, tx, key, "verified original channel sharing receiver", current.FundingAccount, clearing, item.Amount); e != nil {
				return e
			}
			var id int64
			if e = tx.QueryRow(ctx, "SELECT id FROM journals WHERE event_key=$1", key).Scan(&id); e != nil {
				return e
			}
			journal = id
		}
		if _, e = tx.Exec(ctx, "UPDATE channel_sharing_order_items SET state=$3,provider_detail_no=$4,failure_code=$5,last_observation_id=$6,paid_journal_id=$7 WHERE sharing_id=$1 AND receiver_id=$2", current.ID, item.ReceiverID, item.State, item.ProviderDetailNo, item.FailureCode, observation, journal); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_orders SET state=$2,provider_order_no=$3,completed=$4,last_observation_id=$5,failure_proof=NULL WHERE id=$1", current.ID, result.State, result.ProviderOrderNo, result.State != "pending", observation); e != nil {
		return e
	}
	if e = sharingConfigurationAudit(ctx, tx, actor, "channel-sharing-orders", current.ID, source+"-result", result.State); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (g ProviderFlow) QueryChannelSharing(ctx context.Context, mid int64, no string, actor TransferBatchActor) (ChannelSharingOrderView, error) {
	if g.Store == nil {
		return ChannelSharingOrderView{}, ErrInvalid
	}
	r, e := g.Store.readSharingOrder(ctx, mid, no, actor)
	if e != nil {
		return r.ChannelSharingOrderView, e
	}
	return g.querySharingOrderRecord(ctx, r, actor)
}

// ReconcileChannelSharing only observes a previously claimed operation. Its
// stable upstream number and account come from authenticated original records.
func (g ProviderFlow) ReconcileChannelSharing(ctx context.Context, id int64) (ChannelSharingOrderView, error) {
	if g.Store == nil {
		return ChannelSharingOrderView{}, ErrInvalid
	}
	r, e := g.Store.readSharingOrderID(ctx, id)
	if e != nil {
		return r.ChannelSharingOrderView, e
	}
	// A keyed rejection with no accepted observation is already terminal for
	// this PS intent. The separately authorized PF workflow handles its reserve.
	if r.State == "failed" && !r.Completed && r.LastObservationID == 0 {
		return r.ChannelSharingOrderView, nil
	}
	return g.querySharingOrderRecord(ctx, r, TransferBatchActor{Role: "system", Name: "channel-sharing-worker"})
}

func (g ProviderFlow) querySharingOrderRecord(ctx context.Context, r sharingOrderRecord, actor TransferBatchActor) (ChannelSharingOrderView, error) {
	if r.FundsReleased {
		return r.ChannelSharingOrderView, nil
	}
	if r.Completed {
		return g.finishObservedSharingRecord(ctx, r, actor)
	}
	if r.State == "unsent" {
		return r.ChannelSharingOrderView, ErrState
	}
	p, e := g.sharingOrderProvider(ctx, g.Store.pool, r)
	if e != nil {
		return r.ChannelSharingOrderView, e
	}
	request, e := g.Store.readSharingOrderRequest(ctx, r)
	if e != nil {
		return r.ChannelSharingOrderView, e
	}
	result, e := p.QueryChannelSharing(ctx, request)
	if e != nil {
		return r.ChannelSharingOrderView, e
	}
	if e = g.Store.applySharingObservation(ctx, r, request, result, "query", actor); e != nil {
		return r.ChannelSharingOrderView, e
	}
	r, e = g.Store.readSharingOrderID(ctx, r.ID)
	if e != nil {
		return r.ChannelSharingOrderView, e
	}
	return g.finishObservedSharingRecord(ctx, r, actor)
}
func (g ProviderFlow) finishObservedSharing(ctx context.Context, mid int64, no string, actor TransferBatchActor) (ChannelSharingOrderView, error) {
	r, e := g.Store.readSharingOrder(ctx, mid, no, actor)
	if e != nil {
		return r.ChannelSharingOrderView, e
	}
	return g.finishObservedSharingRecord(ctx, r, actor)
}

func (g ProviderFlow) finishObservedSharingRecord(ctx context.Context, r sharingOrderRecord, actor TransferBatchActor) (ChannelSharingOrderView, error) {
	if !r.Completed || r.FundsReleased {
		return r.ChannelSharingOrderView, nil
	}
	p, e := g.sharingOrderProvider(ctx, g.Store.pool, r)
	if e != nil {
		return r.ChannelSharingOrderView, e
	}
	proof := "alipay_royalty_finish"
	switch r.Payment.Product {
	case "wxpay_v3_direct", "wxpay_v2_single_direct":
		remaining, ok := p.(adapter.ChannelSharingRemainingProvider)
		if !ok {
			return r.ChannelSharingOrderView, adapter.ErrUnsupported
		}
		amount, e := remaining.ChannelSharingRemaining(ctx, r.ProviderTradeNo)
		if e != nil {
			return r.ChannelSharingOrderView, e
		}
		if amount != 0 {
			return r.ChannelSharingOrderView, ErrState
		}
		proof = "wechat_remaining_zero"
	case "alipay_direct":
		// Only a real PS with complete receiver outcomes and the original explicit
		// royalty_finish flag can use this contract. An empty standalone PF cannot.
		if !r.AutoFinish || len(r.Items) == 0 {
			return r.ChannelSharingOrderView, ErrState
		}
	default:
		return r.ChannelSharingOrderView, adapter.ErrUnsupported
	}
	if e = g.Store.completeChannelSharing(ctx, r, proof, actor); e != nil {
		return r.ChannelSharingOrderView, e
	}
	r, e = g.Store.readSharingOrderID(ctx, r.ID)
	return r.ChannelSharingOrderView, e
}
func (s *Store) completeChannelSharing(ctx context.Context, original sharingOrderRecord, proof string, actor TransferBatchActor) error {
	if !sharingResultActorValid(actor, original.MerchantID) || proof != "wechat_remaining_zero" && proof != "alipay_royalty_finish" {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT id FROM orders WHERE id=$1 FOR UPDATE", original.OrderID); e != nil {
		return e
	}
	r, e := s.scanSharingOrder(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingOrderColumns+" FROM channel_sharing_orders WHERE id=$1 FOR UPDATE", original.ID))
	if e != nil {
		return e
	}
	if r.FundsReleased {
		return tx.Commit(ctx)
	}
	if !r.Completed || !r.AutoFinish {
		return ErrState
	}
	released := r.Payment.MerchantNet
	expectedUnsplit := r.Payment.GrossAmount
	for _, item := range r.Items {
		if item.State == "succeeded" {
			released -= item.Amount
			expectedUnsplit -= item.Amount
		} else if item.State != "failed" {
			return ErrState
		}
	}
	if released < 0 {
		return ErrConflict
	}
	evidence := sharingCompletionEvidence{Kind: proof, ExpectedUnsplit: expectedUnsplit, ReleaseAmount: released}
	if proof == "wechat_remaining_zero" {
		zero := int64(0)
		evidence.ObservedRemaining = &zero
	}
	proofHash, e := s.sharingCompletionFingerprint(r, evidence)
	if e != nil {
		return e
	}
	var journal any
	if released > 0 {
		var available int64
		if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind='available' AND scope_id=0", r.MerchantID).Scan(&available); e != nil {
			return e
		}
		key := fmt.Sprintf("channel-sharing:%d:release", r.ID)
		if e = postByAccountIDs(ctx, tx, key, "verified channel sharing remainder released", r.FundingAccount, available, released); e != nil {
			return e
		}
		var id int64
		if e = tx.QueryRow(ctx, "SELECT id FROM journals WHERE event_key=$1", key).Scan(&id); e != nil {
			return e
		}
		journal = id
	}
	if _, e = tx.Exec(ctx, "INSERT INTO channel_sharing_completions(order_id,sharing_id,proof_kind,expected_unsplit,observed_remaining,proof_hash,release_amount,release_journal_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", r.OrderID, r.ID, proof, expectedUnsplit, evidence.ObservedRemaining, proofHash, released, journal); e != nil {
		return e
	}
	if e = sharingConfigurationAudit(ctx, tx, actor, "channel-sharing-orders", r.ID, "release-remainder", "completed"); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
