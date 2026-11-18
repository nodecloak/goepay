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

type channelGroupPolicy struct {
	group        merchantpolicy.EffectiveGroup
	info         grouppolicy.Info
	routingGroup int64
	routingInfo  grouppolicy.Info
}

func parseChannelGroupInfo(body []byte) (grouppolicy.Info, error) {
	var fields struct {
		Enabled *bool           `json:"enabled"`
		Info    json.RawMessage `json:"info"`
	}
	if json.Unmarshal(body, &fields) != nil {
		return nil, ErrInvalid
	}
	if fields.Enabled != nil && !*fields.Enabled {
		return nil, ErrUnavailable
	}
	if len(fields.Info) == 0 {
		fields.Info = json.RawMessage(`{}`)
	}
	info, err := grouppolicy.ParseInfo(fields.Info)
	if err != nil {
		return nil, ErrInvalid
	}
	return info, nil
}

func channelGroup(ctx context.Context, tx pgx.Tx, mid int64, lock bool) (channelGroupPolicy, error) {
	var out channelGroupPolicy
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	rows, e := tx.Query(ctx, `SELECT body FROM backoffice_records WHERE resource='pay-types' AND body->>'type'='usdt'`+suffix)
	if e != nil {
		return out, e
	}
	open := false
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return out, e
		}
		var item struct {
			Enabled bool `json:"enabled"`
		}
		if json.Unmarshal(raw, &item) != nil {
			rows.Close()
			return out, ErrInvalid
		}
		open = open || item.Enabled
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return out, e
	}
	if !open {
		return out, ErrUnavailable
	}
	// Group zero is the existing system-default assignment. Lock it before
	// higher group IDs so inheritance never reverses the effective-group lock
	// order. Keep it locked until invoice commit, including the final proof.
	var defaultBody []byte
	if e = tx.QueryRow(ctx, `SELECT body FROM backoffice_records WHERE resource='groups' AND id=0`+suffix).Scan(&defaultBody); e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	g, e := merchantpolicy.ReadEffectiveGroup(ctx, tx, mid, lock)
	if e != nil {
		return out, e
	}
	info, e := parseChannelGroupInfo(g.Body)
	if e != nil {
		return out, e
	}
	out = channelGroupPolicy{group: g, info: info, routingGroup: g.ID, routingInfo: info}
	if info["usdt"].RoutingChoice().Kind() == grouppolicy.Inherit {
		if g.ID == 0 || len(defaultBody) == 0 {
			return out, ErrUnavailable
		}
		out.routingGroup = 0
		out.routingInfo, e = parseChannelGroupInfo(defaultBody)
		if e != nil {
			return out, e
		}
	}
	switch out.routingInfo["usdt"].RoutingChoice().Kind() {
	case grouppolicy.First, grouppolicy.Channel:
	case grouppolicy.Inherit, grouppolicy.Disabled:
		return out, ErrUnavailable
	default:
		return out, ErrInvalid
	}
	return out, nil
}

func (s *Service) channelCandidates(ctx context.Context, tx pgx.Tx, network string, explicit int64, choice grouppolicy.Choice) ([]Channel, error) {
	if explicit < 0 || network != "" && network != "tron" && network != "ethereum" {
		return nil, ErrInvalid
	}
	if choice.Kind() == grouppolicy.Channel {
		if explicit > 0 && explicit != choice.ID() {
			return nil, ErrInvalid
		}
		explicit = choice.ID()
	}
	rows, e := tx.Query(ctx, `SELECT `+channelColumns+` FROM crypto_channels WHERE enabled AND NOT archived AND ($1::bigint=0 OR id=$1) AND ($2::text='' OR network=$2) ORDER BY id`, explicit, network)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Channel{}
	for rows.Next() {
		c, e := scanChannel(rows)
		if e != nil {
			return nil, e
		}
		if s.Networks[c.Network].Enabled {
			out = append(out, c)
		}
	}
	if err := rows.Err(); err != nil || !s.ManageNetworks {
		return out, err
	}
	// Check each chain once. Admission locks and rechecks only the candidate
	// it may commit, so an unavailable chain cannot block another candidate.
	enabled := make(map[string]bool, 2)
	ready := out[:0]
	for _, channel := range out {
		active, checked := enabled[channel.Network]
		if !checked {
			active, e = s.networkEnabled(ctx, tx, channel.Network, false)
			if e != nil {
				return nil, e
			}
			enabled[channel.Network] = active
		}
		if active {
			ready = append(ready, channel)
		}
	}
	return ready, nil
}

func channelOrderLimits(c Channel, amount int64) (eligible bool, day int64, err error) {
	min, err := channelLimit(c.MinAmount)
	if err != nil {
		return false, 0, err
	}
	max, err := channelLimit(c.MaxAmount)
	if err != nil {
		return false, 0, err
	}
	day, err = channelLimit(c.DayLimit)
	if err != nil {
		return false, 0, err
	}
	return amount >= min && (max == 0 || amount <= max) && (day == 0 || amount <= day), day, nil
}

func (s *Service) lockChannelForInvoice(ctx context.Context, tx pgx.Tx, id, amount int64) (Channel, error) {
	// Roll back rejected candidates' locks before trying the next channel.
	// Releasing the chosen savepoint keeps its lock through invoice commit.
	candidate, err := tx.Begin(ctx)
	if err != nil {
		return Channel{}, err
	}
	defer candidate.Rollback(ctx)
	c, err := readChannel(ctx, candidate, id, true)
	if err != nil {
		return Channel{}, err
	}
	eligible, day, err := channelOrderLimits(c, amount)
	if err != nil {
		return Channel{}, err
	}
	eligible = eligible && c.Enabled && !c.Archived
	if eligible && day > 0 {
		// Read committed usage after acquiring the channel lock. Expired and
		// pending invoices both reserve capacity on their UTC creation date.
		// Keep the sum numeric: usage from a previously unlimited channel
		// may exceed int64 before a new limit is configured.
		if err = candidate.QueryRow(ctx, `
			SELECT COALESCE(sum(amount),0) <= $2::bigint
			  FROM crypto_invoices
			 CROSS JOIN (
				SELECT date_trunc('day',statement_timestamp() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS day_start
			 ) utc
			 WHERE channel_id=$1
			   AND created_at >= utc.day_start
			   AND created_at < utc.day_start + interval '24 hours'
		`, c.ID, day-amount).Scan(&eligible); err != nil {
			return Channel{}, err
		}
	}
	if eligible && s.ManageNetworks {
		eligible, err = s.networkEnabled(ctx, candidate, c.Network, true)
		if err != nil {
			return Channel{}, err
		}
	}
	if !eligible {
		if err = candidate.Rollback(ctx); err != nil {
			return Channel{}, err
		}
		return Channel{}, ErrUnavailable
	}
	return c, candidate.Commit(ctx)
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
	policy, e := channelGroup(ctx, tx, in.MerchantID, true)
	if e != nil {
		return out, e
	}
	channels, e := s.channelCandidates(ctx, tx, in.Network, in.ChannelID, policy.routingInfo["usdt"].RoutingChoice())
	if e != nil {
		return out, e
	}
	for _, candidate := range channels {
		eligible, _, e := channelOrderLimits(candidate, n)
		if e != nil {
			return out, e
		}
		if !eligible {
			continue
		}
		c, e := s.lockChannelForInvoice(ctx, tx, candidate.ID, n)
		if errors.Is(e, ErrUnavailable) {
			continue
		}
		if e != nil {
			return out, e
		}
		fee, source := c.FeeBPS, "channel"
		if retained, set := policy.info["usdt"].RetainedBPS(); set {
			fee, source = 10000-retained, "group"
		}
		canonical, e := policy.info.CanonicalJSON()
		if e != nil {
			return out, e
		}
		routing, e := policy.routingInfo.CanonicalJSON()
		if e != nil {
			return out, e
		}
		fields := map[string]any{
			"channel_id": strconv.FormatInt(c.ID, 10), "revision": strconv.FormatInt(c.Revision, 10),
			"name": c.Name, "asset": "USDT", "network": c.Network, "fee_bps": fee, "fee_source": source,
			"group_id": strconv.FormatInt(policy.group.ID, 10), "group_policy": json.RawMessage(canonical),
			"routing_group_id": strconv.FormatInt(policy.routingGroup, 10), "routing_group_policy": json.RawMessage(routing),
			"min_amount": c.MinAmount, "max_amount": c.MaxAmount, "day_limit": c.DayLimit,
			"day_basis": "UTC creation date", "quote_cny_per_usdt": in.QuoteRate,
		}
		if in.QuoteRate != "" {
			fields["quote_valid_until"] = in.QuoteValidUntil.UTC()
		}
		snapshot, e := json.Marshal(fields)
		if e != nil {
			return out, e
		}
		if e = currentGroupDeadline(ctx, tx, policy.group.ValidUntil); e != nil {
			return out, e
		}
		return channelAdmission{channel: c, feeBPS: fee, feeSource: source, snapshot: snapshot, validUntil: policy.group.ValidUntil}, nil
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
	policy, e := channelGroup(ctx, tx, mid, false)
	if errors.Is(e, ErrUnavailable) || errors.Is(e, ErrInvalid) || errors.Is(e, merchantpolicy.ErrGroupPolicy) {
		return []Channel{}, nil
	}
	if e != nil {
		return nil, e
	}
	items, e := s.channelCandidates(ctx, tx, network, 0, policy.routingInfo["usdt"].RoutingChoice())
	if e != nil {
		return nil, e
	}
	if e = currentGroupDeadline(ctx, tx, policy.group.ValidUntil); e != nil {
		return []Channel{}, nil
	}
	for i := range items {
		if retained, set := policy.info["usdt"].RetainedBPS(); set {
			items[i].FeeBPS = 10000 - retained
		}
	}
	return items, tx.Commit(ctx)
}
