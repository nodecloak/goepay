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
	"strings"
	"time"
)

const matrixMaxFacts = 50000

var merchantMatrixSlots = make(chan struct{}, 4)

type merchantMatrixQuery struct {
	Method, Metric, Sort, Direction string
	From, Until                     time.Time
	Page, Limit                     int
	CSV                             bool
}

func parseMerchantMatrixQuery(v url.Values) (merchantMatrixQuery, error) {
	q := merchantMatrixQuery{Method: "type", Metric: "0", Sort: "total", Direction: "desc", Page: 1, Limit: 20}
	allowed := map[string]bool{"view": true, "method": true, "type": true, "from": true, "to": true, "page": true, "limit": true, "format": true, "sort": true, "direction": true}
	for k, x := range v {
		if !allowed[k] || len(x) != 1 {
			return q, fmt.Errorf("invalid matrix query")
		}
	}
	if v.Get("view") != "matrix" {
		return q, fmt.Errorf("matrix view required")
	}
	if v.Has("method") {
		q.Method = v.Get("method")
	}
	if q.Method != "type" && q.Method != "channel" {
		return q, fmt.Errorf("invalid matrix method")
	}
	if v.Has("type") {
		q.Metric = v.Get("type")
	}
	if q.Metric != "0" && q.Metric != "1" && q.Metric != "2" && q.Metric != "3" {
		return q, fmt.Errorf("invalid matrix metric")
	}
	if v.Has("sort") {
		q.Sort = v.Get("sort")
	}
	if v.Has("direction") {
		q.Direction = v.Get("direction")
	}
	if len(q.Sort) < 1 || len(q.Sort) > 64 || q.Direction != "asc" && q.Direction != "desc" {
		return q, fmt.Errorf("invalid sort")
	}
	for k, target := range map[string]*int{"page": &q.Page, "limit": &q.Limit} {
		if raw := v.Get(k); raw != "" {
			n, e := strconv.Atoi(raw)
			if e != nil || n < 1 || strconv.Itoa(n) != raw {
				return q, fmt.Errorf("invalid pagination")
			}
			*target = n
		}
	}
	if q.Page > 100000 || q.Limit > 100 {
		return q, fmt.Errorf("invalid pagination")
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	today := time.Now().In(zone)
	start := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, zone)
	end := start
	for k, target := range map[string]*time.Time{"from": &start, "to": &end} {
		if raw := v.Get(k); raw != "" {
			d, e := time.ParseInLocation("2006-01-02", raw, zone)
			if e != nil || d.Year() < 1 || d.Format("2006-01-02") != raw {
				return q, fmt.Errorf("invalid date")
			}
			*target = d
		}
	}
	if start.After(end) || end.Sub(start) > 366*24*time.Hour {
		return q, fmt.Errorf("invalid date range (maximum 367 days)")
	}
	q.From = start
	q.Until = end.AddDate(0, 0, 1)
	if v.Has("format") {
		if v.Get("format") != "csv" {
			return q, fmt.Errorf("format must be csv")
		}
		q.CSV = true
	}
	return q, nil
}

type matrixAmount struct {
	Minor   big.Int
	Missing bool
}

func (a *matrixAmount) add(raw *string, missing bool) error {
	a.Missing = a.Missing || missing
	if raw == nil {
		a.Missing = true
		return nil
	}
	var n big.Int
	if _, ok := n.SetString(*raw, 10); !ok {
		return fmt.Errorf("invalid exact report amount")
	}
	a.Minor.Add(&a.Minor, &n)
	return nil
}
func (a *matrixAmount) value() any {
	if a.Missing {
		return nil
	}
	var n, whole, part big.Int
	n.Abs(&a.Minor)
	whole.QuoRem(&n, big.NewInt(100), &part)
	sign := ""
	if a.Minor.Sign() < 0 {
		sign = "-"
	}
	return fmt.Sprintf("%s%s.%02d", sign, whole.String(), part.Int64())
}

type merchantMatrixAccumulator struct {
	Rows         map[string]map[string]*matrixAmount
	Totals       map[string]*matrixAmount
	Dimensions   map[string]bool
	MissingCosts int64
}

func newMerchantMatrixAccumulator() *merchantMatrixAccumulator {
	return &merchantMatrixAccumulator{Rows: map[string]map[string]*matrixAmount{}, Totals: map[string]*matrixAmount{"total": {}}, Dimensions: map[string]bool{}}
}
func (a *merchantMatrixAccumulator) add(mid, cell string, raw *string, missing int64) error {
	n, e := strconv.ParseInt(mid, 10, 64)
	if e != nil || n <= 0 {
		return fmt.Errorf("invalid report merchant")
	}
	if err := a.dimension(cell); err != nil {
		return err
	}
	row := a.Rows[mid]
	if row == nil {
		row = map[string]*matrixAmount{}
		a.Rows[mid] = row
	}
	for _, target := range []map[string]*matrixAmount{row, a.Totals} {
		for _, key := range []string{"total", cell} {
			if target[key] == nil {
				target[key] = &matrixAmount{}
			}
			if e = target[key].add(raw, missing > 0); e != nil {
				return e
			}
		}
	}
	a.MissingCosts += missing
	return nil
}
func (a *merchantMatrixAccumulator) row(mid string) map[string]any {
	out := map[string]any{"uid": mid}
	values := a.Rows[mid]
	if mid == "总计" {
		values = a.Totals
	}
	for key := range a.Dimensions {
		out[key] = "0.00"
	}
	out["total"] = "0.00"
	for key, amount := range values {
		out[key] = amount.value()
	}
	return out
}
func matrixColumnLabel(key string) string {
	switch key {
	case "type_alipay":
		return "支付宝"
	case "type_wxpay":
		return "微信支付"
	case "type_bank":
		return "银行卡"
	case "type_sandbox":
		return "本地模拟"
	case "type_unknown":
		return "方式未记录"
	case "channel_unassigned":
		return "通道未分配"
	}
	if strings.HasPrefix(key, "channel_") {
		return "通道 #" + strings.TrimPrefix(key, "channel_")
	}
	return key
}

func (s *Server) merchantMatrixReport(w http.ResponseWriter, r *http.Request, role string, session identity.Session, name string) bool {
	if (name != "merchants" && name != "transfer-merchants") || r.URL.Query().Get("view") != "matrix" {
		return false
	}
	if role != "admin" {
		response(w, 403, -1, "administrator report", nil)
		return true
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		response(w, 400, -1, "invalid matrix query", nil)
		return true
	}
	q, err := parseMerchantMatrixQuery(values)
	if err != nil || name == "transfer-merchants" && q.Metric != "0" {
		response(w, 400, -1, "invalid matrix query", nil)
		return true
	}
	select {
	case merchantMatrixSlots <- struct{}{}:
		defer func() { <-merchantMatrixSlots }()
	default:
		w.Header().Set("Retry-After", "5")
		response(w, 429, -1, "reports busy", nil)
		return true
	}
	if q.CSV {
		select {
		case csvExportSlots <- struct{}{}:
			defer func() { <-csvExportSlots }()
		default:
			w.Header().Set("Retry-After", "5")
			response(w, 429, -1, "CSV exports busy", nil)
			return true
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	acc := newMerchantMatrixAccumulator()
	source := "local_verified_payment_facts"
	if name == "transfer-merchants" {
		source = "local_resolved_transfer_facts"
		facts, e := s.Store.TransferReportingFacts(ctx, q.From, q.Until, matrixMaxFacts+1)
		if e != nil {
			s.fail(w, e)
			return true
		}
		if len(facts) > matrixMaxFacts {
			response(w, 422, -1, "matrix exceeds 50000 source transfers; narrow dates", nil)
			return true
		}
		for _, fact := range facts {
			cell := "type_" + fact.Type
			if q.Method == "channel" {
				cell = "channel_unassigned"
				if fact.ChannelID > 0 {
					cell = "channel_" + strconv.FormatInt(fact.ChannelID, 10)
				}
			}
			raw := strconv.FormatInt(fact.Amount, 10)
			if e = acc.add(strconv.FormatInt(fact.MerchantID, 10), cell, &raw, 0); e != nil {
				response(w, 422, -1, e.Error(), nil)
				return true
			}
		}
	} else {
		expr := "o.amount::numeric"
		join := " LEFT JOIN provider_attempts p ON p.order_id=o.id"
		switch q.Metric {
		case "2":
			expr = "o.merchant_amount::numeric"
		case "3":
			source = "local_order_cost_snapshot_estimate"
			join += " LEFT JOIN referral_profit_costs c ON c.order_id=o.id"
			expr = "CASE WHEN c.order_id IS NOT NULL THEN o.platform_fee::numeric-c.cost_whole-CASE WHEN c.cost_remainder>50000000 THEN 1 ELSE 0 END END"
		}
		cell := "'type_'||o.type"
		if q.Method == "channel" {
			cell = "'channel_'||COALESCE(p.channel_id::text,'unassigned')"
		}
		query := "SELECT o.merchant_id::text," + cell + ",sum(" + expr + ")::text,(count(*)-count(" + expr + "))::bigint FROM orders o" + join + " WHERE o.status='paid' AND o.paid_at>=$1 AND o.paid_at<$2 GROUP BY 1,2 LIMIT 50001"
		rows, e := s.Pool.Query(ctx, query, q.From, q.Until)
		if e != nil {
			s.fail(w, e)
			return true
		}
		count := 0
		for rows.Next() {
			count++
			if count > matrixMaxFacts {
				rows.Close()
				response(w, 422, -1, "matrix exceeds 50000 merchant/dimension groups; narrow dates", nil)
				return true
			}
			var mid, cell string
			var raw *string
			var missing int64
			if e = rows.Scan(&mid, &cell, &raw, &missing); e == nil {
				e = acc.add(mid, cell, raw, missing)
			}
			if e != nil {
				rows.Close()
				s.fail(w, e)
				return true
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			s.fail(w, e)
			return true
		}
	}
	if err := s.merchantMatrixConfiguredDimensions(acc, q.Method, name == "transfer-merchants"); err != nil {
		response(w, 422, -1, err.Error(), nil)
		return true
	}
	dimensions := make([]string, 0, len(acc.Dimensions))
	for key := range acc.Dimensions {
		dimensions = append(dimensions, key)
	}
	sort.Strings(dimensions)
	columns := []reportColumn{{"uid", "商户ID"}, {"total", "总计"}}
	for _, key := range dimensions {
		columns = append(columns, reportColumn{key, matrixColumnLabel(key)})
	}
	ids := make([]string, 0, len(acc.Rows))
	for id := range acc.Rows {
		ids = append(ids, id)
	}
	if q.Sort != "uid" && q.Sort != "total" && !acc.Dimensions[q.Sort] {
		response(w, 400, -1, "invalid matrix sort column", nil)
		return true
	}
	sort.Slice(ids, func(i, j int) bool {
		x, _ := strconv.ParseInt(ids[i], 10, 64)
		y, _ := strconv.ParseInt(ids[j], 10, 64)
		comparison := 0
		if q.Sort == "uid" {
			if x < y {
				comparison = -1
			} else if x > y {
				comparison = 1
			}
		} else {
			a, b := acc.Rows[ids[i]][q.Sort], acc.Rows[ids[j]][q.Sort]
			if a == nil {
				a = &matrixAmount{}
			}
			if b == nil {
				b = &matrixAmount{}
			}
			if a.Missing != b.Missing {
				return !a.Missing
			}
			if !a.Missing {
				comparison = a.Minor.Cmp(&b.Minor)
			}
		}
		if comparison != 0 {
			if q.Direction == "asc" {
				return comparison < 0
			}
			return comparison > 0
		}
		return x < y
	})
	summary := acc.row("总计")
	if q.CSV {
		if len(ids)+1 > csvExportMaxRows || (len(ids)+1)*len(columns) > 250000 {
			response(w, 422, -1, "CSV matrix too large; narrow dates", nil)
			return true
		}
		b := &csvBoundedBuffer{}
		_, _ = b.Write([]byte("\xef\xbb\xbf"))
		writer := csv.NewWriter(b)
		head := make([]string, len(columns))
		for i, c := range columns {
			head[i] = c.Label
		}
		e := writer.Write(head)
		write := func(row map[string]any) {
			if e != nil {
				return
			}
			record := make([]string, len(columns))
			for i, c := range columns {
				record[i] = csvExportCell(row[c.Key])
			}
			e = writer.Write(record)
		}
		write(summary)
		for _, id := range ids {
			write(acc.row(id))
		}
		writer.Flush()
		if e == nil {
			e = writer.Error()
		}
		if e != nil {
			response(w, 422, -1, "CSV matrix too large; narrow dates", nil)
			return true
		}
		s.finishCSVExport(ctx, w, session, "reports/"+name+"-matrix", b, len(ids)+1)
		return true
	}
	items := []map[string]any{}
	start := (q.Page - 1) * q.Limit
	end := start + q.Limit
	if end > len(ids) {
		end = len(ids)
	}
	for i := start; i < end; i++ {
		items = append(items, acc.row(ids[i]))
	}
	response(w, 200, 0, "", map[string]any{"columns": columns, "items": items, "summary": summary, "total": len(ids), "page": q.Page, "limit": q.Limit, "method": q.Method, "metric": q.Metric, "missing_costs": strconv.FormatInt(acc.MissingCosts, 10), "source": source, "timezone": "Asia/Shanghai", "from": q.From.Format("2006-01-02"), "to": q.Until.AddDate(0, 0, -1).Format("2006-01-02")})
	return true
}
