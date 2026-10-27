package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/grouppolicy"
	"sort"
	"strconv"
)

// ProjectSubchannels performs authenticated local reads and offline adapter
// validation. It creates no materials, provider requests, cache writes or jobs.
func (g ProviderFlow) ProjectSubchannels(ctx context.Context, a SubchannelActor, items []PaymentSubchannel) ([]PaymentSubchannel, error) {
	if g.Store == nil {
		return nil, ErrInvalid
	}
	tx, e := g.Store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if e = a.check(ctx, tx); e != nil {
		return nil, e
	}
	type ownerState struct {
		enabled, group bool
		routing        GroupPaymentRouting
		body           []byte
	}
	type parentState struct {
		available, verified, configured bool
		policy                          runtimeChannelPolicy
	}
	owners := map[string]ownerState{}
	parents := map[int64]parentState{}
	merchantPolicies := map[int64]SubchannelMerchantPolicy{}
	childIDs, parentIDs := []int64{}, []int64{}
	for _, c := range items {
		childIDs = append(childIDs, c.ID)
		parentIDs = append(parentIDs, c.ParentID)
	}
	childRisks, e := g.Store.coreRiskSubjectStatusesTx(ctx, tx, "subchannel", childIDs)
	if e != nil {
		return nil, e
	}
	parentRisks, e := g.Store.coreRiskSubjectStatusesTx(ctx, tx, "channel", parentIDs)
	if e != nil {
		return nil, e
	}
	for i := range items {
		c := &items[i]
		if !a.allows(c.MerchantID) {
			return nil, ErrNotFound
		}
		policy, knownPolicy := merchantPolicies[c.MerchantID]
		if !knownPolicy {
			policy, e = g.Store.childMerchantPolicyTx(ctx, tx, c.MerchantID, false)
			if e != nil {
				return nil, e
			}
			merchantPolicies[c.MerchantID] = policy
		}
		childRisk, parentRisk := childRisks[c.ID], parentRisks[c.ParentID]
		c.CoreRisk = &childRisk
		c.ParentCoreRisk = &parentRisk
		c.RollType = policy.RollType
		c.Methods = []string{}
		c.Fields = []SubchannelFieldState{}
		c.AccountAvailable = false
		c.BrowserTestAvailable = false
		c.ParentAvailable = false
		c.UnavailableReason = "account_unconfirmed"
		c.FeeRateBPS = nil
		c.FeeSource = "unknown"
		c.FeePolicy = "unknown"
		factory := g.SubchannelFactories[c.ParentID]
		if factory == nil {
			continue
		}
		material, e := g.Store.openChildAccount(c.account)
		if e != nil {
			return nil, e
		}
		for _, field := range factory.Schema().Fields {
			c.Fields = append(c.Fields, SubchannelFieldState{Name: field.Name, Secret: field.Secret, Present: material.Values[field.Name] != ""})
		}
		_, execution, e := g.WithSubchannelAccountTx(ctx, tx, *c)
		if e != nil {
			if errors.Is(e, ErrConflict) || errors.Is(e, ErrInvalid) || errors.Is(e, ErrSubchannelUnavailable) {
				continue
			}
			return nil, e
		}
		c.AccountAvailable = true
		c.Methods = append(c.Methods, execution.Payment.Capabilities().Methods...)
		sort.Strings(c.Methods)
		parent, known := parents[c.ParentID]
		if !known {
			binding, readErr := g.RuntimeChannel(c.ParentID)
			if readErr == nil && binding.kind == c.PayType {
				policy, configured, readErr := g.Store.runtimePolicyTx(ctx, tx, binding, false)
				if readErr != nil && !errors.Is(readErr, ErrConflict) && !errors.Is(readErr, ErrState) {
					return nil, readErr
				}
				if readErr == nil {
					parent.policy, parent.configured, parent.verified = policy, configured, true
					if policy.enabled {
						status, e := g.Store.ChannelWriteStatusTx(ctx, tx, c.ParentID)
						if e != nil {
							return nil, e
						}
						parent.available = !status.Blocked
					}
				}
			}
			parents[c.ParentID] = parent
		}
		c.ParentAvailable = parent.available && !parentRisk.Blocked
		key := fmt.Sprintf("%d/%s", c.MerchantID, c.PayType)
		owner, known := owners[key]
		if !known {
			if e = tx.QueryRow(ctx, `SELECT enabled AND COALESCE((to_jsonb(m)->>'pay')::int,1)=1 FROM merchants m WHERE id=$1 FOR SHARE`, c.MerchantID).Scan(&owner.enabled); e != nil {
				return nil, e
			}
			group, e := g.Store.GroupPaymentRoutingTx(ctx, tx, c.MerchantID, c.PayType)
			if e != nil && !errors.Is(e, ErrState) && !errors.Is(e, ErrInvalid) {
				return nil, e
			}
			owner.group = e == nil && group.Choice.Kind() == grouppolicy.Subchannels
			owner.routing = group
			if owner.group {
				if e = tx.QueryRow(ctx, `SELECT body FROM backoffice_records WHERE resource='groups' AND id=$1 FOR SHARE`, group.GroupID).Scan(&owner.body); e != nil {
					return nil, e
				}
			}
			owners[key] = owner
		}
		if owner.group && parent.verified {
			snapshot, err := feePolicy(ctx, tx, strconv.FormatInt(owner.routing.GroupID, 10), c.PayType, 10000)
			if err == nil {
				var source string
				snapshot, source, err = effectiveRuntimeFee(snapshot, parent.configured, parent.policy.retained, owner.body, c.PayType, 10000)
				if err == nil {
					rate := snapshot.Rate
					c.FeeRateBPS = &rate
					c.FeeSource = source
					c.FeePolicy = snapshot.Policy
				}
			}
			if err != nil && !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrState) && !errors.Is(err, ErrConflict) {
				return nil, err
			}
		}
		switch {
		case !owner.enabled:
			c.UnavailableReason = "merchant_disabled"
		case !owner.group:
			c.UnavailableReason = "group_unavailable"
		case childRisk.Blocked:
			c.UnavailableReason = "risk_limited"
		case !c.ParentAvailable:
			c.UnavailableReason = "parent_unavailable"
		case c.FeeRateBPS == nil:
			c.UnavailableReason = "fee_unconfirmed"
		case !g.AllowLive:
			c.UnavailableReason = "writes_disabled"
		case !c.Enabled:
			c.UnavailableReason = "disabled"
		default:
			c.UnavailableReason = ""
			for _, method := range c.Methods {
				if method == "page" || method == "wap" || method == "native" || method == "h5" || method == "app" {
					c.BrowserTestAvailable = true
				}
			}
		}
	}
	if e = a.check(ctx, tx); e != nil {
		return nil, e
	}
	return items, tx.Commit(ctx)
}
