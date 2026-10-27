package payment

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/merchantpolicy"
)

// CurrentChannelFee contains only the current new-order fee, never account,
// product identity, credentials, costs or historical terms. Nil means unknown.
type CurrentChannelFee struct {
	RateBPS *int64
	Source  string
}

// CurrentChannelFees reads a caller-supplied, already owner-scoped registry
// allowlist. Policy and append-only anchors are batched; provider methods are
// never invoked. This is a current display, separate from immutable order fees.
func (g ProviderFlow) CurrentChannelFees(ctx context.Context, mid int64, ids []int64) (map[int64]CurrentChannelFee, error) {
	out := map[int64]CurrentChannelFee{}
	bindings := map[int64]RuntimeChannel{}
	for _, id := range ids {
		out[id] = CurrentChannelFee{Source: "unknown"}
		if b, err := g.RuntimeChannel(id); err == nil {
			bindings[id] = b
		}
	}
	if len(bindings) == 0 || g.Store == nil || mid <= 0 {
		return out, nil
	}
	keys := make([]int64, 0, len(bindings))
	for id := range bindings {
		keys = append(keys, id)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	tx, err := g.Store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	group, err := merchantpolicy.ReadEffectiveGroup(ctx, tx, mid, true)
	if err != nil {
		if errors.Is(err, merchantpolicy.ErrGroupPolicy) || errors.Is(err, pgx.ErrNoRows) {
			return out, nil
		}
		return nil, err
	}
	policies := map[int64]runtimeChannelPolicy{}
	rows, err := tx.Query(ctx, `SELECT channel_id,pay_type,identity_hash,retained_bps,mode,enabled,revision,proof_hash FROM runtime_channel_policies WHERE channel_id=ANY($1::bigint[])`, keys)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p runtimeChannelPolicy
		if err = rows.Scan(&p.channel, &p.kind, &p.identity, &p.retained, &p.mode, &p.enabled, &p.revision, &p.proof); err != nil {
			rows.Close()
			return nil, err
		}
		policies[p.channel] = p
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	updates := map[int64]int64{}
	rows, err = tx.Query(ctx, `SELECT resource_id,count(*) FROM backoffice_audit WHERE resource='runtime-channel-policies' AND operation='runtime-channel-policy-update' AND resource_id=ANY($1::bigint[]) GROUP BY resource_id`, keys)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, n int64
		if err = rows.Scan(&id, &n); err != nil {
			rows.Close()
			return nil, err
		}
		updates[id] = n
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	type mergedGroup struct {
		f   feeSnapshot
		err error
	}
	groups := map[string]mergedGroup{}
	for _, id := range keys {
		b := bindings[id]
		p, configured := policies[id]
		if configured {
			if !g.Store.runtimePolicyValid(b, p, updates[id]) || !p.enabled {
				continue
			}
		} else if updates[id] != 0 {
			continue
		}
		base, ok := groups[b.kind]
		if !ok {
			base.f, base.err = feePolicy(ctx, tx, strconv.FormatInt(group.ID, 10), b.kind, 10000)
			groups[b.kind] = base
		}
		if base.err != nil {
			if errors.Is(base.err, ErrInvalid) || errors.Is(base.err, ErrState) || errors.Is(base.err, ErrConflict) {
				continue
			}
			return nil, base.err
		}
		f, source, mergeErr := effectiveRuntimeFee(base.f, configured, p.retained, group.Body, b.kind, 10000)
		if mergeErr != nil {
			continue
		}
		rate := f.Rate
		out[id] = CurrentChannelFee{RateBPS: &rate, Source: source}
	}
	if !group.ValidUntil.IsZero() {
		var now time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return nil, err
		}
		if !now.Before(group.ValidUntil) {
			for id := range out {
				out[id] = CurrentChannelFee{Source: "unknown"}
			}
		}
	}
	return out, tx.Commit(ctx)
}
