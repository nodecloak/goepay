package server

import (
	"context"
	"encoding/csv"
	"fmt"
	"goepay/internal/identity"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

var originalOrderStatisticsSlots = make(chan struct{}, 4)

type originalStatisticsQuery struct {
	from, until       time.Time
	merchant, channel int64
	kind              string
	csv               bool
}

func parseOriginalStatisticsQuery(values url.Values, daily bool) (originalStatisticsQuery, error) {
	q := originalStatisticsQuery{}
	invalid := fmt.Errorf("invalid original statistics filter")
	for key, v := range values {
		if len(v) != 1 || (key != "view" && key != "type" && key != "uid" && key != "format" && !(daily && key == "channel") && !(!daily && (key == "from" || key == "to"))) {
			return q, invalid
		}
	}
	if values.Get("view") != "original" {
		return q, invalid
	}
	q.kind = values.Get("type")
	if q.kind != "" && q.kind != "alipay" && q.kind != "wxpay" && q.kind != "sandbox" {
		return q, invalid
	}
	for key, target := range map[string]*int64{"uid": &q.merchant, "channel": &q.channel} {
		if raw := values.Get(key); raw != "" {
			n, e := strconv.ParseInt(raw, 10, 64)
			if e != nil || n <= 0 || strconv.FormatInt(n, 10) != raw {
				return q, invalid
			}
			*target = n
		}
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	now := time.Now().In(zone)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	q.from = today
	q.until = today
	if daily {
		q.from = today.AddDate(0, 0, -6)
	} else {
		for key, target := range map[string]*time.Time{"from": &q.from, "to": &q.until} {
			if raw, exists := values[key]; exists {
				d, e := time.ParseInLocation("2006-01-02", raw[0], zone)
				if e != nil || d.Year() < 1 || d.Format("2006-01-02") != raw[0] {
					return q, invalid
				}
				*target = d
			}
		}
	}
	if q.from.After(q.until) || q.until.Sub(q.from) > 366*24*time.Hour {
		return q, invalid
	}
	q.until = q.until.AddDate(0, 0, 1)
	if values.Has("format") {
		if values.Get("format") != "csv" {
			return q, invalid
		}
		q.csv = true
	}
	return q, nil
}

type originalStatisticsGroup struct {
	paid, refunded, profit     matrixAmount
	orders, successes, missing big.Int
}

func (g *originalStatisticsGroup) add(paid, refunded, profit string, orders, successes, missing string) error {
	var m big.Int
	if _, ok := m.SetString(missing, 10); !ok {
		return fmt.Errorf("invalid report missing count")
	}
	for _, v := range []struct {
		target  *matrixAmount
		raw     string
		unknown bool
	}{{&g.paid, paid, false}, {&g.refunded, refunded, false}, {&g.profit, profit, m.Sign() > 0}} {
		if e := v.target.add(&v.raw, v.unknown); e != nil {
			return e
		}
	}
	for _, v := range []struct {
		target *big.Int
		raw    string
	}{{&g.orders, orders}, {&g.successes, successes}, {&g.missing, missing}} {
		var n big.Int
		if _, ok := n.SetString(v.raw, 10); !ok || n.Sign() < 0 {
			return fmt.Errorf("invalid report count")
		}
		v.target.Add(v.target, &n)
	}
	return nil
}
func (g *originalStatisticsGroup) row(key string, daily bool) map[string]any {
	row := map[string]any{"realmoney": g.paid.value(), "refundmoney": g.refunded.value(), "profitmoney": g.profit.value(), "orderCount": g.orders.String(), "successCount": g.successes.String(), "missing_costs": g.missing.String(), "successRate": nil}
	if daily {
		row["day"] = key
	} else {
		row["channel"] = key
		row["channelName"] = "通道 #" + key
		if key == "unassigned" {
			row["channelName"] = "通道未分配"
		}
		if key == "total" {
			row["channelName"] = "总计"
		}
	}
	if g.orders.Sign() > 0 {
		var scaled, ratio big.Int
		scaled.Mul(&g.successes, big.NewInt(10000))
		// Round the percentage to two decimals using exact integers.
		var numerator, denominator big.Int
		numerator.Mul(&scaled, big.NewInt(2))
		numerator.Add(&numerator, &g.orders)
		denominator.Mul(&g.orders, big.NewInt(2))
		ratio.Quo(&numerator, &denominator)
		row["successRate"] = fmt.Sprintf("%s.%02d", new(big.Int).Quo(&ratio, big.NewInt(100)).String(), new(big.Int).Mod(&ratio, big.NewInt(100)).Int64())
	}
	return row
}

func (s *Server) originalOrderStatistics(w http.ResponseWriter, r *http.Request, role string, session identity.Session, name string) {
	if role != "admin" {
		response(w, 403, -1, "administrator report", nil)
		return
	}
	values, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		response(w, 400, -1, "invalid statistics query", nil)
		return
	}
	daily := name == "daily"
	q, e := parseOriginalStatisticsQuery(values, daily)
	if e != nil {
		response(w, 400, -1, e.Error(), nil)
		return
	}
	select {
	case originalOrderStatisticsSlots <- struct{}{}:
		defer func() { <-originalOrderStatisticsSlots }()
	default:
		w.Header().Set("Retry-After", "5")
		response(w, 429, -1, "statistics busy", nil)
		return
	}
	if q.csv {
		select {
		case csvExportSlots <- struct{}{}:
			defer func() { <-csvExportSlots }()
		default:
			w.Header().Set("Retry-After", "5")
			response(w, 429, -1, "CSV exports busy", nil)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	tx, e := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		s.fail(w, e)
		return
	}
	defer tx.Rollback(ctx)
	dimension := "COALESCE(p.channel_id::text,'unassigned')"
	if daily {
		dimension = "to_char(o.created_at AT TIME ZONE 'Asia/Shanghai','YYYY-MM-DD')"
	}
	// Every join is one-to-one. Aggregate successful refunds within each order
	// before grouping, so two partial refunds cannot multiply payment/counts.
	query := `SELECT ` + dimension + `,COALESCE(sum(o.amount::numeric) FILTER(WHERE o.status='paid'),0)::text,
 COALESCE(sum(refunded.amount) FILTER(WHERE o.status='paid'),0)::text,
 COALESCE(sum(o.platform_fee::numeric-c.cost_whole-CASE WHEN c.cost_remainder>50000000 THEN 1 ELSE 0 END) FILTER(WHERE o.status='paid' AND c.order_id IS NOT NULL),0)::text,
 count(*)::text,count(*) FILTER(WHERE o.status='paid')::text,count(*) FILTER(WHERE o.status='paid' AND c.order_id IS NULL)::text
 FROM orders o LEFT JOIN provider_attempts p ON p.order_id=o.id
 LEFT JOIN referral_profit_costs c ON c.order_id=o.id
 LEFT JOIN LATERAL(SELECT COALESCE(sum(r.amount::numeric),0) AS amount FROM refunds r WHERE r.order_id=o.id AND r.status='succeeded') refunded ON true
 WHERE o.created_at>=$1 AND o.created_at<$2 AND ($3::bigint=0 OR o.merchant_id=$3) AND ($4::text='' OR o.type=$4) AND ($5::bigint=0 OR p.channel_id=$5)
 GROUP BY 1 ORDER BY 1 LIMIT 5001`
	rows, e := tx.Query(ctx, query, q.from, q.until, q.merchant, q.kind, q.channel)
	if e != nil {
		s.fail(w, e)
		return
	}
	groups := map[string]*originalStatisticsGroup{}
	keys := []string{}
	summary := &originalStatisticsGroup{}
	if daily {
		for day := q.from; day.Before(q.until); day = day.AddDate(0, 0, 1) {
			key := day.Format("2006-01-02")
			keys = append(keys, key)
			groups[key] = &originalStatisticsGroup{}
		}
	}
	count := 0
	for rows.Next() {
		count++
		if count > 5000 {
			rows.Close()
			response(w, 422, -1, "statistics exceed 5000 groups; narrow filters", nil)
			return
		}
		var key, paid, refund, profit, orders, successes, missing string
		if e = rows.Scan(&key, &paid, &refund, &profit, &orders, &successes, &missing); e != nil {
			break
		}
		g := groups[key]
		if g == nil {
			g = &originalStatisticsGroup{}
			groups[key] = g
			keys = append(keys, key)
		}
		if e = g.add(paid, refund, profit, orders, successes, missing); e != nil {
			break
		}
		if e = summary.add(paid, refund, profit, orders, successes, missing); e != nil {
			break
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		s.fail(w, e)
		return
	}
	if e = tx.Commit(ctx); e != nil {
		s.fail(w, e)
		return
	}
	if !daily {
		sort.Slice(keys, func(i, j int) bool {
			if keys[i] == "unassigned" {
				return false
			}
			if keys[j] == "unassigned" {
				return true
			}
			a, _ := strconv.ParseInt(keys[i], 10, 64)
			b, _ := strconv.ParseInt(keys[j], 10, 64)
			return a < b
		})
	}
	items := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		items = append(items, groups[key].row(key, daily))
	}
	first := reportColumn{"channelName", "支付通道"}
	if daily {
		first = reportColumn{"day", "日期"}
	}
	columns := []reportColumn{first, {"realmoney", "支付金额"}, {"refundmoney", "退款金额"}, {"profitmoney", "手续费利润"}, {"orderCount", "总订单数"}, {"successCount", "成功订单数"}, {"successRate", "支付成功率"}}
	total := summary.row("total", false)
	if q.csv {
		b := &csvBoundedBuffer{}
		_, _ = b.Write([]byte("\xef\xbb\xbf"))
		writer := csv.NewWriter(b)
		header := []string{}
		for _, c := range columns {
			header = append(header, c.Label)
		}
		e = writer.Write(header)
		csvRows := items
		if !daily {
			csvRows = append([]map[string]any{total}, items...)
		}
		for _, row := range csvRows {
			if e != nil {
				break
			}
			record := []string{}
			for _, c := range columns {
				v := row[c.Key]
				if v == nil {
					v = "未知"
				}
				record = append(record, csvExportCell(v))
			}
			e = writer.Write(record)
		}
		writer.Flush()
		if e == nil {
			e = writer.Error()
		}
		if e != nil {
			response(w, 422, -1, "CSV too large", nil)
			return
		}
		s.finishCSVExport(ctx, w, session, "reports/"+name+"-original", b, len(csvRows))
		return
	}
	response(w, 200, 0, "", map[string]any{"columns": columns, "items": items, "summary": total, "total": len(items), "from": q.from.Format("2006-01-02"), "to": q.until.AddDate(0, 0, -1).Format("2006-01-02"), "timezone": "Asia/Shanghai", "source": "created_order_cohort_with_successful_refunds_and_original_cost_estimates", "missing_costs": summary.missing.String()})
}
