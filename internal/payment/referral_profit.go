package payment

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type referralProfitCost struct {
	OrderID, MerchantID, ChannelID                                   int64
	Version                                                          int
	Method, Provider, AppID, Revision                                string
	RateUnits, Amount, Fee, CostWhole, CostRemainder, Profit, Reward int64
	AccountHash, TermsHash, Proof                                    []byte
}

func (s *Store) profitDigest(domain string, fact any) ([]byte, error) {
	// New family; original v1/v2 domains and serialized facts remain unchanged.
	return s.referralDigest("profit-cost-v3/"+domain, fact)
}
func (s *Store) profitCostProof(c referralProfitCost) ([]byte, error) {
	return s.profitDigest("birth", []any{c.Version, c.OrderID, c.MerchantID, c.ChannelID, c.Method, c.Provider, c.AppID, c.AccountHash, c.Revision, c.RateUnits, c.Amount, c.Fee, c.CostWhole, c.CostRemainder, c.Profit, c.Reward, c.TermsHash})
}
func (s *Store) prepareReferralProfit(o Order, charge104, rate int64, q *PaymentCostQuote) (*referralProfitCost, error) {
	if q == nil || q.channelID <= 0 || !providerMethodPattern.MatchString(q.method) {
		return nil, ErrInvalid
	}
	e := q.estimate
	var scope []string
	if json.Unmarshal([]byte(e.AccountScope), &scope) != nil || len(scope) != 4 || scope[0] != o.Type || scope[0] != e.Provider || scope[1] != e.AppID || scope[1] == "" || scope[2] == "" || scope[3] != "account" || e.Provider == "alipay" && (!strings.HasPrefix(scope[2], "seller:") || len(scope[2]) <= 7) || e.Provider != "alipay" && e.Provider != "wxpay" || !isLowerHex64(e.Revision) {
		return nil, ErrInvalid
	}
	calc, err := calculateReferralProfitCost(charge104, o.PlatformFee, e.RateUnits, rate)
	if err != nil {
		return nil, err
	}
	account, err := s.providerAccountHash(e.AccountScope)
	if err != nil {
		return nil, err
	}
	return &referralProfitCost{OrderID: o.ID, MerchantID: o.MerchantID, ChannelID: q.channelID, Version: 3, Method: q.method, Provider: e.Provider, AppID: e.AppID, Revision: e.Revision, RateUnits: e.RateUnits, Amount: charge104, Fee: o.PlatformFee, CostWhole: calc.CostWhole, CostRemainder: calc.CostRemainder, Profit: calc.Profit, Reward: calc.Reward, AccountHash: account}, nil
}
func isLowerHex64(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (s *Store) insertReferralProfit(ctx context.Context, tx pgx.Tx, t referralOrderTerms, c *referralProfitCost) error {
	c.Reward = t.RewardAmount
	c.TermsHash = t.ProofHash
	var err error
	c.Proof, err = s.profitCostProof(*c)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO referral_profit_costs(order_id,merchant_id,source_version,channel_id,method,provider,app_id,account_hash,config_revision,rate_units,amount,platform_fee,cost_whole,cost_remainder,profit_amount,reward_amount,terms_hash,proof_hash)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`, c.OrderID, c.MerchantID, c.Version, c.ChannelID, c.Method, c.Provider, c.AppID, c.AccountHash, c.Revision, c.RateUnits, c.Amount, c.Fee, c.CostWhole, c.CostRemainder, c.Profit, c.Reward, c.TermsHash, c.Proof)
	if err != nil {
		return err
	}
	audit, err := s.profitDigest("audit", []any{c.OrderID, "payment-engine", c.Proof})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO referral_profit_cost_events(order_id,event_key,actor,subject_hash,proof_hash)VALUES($1,$2,'payment-engine',$3,$4)`, c.OrderID, fmt.Sprintf("referral-profit:%d:birth", c.OrderID), c.Proof, audit)
	return err
}

func (s *Store) readReferralProfit(ctx context.Context, q referralReader, o Order, t referralOrderTerms) (*referralProfitCost, error) {
	_, basis, err := s.referralOriginalMode(ctx, q, t)
	if err != nil {
		return nil, err
	}
	var c referralProfitCost
	err = q.QueryRow(ctx, `SELECT order_id,merchant_id,source_version,channel_id,method,provider,app_id,account_hash,config_revision,rate_units,amount,platform_fee,cost_whole,cost_remainder,profit_amount,reward_amount,terms_hash,proof_hash FROM referral_profit_costs WHERE order_id=$1`, o.ID).Scan(&c.OrderID, &c.MerchantID, &c.Version, &c.ChannelID, &c.Method, &c.Provider, &c.AppID, &c.AccountHash, &c.Revision, &c.RateUnits, &c.Amount, &c.Fee, &c.CostWhole, &c.CostRemainder, &c.Profit, &c.Reward, &c.TermsHash, &c.Proof)
	required := basis == "profit" && t.Purpose == "payment" && t.RelationID > 0 && t.Reason != "disabled" && t.Reason != "promoter_disabled"
	if errors.Is(err, pgx.ErrNoRows) {
		if required {
			return nil, ErrConflict
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	charge104, _, chargeError104 := s.readBuyerSource104(ctx, q, o)
	if chargeError104 != nil {
		return nil, chargeError104
	}
	if !required || c.Version != 3 || c.OrderID != o.ID || c.MerchantID != o.MerchantID || c.Provider != o.Type || c.Amount != charge104 || c.Fee != o.PlatformFee || c.Reward != t.RewardAmount || !hmac.Equal(c.TermsHash, t.ProofHash) || len(c.AccountHash) != 32 || !isLowerHex64(c.Revision) || c.AppID == "" || c.ChannelID <= 0 || !providerMethodPattern.MatchString(c.Method) {
		return nil, ErrConflict
	}
	calc, err := calculateReferralProfitCost(c.Amount, c.Fee, c.RateUnits, t.RateBPS)
	if err != nil || calc.CostWhole != c.CostWhole || calc.CostRemainder != c.CostRemainder || calc.Profit != c.Profit || (t.Enabled || t.Reason == "zero_rate" || t.Reason == "zero_fee") && calc.Reward != c.Reward {
		return nil, ErrConflict
	}
	proof, err := s.profitCostProof(c)
	if err != nil || !hmac.Equal(proof, c.Proof) {
		return nil, ErrConflict
	}
	audit, err := s.profitDigest("audit", []any{c.OrderID, "payment-engine", c.Proof})
	if err != nil {
		return nil, err
	}
	var valid bool
	err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM referral_profit_cost_events WHERE order_id=$1 AND event_key=$2 AND actor='payment-engine' AND subject_hash=$3 AND proof_hash=$4)`, o.ID, fmt.Sprintf("referral-profit:%d:birth", o.ID), c.Proof, audit).Scan(&valid)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, ErrConflict
	}
	var attemptID, channel int64
	var method string
	var account []byte
	err = q.QueryRow(ctx, `SELECT a.id,a.channel_id,a.method,p.account_hash FROM provider_attempts a LEFT JOIN provider_attempt_accounts p ON p.attempt_id=a.id WHERE a.order_id=$1`, o.ID).Scan(&attemptID, &channel, &method, &account)
	if errors.Is(err, pgx.ErrNoRows) {
		if o.Status == "paid" {
			return nil, ErrConflict
		}
		return &c, nil
	}
	if err != nil {
		return nil, err
	}
	if channel != c.ChannelID || method != c.Method || !hmac.Equal(account, c.AccountHash) {
		return nil, ErrConflict
	}
	if o.Status == "paid" {
		if err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM verified_payment_receipts WHERE order_id=$1 AND channel_id=$2 AND provider_trade_no<>'')`, o.ID, c.ChannelID).Scan(&valid); err != nil {
			return nil, err
		}
		if !valid {
			return nil, ErrConflict
		}
	}
	return &c, nil
}

func (s *Store) validateReferralProfitAttempt(ctx context.Context, tx pgx.Tx, oid, channel int64, method, appID, scope string) error {
	// Authenticate original policy IDs before choosing a family. A raw SQL
	// basis hint could be changed to fee along with removal of the cost row.
	o, err := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", oid))
	if err != nil {
		return err
	}
	t, err := s.readReferralTermsBase(ctx, tx, o)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	t.profit, err = s.readReferralProfit(ctx, tx, o, t)
	if err != nil {
		return err
	}
	if t.profit == nil {
		return nil
	} // A later original group policy may override profit.
	if _, err = s.readReferralDailyTerms(ctx, tx, o, t); err != nil {
		return err
	}
	h, err := s.providerAccountHash(scope)
	if err != nil {
		return ErrConflict
	}
	if t.profit.ChannelID != channel || t.profit.Method != method || t.profit.AppID != appID || !hmac.Equal(h, t.profit.AccountHash) {
		return ErrConflict
	}
	return nil
}
