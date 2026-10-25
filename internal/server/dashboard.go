package server

import (
	"encoding/json"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"time"
)

// Every business figure is read in one PostgreSQL statement snapshot. Tenant
// identity is supplied by the authenticated handler, never the query string.
const dashboardSQL = `WITH
 merchant_group AS (SELECT m.id,m.group_baseline_unresolved AND NOT EXISTS(SELECT 1 FROM memberships WHERE merchant_id=m.id AND expires_at>now()) AS group_baseline_unresolved,COALESCE((SELECT group_id FROM memberships WHERE merchant_id=m.id AND expires_at>now()),NULLIF(m.base_group_id,0)) group_id FROM merchants m WHERE m.id=$1),
 fee_enabled AS (SELECT COALESCE((SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='payment_fee_enabled'),'false') value),
 rate AS (SELECT CASE WHEN f.value='false' THEN 'disabled' WHEN f.value<>'true' OR g.group_baseline_unresolved THEN 'unavailable' WHEN g.group_id IS NULL THEN 'ready' WHEN b.body->>'enabled'='true' AND CASE WHEN COALESCE(b.body->>'rate_bps','0')~'^(0|[1-9][0-9]{0,4})$' THEN COALESCE(b.body->>'rate_bps','0')::numeric<=10000 ELSE false END THEN 'ready' ELSE 'unavailable' END state,COALESCE(b.body->>'rate_bps','0') rate_bps,COALESCE(b.body->>'name','默认用户组') group_name FROM merchant_group g CROSS JOIN fee_enabled f LEFT JOIN backoffice_records b ON b.id=g.group_id AND b.resource='groups'),
 order_totals AS (SELECT count(*) orders,count(*) FILTER(WHERE status='paid') paid FROM orders WHERE ($1::bigint=0 OR merchant_id=$1)),
 balance AS (SELECT COALESCE(sum(balance) FILTER(WHERE kind='available'),0) available,COALESCE(sum(balance) FILTER(WHERE kind IN('reserved','settlement_reserve','channel_sharing_reserve','order_income_hold','order_income_reserve','order_income_refund_reserve')),0) frozen FROM accounts WHERE owner_id>0 AND ($1::bigint=0 OR owner_id=$1)),
 created AS (SELECT (created_at AT TIME ZONE 'Asia/Shanghai')::date AS business_day,count(*) n FROM orders WHERE ($1::bigint=0 OR merchant_id=$1) AND created_at>=$2::timestamptz-interval '6 days' AND created_at<$2::timestamptz+interval '1 day' GROUP BY 1),
 paid AS (SELECT id,merchant_id,type,paid_at,amount,platform_fee FROM orders WHERE ($1::bigint=0 OR merchant_id=$1) AND status='paid' AND paid_at>=$2::timestamptz-interval '6 days' AND paid_at<$2::timestamptz+interval '1 day'),
 paid_daily AS (SELECT (p.paid_at AT TIME ZONE 'Asia/Shanghai')::date AS business_day,p.type,count(*) n,sum(p.amount)::numeric amount,sum(p.platform_fee)::numeric fee,CASE WHEN count(*)=count(c.order_id) THEN sum(p.platform_fee::numeric-c.cost_whole-CASE WHEN c.cost_remainder>50000000 THEN 1 ELSE 0 END) END estimated_profit,count(*)-count(c.order_id) missing_costs FROM paid p LEFT JOIN referral_profit_costs c ON c.order_id=p.id GROUP BY 1,2),
 calendar AS (SELECT (($2::timestamptz AT TIME ZONE 'Asia/Shanghai')::date+i)::text AS business_day FROM generate_series(-6,0)i),
 settlement_cash AS (
 SELECT r.id,r.merchant_id,j.created_at,e.amount::numeric amount FROM settlement_payouts p JOIN ledger_requests r ON r.id=p.request_id JOIN journals j ON j.event_key='settlement-payout:'||p.request_id::text||':principal' OR j.event_key='transfer:'||p.transfer_id::text||':bank-return:settlement-principal' JOIN ledger_entries e ON e.journal_id=j.id JOIN accounts a ON a.id=e.account_id AND a.owner_id=0 AND a.kind='clearing' AND a.scope_id=0 WHERE ($1::bigint=0 OR r.merchant_id=$1)
 ),
 transfer_cash AS (SELECT t.id,t.cost_amount,t.resolved_at FROM transfers t WHERE ($1::bigint=0 OR t.merchant_id=$1) AND t.status='succeeded' AND NOT EXISTS(SELECT 1 FROM settlement_payouts p WHERE p.transfer_id=t.id) AND NOT EXISTS(SELECT 1 FROM bank_transfer_lifecycle b WHERE b.transfer_id=t.id AND b.state IN('reversed','reversal_pending'))),
 pending_settlements AS (SELECT r.id::text,r.external_no,(r.amount::numeric/100)::numeric(38,2)::text amount,r.status,r.created_at,COALESCE(t.status,'') payout_status,COALESCE(p.send_state,'') send_state,COALESCE(b.state,'') bank_state FROM ledger_requests r LEFT JOIN settlement_payouts p ON p.request_id=r.id LEFT JOIN transfers t ON t.id=p.transfer_id LEFT JOIN bank_transfer_lifecycle b ON b.transfer_id=t.id WHERE r.kind='settlements' AND ($1::bigint=0 OR r.merchant_id=$1) ORDER BY r.id DESC LIMIT 5)
 SELECT jsonb_build_object(
 'orders',(SELECT orders FROM order_totals),'paid_orders',(SELECT paid FROM order_totals),
 'available_money',(SELECT (available/100)::numeric(38,2)::text FROM balance),'frozen_money',(SELECT (frozen/100)::numeric(38,2)::text FROM balance),
 'summary',jsonb_build_object(
 'today_orders',COALESCE((SELECT n FROM created WHERE business_day=($2::timestamptz AT TIME ZONE 'Asia/Shanghai')::date),0)::text,
 'today_paid_orders',(SELECT count(*) FROM paid WHERE paid_at>=$2)::text,
 'today_money',(SELECT (COALESCE(sum(amount) FILTER(WHERE paid_at>=$2),0)::numeric/100)::numeric(38,2)::text FROM paid),
 'yesterday_money',(SELECT (COALESCE(sum(amount) FILTER(WHERE paid_at>=$2::timestamptz-interval '1 day' AND paid_at<$2),0)::numeric/100)::numeric(38,2)::text FROM paid),
 'today_settled_money',(SELECT (COALESCE(sum(amount) FILTER(WHERE created_at>=$2 AND created_at<$2::timestamptz+interval '1 day'),0)/100)::numeric(38,2)::text FROM settlement_cash),
 'yesterday_settled_money',(SELECT (COALESCE(sum(amount) FILTER(WHERE created_at>=$2::timestamptz-interval '1 day' AND created_at<$2),0)/100)::numeric(38,2)::text FROM settlement_cash),
 'settled_money',(SELECT (COALESCE(sum(amount),0)/100)::numeric(38,2)::text FROM settlement_cash),
 'today_transfer_money',(SELECT (COALESCE(sum(cost_amount) FILTER(WHERE resolved_at>=$2 AND resolved_at<$2::timestamptz+interval '1 day'),0)::numeric/100)::numeric(38,2)::text FROM transfer_cash),
 'yesterday_transfer_money',(SELECT (COALESCE(sum(cost_amount) FILTER(WHERE resolved_at>=$2::timestamptz-interval '1 day' AND resolved_at<$2),0)::numeric/100)::numeric(38,2)::text FROM transfer_cash),
 'today_register',CASE WHEN $1=0 THEN (SELECT count(*)::text FROM merchants WHERE created_at>=$2 AND created_at<$2::timestamptz+interval '1 day') END,
 'total_users',CASE WHEN $1=0 THEN (SELECT count(*)::text FROM merchants) END),
 'trend',COALESCE((SELECT jsonb_agg(jsonb_build_object('date',c.business_day,'orders',COALESCE(o.n,0)::text,'paid_orders',COALESCE(p.n,0)::text,'amount',(COALESCE(p.amount,0)/100)::numeric(38,2)::text,'alipay',(COALESCE(p.alipay,0)/100)::numeric(38,2)::text,'wxpay',(COALESCE(p.wxpay,0)/100)::numeric(38,2)::text) ORDER BY c.business_day) FROM calendar c LEFT JOIN created o ON o.business_day::text=c.business_day LEFT JOIN (SELECT business_day,sum(n) n,sum(amount) amount,sum(amount) FILTER(WHERE type='alipay') alipay,sum(amount) FILTER(WHERE type='wxpay') wxpay FROM paid_daily GROUP BY business_day)p ON p.business_day::text=c.business_day),'[]'::jsonb),
 'payment_mix',COALESCE((SELECT jsonb_agg(jsonb_build_object('type',type,'orders',n::text,'amount',(amount/100)::numeric(38,2)::text) ORDER BY type) FROM paid_daily WHERE business_day=($2::timestamptz AT TIME ZONE 'Asia/Shanghai')::date),'[]'::jsonb),
 'fee_trend',CASE WHEN $1=0 THEN COALESCE((SELECT jsonb_agg(jsonb_build_object('date',business_day::text,'type',type,'fee',(fee/100)::numeric(38,2)::text,'estimated_profit',(estimated_profit/100)::numeric(38,2)::text,'missing_cost_orders',missing_costs::text) ORDER BY business_day,type) FROM paid_daily),'[]'::jsonb) END,
 'todos',CASE WHEN $1=0 THEN jsonb_build_object('complaints',(SELECT count(*)::text FROM backoffice_records WHERE resource='complaints' AND body->>'status' IN('open','in_progress')),'merchant_audits',(SELECT count(*)::text FROM merchants WHERE pay=2),'settlements',(SELECT count(*)::text FROM ledger_requests WHERE kind='settlements' AND status='pending'),'transfers',(SELECT count(*)::text FROM transfers WHERE status IN('pending','unknown'))) END,
 'merchant',CASE WHEN $1>0 THEN (SELECT jsonb_build_object('id',id::text,'name',name,'pay',pay::text,'enabled',enabled) FROM merchants WHERE id=$1) END,
 'fee_schedule',CASE WHEN $1>0 THEN (SELECT jsonb_build_object('state',state,'group_name',group_name,'rate_bps',CASE WHEN state='disabled' THEN '0' WHEN state='ready' THEN rate_bps END,'source','merchant_group_payment_fee') FROM rate) END,
 'announcements',COALESCE((SELECT jsonb_agg(to_jsonb(a)) FROM (SELECT id::text,body->>'title' title,body->>'content' content,created_at FROM backoffice_records WHERE resource='announcements' AND body->>'enabled'='true' AND body->>'audience' IN('all',CASE WHEN $1=0 THEN 'admin' ELSE 'merchant' END) ORDER BY id DESC LIMIT 10)a),'[]'::jsonb),
 'recent_settlements',COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY p.created_at DESC) FROM pending_settlements p),'[]'::jsonb),
 'runtime',CASE WHEN $1=0 THEN jsonb_build_object('postgresql',current_setting('server_version'),'server_time',clock_timestamp()) END
 )`

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request, mid int64) {
	if r.URL.RawQuery != "" {
		response(w, 400, -1, "dashboard query parameters not accepted", nil)
		return
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	now := time.Now().In(zone)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	var raw []byte
	if e := s.Pool.QueryRow(r.Context(), dashboardSQL, mid, day).Scan(&raw); e != nil {
		s.fail(w, e)
		return
	}
	var data map[string]any
	if e := json.Unmarshal(raw, &data); e != nil {
		s.fail(w, e)
		return
	}
	data["sandbox"] = s.Sandbox
	data["timezone"] = "Asia/Shanghai"
	data["business_date"] = day.Format("2006-01-02")
	data["source"] = "local_ledger_snapshot"
	data["as_of"] = time.Now().UTC().Format(time.RFC3339)
	if mid == 0 {
		if run, ok := data["runtime"].(map[string]any); ok {
			run["go"] = runtime.Version()
			run["web"] = "Go API / nginx"
		}
	} else {
		channels := []map[string]string{}
		ids := append([]int64{}, s.MerchantChannels[mid]...)
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		var previous int64
		for _, id := range ids {
			if id == previous {
				continue
			}
			previous = id
			if s.ProviderFlow != nil {
				if provider := s.ProviderFlow.Providers[id]; provider != nil {
					channels = append(channels, map[string]string{"channel_id": strconv.FormatInt(id, 10), "type": provider.ID()})
				}
			}
		}
		data["payment_channels"] = channels
	}
	response(w, 200, 0, "", data)
}
