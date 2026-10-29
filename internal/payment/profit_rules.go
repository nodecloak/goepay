package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strconv"
)

// ExecuteProfitRule calculates a fixed-point share of a paid order, reserves the
// request, and records allocation in one transaction. Explicit review is separate.
func (s *Store) ExecuteProfitRule(ctx context.Context, ruleID int64, tradeNo, actor string) (LedgerRequest, error) {
	if ruleID <= 0 || tradeNo == "" || actor == "" {
		return LedgerRequest{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return LedgerRequest{}, e
	}
	defer tx.Rollback(ctx)
	o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE trade_no=$1 FOR UPDATE", tradeNo))
	if e != nil {
		return LedgerRequest{}, e
	}
	var existing int64
	e = tx.QueryRow(ctx, "SELECT request_id FROM order_profit_allocations WHERE order_id=$1 AND rule_id=$2 ORDER BY id LIMIT 1", o.ID, ruleID).Scan(&existing)
	if e == nil {
		// Replay: the rule already allocated to this order (any leg present).
		return scanLedgerRequest(tx.QueryRow(ctx, "SELECT "+requestColumns+" FROM ledger_requests WHERE id=$1", existing))
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return LedgerRequest{}, e
	}
	if o.Status != "paid" {
		return LedgerRequest{}, ErrState
	}
	if e = s.orderIncomeRefundAllowed(ctx, tx, o.ID); e != nil {
		return LedgerRequest{}, e
	}
	var owner int64
	var receiversRaw, rateRaw, recipientRaw *string
	e = tx.QueryRow(ctx, "SELECT owner_id,body->>'receivers',body->>'rate_bps',body->>'recipient_id' FROM backoffice_records WHERE resource='profitsharing-rules' AND id=$1 AND body->>'enabled'='true' FOR SHARE", ruleID).Scan(&owner, &receiversRaw, &rateRaw, &recipientRaw)
	if e != nil {
		return LedgerRequest{}, ErrNotFound
	}
	if owner != o.MerchantID {
		return LedgerRequest{}, ErrConflict
	}
	legs, e := profitRuleLegs(owner, receiversRaw, rateRaw, recipientRaw)
	if e != nil {
		return LedgerRequest{}, e
	}
	// One ledger request + allocation row per receiver leg, all in this single
	// transaction, so a mid-leg failure rolls the whole set back (no partial
	// money movement). Each leg's share is floor(order.amount * rate / 10000) —
	// a per-leg floor with the remainder dropped (never redistributed), matching
	// the original single-receiver rounding and enforced by order_profit_source
	// guard. The legs can never sum above the source: each allocation's guard
	// re-reads the retained-income remaining, which shrinks as earlier legs of
	// this same transaction are inserted.
	//
	// A single-leg rule keeps the ORIGINAL on-chain shape (recipient_id NULL,
	// external_no 'order:<trade>:rule:<rule>') so already-stored single rows stay
	// valid; a multi-leg rule writes per-recipient rows (recipient_id set,
	// ':recipient:<id>' external_no suffix).
	single := len(legs) == 1
	source, e := s.orderIncomeSource(ctx, tx, o)
	if e != nil {
		return LedgerRequest{}, e
	}
	var allocated int64
	var first LedgerRequest
	for i, leg := range legs {
		amount := o.Amount/10000*leg.rate + o.Amount%10000*leg.rate/10000
		if amount <= 0 {
			return LedgerRequest{}, ErrInvalid
		}
		// Typed insufficiency: the legs together never exceed the retained
		// original source. Checked in Go so the caller sees ErrInsufficient
		// rather than the guard's raw error; the DB guard remains the backstop.
		allocated += amount
		if allocated > source.Remaining {
			return LedgerRequest{}, ErrInsufficient
		}
		externalNo := fmt.Sprintf("order:%s:rule:%d", tradeNo, ruleID)
		var recipientCol any
		if !single {
			externalNo = fmt.Sprintf("order:%s:rule:%d:recipient:%d", tradeNo, ruleID, leg.recipient)
			recipientCol = leg.recipient
		}
		v, e := s.requestFundsTx(ctx, tx, "profitsharing", owner, leg.recipient, externalNo, amount, actor)
		if e != nil {
			return LedgerRequest{}, e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO order_profit_allocations(order_id,rule_id,request_id,amount,rate_bps,recipient_id) VALUES($1,$2,$3,$4,$5,$6)", o.ID, ruleID, v.ID, amount, leg.rate, recipientCol); e != nil {
			return LedgerRequest{}, e
		}
		if i == 0 {
			first = v
		}
	}
	return first, tx.Commit(ctx)
}

type profitLeg struct {
	recipient, rate int64
}

// profitRuleLegs reads a rule's receivers: the multi-receiver `receivers` JSON
// array when present, else the legacy single recipient_id+rate_bps. It enforces
// 1..10 distinct recipients (none equal to the owner) and each rate 1..10000;
// the rule-level Σ<=100% was enforced at write time by validateRelations.
func profitRuleLegs(owner int64, receiversRaw, rateRaw, recipientRaw *string) ([]profitLeg, error) {
	if receiversRaw != nil && *receiversRaw != "" {
		var list []struct {
			RecipientID string `json:"recipient_id"`
			RateBPS     string `json:"rate_bps"`
		}
		if json.Unmarshal([]byte(*receiversRaw), &list) != nil || len(list) < 1 || len(list) > 10 {
			return nil, ErrInvalid
		}
		seen := map[int64]bool{}
		legs := make([]profitLeg, 0, len(list))
		for _, item := range list {
			recipient, e := strconv.ParseInt(item.RecipientID, 10, 64)
			if e != nil || recipient <= 0 || recipient == owner || seen[recipient] {
				return nil, ErrInvalid
			}
			rate, e := strconv.ParseInt(item.RateBPS, 10, 64)
			if e != nil || rate < 1 || rate > 10000 {
				return nil, ErrInvalid
			}
			seen[recipient] = true
			legs = append(legs, profitLeg{recipient: recipient, rate: rate})
		}
		return legs, nil
	}
	if rateRaw == nil || recipientRaw == nil {
		return nil, ErrInvalid
	}
	rate, e := strconv.ParseInt(*rateRaw, 10, 64)
	if e != nil || rate < 1 || rate > 10000 {
		return nil, ErrInvalid
	}
	recipient, e := strconv.ParseInt(*recipientRaw, 10, 64)
	if e != nil || recipient <= 0 || recipient == owner {
		return nil, ErrInvalid
	}
	return []profitLeg{{recipient: recipient, rate: rate}}, nil
}
