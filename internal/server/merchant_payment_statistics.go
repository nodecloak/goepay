package server

import (
	"context"
	"goepay/internal/identity"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

var paymentStatisticsMethods = []struct{ kind, name string }{{"alipay", "支付宝"}, {"wxpay", "微信支付"}, {"qqpay", "QQ钱包"}, {"bank", "银联支付"}, {"jdpay", "京东支付"}, {"paypal", "PayPal"}, {"douyinpay", "抖音支付"}, {"ecny", "数字人民币"}, {"sandbox", "本地模拟"}}

type statisticsMethod = struct{ kind, name string }

// statisticsShownMethods keeps the original's view, which lists the enabled
// pre_type rows: every open pay type (支付方式 records, or the original install
// defaults: 支付宝 and 微信支付) and GOEPAY's local sandbox, plus any type that
// still has orders in the window, so no amount disappears from the report.
func statisticsShownMethods(ctx context.Context, tx pgx.Tx, groups map[string]map[string]*originalStatisticsGroup) ([]statisticsMethod, error) {
	out := []statisticsMethod{}
	for _, m := range paymentStatisticsMethods {
		show := m.kind == "sandbox"
		if !show {
			open, e := payTypeCatalogOpen(ctx, tx, m.kind)
			if e != nil {
				return nil, e
			}
			show = open
		}
		for _, day := range groups {
			if g := day[m.kind]; g != nil && g.orders.Sign() > 0 {
				show = true
			}
		}
		if show {
			out = append(out, m)
		}
	}
	return out, nil
}

func paymentStatisticsRow(g *originalStatisticsGroup, kind, name string) map[string]any {
	rate := g.row("", false)["successRate"]
	if rate == nil {
		rate = "0.00"
	}
	return map[string]any{"type": kind, "showname": name, "money": g.paid.value(), "count": g.orders.String(), "success_count": g.successes.String(), "rate": rate}
}

func paymentStatisticsDay(at time.Time, methods map[string]*originalStatisticsGroup, total *originalStatisticsGroup, shown []statisticsMethod) map[string]any {
	items := make([]map[string]any, 0, len(shown))
	for _, method := range shown {
		items = append(items, paymentStatisticsRow(methods[method.kind], method.kind, method.name))
	}
	return map[string]any{"date": at.Format("2006-01-02"), "items": items, "summary": paymentStatisticsRow(total, "total", "总计")}
}

// This is an order creation cohort. Each order contributes once, regardless of
// provider attempts or later refunds. Decimal money never passes through float.
func (s *Server) merchantPaymentStatistics(w http.ResponseWriter, r *http.Request, role string, session identity.Session) {
	if role != "admin" || session.User.Role != "admin" || session.User.ID <= 0 {
		response(w, 403, -1, "administrator merchant statistics required", nil)
		return
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil || len(q) != 1 || len(q["uid"]) != 1 {
		response(w, 400, -1, "invalid merchant statistics filter", nil)
		return
	}
	raw := q.Get("uid")
	mid, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || mid <= 0 || strconv.FormatInt(mid, 10) != raw {
		response(w, 400, -1, "invalid merchant statistics filter", nil)
		return
	}
	// Share capacity with the original group dialog rather than allowing each
	// aggregate endpoint to independently saturate the database pool.
	select {
	case groupStatisticsSlots <- struct{}{}:
		defer func() { <-groupStatisticsSlots }()
	default:
		w.Header().Set("Retry-After", "5")
		response(w, 429, -1, "payment statistics busy", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	tx, e := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		s.fail(w, e)
		return
	}
	defer tx.Rollback(ctx)
	var exists bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM merchants WHERE id=$1)`, mid).Scan(&exists); e != nil {
		s.fail(w, e)
		return
	}
	if !exists {
		response(w, 404, -1, "merchant not found", nil)
		return
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	now := time.Now().In(zone)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	start := today.AddDate(0, 0, -6)
	groups := map[string]map[string]*originalStatisticsGroup{}
	totals := map[string]*originalStatisticsGroup{}
	for i := 0; i < 7; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		groups[day] = map[string]*originalStatisticsGroup{}
		totals[day] = &originalStatisticsGroup{}
		for _, method := range paymentStatisticsMethods {
			groups[day][method.kind] = &originalStatisticsGroup{}
		}
	}
	rows, e := tx.Query(ctx, `SELECT to_char(o.created_at AT TIME ZONE 'Asia/Shanghai','YYYY-MM-DD'),o.type,
 COALESCE(sum(o.amount::numeric) FILTER(WHERE o.status='paid'),0)::text,count(*)::text,count(*) FILTER(WHERE o.status='paid')::text
 FROM orders o WHERE o.merchant_id=$1 AND o.created_at >= $2 AND o.created_at < $3
 GROUP BY 1,2 ORDER BY 1,2`, mid, start, today.AddDate(0, 0, 1))
	if e != nil {
		s.fail(w, e)
		return
	}
	for rows.Next() {
		var day, kind, money, count, success string
		if e = rows.Scan(&day, &kind, &money, &count, &success); e != nil {
			break
		}
		g := groups[day][kind]
		if g == nil {
			rows.Close()
			response(w, 422, -1, "merchant statistics contains an unsupported payment method", nil)
			return
		}
		if e = g.add(money, "0", "0", count, success, "0", "0"); e != nil {
			break
		}
		if e = totals[day].add(money, "0", "0", count, success, "0", "0"); e != nil {
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
	shown, e := statisticsShownMethods(ctx, tx, groups)
	if e != nil {
		s.fail(w, e)
		return
	}
	if e = tx.Commit(ctx); e != nil {
		s.fail(w, e)
		return
	}
	trend := make([]map[string]any, 0, 7)
	for i := 0; i < 7; i++ {
		at := start.AddDate(0, 0, i)
		key := at.Format("2006-01-02")
		trend = append(trend, paymentStatisticsDay(at, groups[key], totals[key], shown))
	}
	response(w, 200, 0, "", map[string]any{"merchant_id": raw, "timezone": "Asia/Shanghai", "cohort": "merchant_orders_created", "columns": []reportColumn{{"showname", "支付方式"}, {"money", "订单金额"}, {"count", "订单数量"}, {"rate", "成功率"}}, "days": map[string]any{"today": trend[6], "yesterday": trend[5]}, "trend": trend})
}
