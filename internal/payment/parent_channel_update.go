package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"math"
	"sort"
	"strings"
)

type ParentChannelPatch struct {
	Expected   string
	Name, Rate *string
	Enabled    *bool
	Archived   *bool
	Mode       *int
	Account    *adapter.ParentChannelAccountChange
}

func (s *Store) UpdateParentChannel(ctx context.Context, actor ParentChannelActor, id int64, in ParentChannelPatch, factories map[int64]adapter.ParentAccountFactory) (ParentChannelView, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return ParentChannelView{}, e
	}
	defer tx.Rollback(ctx)
	if e = parentChannelLock(ctx, tx, id); e != nil {
		return ParentChannelView{}, e
	}
	out, e := s.updateParentChannelTx(ctx, tx, actor, id, in, factories)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

func (s *Store) updateParentChannelTx(ctx context.Context, tx pgx.Tx, actor ParentChannelActor, id int64, in ParentChannelPatch, factories map[int64]adapter.ParentAccountFactory) (ParentChannelView, error) {
	var out ParentChannelView
	if id <= 0 || len(in.Expected) != 64 || in.Mode != nil && *in.Mode != 0 || in.Archived != nil && !*in.Archived {
		return out, ErrInvalid
	}
	if in.Name != nil {
		value := strings.TrimSpace(*in.Name)
		if !validParentName(value) {
			return out, ErrInvalid
		}
		in.Name = &value
	}
	var rate int64
	var e error
	if in.Rate != nil {
		rate, e = ParseChannelRetainedRate(*in.Rate)
		if e != nil {
			return out, e
		}
	}
	if e = actor.check(ctx, tx); e != nil {
		return out, e
	}
	h, e := s.readParentHead(ctx, tx, id)
	if e != nil {
		return out, e
	}
	if h.archived {
		return out, ErrConflict
	}
	a, e := s.readParentAccount(ctx, tx, id, h.version)
	if e != nil {
		return out, e
	}
	p, configured, e := s.runtimePolicyTx(ctx, tx, h.fee, false)
	if e != nil {
		return out, e
	}
	if !configured && (!h.legacy || h.id != h.source) {
		return out, ErrConflict
	}
	if !configured && (in.Enabled != nil || in.Mode != nil) && in.Rate == nil {
		// The original legacy fee can depend on the paying group. A first
		// typed policy needs an explicit rate; credential edits retain legacy.
		return out, ErrInvalid
	}
	factory := factories[h.source]
	before, e := s.parentView(h, a, factory, p)
	if e != nil {
		return out, e
	}
	if !hmac.Equal([]byte(before.Fingerprint), []byte(in.Expected)) {
		return out, ErrConflict
	}
	if in.Account != nil {
		if factory == nil {
			return out, ErrState
		}
		raw, e := s.openParentAccount(a)
		if e != nil {
			return out, e
		}
		execution, next, e := factory.ChangeSnapshot(id, raw, *in.Account)
		if e != nil {
			return out, ErrInvalid
		}
		if !bytes.Equal(raw, next) {
			if h.version == math.MaxInt64 {
				return out, ErrState
			}
			h.version++
			a, e = s.sealParentAccount(id, h.version, h.source, h.kind, execution, next)
			if e != nil {
				return out, e
			}
			if e = s.insertParentAccount(ctx, tx, a); e != nil {
				return out, e
			}
		}
	}
	if in.Enabled != nil && *in.Enabled {
		if factory == nil {
			return out, ErrState
		}
		raw, e := s.openParentAccount(a)
		if e != nil {
			return out, e
		}
		if _, e = factory.Restore(id, raw); e != nil {
			return out, ErrState
		}
	}
	if in.Name != nil {
		h.name = *in.Name
	}
	if in.Archived != nil {
		h.archived = *in.Archived
	}
	if h.revision == math.MaxInt64 {
		return out, ErrState
	}
	h.revision++
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&h.updated); e != nil {
		return out, e
	}
	h.proof, e = s.parentHeadProof(h)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `UPDATE runtime_parent_channels SET name=$2,revision=$3,account_version=$4,proof_hash=$5,updated_at=$6,archived=$7 WHERE id=$1`, id, h.name, h.revision, h.version, h.proof, h.updated, h.archived); e != nil {
		return out, e
	}
	if in.Rate != nil || in.Enabled != nil || in.Mode != nil {
		if p.revision == math.MaxInt64 {
			return out, ErrState
		}
		if in.Rate != nil {
			p.retained = rate
		}
		if in.Enabled != nil {
			p.enabled = *in.Enabled
		}
		p.mode = 0
		p.revision++
		p.proof, e = s.runtimePolicyProof(p)
		if e != nil {
			return out, e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO runtime_channel_policies(channel_id,pay_type,identity_hash,retained_bps,mode,enabled,revision,proof_hash) VALUES($1,$6,$7,$2,0,$3,$4,$5) ON CONFLICT(channel_id) DO UPDATE SET retained_bps=EXCLUDED.retained_bps,enabled=EXCLUDED.enabled,revision=EXCLUDED.revision,proof_hash=EXCLUDED.proof_hash`, id, p.retained, p.enabled, p.revision, p.proof, p.kind, p.identity); e != nil {
			return out, e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'runtime-channel-policies',$3,'runtime-channel-policy-update',jsonb_build_object('retained_bps',$4::text,'revision',$5::text,'enabled',$6::boolean,'mode',0))`, actor.User.ID, actor.User.Username, id, fmt.Sprint(p.retained), fmt.Sprint(p.revision), p.enabled); e != nil {
			return out, e
		}
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'runtime-parent-channels',$3,'runtime-parent-write',jsonb_build_object('revision',$4::text,'account_version',$5::text,'action','update'))`, actor.User.ID, actor.User.Username, id, fmt.Sprint(h.revision), fmt.Sprint(h.version)); e != nil {
		return out, e
	}
	out, e = s.parentView(h, a, factory, p)
	if e != nil {
		return out, e
	}
	if e = actor.check(ctx, tx); e != nil {
		return out, e
	}
	out.Enabled = out.Enabled && !h.archived
	return out, nil
}

type ParentChannelBatchItem struct {
	ID       int64
	Expected string
}

func (s *Store) BatchParentChannels(ctx context.Context, actor ParentChannelActor, action string, items []ParentChannelBatchItem, factories map[int64]adapter.ParentAccountFactory) ([]ParentChannelView, error) {
	if len(items) == 0 || len(items) > 100 || action != "open" && action != "close" && action != "delete" {
		return nil, ErrInvalid
	}
	items = append([]ParentChannelBatchItem{}, items...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	for i, item := range items {
		if item.ID <= 0 || len(item.Expected) != 64 || i > 0 && items[i-1].ID == item.ID {
			return nil, ErrInvalid
		}
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	for _, item := range items {
		if e = parentChannelLock(ctx, tx, item.ID); e != nil {
			return nil, e
		}
	}
	out := []ParentChannelView{}
	for _, item := range items {
		patch := ParentChannelPatch{Expected: item.Expected}
		enabled := action == "open"
		archived := true
		if action == "delete" {
			patch.Archived = &archived
		} else {
			patch.Enabled = &enabled
		}
		view, e := s.updateParentChannelTx(ctx, tx, actor, item.ID, patch, factories)
		if e != nil {
			return nil, e
		}
		out = append(out, view)
	}
	if e = actor.check(ctx, tx); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}
