package server

import (
	"context"
	"encoding/csv"
	"goepay/internal/identity"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

var provinceStatisticsSlots = make(chan struct{}, 4)

func (s *Server) originalProvinceStatistics(w http.ResponseWriter, r *http.Request, role string, session identity.Session) {
	invalid := func() { response(w, 400, -1, "invalid province statistics filter", nil) }
	values, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		invalid()
		return
	}
	allowed := map[string]bool{"view": true, "from": true, "to": true, "uid": true, "channel": true, "format": true}
	for key, v := range values {
		if !allowed[key] || len(v) != 1 {
			invalid()
			return
		}
	}
	if role != "admin" && (values.Has("uid") || values.Has("channel")) {
		invalid()
		return
	}
	base := url.Values{}
	for _, key := range []string{"view", "from", "to", "uid", "format"} {
		if v, ok := values[key]; ok {
			base[key] = v
		}
	}
	q, e := parseOriginalStatisticsQuery(base, false)
	if e != nil {
		invalid()
		return
	}
	if raw := values.Get("channel"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n <= 0 || strconv.FormatInt(n, 10) != raw {
			invalid()
			return
		}
		q.channel = n
	}
	if role != "admin" {
		q.merchant = session.User.ID
		if q.merchant <= 0 {
			response(w, 403, -1, "merchant identity required", nil)
			return
		}
	}
	select {
	case provinceStatisticsSlots <- struct{}{}:
		defer func() { <-provinceStatisticsSlots }()
	default:
		w.Header().Set("Retry-After", "5")
		response(w, 429, -1, "province statistics busy", nil)
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
	rows, e := tx.Query(ctx, `SELECT COALESCE(NULLIF(btrim(d.province),''),'未知'),sum(o.amount::numeric)::text,count(*)::text
 FROM orders o LEFT JOIN order_reporting d ON d.order_id=o.id LEFT JOIN provider_attempts p ON p.order_id=o.id
 WHERE o.status='paid' AND o.created_at>=$1 AND o.created_at<$2 AND ($3::bigint=0 OR o.merchant_id=$3) AND ($4::bigint=0 OR p.channel_id=$4)
 GROUP BY 1 ORDER BY sum(o.amount::numeric) DESC,1 ASC LIMIT 5001`, q.from, q.until, q.merchant, q.channel)
	if e != nil {
		s.fail(w, e)
		return
	}
	items := []map[string]any{}
	total, unknown := matrixAmount{}, matrixAmount{}
	count, unknownCount := big.Int{}, big.Int{}
	for rows.Next() {
		if len(items) >= 5000 {
			rows.Close()
			response(w, 422, -1, "province statistics exceed 5000 groups; narrow filters", nil)
			return
		}
		var name, amount, n string
		if e = rows.Scan(&name, &amount, &n); e != nil {
			break
		}
		var number big.Int
		if _, ok := number.SetString(n, 10); !ok || number.Sign() < 0 {
			rows.Close()
			response(w, 500, -1, "invalid province count", nil)
			return
		}
		row := matrixAmount{}
		if e = row.add(&amount, false); e != nil {
			break
		}
		if e = total.add(&amount, false); e != nil {
			break
		}
		count.Add(&count, &number)
		if name == "未知" {
			if e = unknown.add(&amount, false); e != nil {
				break
			}
			unknownCount.Add(&unknownCount, &number)
		}
		items = append(items, map[string]any{"name": name, "money": row.value(), "count": n})
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
	if q.csv {
		b := &csvBoundedBuffer{}
		_, _ = b.Write([]byte("\xef\xbb\xbf"))
		writer := csv.NewWriter(b)
		e = writer.Write([]string{"地区", "交易金额（元）", "交易笔数"})
		for _, row := range items {
			if e != nil {
				break
			}
			e = writer.Write([]string{csvExportCell(row["name"]), csvExportCell(row["money"]), csvExportCell(row["count"])})
		}
		writer.Flush()
		if e == nil {
			e = writer.Error()
		}
		if e != nil {
			response(w, 422, -1, "CSV too large", nil)
			return
		}
		s.finishCSVExport(ctx, w, session, "reports/provinces-original", b, len(items))
		return
	}
	response(w, 200, 0, "", map[string]any{"items": items, "total_money": total.value(), "total_count": count.String(), "unknown_money": unknown.value(), "unknown_count": unknownCount.String(), "from": q.from.Format("2006-01-02"), "to": q.until.AddDate(0, 0, -1).Format("2006-01-02"), "source": "recorded_province_labels_of_paid_created_order_cohort", "timezone": "Asia/Shanghai"})
}
