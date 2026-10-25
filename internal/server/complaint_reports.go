package server

import (
	"encoding/json"
	"goepay/internal/identity"
	"net/http"
	"time"
)

// This report counts local registered cases. It never polls a provider. A
// missing amount stays unknown rather than being presented as zero money.
const complaintOverviewCTE = `WITH cases AS (
 SELECT owner_id, created_at, body->>'pay_type' AS pay_type,
 CASE WHEN body->>'amount' ~ '^(0|[1-9][0-9]{0,11})$' AND (body->>'amount')::numeric <= 100000000000
 THEN (body->>'amount')::numeric ELSE NULL END AS amount
 FROM backoffice_records WHERE resource='complaints' AND created_at >= $1::timestamptz-interval '6 days' AND created_at < $1::timestamptz+interval '1 day'
), stats AS (
 SELECT owner_id,
 count(*) FILTER(WHERE created_at >= $1) AS today_count,
 CASE WHEN count(*) FILTER(WHERE created_at >= $1 AND amount IS NULL)>0 THEN NULL ELSE COALESCE(sum(amount) FILTER(WHERE created_at >= $1),0) END AS today_amount,
 count(*) FILTER(WHERE created_at >= $1 AND pay_type='alipay') AS today_alipay,
 count(*) FILTER(WHERE created_at >= $1 AND pay_type='wxpay') AS today_wxpay,
 count(*) FILTER(WHERE created_at >= $1-interval '1 day' AND created_at < $1) AS yesterday_count,
 CASE WHEN count(*) FILTER(WHERE created_at >= $1-interval '1 day' AND created_at < $1 AND amount IS NULL)>0 THEN NULL ELSE COALESCE(sum(amount) FILTER(WHERE created_at >= $1-interval '1 day' AND created_at < $1),0) END AS yesterday_amount,
 count(*) FILTER(WHERE created_at >= $1-interval '1 day' AND created_at < $1 AND pay_type='alipay') AS yesterday_alipay,
 count(*) FILTER(WHERE created_at >= $1-interval '1 day' AND created_at < $1 AND pay_type='wxpay') AS yesterday_wxpay,
 count(*) AS recent_count FROM cases GROUP BY owner_id
), payments AS (
 SELECT merchant_id,count(*) AS paid_count FROM orders
 WHERE created_at >= $1::timestamptz-interval '6 days' AND created_at < $1::timestamptz+interval '1 day' AND status='paid' GROUP BY merchant_id
), report AS (
 SELECT owner_id::text AS dimension, today_count::text, (today_amount/100)::numeric(38,2)::text AS today_money,
 today_alipay::text,today_wxpay::text,yesterday_count::text,(yesterday_amount/100)::numeric(38,2)::text AS yesterday_money,
 yesterday_alipay::text,yesterday_wxpay::text,
 CASE WHEN p.paid_count>0 THEN (s.recent_count::numeric*100/p.paid_count)::numeric(38,2)::text ELSE NULL END AS complaint_rate
 FROM stats s LEFT JOIN payments p ON p.merchant_id=s.owner_id
) `

func (s *Server) complaintOverview(w http.ResponseWriter, r *http.Request, session identity.Session, day time.Time, page, limit int, export bool) {
	// The original overview is anchored to today/yesterday. Custom date reports
	// remain available through the existing default view, never relabelled today.
	if r.URL.Query().Has("from") || r.URL.Query().Has("to") {
		response(w, 400, -1, "overview uses today, yesterday and the latest 7 business days", nil)
		return
	}
	if export {
		s.exportCSV(w, r, session, "reports/complaints-overview", complaintOverviewCTE+"SELECT * FROM report ORDER BY today_count::bigint DESC,dimension::bigint", []any{day})
		return
	}
	query := complaintOverviewCTE + `SELECT jsonb_build_object(
 'items',COALESCE((SELECT jsonb_agg(to_jsonb(paged)) FROM (SELECT * FROM report ORDER BY today_count::bigint DESC,dimension::bigint LIMIT $2 OFFSET $3) paged),'[]'::jsonb),
 'total',(SELECT count(*) FROM report),
 'summary',(SELECT jsonb_build_object(
  'today_count',COALESCE(sum(today_count),0)::text,
  'today_money',CASE WHEN count(*) FILTER(WHERE today_amount IS NULL)>0 THEN NULL ELSE (COALESCE(sum(today_amount),0)/100)::numeric(38,2)::text END,
  'today_alipay',COALESCE(sum(today_alipay),0)::text,'today_wxpay',COALESCE(sum(today_wxpay),0)::text,
  'yesterday_count',COALESCE(sum(yesterday_count),0)::text,
  'yesterday_money',CASE WHEN count(*) FILTER(WHERE yesterday_amount IS NULL)>0 THEN NULL ELSE (COALESCE(sum(yesterday_amount),0)/100)::numeric(38,2)::text END,
  'yesterday_alipay',COALESCE(sum(yesterday_alipay),0)::text,'yesterday_wxpay',COALESCE(sum(yesterday_wxpay),0)::text) FROM stats))`
	var raw []byte
	if e := s.Pool.QueryRow(r.Context(), query, day, limit, (page-1)*limit).Scan(&raw); e != nil {
		s.fail(w, e)
		return
	}
	var data map[string]any
	if e := json.Unmarshal(raw, &data); e != nil {
		s.fail(w, e)
		return
	}
	data["columns"] = []reportColumn{{"dimension", "商户ID"}, {"today_count", "今日投诉总条数"}, {"today_money", "今日投诉金额（元）"}, {"today_alipay", "今日支付宝投诉"}, {"today_wxpay", "今日微信投诉"}, {"yesterday_count", "昨日投诉总条数"}, {"yesterday_money", "昨日投诉金额（元）"}, {"yesterday_alipay", "昨日支付宝投诉"}, {"yesterday_wxpay", "昨日微信投诉"}, {"complaint_rate", "近7日投诉/支付订单（%）"}}
	data["source"] = "local_registered_complaints"
	data["business_date"] = day.Format("2006-01-02")
	data["timezone"] = "Asia/Shanghai"
	data["page"] = page
	data["limit"] = limit
	response(w, 200, 0, "", data)
}
