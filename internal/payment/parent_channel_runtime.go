package payment

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

type ParentRuntimeReader interface {
	fundsReader
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// The fee anchor and execution account have separate private provenance.
// A new version never impersonates the original fee account or product.
type parentExecutionBinding struct {
	fee                RuntimeChannel
	account            parentChannelAccount
	headProof          []byte
	merchant           int64
	granted            bool
	live               func() bool
	authorized         func() bool
	bootstrapRaw       []byte
	bootstrapExecution adapter.ParentChannelExecution
	execution          adapter.ParentChannelExecution
	paymentVariant     string
	variantFrozen      bool
	// Set only by the verified pre103 child permission-anchor constructor.
	// This is not serialized and grants no parent execution authority.
	legacyChildAuthority bool
}

func (g ProviderFlow) paymentWritesEnabled() bool {
	return g.AllowLive && (g.parentLive == nil || g.parentLive())
}

// A request may add a live process policy without mutating the shared SDK
// registry or replacing any prior operator guard captured by a constructor.
func (g ProviderFlow) WithCurrentWritePolicy(current func() bool) ProviderFlow {
	previous := g.parentLive
	g.parentLive = func() bool { return (current == nil || current()) && (previous == nil || previous()) }
	return g
}

func parentHasChannel(ids []int64, id int64) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func (g ProviderFlow) withParentExecution(binding *parentExecutionBinding, execution adapter.ParentChannelExecution) ProviderFlow {
	binding.live = g.parentLive
	binding.execution = execution
	if g.parentOperator != nil {
		binding.authorized = func() bool { return g.parentOperator(binding.merchant, binding.account.source) }
	}
	out := g
	out.Providers = make(map[int64]adapter.Provider, len(g.Providers)+1)
	for id, p := range g.Providers {
		out.Providers[id] = p
	}
	out.parentBindings = make(map[int64]*parentExecutionBinding, len(g.parentBindings)+1)
	for id, b := range g.parentBindings {
		out.parentBindings[id] = b
	}
	if bound, ok := g.boundParents[binding.account.id]; !ok || bound.child == nil {
		out.Providers[binding.account.id] = execution.Payment
	}
	out.parentBindings[binding.account.id] = binding
	return out
}

// ParentExecutionProfiles projects only trusted, process-local providers from
// the same version used for payment. Nil entries explicitly remove old profiles.
func (g ProviderFlow) ParentExecutionProfiles() (map[int64]adapter.WebOAuthProvider, map[int64]adapter.MiniOAuthProvider) {
	web := map[int64]adapter.WebOAuthProvider{}
	mini := map[int64]adapter.MiniOAuthProvider{}
	for id, b := range g.parentBindings {
		if bound, ok := g.boundParents[id]; ok && bound.child != nil {
			continue
		}
		web[id] = b.execution.Web
		mini[id] = b.execution.Mini
	}
	return web, mini
}

// Trusted in-process metadata for original execution views. This is not an
// authorization grant: the server intersects these origins with its live root
// allowlist and admission rechecks the signed durable grant before exchange.
func (g ProviderFlow) ParentExecutionPermissionOrigins() map[int64]map[int64]int64 {
	out := map[int64]map[int64]int64{}
	for id, b := range g.parentBindings {
		if out[b.merchant] == nil {
			out[b.merchant] = map[int64]int64{}
		}
		out[b.merchant][id] = b.account.source
	}
	return out
}

func (g ProviderFlow) CheckParentIdentityAdmission(ctx context.Context, mid, channel int64) error {
	b := g.parentBindings[channel]
	if b == nil {
		return nil
	}
	if !g.paymentWritesEnabled() || g.Store == nil {
		return ErrLiveDisabled
	}
	tx, err := g.Store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = parentChannelLock(ctx, tx, channel); err != nil {
		return err
	}
	if err = g.Store.checkParentRuntimeWriteTx(ctx, tx, b, mid, channel); err != nil {
		return err
	}
	if b.execution.PaymentAllowed != nil && !b.execution.PaymentAllowed(b.paymentVariant, mid) {
		return ErrConflict
	}
	return tx.Commit(ctx)
}

func (g ProviderFlow) restoreParentExecution(a parentChannelAccount) (adapter.ParentChannelExecution, error) {
	factory := g.ParentFactories[a.source]
	if factory == nil || factory.OriginID() != a.source || factory.PayType() != a.kind {
		return adapter.ParentChannelExecution{}, ErrConflict
	}
	raw, e := g.Store.openParentAccount(a)
	if e != nil {
		return adapter.ParentChannelExecution{}, e
	}
	execution, e := factory.Restore(a.id, raw)
	if e != nil {
		return adapter.ParentChannelExecution{}, e
	}
	actual, e := (ProviderFlow{Store: g.Store, Providers: map[int64]adapter.Provider{a.id: execution.Payment}}).RuntimeChannel(a.id)
	if e != nil || !hmac.Equal(actual.identity, a.identity) || !hmac.Equal(actual.accountHash, a.accountHash) || actual.product != a.product {
		return adapter.ParentChannelExecution{}, ErrConflict
	}
	return execution, nil
}

// RuntimeParentChannels builds a request-local catalog. A signed grant alone
// cannot expand the current operator allowlist; both sources must agree.
// No provider request, cache write, or persistent configuration write occurs.
func (g *ProviderFlow) RuntimeParentChannels(ctx context.Context, mid int64, operator map[int64][]int64, current ...func(int64, int64) bool) (ProviderFlow, []int64, error) {
	if g.Store == nil || mid <= 0 {
		return *g, nil, ErrInvalid
	}
	tx, err := g.Store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return *g, nil, err
	}
	defer tx.Rollback(ctx)
	out, allowed, err := g.RuntimeParentChannelsSnapshot(ctx, tx, mid, operator, current...)
	if err != nil {
		return *g, nil, err
	}
	return out, allowed, tx.Commit(ctx)
}

// RuntimeParentChannelsSnapshot restores the same verified catalog inside the
// caller's snapshot. It never commits, mutates grants, or calls a provider.
func (g *ProviderFlow) RuntimeParentChannelsSnapshot(ctx context.Context, tx ParentRuntimeReader, mid int64, operator map[int64][]int64, current ...func(int64, int64) bool) (ProviderFlow, []int64, error) {
	allowed := append([]int64(nil), operator[mid]...)
	if g.Store == nil || mid <= 0 || tx == nil {
		return *g, nil, ErrInvalid
	}
	var e error
	if _, e = g.Store.parentPaymentSchema(ctx, tx); e != nil {
		return *g, nil, e
	}
	states, e := g.Store.readParentStates(ctx, tx)
	if e != nil {
		return *g, nil, e
	}
	grants := map[int64]int64{}
	if len(states) > 0 {
		rows, e := tx.Query(ctx, `SELECT channel_id,source_channel_id,proof_hash FROM runtime_parent_channel_grants WHERE merchant_id=$1 ORDER BY channel_id`, mid)
		if e != nil {
			return *g, nil, e
		}
		for rows.Next() {
			var id, source int64
			var proof []byte
			if e = rows.Scan(&id, &source, &proof); e != nil {
				rows.Close()
				return *g, nil, e
			}
			want, pe := g.Store.referralDigest("parentchannel180/grant", []any{id, mid, source})
			if pe != nil || !hmac.Equal(want, proof) {
				rows.Close()
				return *g, nil, ErrConflict
			}
			grants[id] = source
		}
		rows.Close()
		if e = rows.Err(); e != nil {
			return *g, nil, e
		}
	}
	out := *g
	if out.parentOperator == nil {
		out.parentOperator = func(merchant, source int64) bool { return parentHasChannel(operator[merchant], source) }
	}
	if len(current) == 1 && current[0] != nil {
		out.parentOperator = current[0]
	}
	if len(current) > 1 {
		return *g, nil, ErrInvalid
	}
	if g.parentLive != nil {
		out.parentLive = g.parentLive
	} else {
		out.parentLive = func() bool { return g.AllowLive }
	}
	out.Providers = make(map[int64]adapter.Provider, len(g.Providers)+len(states))
	for id, p := range g.Providers {
		out.Providers[id] = p
	}
	extras := []int64{}
	managed := map[int64]bool{}
	for _, state := range states {
		h, a := state.head, state.account
		out, e = out.withParentChildFactory(a)
		if e != nil {
			return *g, nil, e
		}
		managed[h.id] = true
		if bound, ok := out.boundParents[h.id]; !ok || bound.child == nil {
			delete(out.Providers, h.id)
		}
		authorized := parentHasChannel(operator[mid], h.source) && out.parentOperator(mid, h.source) && (h.legacy && h.id == h.source || grants[h.id] == h.source)
		if !authorized || h.archived || !state.policy.enabled {
			continue
		}
		execution, e := g.restoreParentExecution(a)
		if e != nil {
			return *g, nil, e
		}
		out = out.withParentExecution(&parentExecutionBinding{fee: h.fee, account: a, headProof: h.proof, merchant: mid, granted: true}, execution)
		if !parentHasChannel(allowed, h.id) {
			extras = append(extras, h.id)
		}
	}
	for _, id := range operator[mid] {
		if !out.parentOperator(mid, id) {
			if bound, ok := out.boundParents[id]; !ok || bound.child == nil {
				delete(out.Providers, id)
			}
			continue
		}
		if managed[id] || g.ParentFactories[id] == nil || g.Providers[id] == nil {
			continue
		}
		factory := g.ParentFactories[id]
		if factory.OriginID() != id {
			return *g, nil, ErrConflict
		}
		execution, raw, e := factory.BootstrapSnapshot(id)
		if e != nil {
			return *g, nil, e
		}
		actual, e := (ProviderFlow{Store: g.Store, Providers: map[int64]adapter.Provider{id: execution.Payment}}).RuntimeChannel(id)
		if e != nil {
			return *g, nil, e
		}
		loaded, e := g.RuntimeChannel(id)
		if e != nil || !hmac.Equal(actual.identity, loaded.identity) {
			return *g, nil, ErrConflict
		}
		binding := &parentExecutionBinding{fee: loaded, account: parentChannelAccount{id: id, version: 1, source: id, kind: loaded.kind}, merchant: mid, granted: true, bootstrapRaw: raw, bootstrapExecution: execution}
		out = out.withParentExecution(binding, execution)
	}
	sort.Slice(extras, func(i, j int) bool { return extras[i] < extras[j] })
	allowed = append(allowed, extras...)
	return out, allowed, nil
}

func (s *Store) checkParentExecutionCurrent(ctx context.Context, tx pgx.Tx, b *parentExecutionBinding, mid int64) error {
	if b == nil || b.merchant != mid || !b.granted {
		return ErrConflict
	}
	if b.live != nil && !b.live() {
		return ErrLiveDisabled
	}
	if b.authorized != nil && !b.authorized() {
		return ErrConflict
	}
	if b.execution.PaymentAllowed != nil && !b.execution.PaymentAllowed(b.paymentVariant, mid) {
		return ErrConflict
	}
	h, e := s.readParentHead(ctx, tx, b.account.id)
	if e != nil {
		return e
	}
	if h.archived {
		return ErrState
	}
	if h.version != b.account.version || !hmac.Equal(h.proof, b.headProof) || !hmac.Equal(h.fee.identity, b.fee.identity) {
		return ErrConflict
	}
	return s.checkParentCurrentGrantTx(ctx, tx, h, mid)
}

func (s *Store) checkParentCurrentGrantTx(ctx context.Context, q fundsReader, h parentChannelHead, mid int64) error {
	if !h.legacy {
		var source int64
		var proof []byte
		if e := q.QueryRow(ctx, `SELECT source_channel_id,proof_hash FROM runtime_parent_channel_grants WHERE channel_id=$1 AND merchant_id=$2`, h.id, mid).Scan(&source, &proof); e != nil {
			return ErrConflict
		}
		expected, e := s.referralDigest("parentchannel180/grant", []any{h.id, mid, h.source})
		if e != nil || source != h.source || !hmac.Equal(expected, proof) {
			return ErrConflict
		}
	}
	return nil
}

func (g ProviderFlow) ParentPaymentIntent(ctx context.Context, mid int64, no string) (ProviderFlow, error) {
	if g.Store == nil || mid <= 0 || no == "" {
		return g, ErrInvalid
	}
	tx, e := g.Store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return g, e
	}
	defer tx.Rollback(ctx)
	out, e := g.parentPaymentIntentTx(ctx, tx, mid, no)
	if e != nil {
		return g, e
	}
	return out, tx.Commit(ctx)
}
func (g ProviderFlow) parentPaymentIntentTx(ctx context.Context, q fundsReader, mid int64, no string) (ProviderFlow, error) {
	source, found, e := g.Store.readParentPaymentSource(ctx, q, mid, no)
	if e != nil {
		return g, e
	}
	if !found {
		return g.withLegacyChildParentAuthority(ctx, q, mid, no)
	}
	a, e := g.Store.readParentAccountReader(ctx, q, source.Channel, source.AccountVersion)
	if e != nil {
		return g, e
	}
	if a.source != source.Source || a.kind != source.Kind || !hmac.Equal(a.proof, source.AccountProof) {
		return g, ErrConflict
	}
	execution, e := g.restoreParentExecution(a)
	if e != nil {
		return g, e
	}
	execution, e = g.selectOriginalParentPayment(a, source, execution)
	if e != nil {
		return g, e
	}
	fee := RuntimeChannel{id: source.Channel, kind: source.Kind, methods: source.FeeMethods, product: source.FeeProduct, identity: source.FeeIdentity, accountHash: source.FeeAccountHash}
	out := g.withParentExecution(&parentExecutionBinding{fee: fee, account: a, merchant: mid, paymentVariant: source.PaymentVariant, variantFrozen: true}, execution)
	return out, nil
}

// A complete pre103 child needs no invented parent secret history. If its
// physical parent was adopted later, read only the current permission anchor;
// restoration of the real SDK still belongs exclusively to the sealed child.
func (g ProviderFlow) withLegacyChildParentAuthority(ctx context.Context, q fundsReader, mid int64, no string) (ProviderFlow, error) {
	if len(g.ParentFactories) == 0 {
		return g, nil
	}
	ready, err := g.Store.parentPaymentSchema(ctx, q)
	if err != nil || !ready {
		return g, err
	}
	child, err := g.Store.readChildRoute(ctx, q, mid, no)
	if err != nil || child == nil {
		return g, err
	}
	head, err := g.Store.readParentHead(ctx, q, child.parent)
	if errors.Is(err, ErrNotFound) {
		return g, nil
	}
	if err != nil {
		return g, err
	}
	quote, found, err := g.Store.readRouteFeeQuote(ctx, q, mid, no)
	if err != nil || !found || !childCommitmentEqual(quote.facts.Child, child.commitment()) || !hmac.Equal(head.fee.identity, quote.facts.ParentIdentity) {
		return g, ErrConflict
	}
	binding := legacyChildAuthorityBinding(head, mid, g.parentLive)
	if g.parentOperator != nil {
		binding.authorized = func() bool { return g.parentOperator(mid, head.source) }
	}
	out := g
	out.parentBindings = make(map[int64]*parentExecutionBinding, len(g.parentBindings)+1)
	for id, old := range g.parentBindings {
		out.parentBindings[id] = old
	}
	out.parentBindings[head.id] = binding
	return out, nil
}

func legacyChildAuthorityBinding(head parentChannelHead, mid int64, live func() bool) *parentExecutionBinding {
	return &parentExecutionBinding{fee: head.fee, account: parentChannelAccount{id: head.id, version: head.version, source: head.source, kind: head.kind}, merchant: mid, live: live, legacyChildAuthority: true}
}

func (g ProviderFlow) parentPaymentTrade(ctx context.Context, trade string) (ProviderFlow, error) {
	if g.Store == nil {
		return g, ErrInvalid
	}
	o, e := g.Store.OrderByTradeNo(ctx, trade)
	if e != nil {
		return g, e
	}
	return g.ParentPaymentIntent(ctx, o.MerchantID, o.OutTradeNo)
}

func parentOriginKey(mid int64, no string) string { return fmt.Sprintf("%d:%s", mid, no) }

func (g ProviderFlow) parentPaymentOrder(ctx context.Context, q fundsReader, mid, id int64) (ProviderFlow, error) {
	if g.Store == nil || mid <= 0 || id <= 0 {
		return g, ErrInvalid
	}
	var owner int64
	var no string
	if e := q.QueryRow(ctx, `SELECT merchant_id,out_trade_no FROM orders WHERE id=$1`, id).Scan(&owner, &no); e != nil {
		return g, e
	}
	if owner != mid {
		return g, ErrConflict
	}
	return g.parentPaymentIntentTx(ctx, q, mid, no)
}
