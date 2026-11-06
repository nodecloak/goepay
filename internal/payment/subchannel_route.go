package payment

import (
	"context"
	"crypto/hmac"
	"errors"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/grouppolicy"
	"time"
)

// ChildExecutionBinding never accepts console fields. Its private proof comes
// from a verified immutable encrypted account and the actual adapter identity.
type ChildExecutionBinding struct {
	child, version, merchant, parent           int64
	accountProof, identity, accountHash, proof []byte
	product                                    string
	selectedAt                                 time.Time
	live                                       func() bool
	authorized                                 func() bool
}

// Authenticated account material can become ineligible after trusted template
// rotation or certificate expiry. It is skipped only for new selection; corrupt
// source evidence still fails the entire request closed.
var ErrSubchannelUnavailable = errors.New("payment subchannel account is not currently executable")

type routeChildCommitment struct {
	ChildID, AccountVersion                     int64
	SourceProof, ExecutionIdentity, AccountHash []byte
	Product                                     string
}

func (b *ChildExecutionBinding) commitment() *routeChildCommitment {
	if b == nil {
		return nil
	}
	return &routeChildCommitment{b.child, b.version, b.proof, b.identity, b.accountHash, b.product}
}
func (s *Store) childRouteProof(mid int64, no string, b ChildExecutionBinding) ([]byte, error) {
	return s.referralDigest("payment-subchannel168/route", []any{mid, no, b.parent, b.child, b.version, b.accountProof, b.identity, b.accountHash, b.product, b.selectedAt})
}
func (s *Store) SubchannelCandidatesTx(ctx context.Context, tx pgx.Tx, mid int64, kind string) ([]PaymentSubchannel, int, error) {
	rows, e := tx.Query(ctx, `SELECT `+joinedSubchannelColumns()+` FROM payment_subchannels c `+joinedSubchannelAccount+` WHERE c.merchant_id=$1 AND c.pay_type=$2 ORDER BY c.id`, mid, kind)
	if e != nil {
		return nil, 0, e
	}
	heads := []PaymentSubchannel{}
	for rows.Next() {
		c, e := s.scanJoinedSubchannel(rows)
		if e != nil {
			rows.Close()
			return nil, 0, e
		}
		heads = append(heads, c)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, 0, e
	}
	ids := []int64{}
	for _, c := range heads {
		if c.Enabled && !c.Archived {
			ids = append(ids, c.ID)
		}
	}
	risks, e := s.coreRiskSubjectStatusesTx(ctx, tx, "subchannel", ids)
	if e != nil {
		return nil, 0, e
	}
	out := []PaymentSubchannel{}
	for _, c := range heads {
		if c.Enabled && !c.Archived && !risks[c.ID].Blocked {
			out = append(out, c)
		}
	}
	policy, e := s.childMerchantPolicyTx(ctx, tx, mid, false)
	if e != nil {
		return nil, 0, e
	}
	return out, policy.RollType, nil
}

// Refresh all originally observed candidates after the canonical parent/child
// advisory locks. One joined statement also locks their current mutable heads.
func (s *Store) LockedSubchannelsTx(ctx context.Context, tx pgx.Tx, mid int64, ids []int64) (map[int64]PaymentSubchannel, error) {
	out := map[int64]PaymentSubchannel{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, e := tx.Query(ctx, `SELECT `+joinedSubchannelColumns()+` FROM payment_subchannels c `+joinedSubchannelAccount+` WHERE c.merchant_id=$1 AND c.id=ANY($2::bigint[]) ORDER BY c.id FOR SHARE OF c`, mid, ids)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		c, e := s.scanJoinedSubchannel(rows)
		if e != nil {
			return nil, e
		}
		out[c.ID] = c
	}
	return out, rows.Err()
}

func (s *Store) ChildLastUsedBatchTx(ctx context.Context, tx pgx.Tx, ids []int64) (map[int64]int64, error) {
	out := map[int64]int64{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, e := tx.Query(ctx, `SELECT i.id,h.subchannel_id IS NOT NULL,COALESCE(h.used_millis,0),latest.n,latest.ms,
 COALESCE(r.merchant_id,0),COALESCE(r.parent_id,0),COALESCE(r.subchannel_id,0),COALESCE(r.account_version,0),COALESCE(r.account_proof,''::bytea),COALESCE(r.execution_identity,''::bytea),COALESCE(r.account_hash,''::bytea),COALESCE(r.product,''),COALESCE(r.proof_hash,''::bytea),COALESCE(r.selected_at,'epoch'::timestamptz),COALESCE(h.out_trade_no,'')
 FROM unnest($1::bigint[]) i(id) LEFT JOIN payment_subchannel_last_used h ON h.subchannel_id=i.id
 LEFT JOIN payment_subchannel_routes r ON r.merchant_id=h.merchant_id AND r.out_trade_no=h.out_trade_no
 CROSS JOIN LATERAL (SELECT count(*) n,COALESCE(max(floor(extract(epoch FROM s.selected_at)*1000)::bigint),0) ms FROM payment_subchannel_routes s WHERE s.subchannel_id=i.id) latest`, ids)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var id, ms, history, latest int64
		var exists bool
		var b ChildExecutionBinding
		var no string
		if e = rows.Scan(&id, &exists, &ms, &history, &latest, &b.merchant, &b.parent, &b.child, &b.version, &b.accountProof, &b.identity, &b.accountHash, &b.product, &b.proof, &b.selectedAt, &no); e != nil {
			return nil, e
		}
		if exists {
			proof, e := s.childRouteProof(b.merchant, no, b)
			if e != nil || history <= 0 || ms != latest || b.child != id || b.selectedAt.UnixMilli() != ms || !hmac.Equal(proof, b.proof) {
				return nil, ErrConflict
			}
		} else if history != 0 {
			return nil, ErrConflict
		}
		out[id] = ms
	}
	return out, rows.Err()
}

func (g ProviderFlow) WithSubchannelAccountTx(ctx context.Context, tx fundsReader, c PaymentSubchannel) (ProviderFlow, adapter.SubchannelExecution, error) {
	return g.withSubchannelAccountSource(ctx, tx, c, false)
}
func (g ProviderFlow) withSubchannelAccountSource(ctx context.Context, tx fundsReader, c PaymentSubchannel, historical bool) (ProviderFlow, adapter.SubchannelExecution, error) {
	var execution adapter.SubchannelExecution
	f := g.SubchannelFactories[c.ParentID]
	if g.Store == nil || c.account.Child != c.ID || c.account.Version != c.AccountVersion {
		return g, execution, ErrConflict
	}
	values, e := g.Store.openChildAccount(c.account)
	if e != nil {
		return g, execution, e
	}
	if historical && values.ParentOrigin > 0 {
		origin := g.SubchannelFactories[values.ParentOrigin]
		derive, ok := origin.(interface {
			DeriveParentSubchannel(int64, []byte) (adapter.SubchannelFactory, error)
		})
		if !ok {
			return g, execution, ErrConflict
		}
		f, e = derive.DeriveParentSubchannel(c.ParentID, values.ParentSnapshot)
		if e != nil {
			return g, execution, ErrConflict
		}
	}
	if f == nil || f.Schema().ParentID != c.ParentID {
		if !historical {
			return g, execution, ErrSubchannelUnavailable
		}
		return g, execution, ErrConflict
	}
	currentSchema, e := g.Store.childSchemaCommitment(f.Schema().Fingerprint)
	if e != nil {
		return g, execution, e
	}
	if !historical && currentSchema != c.account.Schema {
		return g, execution, ErrSubchannelUnavailable
	}
	execution, e = f.Restore(values.Snapshot)
	if e != nil || execution.Payment == nil || execution.Payment.ID() != c.PayType {
		if !historical {
			return g, execution, ErrSubchannelUnavailable
		}
		return g, execution, ErrConflict
	}
	originalSchema, e := g.Store.childSchemaCommitment(execution.SchemaFingerprint)
	if e != nil || originalSchema != c.account.Schema {
		return g, execution, ErrConflict
	}
	parent, e := g.RuntimeChannel(c.ParentID)
	if e != nil || parent.kind != c.PayType {
		return g, execution, ErrConflict
	}
	childFlow := g
	childFlow.boundParents = nil
	childFlow.parentBindings = nil
	childFlow.Providers = map[int64]adapter.Provider{}
	for id, p := range g.Providers {
		childFlow.Providers[id] = p
	}
	childFlow.Providers[c.ParentID] = execution.Payment
	actual, e := childFlow.RuntimeChannel(c.ParentID)
	if e != nil {
		return g, execution, e
	}
	parent.child = &ChildExecutionBinding{child: c.ID, version: c.AccountVersion, merchant: c.MerchantID, parent: c.ParentID, accountProof: c.account.Proof, identity: actual.identity, accountHash: actual.accountHash, product: actual.product}
	parent.child.live = g.parentLive
	if g.parentOperator != nil {
		origin := c.ParentID
		if binding := g.parentBindings[c.ParentID]; binding != nil {
			origin = binding.account.source
		}
		parent.child.authorized = func() bool { return g.parentOperator(c.MerchantID, origin) }
	}
	childFlow.parentBindings = g.parentBindings
	childFlow.boundParents = map[int64]RuntimeChannel{c.ParentID: parent}
	return childFlow, execution, nil
}
func (g ProviderFlow) SealSubchannelRouteTx(ctx context.Context, tx pgx.Tx, mid int64, no string) error {
	b, e := g.RuntimeChannelFromSelection()
	if e != nil {
		return e
	}
	if b.child.merchant != mid {
		return ErrConflict
	}
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&b.child.selectedAt); e != nil {
		return e
	}
	b.child.proof, e = g.Store.childRouteProof(mid, no, *b.child)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO payment_subchannel_routes(merchant_id,out_trade_no,parent_id,subchannel_id,account_version,account_proof,execution_identity,account_hash,product,proof_hash,selected_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, mid, no, b.id, b.child.child, b.child.version, b.child.accountProof, b.child.identity, b.child.accountHash, b.child.product, b.child.proof, b.child.selectedAt)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO payment_subchannel_last_used(subchannel_id,merchant_id,out_trade_no,used_millis) VALUES($1,$2,$3,$4) ON CONFLICT(subchannel_id) DO UPDATE SET merchant_id=EXCLUDED.merchant_id,out_trade_no=EXCLUDED.out_trade_no,used_millis=EXCLUDED.used_millis`, b.child.child, mid, no, b.child.selectedAt.UnixMilli())
	return e
}
func (g ProviderFlow) RuntimeChannelFromSelection() (RuntimeChannel, error) {
	if len(g.boundParents) != 1 {
		return RuntimeChannel{}, ErrInvalid
	}
	for _, b := range g.boundParents {
		if b.child != nil {
			return b, nil
		}
	}
	return RuntimeChannel{}, ErrInvalid
}
func (s *Store) readChildRoute(ctx context.Context, q fundsReader, mid int64, no string) (*ChildExecutionBinding, error) {
	exists, e := s.childSchemaPresent(ctx, q, mid, no)
	if e != nil {
		return nil, e
	}
	if !exists {
		return nil, nil
	}
	var b ChildExecutionBinding
	e = q.QueryRow(ctx, `SELECT merchant_id,parent_id,subchannel_id,account_version,account_proof,execution_identity,account_hash,product,proof_hash,selected_at FROM payment_subchannel_routes WHERE merchant_id=$1 AND out_trade_no=$2`, mid, no).Scan(&b.merchant, &b.parent, &b.child, &b.version, &b.accountProof, &b.identity, &b.accountHash, &b.product, &b.proof, &b.selectedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	proof, e := s.childRouteProof(mid, no, b)
	if e != nil || !hmac.Equal(proof, b.proof) {
		return nil, ErrConflict
	}
	return &b, nil
}

// ForPaymentIntent restores the original account version. Current head changes
// never select another account for a retry, callback or reconciliation.
func (g ProviderFlow) ForPaymentIntent(ctx context.Context, mid int64, no string, parentID int64) (ProviderFlow, adapter.SubchannelExecution, error) {
	var execution adapter.SubchannelExecution
	if g.Store == nil {
		return g, execution, ErrInvalid
	}
	tx, e := g.Store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return g, execution, e
	}
	defer tx.Rollback(ctx)
	out, execution, e := g.ForPaymentIntentTx(ctx, tx, mid, no, parentID)
	if e != nil {
		return g, execution, e
	}
	return out, execution, tx.Commit(ctx)
}
func (g ProviderFlow) ForPaymentIntentTx(ctx context.Context, tx fundsReader, mid int64, no string, parentID int64) (ProviderFlow, adapter.SubchannelExecution, error) {
	var execution adapter.SubchannelExecution
	if e := g.Store.authenticateOriginalOrderBirth(ctx, tx, mid, no); e != nil {
		return g, execution, e
	}
	resolved, err := g.parentPaymentIntentTx(ctx, tx, mid, no)
	if err != nil {
		return g, execution, err
	}
	g = resolved
	schema, e := g.Store.childSchemaPresent(ctx, tx, mid, no)
	if e != nil {
		return g, execution, e
	}
	if !schema {
		return g, execution, nil
	}
	b, e := g.Store.readChildRoute(ctx, tx, mid, no)
	if e != nil {
		return g, execution, e
	}
	if b == nil {
		var required bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_route_fee_quotes WHERE merchant_id=$1 AND out_trade_no=$2 AND facts ? 'child_execution')`, mid, no).Scan(&required); e != nil {
			return g, execution, e
		}
		if required {
			return g, execution, ErrConflict
		}
		return g, execution, nil
	}
	quote, found, e := g.Store.readRouteFeeQuote(ctx, tx, mid, no)
	if e != nil {
		return g, execution, e
	}
	if !found || !childCommitmentEqual(quote.facts.Child, b.commitment()) {
		return g, execution, ErrConflict
	}
	if b.parent != parentID {
		return g, execution, ErrConflict
	}
	a, e := g.Store.childAccount(ctx, tx, b.child, b.version)
	if e != nil || a.Merchant != mid || a.Parent != parentID || !hmac.Equal(a.Proof, b.accountProof) {
		return g, execution, ErrConflict
	}
	c := PaymentSubchannel{ID: b.child, MerchantID: mid, ParentID: parentID, PayType: a.Kind, AccountVersion: a.Version, account: a}
	out, execution, e := g.withSubchannelAccountSource(ctx, tx, c, true)
	if e != nil {
		return g, execution, e
	}
	actual := out.boundParents[parentID].child
	if actual.product != b.product || !hmac.Equal(actual.identity, b.identity) || !hmac.Equal(actual.accountHash, b.accountHash) {
		return g, execution, ErrConflict
	}
	b.live, b.authorized = actual.live, actual.authorized
	out.boundParents[parentID] = func() RuntimeChannel { parent := out.boundParents[parentID]; parent.child = b; return parent }()
	return out, execution, nil
}
func (g ProviderFlow) forPaymentTrade(ctx context.Context, trade string, parent int64) (ProviderFlow, error) {
	o, e := g.Store.OrderByTradeNo(ctx, trade)
	if e != nil {
		return g, e
	}
	out, _, e := g.ForPaymentIntent(ctx, o.MerchantID, o.OutTradeNo, parent)
	return out, e
}
func (g ProviderFlow) SubchannelID() int64 {
	for _, b := range g.boundParents {
		if b.child != nil {
			return b.child.child
		}
	}
	return 0
}
func (s *Store) ChildLastUsedTx(ctx context.Context, tx pgx.Tx, id int64) (int64, error) {
	all, e := s.ChildLastUsedBatchTx(ctx, tx, []int64{id})
	return all[id], e
}
func childCommitmentEqual(a, b *routeChildCommitment) bool {
	return a != nil && b != nil && a.ChildID == b.ChildID && a.AccountVersion == b.AccountVersion && a.Product == b.Product && hmac.Equal(a.SourceProof, b.SourceProof) && hmac.Equal(a.ExecutionIdentity, b.ExecutionIdentity) && hmac.Equal(a.AccountHash, b.AccountHash)
}

func (s *Store) checkChildFirstSendTx(ctx context.Context, tx pgx.Tx, o Order, b *RuntimeChannel) error {
	if b == nil || b.child == nil {
		return ErrConflict
	}
	if b.child.live != nil && !b.child.live() {
		return ErrLiveDisabled
	}
	if b.child.authorized != nil && !b.child.authorized() {
		return ErrConflict
	}
	c, e := s.readSubchannel(ctx, tx, tx.QueryRow(ctx, `SELECT `+childColumns+` FROM payment_subchannels WHERE id=$1 AND merchant_id=$2 FOR SHARE`, b.child.child, o.MerchantID))
	if e != nil {
		return e
	}
	if !c.Enabled || c.PayType != o.Type || b.child.merchant != o.MerchantID {
		return ErrState
	}
	group, e := s.GroupPaymentRoutingTx(ctx, tx, o.MerchantID, o.Type)
	if e != nil {
		return e
	}
	if group.Choice.Kind() != grouppolicy.Subchannels {
		return ErrState
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return e
	}
	if !group.ValidUntil.IsZero() && !now.Before(group.ValidUntil) {
		return ErrState
	}
	return s.checkCoreRiskChildAdmissionTx(ctx, tx, b.child.child)
}
