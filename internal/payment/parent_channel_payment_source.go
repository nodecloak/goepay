package payment

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
)

type parentPaymentSource struct {
	Version                                                          int
	Merchant, Channel, AccountVersion, Source, Amount                int64
	OutTradeNo, Kind, Method                                         string
	AccountProof, FeeIdentity, FeeAccountHash, QuoteProof, RouteHash []byte
	FeeProduct                                                       string
	FeeMethods                                                       []string
	PaymentVariant                                                   string `json:",omitempty"`
	ExecutionAccountHash                                             []byte `json:",omitempty"`
}

func (s *Store) parentPaymentSchema(ctx context.Context, q fundsReader) (bool, error) {
	var tables int
	var migrations bool
	if e := q.QueryRow(ctx, `SELECT (SELECT count(*) FROM unnest(ARRAY['runtime_parent_channels','runtime_parent_channel_accounts','runtime_parent_channel_grants','runtime_parent_channel_requests','runtime_parent_payment_sources']) t(name) WHERE to_regclass(current_schema()||'.'||name) IS NOT NULL),to_regclass(current_schema()||'.schema_migrations') IS NOT NULL`).Scan(&tables, &migrations); e != nil {
		return false, e
	}
	if tables == 5 {
		return true, nil
	}
	var registered, origin bool
	if migrations {
		if e := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='000103_runtime_parent_channel_accounts.up.sql')`).Scan(&registered); e != nil {
			return false, e
		}
	}
	if e := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource IN ('runtime-parent-channels','runtime-parent-payment-intents'))`).Scan(&origin); e != nil {
		return false, e
	}
	if tables != 0 || registered || origin {
		return false, ErrConflict
	}
	return false, nil
}

func (s *Store) sealParentPaymentSource(ctx context.Context, tx pgx.Tx, b RuntimeChannel, quote routeFeeQuote) error {
	if b.parent == nil {
		return nil
	}
	f := quote.facts
	if e := s.captureParentBootstrap(ctx, tx, b.parent); e != nil {
		return e
	}
	if e := s.checkParentExecutionCurrent(ctx, tx, b.parent, f.MerchantID); e != nil {
		return e
	}
	a := b.parent.account
	source := parentPaymentSource{Version: 1, Merchant: f.MerchantID, Channel: b.id, AccountVersion: a.version, Source: a.source, Amount: f.Amount, OutTradeNo: f.OutTradeNo, Kind: f.PayType, Method: f.Method, AccountProof: a.proof, FeeIdentity: b.identity, FeeAccountHash: b.accountHash, FeeProduct: b.product, FeeMethods: b.methods, QuoteProof: quote.proof, RouteHash: f.RouteHash}
	if b.parent.paymentVariant != "" {
		source.PaymentVariant = b.parent.paymentVariant
		executionHash, err := parentExecutionAccountHash(s, b.parent.execution.Payment)
		if err != nil {
			return err
		}
		source.ExecutionAccountHash = executionHash
	}
	proof, e := s.referralDigest("parentchannel180/payment-source", source)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(source)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO runtime_parent_payment_sources(merchant_id,out_trade_no,channel_id,account_version,facts,proof_hash) VALUES($1,$2,$3,$4,$5::jsonb,$6)`, source.Merchant, source.OutTradeNo, source.Channel, source.AccountVersion, string(raw), proof); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('merchant',$1,'runtime-parent-router','runtime-parent-payment-intents',$1,'runtime-parent-intent',jsonb_build_object('out_trade_no',$2::text,'channel_id',$3::text,'account_version',$4::text))`, source.Merchant, source.OutTradeNo, strconv.FormatInt(source.Channel, 10), strconv.FormatInt(source.AccountVersion, 10))
	return e
}

func (s *Store) readParentPaymentSource(ctx context.Context, q fundsReader, mid int64, no string) (parentPaymentSource, bool, error) {
	var source parentPaymentSource
	ready, e := s.parentPaymentSchema(ctx, q)
	if e != nil || !ready {
		return source, false, e
	}
	var raw, proof []byte
	var channel, version int64
	e = q.QueryRow(ctx, `SELECT channel_id,account_version,facts,proof_hash FROM runtime_parent_payment_sources WHERE merchant_id=$1 AND out_trade_no=$2`, mid, no).Scan(&channel, &version, &raw, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		var origin bool
		if e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='runtime-parent-payment-intents' AND operation='runtime-parent-intent' AND resource_id=$1 AND changes->>'out_trade_no'=$2)`, mid, no).Scan(&origin); e != nil {
			return source, false, e
		}
		if origin {
			return source, false, ErrConflict
		}
		// A pre103 child has its own authenticated account, route and quote.
		// A later managed parent cannot turn that complete original child into
		// an unproven parent account. New103 origins above still require103.
		child, err := s.readChildRoute(ctx, q, mid, no)
		if err != nil {
			return source, false, err
		}
		if child != nil {
			quote, found, err := s.readRouteFeeQuote(ctx, q, mid, no)
			if err != nil || !found || !childCommitmentEqual(quote.facts.Child, child.commitment()) {
				return source, false, ErrConflict
			}
			return source, false, nil
		}
		// A versioned current account cannot retroactively authenticate the
		// secret/certificate of a pre-version attempt, even for the same scope.
		// Keep historical rows untouched and report unknown before provider I/O.
		var managed bool
		if e = q.QueryRow(ctx, `WITH channels AS (
 SELECT a.channel_id FROM provider_attempts a JOIN orders o ON o.id=a.order_id WHERE o.merchant_id=$1 AND o.out_trade_no=$2
 UNION SELECT channel_id FROM provider_route_assignments WHERE merchant_id=$1 AND out_trade_no=$2
 ) SELECT EXISTS(SELECT 1 FROM channels c WHERE EXISTS(SELECT 1 FROM runtime_parent_channels h WHERE h.id=c.channel_id) OR EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='runtime-parent-channels' AND a.operation='runtime-parent-write' AND a.resource_id=c.channel_id))`, mid, no).Scan(&managed); e != nil {
			return source, false, e
		}
		if managed {
			return source, false, ErrConflict
		}
		return source, false, nil
	}
	if e != nil {
		return source, false, e
	}
	if len(raw) > 32768 || parentFactsJSON(raw, &source) != nil {
		return source, false, ErrConflict
	}
	want, e := s.referralDigest("parentchannel180/payment-source", source)
	if e != nil || source.Version != 1 || source.Merchant != mid || source.OutTradeNo != no || source.Channel != channel || source.AccountVersion != version || !hmac.Equal(want, proof) {
		return source, false, ErrConflict
	}
	if source.PaymentVariant == "" && len(source.ExecutionAccountHash) != 0 || source.PaymentVariant != "" && (source.PaymentVariant != "web" && source.PaymentVariant != "mini" || len(source.ExecutionAccountHash) != 32 || source.Kind != "wxpay" || source.Method != "jsapi" && source.Method != "applet") {
		return source, false, ErrConflict
	}
	quote, found, e := s.readRouteFeeQuote(ctx, q, mid, no)
	if e != nil {
		return source, false, e
	}
	if !found || source.Channel != quote.facts.ChannelID || source.Kind != quote.facts.PayType || source.Method != quote.facts.Method || source.Amount != quote.facts.Amount || !hmac.Equal(source.QuoteProof, quote.proof) || !hmac.Equal(source.RouteHash, quote.facts.RouteHash) || !hmac.Equal(source.FeeIdentity, quote.facts.ParentIdentity) || !hmac.Equal(source.FeeAccountHash, quote.facts.AccountHash) || source.FeeProduct != quote.facts.ParentProduct {
		return source, false, ErrConflict
	}
	return source, true, nil
}

func (s *Store) parentAttemptAccount(ctx context.Context, tx pgx.Tx, o Order, bound *RuntimeChannel, quote *routeFeeQuote) ([]byte, error) {
	source, found, e := s.readParentPaymentSource(ctx, tx, o.MerchantID, o.OutTradeNo)
	if e != nil {
		return nil, e
	}
	if !found {
		if bound != nil && bound.parent != nil && (quote == nil || quote.facts.Child == nil) {
			return nil, ErrConflict
		}
		if quote == nil {
			return nil, nil
		}
		if quote.facts.Child != nil {
			if bound == nil || bound.child == nil || !childCommitmentEqual(quote.facts.Child, bound.child.commitment()) {
				return nil, ErrConflict
			}
			return quote.facts.Child.AccountHash, nil
		}
		return quote.facts.AccountHash, nil
	}
	if quote == nil || bound == nil || bound.parent == nil {
		return nil, ErrConflict
	}
	a := bound.parent.account
	if a.id != source.Channel || a.version != source.AccountVersion || a.source != source.Source || !hmac.Equal(a.proof, source.AccountProof) {
		return nil, ErrConflict
	}
	if quote.facts.Child != nil {
		if bound.child == nil || !childCommitmentEqual(quote.facts.Child, bound.child.commitment()) {
			return nil, ErrConflict
		}
		return quote.facts.Child.AccountHash, nil
	}
	if source.PaymentVariant != "" {
		actual, err := parentExecutionAccountHash(s, bound.parent.execution.Payment)
		if err != nil || source.PaymentVariant != bound.parent.paymentVariant || !hmac.Equal(actual, source.ExecutionAccountHash) {
			return nil, ErrConflict
		}
		return source.ExecutionAccountHash, nil
	}
	return a.accountHash, nil
}

// Called after every attempt/job write. Current off controls still gate the
// first send; an already selected version remains immutable across rotations.
func (s *Store) parentAttemptFinal(ctx context.Context, tx pgx.Tx, o Order, bound *RuntimeChannel) error {
	if e := s.parentTestFirstSend(ctx, tx, o); e != nil {
		return e
	}
	if bound == nil || bound.parent == nil {
		return nil
	}
	h, e := s.readParentHead(ctx, tx, bound.id)
	if e != nil {
		return e
	}
	if h.archived {
		return ErrState
	}
	if e = s.checkParentCurrentGrantTx(ctx, tx, h, o.MerchantID); e != nil {
		return e
	}
	policy, _, e := s.runtimePolicyTx(ctx, tx, bound.parent.fee, false)
	if e != nil {
		return e
	}
	if !policy.enabled {
		return ErrState
	}
	quote, e := s.validateOrderRouteFeeTerms(ctx, tx, o)
	if e != nil {
		return e
	}
	if quote == nil {
		return ErrConflict
	}
	if e = quote.deadline(ctx, tx); e != nil {
		return e
	}
	if bound.parent.live != nil && !bound.parent.live() {
		return ErrLiveDisabled
	}
	if bound.parent.authorized != nil && !bound.parent.authorized() {
		return ErrConflict
	}
	if bound.parent.execution.PaymentAllowed != nil && !bound.parent.execution.PaymentAllowed(bound.parent.paymentVariant, o.MerchantID) {
		return ErrConflict
	}
	return nil
}
