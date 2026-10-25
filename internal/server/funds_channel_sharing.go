package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/payment"
)

func channelSharingFundsTarget(p []string) (fundsTargetDefinition, bool) {
	if len(p) < 2 || p[0] != "channel-sharing" {
		return fundsTargetDefinition{}, false
	}
	if len(p) == 2 && p[1] == "orders" {
		return fundsTargetDefinition{guard: "channel-sharing-submit", category: "channel-sharing", keys: []string{"merchant_id", "trade_no", "external_no"}}, true
	}
	if len(p) == 2 && p[1] == "returns" {
		return fundsTargetDefinition{guard: "channel-sharing-return", category: "channel-sharing-return", keys: []string{"merchant_id", "sharing_external_no", "receiver_id", "external_no", "amount"}}, true
	}
	if len(p) == 2 && p[1] == "finishes" {
		return fundsTargetDefinition{guard: "channel-sharing-finish", category: "channel-sharing-finish", keys: []string{"merchant_id", "trade_no", "external_no"}}, true
	}
	if len(p) >= 3 {
		if _, ok := positiveFundsID(p[2]); !ok {
			return fundsTargetDefinition{}, false
		}
	}
	if len(p) == 3 && p[1] == "rules" {
		return fundsTargetDefinition{guard: "channel-sharing-rule", category: "channel-sharing-rule", id: p[2], keys: []string{"merchant_id", "expected", "enabled", "minimum_amount", "methods", "items"}}, true
	}
	if len(p) == 4 && p[1] == "rules" && p[3] == "disable" {
		return fundsTargetDefinition{guard: "channel-sharing-disable", category: "channel-sharing-disable", id: p[2], keys: []string{"merchant_id", "expected"}}, true
	}
	if len(p) == 4 && p[1] == "receivers" && (p[3] == "bind" || p[3] == "unbind") {
		kind := "channel-sharing-" + p[3]
		return fundsTargetDefinition{guard: kind, category: kind, id: p[2], keys: []string{"merchant_id", "external_no"}}, true
	}
	return fundsTargetDefinition{}, false
}

func normalizeSharingFundsBody(raw []byte, d fundsTargetDefinition) (map[string]json.RawMessage, []byte, error) {
	var fields map[string]json.RawMessage
	if strictFundsJSON(raw, &fields) != nil || fields == nil {
		return nil, nil, payment.ErrInvalid
	}
	allowed := map[string]bool{}
	for _, key := range d.keys {
		allowed[key] = true
	}
	for key, value := range fields {
		if !allowed[key] {
			return nil, nil, payment.ErrInvalid
		}
		switch key {
		case "enabled":
			var b bool
			if json.Unmarshal(value, &b) != nil || string(value) == "null" {
				return nil, nil, payment.ErrInvalid
			}
		case "methods":
			var a []string
			if json.Unmarshal(value, &a) != nil || len(a) < 1 || len(a) > 10 {
				return nil, nil, payment.ErrInvalid
			}
		case "items":
			var a []payment.ChannelSharingRuleItem
			decoder := json.NewDecoder(bytes.NewReader(value))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&a) != nil || len(a) < 1 || len(a) > 50 {
				return nil, nil, payment.ErrInvalid
			}
		default:
			var text string
			if json.Unmarshal(value, &text) != nil || string(value) == "null" {
				return nil, nil, payment.ErrInvalid
			}
			switch key {
			case "merchant_id", "receiver_id", "amount":
				if _, ok := positiveFundsID(text); !ok {
					return nil, nil, payment.ErrInvalid
				}
			case "expected", "minimum_amount":
				n, e := strconv.ParseInt(text, 10, 64)
				if e != nil || n < 0 || strconv.FormatInt(n, 10) != text {
					return nil, nil, payment.ErrInvalid
				}
			case "external_no", "trade_no", "sharing_external_no":
				if !sharingConsoleNo.MatchString(text) {
					return nil, nil, payment.ErrInvalid
				}
			}
		}
	}
	for _, key := range d.keys {
		if _, ok := fields[key]; !ok {
			return nil, nil, payment.ErrInvalid
		}
	}
	canonical, e := json.Marshal(fields)
	return fields, canonical, e
}

// Bind authorization to the immutable original declaration, exact version and
// receiver identities. Raw receiving accounts never enter the preview response.
func (s *Server) fundsChannelSharing(ctx context.Context, q identity.FundsQuerier, d fundsTargetDefinition, fields map[string]json.RawMessage, summary map[string]string) (int64, int64, json.RawMessage, error) {
	mid, ok := positiveFundsID(fundsText(fields, "merchant_id"))
	if !ok {
		return 0, 0, nil, payment.ErrInvalid
	}
	var channel, gross, net, total int64
	var raw []byte
	var e error
	id, _ := positiveFundsID(d.id)
	switch d.category {
	case "channel-sharing-finish":
		e = q.QueryRow(ctx, `SELECT d.channel_id,d.gross_amount,d.merchant_net,jsonb_build_object('order',jsonb_build_object('id',o.id,'trade_no',o.trade_no,'merchant_id',o.merchant_id),
 'declaration',to_jsonb(d),'funding',to_jsonb(f),'sharing',(SELECT to_jsonb(s) FROM channel_sharing_orders s WHERE s.order_id=o.id),
 'finishes',COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY p.id) FROM channel_sharing_finishes p WHERE p.order_id=o.id),'[]'::jsonb),'receipt',(SELECT to_jsonb(r) FROM verified_payment_receipts r WHERE r.order_id=o.id))
 FROM orders o JOIN provider_attempt_sharing d ON d.order_id=o.id AND d.declared JOIN channel_sharing_fundings f ON f.order_id=o.id
 WHERE o.merchant_id=$1 AND o.trade_no=$2`, mid, fundsText(fields, "trade_no")).Scan(&channel, &gross, &net, &raw)
		summary["trade_no"] = fundsText(fields, "trade_no")
		summary["gross_amount"] = ledger.Format(gross)
		summary["merchant_net"] = ledger.Format(net)
		summary["expected_unsplit"] = ledger.Format(gross)
		summary["release_amount"] = ledger.Format(net)
		summary["funding_source"] = "original_order_channel_reserve"
		summary["channel_outflow"] = "0.00"
	case "channel-sharing-return":
		receiverID, _ := positiveFundsID(fundsText(fields, "receiver_id"))
		e = q.QueryRow(ctx, `SELECT d.channel_id,jsonb_build_object('sharing',to_jsonb(s),'original_declaration',to_jsonb(d),'receiver_result',to_jsonb(i),'completion',to_jsonb(c),
 'existing_returns',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',r.id,'external_no',r.external_no,'amount',r.amount,'state',r.state) ORDER BY r.id) FROM channel_sharing_returns r WHERE r.sharing_id=s.id AND r.receiver_id=i.receiver_id),'[]'::jsonb))
 FROM channel_sharing_orders s JOIN provider_attempt_sharing d ON d.order_id=s.order_id JOIN channel_sharing_order_items i ON i.sharing_id=s.id AND i.receiver_id=$3
 JOIN channel_sharing_completions c ON c.sharing_id=s.id WHERE s.merchant_id=$1 AND s.external_no=$2 AND s.completed AND i.state='succeeded'`, mid, fundsText(fields, "sharing_external_no"), receiverID).Scan(&channel, &raw)
		summary["original_sharing_request"] = fundsText(fields, "sharing_external_no")
		summary["external_no"] = fundsText(fields, "external_no")
		amount, _ := positiveFundsID(fundsText(fields, "amount"))
		summary["return_amount"] = ledger.Format(amount)
		summary["amount"] = ledger.Format(amount)
		summary["funding_source"] = "verified_original_receiver_outflow"
		summary["buyer_refund"] = "false"
		if e == nil {
			receiver, err := s.Store.FundsChannelSharingReceiverView(ctx, q, receiverID)
			if err != nil {
				return 0, 0, nil, err
			}
			if receiver.MerchantID != mid {
				return 0, 0, nil, payment.ErrNotFound
			}
			summary["recipient"] = receiver.AccountMasked
			summary["recipient_name"] = receiver.NameMasked
		}
	case "channel-sharing":
		e = q.QueryRow(ctx, `SELECT d.channel_id,d.gross_amount,d.merchant_net,COALESCE((SELECT sum(i.amount) FROM provider_attempt_sharing_items i WHERE i.attempt_id=d.attempt_id),0),
 jsonb_build_object('order',jsonb_build_object('id',o.id,'trade_no',o.trade_no,'merchant_id',o.merchant_id),'declaration',to_jsonb(d),
 'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY i.position) FROM provider_attempt_sharing_items i WHERE i.attempt_id=d.attempt_id),
 'funding',to_jsonb(f),'receipt',(SELECT jsonb_build_object('order_id',r.order_id,'provider_trade_no',r.provider_trade_no) FROM verified_payment_receipts r WHERE r.order_id=o.id))
 FROM orders o JOIN provider_attempt_sharing d ON d.order_id=o.id AND d.declared JOIN channel_sharing_fundings f ON f.order_id=o.id
 WHERE o.trade_no=$1 AND o.merchant_id=$2`, fundsText(fields, "trade_no"), mid).Scan(&channel, &gross, &net, &total, &raw)
		summary["trade_no"] = fundsText(fields, "trade_no")
		summary["external_no"] = fundsText(fields, "external_no")
		summary["gross_amount"] = ledger.Format(gross)
		summary["merchant_net"] = ledger.Format(net)
		summary["channel_outflow"] = ledger.Format(total)
		summary["funding_source"] = "original_order_channel_reserve"
		if e == nil {
			var original struct {
				Items []struct {
					ReceiverID int64 `json:"receiver_id"`
					Amount     int64 `json:"amount"`
				} `json:"items"`
			}
			if json.Unmarshal(raw, &original) != nil {
				return 0, 0, nil, payment.ErrInvalid
			}
			for _, item := range original.Items {
				receiver, err := s.Store.FundsChannelSharingReceiverView(ctx, q, item.ReceiverID)
				if err != nil {
					return 0, 0, nil, err
				}
				if receiver.MerchantID != mid {
					return 0, 0, nil, payment.ErrNotFound
				}
				summary[fmt.Sprintf("接收方 %d", item.ReceiverID)] = fmt.Sprintf("%s · %s · %s 元", receiver.AccountMasked, receiver.NameMasked, ledger.Format(item.Amount))
			}
		}
	case "channel-sharing-rule", "channel-sharing-disable":
		channel = id
		e = q.QueryRow(ctx, `SELECT jsonb_build_object('current',(SELECT to_jsonb(r) FROM channel_sharing_rule_current c JOIN channel_sharing_rules r ON r.id=c.rule_id WHERE c.merchant_id=$1 AND c.channel_id=$2),
 'receivers',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',r.id,'channel_id',r.channel_id,'state',r.state,'input_hash',encode(r.input_hash,'hex'),'account_hash',encode(r.account_hash,'hex')) ORDER BY r.id) FROM channel_sharing_receivers r WHERE r.merchant_id=$1 AND r.channel_id=$2),'[]'::jsonb))`, mid, channel).Scan(&raw)
		summary["expected_version"] = fundsText(fields, "expected")
		summary["effect"] = "future_payment_declarations_only"
		if d.category == "channel-sharing-rule" {
			var items []payment.ChannelSharingRuleItem
			if json.Unmarshal(fields["items"], &items) != nil {
				return 0, 0, nil, payment.ErrInvalid
			}
			for _, item := range items {
				receiver, err := s.Store.FundsChannelSharingReceiverView(ctx, q, item.ReceiverID)
				if err != nil {
					return 0, 0, nil, err
				}
				if receiver.MerchantID != mid || receiver.ChannelID != channel {
					return 0, 0, nil, payment.ErrNotFound
				}
				value := fmt.Sprintf("%s · %s · 万分比 %d", receiver.AccountMasked, receiver.NameMasked, item.RateBPS)
				if item.Mode == "fixed" {
					value = fmt.Sprintf("%s · %s · 固定 %s 元", receiver.AccountMasked, receiver.NameMasked, ledger.Format(item.FixedAmount))
				}
				summary[fmt.Sprintf("接收方 %d", item.ReceiverID)] = value
			}
		}
	case "channel-sharing-bind", "channel-sharing-unbind":
		e = q.QueryRow(ctx, `SELECT channel_id,jsonb_build_object('id',id,'merchant_id',merchant_id,'channel_id',channel_id,'provider',provider,'app_id',app_id,'product',product,'input_hash',encode(input_hash,'hex'),'account_hash',encode(account_hash,'hex'),'relation_hash',encode(relation_hash,'hex'),'state',state) FROM channel_sharing_receivers WHERE id=$1 AND merchant_id=$2`, id, mid).Scan(&channel, &raw)
		summary["receiver_id"] = fmt.Sprint(id)
		summary["external_no"] = fundsText(fields, "external_no")
		if e == nil {
			receiver, err := s.Store.FundsChannelSharingReceiverView(ctx, q, id)
			if err != nil {
				return 0, 0, nil, err
			}
			if receiver.MerchantID != mid {
				return 0, 0, nil, payment.ErrNotFound
			}
			summary["recipient"] = receiver.AccountMasked
			summary["recipient_name"] = receiver.NameMasked
		}
	default:
		return 0, 0, nil, payment.ErrInvalid
	}
	if errors.Is(e, pgx.ErrNoRows) {
		e = payment.ErrNotFound
	}
	if e != nil {
		return 0, 0, nil, e
	}
	summary["channel_id"] = fmt.Sprint(channel)
	if d.category == "channel-sharing-disable" {
		return mid, 0, raw, nil
	}
	if s.ProviderFlow == nil || s.Sandbox || s.ProviderFlow.LocalSimulation || !s.sharingConsoleChannel(mid, channel) {
		return 0, 0, nil, adapter.ErrUnsupported
	}
	p, ok := s.ProviderFlow.Providers[channel].(adapter.ChannelSharingProvider)
	if !ok || p.ChannelSharingCapabilities().Product == "" {
		return 0, 0, nil, adapter.ErrUnsupported
	}
	if d.category == "channel-sharing-finish" && !p.ChannelSharingCapabilities().FinishSupported {
		return 0, 0, nil, adapter.ErrUnsupported
	}
	// Provider sharing flags and product profiles are separate from payment caps.
	var snapshot any
	if json.Unmarshal(raw, &snapshot) != nil {
		return 0, 0, nil, payment.ErrInvalid
	}
	combined, _ := json.Marshal(map[string]any{"source": snapshot, "sharing_capabilities": p.ChannelSharingCapabilities()})
	return mid, channel, combined, nil
}
