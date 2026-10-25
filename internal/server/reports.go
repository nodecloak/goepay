package server

import (
	"fmt"
	"goepay/internal/identity"
	"net/http"
	"strconv"
	"time"
)

type reportColumn struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Reports use fixed SQL expressions and exact numeric formatting. Missing source
// dimensions remain explicitly unknown; no IP geolocation is inferred.
func (s *Server) reports(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || parts[1] != "reports" {
		return false
	}
	if r.Method != "GET" {
		response(w, 405, -1, "GET required", nil)
		return true
	}
	if len(parts) != 3 {
		response(w, 404, -1, "report not found", nil)
		return true
	}
	name := parts[2]
	if name == "provinces" && r.URL.Query().Get("view") == "original" {
		s.originalProvinceStatistics(w, r, role, session)
		return true
	}
	if (name == "daily" || name == "channels") && r.URL.Query().Get("view") == "original" {
		s.originalOrderStatistics(w, r, role, session, name)
		return true
	}
	if s.merchantMatrixReport(w, r, role, session, name) {
		return true
	}
	if name == "buyers" && r.URL.Query().Get("view") == "original" {
		s.originalBuyerStatistics(w, r, role, session)
		return true
	}
	view := r.URL.Query()["view"]
	if len(view) > 1 || len(view) == 1 && (name != "complaints" || view[0] != "overview") {
		response(w, 400, -1, "invalid report view", nil)
		return true
	}
	export, exportErr := csvRequested(r)
	if exportErr != nil {
		response(w, 400, -1, exportErr.Error(), nil)
		return true
	}
	if role != "admin" && name != "daily" && name != "provinces" {
		response(w, 403, -1, "administrator report", nil)
		return true
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	today := time.Now().In(zone)
	end := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, zone)
	start := end.AddDate(0, 0, -29)
	var err error
	if raw := r.URL.Query().Get("from"); raw != "" {
		start, err = time.ParseInLocation("2006-01-02", raw, zone)
	}
	if err == nil {
		if raw := r.URL.Query().Get("to"); raw != "" {
			end, err = time.ParseInLocation("2006-01-02", raw, zone)
		}
	}
	if err != nil || start.After(end) || end.Sub(start) > 366*24*time.Hour {
		response(w, 400, -1, "invalid date range (maximum 367 days)", nil)
		return true
	}
	page, limit := 1, 20
	for key, target := range map[string]*int{"page": &page, "limit": &limit} {
		if raw := r.URL.Query().Get(key); raw != "" {
			*target, err = strconv.Atoi(raw)
			if err != nil || *target < 1 {
				response(w, 400, -1, "invalid pagination", nil)
				return true
			}
		}
	}
	if limit > 100 || page > 1000000 {
		response(w, 400, -1, "invalid pagination", nil)
		return true
	}
	if name == "complaints" && len(view) == 1 {
		s.complaintOverview(w, r, session, end, page, limit, export)
		return true
	}
	mid := session.User.ID
	if role == "admin" {
		mid = 0
	}
	dimension, label, table, join, status, amount, owner, created := "", "", "orders o", "", "o.status", "o.amount", "o.merchant_id", "o.created_at"
	netAmount, feeAmount, netLabel := "o.merchant_amount", "o.platform_fee", "商户净额（元）"
	switch name {
	case "daily":
		dimension = "to_char(o.created_at AT TIME ZONE 'Asia/Shanghai','YYYY-MM-DD')"
		label = "日期"
	case "merchants":
		dimension = "o.merchant_id::text"
		label = "商户ID"
	case "channels":
		dimension = "COALESCE(p.channel_id::text,'未分配')"
		join = " LEFT JOIN provider_attempts p ON p.order_id=o.id"
		label = "通道ID"
	case "provinces", "buyers":
		column := "province"
		label = "省份"
		if name == "buyers" {
			column = "buyer"
			label = "买家"
		}
		dimension = "COALESCE(NULLIF(d." + column + ",''),'未知')"
		join = " LEFT JOIN order_reporting d ON d.order_id=o.id"
	case "transfer-accounts", "transfer-channels", "transfer-merchants":
		table = "transfers o"
		join = " LEFT JOIN bank_transfer_lifecycle report_bank ON report_bank.transfer_id=o.id"
		netAmount, feeAmount, netLabel = "o.cost_amount", "o.fee_amount", "成功出款扣款（元）"
		join += " LEFT JOIN settlement_payouts report_settlement ON report_settlement.transfer_id=o.id"
		join += " LEFT JOIN settlement_bank_payouts report_settlement_bank ON report_settlement_bank.transfer_id=o.id"
		netAmount = "CASE WHEN report_settlement_bank.request_id IS NOT NULL THEN report_settlement.net_amount WHEN report_settlement.request_id IS NOT NULL THEN report_settlement.gross_amount ELSE o.cost_amount END"
		feeAmount = "CASE WHEN report_settlement_bank.request_id IS NOT NULL THEN 0 WHEN report_settlement.request_id IS NOT NULL THEN report_settlement.fee_amount ELSE o.fee_amount END"
		dimension = "'未知'::text"
		label = "账户（未记录）"
		status = "CASE WHEN report_bank.state IN ('reversed','reversal_pending') THEN report_bank.state WHEN o.status='succeeded' THEN 'paid' ELSE o.status END"
		if name == "transfer-channels" {
			label = "通道ID"
			join += " LEFT JOIN provider_transfer_attempts p ON p.transfer_id=o.id"
			dimension = "COALESCE(p.channel_id::text,'未分配')"
		}
		if name == "transfer-merchants" {
			dimension = "o.merchant_id::text"
			label = "商户ID"
		}
	case "complaints":
		table = "backoffice_records o"
		dimension = "COALESCE(NULLIF(o.body->>'status',''),'未知')"
		label = "投诉状态"
		owner = "o.owner_id"
		status = "'created'::text"
		amount = "0::bigint"
		netAmount, feeAmount = "0::bigint", "0::bigint"
	default:
		response(w, 404, -1, "report not found", nil)
		return true
	}
	extra := ""
	if name == "complaints" {
		extra = " AND o.resource='complaints'"
	}
	base := fmt.Sprintf(" FROM %s%s WHERE ($1::bigint=0 OR %s=$1) AND %s >= $2 AND %s < $3%s", table, join, owner, created, created, extra)
	moneySQL := func(expression string) string {
		return "(COALESCE(sum(" + expression + ") FILTER(WHERE " + status + "='paid'),0)::numeric/100)::numeric(38,2)::text"
	}
	aggregate := fmt.Sprintf("SELECT %s AS dimension,count(*)::text AS orders,count(*) FILTER(WHERE %s='paid')::text AS paid_orders,%s AS paid_money,%s AS net_money,%s AS fee_money%s GROUP BY 1", dimension, status, moneySQL(amount), moneySQL(netAmount), moneySQL(feeAmount), base)
	args := []any{mid, start, end.AddDate(0, 0, 1)}
	if export {
		s.exportCSV(w, r, session, "reports/"+name, aggregate+" ORDER BY 1 DESC", args)
		return true
	}
	var total int64
	if err = s.Pool.QueryRow(r.Context(), "SELECT count(*) FROM ("+aggregate+") report", args...).Scan(&total); err != nil {
		s.fail(w, err)
		return true
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := s.Pool.Query(r.Context(), aggregate+" ORDER BY 1 DESC LIMIT $4 OFFSET $5", args...)
	if err != nil {
		s.fail(w, err)
		return true
	}
	defer rows.Close()
	items := make([]map[string]string, 0)
	for rows.Next() {
		var d, n, p, m, net, fee string
		if err = rows.Scan(&d, &n, &p, &m, &net, &fee); err != nil {
			s.fail(w, err)
			return true
		}
		items = append(items, map[string]string{"dimension": d, "orders": n, "paid_orders": p, "paid_money": m, "net_money": net, "fee_money": fee})
	}
	if err = rows.Err(); err != nil {
		s.fail(w, err)
		return true
	}
	response(w, 200, 0, "", map[string]any{"columns": []reportColumn{{"dimension", label}, {"orders", "总笔数"}, {"paid_orders", "成功笔数"}, {"paid_money", "成功金额（元）"}, {"net_money", netLabel}, {"fee_money", "平台手续费（元）"}}, "items": items, "total": total, "page": page, "limit": limit, "timezone": "Asia/Shanghai"})
	return true
}
