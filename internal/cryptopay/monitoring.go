package cryptopay

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type MonitoringSnapshot struct {
	AsOf        time.Time          `json:"as_of"`
	WindowStart time.Time          `json:"window_start"`
	WindowEnd   time.Time          `json:"window_end"`
	Networks    []DepositPoolUsage `json:"networks"`
	Channels    []ChannelDayUsage  `json:"channels"`
}

type DepositPoolUsage struct {
	Network          string `json:"network"`
	Enabled          bool   `json:"enabled"`
	DepositTotal     int64  `json:"deposit_total"`
	DepositUsed      int64  `json:"deposit_used"`
	DepositAvailable int64  `json:"deposit_available"`
}

type ChannelDayUsage struct {
	ID           int64           `json:"id,string"`
	Name         string          `json:"name"`
	Network      string          `json:"network"`
	Enabled      bool            `json:"enabled"`
	Archived     bool            `json:"archived"`
	DayLimit     string          `json:"day_limit"`
	DayUsed      string          `json:"day_used"`
	DayRemaining *string         `json:"day_remaining"`
	ByState      InvoiceDayUsage `json:"day_usage_by_state"`
}

type InvoiceDayUsage struct {
	Pending   string `json:"pending"`
	Underpaid string `json:"underpaid"`
	Paid      string `json:"paid"`
	Overpaid  string `json:"overpaid"`
	Expired   string `json:"expired"`
	Late      string `json:"late"`
}

const monitoringChannelLimit = 1000

// The UTC creation window matches channel admission. Include every amount
// reserved against the channel, including closed or expired checkouts. SUM on
// bigint produces a numeric, so an unlimited channel's totals stay exact beyond
// int64 even though each individual invoice fits in int64.
const channelDayUsageSQL = `
SELECT c.id,c.name,c.network,c.enabled,c.archived,c.day_limit,
       trim_scale(COALESCE(u.total,0)*0.000001)::text,
       CASE WHEN c.day_limit>0 THEN trim_scale(GREATEST(c.day_limit-COALESCE(u.total,0),0)*0.000001)::text END,
       trim_scale(COALESCE(u.pending,0)*0.000001)::text,
       trim_scale(COALESCE(u.underpaid,0)*0.000001)::text,
       trim_scale(COALESCE(u.paid,0)*0.000001)::text,
       trim_scale(COALESCE(u.overpaid,0)*0.000001)::text,
       trim_scale(COALESCE(u.expired,0)*0.000001)::text,
       trim_scale(COALESCE(u.late,0)*0.000001)::text
  FROM (SELECT id,name,network,enabled,archived,day_limit FROM crypto_channels ORDER BY id LIMIT $4) c
 CROSS JOIN LATERAL (
	SELECT sum(amount) AS total,
	       sum(amount) FILTER (WHERE state='pending') AS pending,
	       sum(amount) FILTER (WHERE state='underpaid') AS underpaid,
	       sum(amount) FILTER (WHERE state='paid') AS paid,
	       sum(amount) FILTER (WHERE state='overpaid') AS overpaid,
	       sum(amount) FILTER (WHERE state='expired') AS expired,
	       sum(amount) FILTER (WHERE state='late') AS late
	  FROM (
		SELECT amount,CASE WHEN state IN ('pending','underpaid') AND expires_at<$1 THEN 'expired' ELSE state END AS state
		  FROM crypto_invoices
		 WHERE channel_id=c.id AND created_at>=$2 AND created_at<$3
	  ) invoice_day
 ) u
 ORDER BY c.id`

func (s *Service) Monitoring(ctx context.Context) (MonitoringSnapshot, error) {
	if s == nil || s.Pool == nil {
		return MonitoringSnapshot{}, ErrUnavailable
	}
	// A consistent snapshot keeps channel and inventory counts aligned. The
	// original-session proof takes share locks, so PostgreSQL READ ONLY cannot
	// be used here even though the projections make no business writes.
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return MonitoringSnapshot{}, err
	}
	defer tx.Rollback(ctx)
	if err = authorize(ctx, tx); err != nil {
		return MonitoringSnapshot{}, err
	}
	var out MonitoringSnapshot
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&out.AsOf); err != nil {
		return MonitoringSnapshot{}, err
	}
	out.AsOf = out.AsOf.UTC()
	out.WindowStart = out.AsOf.Truncate(24 * time.Hour)
	out.WindowEnd = out.WindowStart.Add(24 * time.Hour)
	out.Networks, err = s.depositPoolUsage(ctx, tx)
	if err != nil {
		return MonitoringSnapshot{}, err
	}
	out.Channels, err = channelDayUsage(ctx, tx, out.AsOf, out.WindowStart, out.WindowEnd)
	if err != nil {
		return MonitoringSnapshot{}, err
	}
	if err = commit(ctx, tx); err != nil {
		return MonitoringSnapshot{}, err
	}
	return out, nil
}

func (s *Service) depositPoolUsage(ctx context.Context, tx pgx.Tx) ([]DepositPoolUsage, error) {
	rows, err := tx.Query(ctx, `SELECT network,count(*),count(*) FILTER (WHERE used)
		FROM crypto_addresses WHERE role='deposit' GROUP BY network`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byNetwork := make(map[string]DepositPoolUsage, 2)
	for rows.Next() {
		var name string
		var total, used int64
		if err = rows.Scan(&name, &total, &used); err != nil {
			return nil, err
		}
		if total > (1<<53)-1 {
			return nil, ErrUnavailable
		}
		byNetwork[name] = DepositPoolUsage{DepositTotal: total, DepositUsed: used, DepositAvailable: total - used}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	pools := make([]DepositPoolUsage, 0, 2)
	for _, name := range []string{"tron", "ethereum"} {
		pool := byNetwork[name]
		pool.Network = name
		pool.Enabled, err = s.networkEnabled(ctx, tx, name, false)
		if err != nil {
			return nil, err
		}
		pools = append(pools, pool)
	}
	return pools, nil
}

func channelDayUsage(ctx context.Context, tx pgx.Tx, now, start, end time.Time) ([]ChannelDayUsage, error) {
	rows, err := tx.Query(ctx, channelDayUsageSQL, now, start, end, monitoringChannelLimit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	channels := []ChannelDayUsage{}
	for rows.Next() {
		var c ChannelDayUsage
		var limit int64
		if err = rows.Scan(&c.ID, &c.Name, &c.Network, &c.Enabled, &c.Archived, &limit, &c.DayUsed, &c.DayRemaining,
			&c.ByState.Pending, &c.ByState.Underpaid, &c.ByState.Paid, &c.ByState.Overpaid, &c.ByState.Expired, &c.ByState.Late); err != nil {
			return nil, err
		}
		c.DayLimit = FormatAmount(limit)
		channels = append(channels, c)
	}
	if len(channels) > monitoringChannelLimit {
		return nil, ErrUnavailable
	}
	return channels, rows.Err()
}
