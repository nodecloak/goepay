package server

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"math/big"
	"sort"
	"strconv"
	"time"
)

func (s *Server) withPaymentExecution(flow payment.ProviderFlow, execution adapter.SubchannelExecution, parent int64) *Server {
	if flow.SubchannelID() == 0 {
		return s
	}
	clone := s.executionView()
	clone.ProviderFlow = &flow
	if clone.subchannelPolicySource == nil {
		clone.subchannelPolicySource = s
	}
	clone.OAuthProviders = map[int64]adapter.WebOAuthProvider{}
	for id, p := range s.OAuthProviders {
		clone.OAuthProviders[id] = p
	}
	delete(clone.OAuthProviders, parent)
	if execution.Web != nil {
		clone.OAuthProviders[parent] = execution.Web
	}
	clone.MiniOAuthProviders = map[int64]adapter.MiniOAuthProvider{}
	for id, p := range s.MiniOAuthProviders {
		clone.MiniOAuthProviders[id] = p
	}
	delete(clone.MiniOAuthProviders, parent)
	if execution.Mini != nil {
		clone.MiniOAuthProviders[parent] = execution.Mini
	}
	return clone
}

func (s *Server) paymentExecutionWriteAllowed() bool {
	root := s
	if s.subchannelPolicySource != nil {
		root = s.subchannelPolicySource
	}
	return root != nil && !root.Sandbox && root.ProviderFlow != nil && root.ProviderFlow.AllowLive && !root.ProviderFlow.LocalSimulation
}
func (s *Server) paymentIntentServer(ctx context.Context, mid int64, no string, parent int64) (*Server, error) {
	flow, execution, e := s.ProviderFlow.ForPaymentIntent(ctx, mid, no, parent)
	if e != nil {
		return nil, e
	}
	if len(flow.ParentFactories) > 0 {
		s = s.withRestoredParentFlow(flow)
	}
	return s.withPaymentExecution(flow, execution, parent), nil
}
func (s *Server) paymentIntentServerTx(ctx context.Context, tx pgx.Tx, mid int64, no string, parent int64) (*Server, error) {
	flow, execution, e := s.ProviderFlow.ForPaymentIntentTx(ctx, tx, mid, no, parent)
	if e != nil {
		return nil, e
	}
	if len(flow.ParentFactories) > 0 {
		s = s.withRestoredParentFlow(flow)
	}
	return s.withPaymentExecution(flow, execution, parent), nil
}
func (s *Server) selectSubchannelRoute(ctx context.Context, tx pgx.Tx, mid int64, p compat.Params, mode providerRouteMode, group payment.GroupPaymentRouting, amount, explicitParent int64) (int64, string, error) {
	explicitChild := int64(0)
	if raw := p["subchannel_id"]; raw != "" {
		var e error
		explicitChild, e = strconv.ParseInt(raw, 10, 64)
		if e != nil || explicitChild <= 0 || strconv.FormatInt(explicitChild, 10) != raw {
			return 0, "", payment.ErrInvalid
		}
	}
	if e := routeLock(ctx, tx, fmt.Sprintf("goepay/route/child-policy/%d", mid)); e != nil {
		return 0, "", e
	}
	candidates, roll, e := s.Store.SubchannelCandidatesTx(ctx, tx, mid, p["type"])
	if e != nil {
		return 0, "", e
	}
	parentSet := map[int64]bool{}
	for _, c := range candidates {
		parentSet[c.ParentID] = true
	}
	ids := []int64{}
	for id := range parentSet {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if e = routeLock(ctx, tx, fmt.Sprintf("goepay/route/channel/%d", id)); e != nil {
			return 0, "", e
		}
	}
	childIDs := make([]int64, 0, len(candidates))
	for _, c := range candidates {
		childIDs = append(childIDs, c.ID)
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('goepay/route/subchannel/'||id::text,0)) FROM (SELECT id FROM unnest($1::bigint[]) x(id) ORDER BY id) ordered`, childIDs); e != nil {
		return 0, "", e
	}
	currentChildren, e := s.Store.LockedSubchannelsTx(ctx, tx, mid, childIDs)
	if e != nil {
		return 0, "", e
	}
	lastUsed, e := s.Store.ChildLastUsedBatchTx(ctx, tx, childIDs)
	if e != nil {
		return 0, "", e
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return 0, "", e
	}
	globalAdmissions := map[int64]*payment.ParentGlobalLimitAdmission{}
	parentEligible := map[int64]bool{}
	for _, id := range ids {
		parent, err := s.ProviderFlow.RuntimeChannel(id)
		if err != nil {
			continue
		}
		allowed, _, err := s.Store.RuntimeChannelAllowedTx(ctx, tx, parent)
		if err != nil {
			return 0, "", err
		}
		if !allowed {
			continue
		}
		status, err := s.Store.ChannelWriteStatusTx(ctx, tx, id)
		if err != nil {
			return 0, "", err
		}
		if status.Blocked {
			continue
		}
		allowed, admission, err := s.Store.CheckRuntimeChannelLimitsTx(ctx, tx, parent, amount, now)
		if err != nil {
			return 0, "", err
		}
		parentEligible[id] = allowed
		globalAdmissions[id] = admission
	}
	type eligibleChild struct {
		child  payment.PaymentSubchannel
		server *Server
		method string
		last   int64
	}
	eligible := []eligibleChild{}
	if !group.ValidUntil.IsZero() && !now.Before(group.ValidUntil) {
		return 0, "", payment.ErrState
	}
	for _, old := range candidates {
		c, found := currentChildren[old.ID]
		if !found || !c.Enabled || c.Archived || c.ParentID != old.ParentID || !parentEligible[c.ParentID] || explicitChild != 0 && explicitChild != c.ID || explicitParent != 0 && explicitParent != c.ParentID || amount < c.MinAmount || c.MaxAmount > 0 && amount > c.MaxAmount {
			continue
		}
		flow, execution, e := s.ProviderFlow.WithSubchannelAccountTx(ctx, tx, c)
		if e != nil {
			if errors.Is(e, payment.ErrSubchannelUnavailable) {
				continue
			}
			return 0, "", e
		}
		childServer := s.withPaymentExecution(flow, execution, c.ParentID)
		method, e := childServer.routeCandidate(mid, c.ParentID, amount, p, mode)
		if e != nil {
			continue
		}
		eligible = append(eligible, eligibleChild{c, childServer, method, lastUsed[c.ID]})
	}
	if len(eligible) == 0 {
		return 0, "", payment.ErrState
	}
	chosen := 0
	switch roll {
	case 0:
		draw, e := rand.Int(rand.Reader, big.NewInt(int64(len(eligible))))
		if e != nil {
			return 0, "", e
		}
		chosen = int(draw.Int64())
	case 1:
		sort.Slice(eligible, func(i, j int) bool {
			if eligible[i].last != eligible[j].last {
				return eligible[i].last < eligible[j].last
			}
			return eligible[i].child.ID < eligible[j].child.ID
		})
	case 2:
		total := int64(0)
		for _, c := range eligible {
			weight := c.child.Weight
			if weight <= 0 {
				weight = 1
			}
			total += weight
		}
		draw, e := rand.Int(rand.Reader, big.NewInt(total))
		if e != nil {
			return 0, "", e
		}
		n := draw.Int64()
		for i, c := range eligible {
			weight := c.child.Weight
			if weight <= 0 {
				weight = 1
			}
			if n < weight {
				chosen = i
				break
			}
			n -= weight
		}
	case 3:
	default:
		return 0, "", payment.ErrState
	}
	c := eligible[chosen]
	if e = c.server.insertProviderRoute(ctx, tx, mid, p, c.child.ParentID, c.method, amount, 0, group.Snapshot, group, globalAdmissions[c.child.ParentID]); e != nil {
		return 0, "", e
	}
	finalDeadline := group.ValidUntil
	if until := globalAdmissions[c.child.ParentID].ValidUntil(); !until.IsZero() && (finalDeadline.IsZero() || until.Before(finalDeadline)) {
		finalDeadline = until
	}
	if !finalDeadline.IsZero() {
		if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return 0, "", e
		}
		if !now.Before(finalDeadline) {
			return 0, "", payment.ErrState
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return 0, "", e
	}
	return c.child.ParentID, c.method, nil
}
func (s *Server) originalOrderServer(ctx context.Context, trade string, parent int64) (*Server, error) {
	if s.Store == nil || s.ProviderFlow == nil {
		return nil, payment.ErrInvalid
	}
	o, e := s.Store.OrderByTradeNo(ctx, trade)
	if e != nil {
		return nil, e
	}
	return s.paymentIntentServer(ctx, o.MerchantID, o.OutTradeNo, parent)
}
