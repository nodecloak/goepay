package cryptopay

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/grouppolicy"
	"goepay/internal/merchantpolicy"
)

type channelAdmission struct {
	channel    Channel
	feeBPS     int64
	feeSource  string
	snapshot   json.RawMessage
	validUntil time.Time
}

func channelGroup(ctx context.Context, tx pgx.Tx, mid int64, lock bool) (merchantpolicy.EffectiveGroup, grouppolicy.Info, error) {
	var empty merchantpolicy.EffectiveGroup
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	rows, e := tx.Query(ctx, `SELECT body FROM backoffice_records WHERE resource='pay-types' AND body->>'type'='usdt'`+suffix)
	if e != nil {
		return empty, nil, e
	}
	open := false
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return empty, nil, e
		}
		var item struct {
			Enabled bool `json:"enabled"`
		}
		if json.Unmarshal(raw, &item) != nil {
			rows.Close()
			return empty, nil, ErrInvalid
		}
		open = open || item.Enabled
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return empty, nil, e
	}
	if !open {
		return empty, nil, ErrUnavailable
	}
	g, e := merchantpolicy.ReadEffectiveGroup(ctx, tx, mid, lock)
	if e != nil {
		return g, nil, e
	}
	var fields struct {
		Info json.RawMessage `json:"info"`
	}
	if json.Unmarshal(g.Body, &fields) != nil {
		return g, nil, ErrInvalid
	}
	if len(fields.Info) == 0 {
		fields.Info = json.RawMessage(`{}`)
	}
	info, e := grouppolicy.ParseInfo(fields.Info)
	if e != nil {
		return g, nil, ErrInvalid
	}
	switch info["usdt"].RoutingChoice().Kind() {
	case grouppolicy.Inherit, grouppolicy.First, grouppolicy.Channel:
	default:
		return g, nil, ErrInvalid
	}
	return g, info, nil
}

func (s *Service) channelCandidates(ctx context.Context, tx pgx.Tx, mid int64, network string, explicit int64, info grouppolicy.Info, lock bool) ([]Channel, error) {
	if explicit < 0 || network != "" && network != "tron" && network != "ethereum" {
		return nil, ErrInvalid
	}
	choice := info["usdt"].RoutingChoice()
	if choice.Kind() == grouppolicy.Channel {
		if explicit > 0 && explicit != choice.ID() {
			return nil, ErrInvalid
		}
		explicit = choice.ID()
	}
	rows, e := tx.Query(ctx, `SELECT c.id FROM crypto_channels c JOIN crypto_channel_grants g ON g.channel_id=c.id WHERE g.merchant_id=$1 AND ($2::bigint=0 OR c.id=$2) AND ($3::text='' OR c.network=$3) ORDER BY c.id`, mid, explicit, network)
	if e != nil {
		return nil, e
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, e
	}
	out := []Channel{}
	for _, id := range ids {
		c, e := readChannel(ctx, tx, id, lock)
		if e != nil {
			return nil, e
		}
		if !c.Enabled || c.Archived || network != "" && c.Network != network {
			continue
		}
		config, exists := s.Networks[c.Network]
		if !exists || !config.Enabled {
			continue
		}
		granted := false
		for _, candidate := range c.MerchantIDs {
			if candidate == strconv.FormatInt(mid, 10) {
				granted = true
				break
			}
		}
		if !granted {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func currentGroupDeadline(ctx context.Context, tx pgx.Tx, until time.Time) error {
	if until.IsZero() {
		return nil
	}
	var now time.Time
	if e := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return e
	}
	if !now.Before(until) {
		return ErrConflict
	}
	return nil
}

func (s *Service) admitChannel(ctx context.Context, tx pgx.Tx, in CreateInvoiceInput, n int64) (channelAdmission, error) {
	var out channelAdmission
	if in.QuoteRate != "" {
		if _, e := ParseAmount(in.QuoteRate); e != nil || in.CNYAmount == 0 {
			return out, ErrInvalid
		}
	}
	g, info, e := channelGroup(ctx, tx, in.MerchantID, true)
	if e != nil {
		return out, e
	}
	channels, e := s.channelCandidates(ctx, tx, in.MerchantID, in.Network, in.ChannelID, info, true)
	if e != nil {
		return out, e
	}
	for _, c := range channels {
		min, e := channelLimit(c.MinAmount)
		if e != nil {
			return out, e
		}
		max, e := channelLimit(c.MaxAmount)
		if e != nil {
			return out, e
		}
		day, e := channelLimit(c.DayLimit)
		if e != nil {
			return out, e
		}
		if n < min || max > 0 && n > max {
			continue
		}
		if day > 0 {
			var used int64
			// Pending intents also reserve daily acceptance capacity: expiry does
			// not prove that an already transferred deposit cannot still arrive.
			if e = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount),0)::bigint FROM crypto_invoices WHERE channel_id=$1 AND created_at >= (date_trunc('day',clock_timestamp() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')`, c.ID).Scan(&used); e != nil {
				return out, e
			}
			if n > day || used > day-n {
				continue
			}
		}
		fee, source := c.FeeBPS, "channel"
		if retained, set := info["usdt"].RetainedBPS(); set {
			fee, source = 10000-retained, "group"
		}
		canonical, e := info.CanonicalJSON()
		if e != nil {
			return out, e
		}
		snapshot, e := json.Marshal(map[string]any{"channel_id": strconv.FormatInt(c.ID, 10), "revision": strconv.FormatInt(c.Revision, 10), "name": c.Name, "asset": "USDT", "network": c.Network, "fee_bps": fee, "fee_source": source, "group_id": strconv.FormatInt(g.ID, 10), "group_policy": json.RawMessage(canonical), "min_amount": c.MinAmount, "max_amount": c.MaxAmount, "day_limit": c.DayLimit, "day_basis": "UTC creation date", "quote_cny_per_usdt": in.QuoteRate})
		if e != nil {
			return out, e
		}
		if e = currentGroupDeadline(ctx, tx, g.ValidUntil); e != nil {
			return out, e
		}
		return channelAdmission{channel: c, feeBPS: fee, feeSource: source, snapshot: snapshot, validUntil: g.ValidUntil}, nil
	}
	return out, ErrUnavailable
}

// AvailableChannels is a read-only capability projection. Creating an invoice
// rechecks these facts under locks and additionally reserves amount quotas.
func (s *Service) AvailableChannels(ctx context.Context, mid int64, network string) ([]Channel, error) {
	if s == nil || s.Pool == nil {
		return nil, ErrUnavailable
	}
	if mid <= 0 || network != "" && network != "tron" && network != "ethereum" {
		return nil, ErrInvalid
	}
	tx, e := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	var pay int
	var enabled bool
	if e = tx.QueryRow(ctx, "SELECT enabled,COALESCE((to_jsonb(m)->>'pay')::int,1) FROM merchants m WHERE id=$1", mid).Scan(&enabled, &pay); e != nil {
		return nil, e
	}
	if !enabled || pay != 1 {
		return []Channel{}, nil
	}
	g, info, e := channelGroup(ctx, tx, mid, false)
	if errors.Is(e, ErrUnavailable) || errors.Is(e, ErrInvalid) || errors.Is(e, merchantpolicy.ErrGroupPolicy) {
		return []Channel{}, nil
	}
	if e != nil {
		return nil, e
	}
	items, e := s.channelCandidates(ctx, tx, mid, network, 0, info, false)
	if e != nil {
		return nil, e
	}
	if e = currentGroupDeadline(ctx, tx, g.ValidUntil); e != nil {
		return []Channel{}, nil
	}
	for i := range items {
		if retained, set := info["usdt"].RetainedBPS(); set {
			items[i].FeeBPS = 10000 - retained
		}
		items[i].MerchantIDs = []string{strconv.FormatInt(mid, 10)}
	}
	return items, tx.Commit(ctx)
}
