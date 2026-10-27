package server

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/payment"
	"math"
)

func (s *Server) resolveOrderBusinessBatchFunds185(ctx context.Context, q identity.FundsQuerier, d fundsTargetDefinition, target string, canonical []byte) (identity.FundsAction, map[string]string, error) {
	var zero identity.FundsAction
	id, _ := positiveFundsID(d.id)
	selection, e := s.Store.OrderBusinessBatchFundsSelection(ctx, q, id)
	if e != nil {
		return zero, nil, e
	}
	var total int64
	digests := []string{}
	for _, item := range selection.Items {
		if item.Amount <= 0 || total > math.MaxInt64-item.Amount {
			return zero, nil, payment.ErrInvalid
		}
		total += item.Amount
		fields := map[string]string{"amount": ledger.Format(item.Amount), "external_no": item.ExternalNo}
		if selection.Action == "freeze-income" {
			fields = map[string]string{"external_no": item.ExternalNo}
		}
		if selection.Action == "unfreeze-income" {
			fields = map[string]string{"hold_id": fmt.Sprint(item.HoldID)}
		}
		body, _ := json.Marshal(fields)
		action, _, err := s.ResolveFundsAction(ctx, q, "/console/v1/admin/orders/"+item.TradeNo+"/"+selection.Action, body)
		if err != nil {
			return zero, nil, err
		}
		digests = append(digests, hex.EncodeToString(action.Digest()))
	}
	raw, _ := json.Marshal([]any{selection.ID, selection.Action, selection.Reason, selection.SourceHash, digests})
	bodyHash, e := s.Store.FundsActionHMAC("order-batch185/body", canonical)
	if e != nil {
		return zero, nil, e
	}
	snapshotHash, e := s.Store.FundsActionHMAC("order-batch185/selection", raw)
	if e != nil {
		return zero, nil, e
	}
	action, e := identity.NewFundsAction("admin.order_business_batch", target, map[string]string{"body_hmac": bodyHash, "snapshot_hmac": snapshotHash})
	label := map[string]string{"refund": "批量API退款", "freeze-income": "批量冻结订单", "unfreeze-income": "批量解冻订单"}[selection.Action]
	return action, map[string]string{"action": label, "target": target, "batch_id": fmt.Sprint(id), "items_count": fmt.Sprint(len(selection.Items)), "amount": ledger.Format(total), "reason": selection.Reason, "policy": "只授权本批固定订单、原资金来源和核对金额；已发送或未知仅按原号恢复"}, e
}
