package server

import (
	"errors"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"goepay/internal/privatesearch"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

const transferEffectiveStatus = "CASE WHEN settlement_payout.send_state IN ('unsent','cancelled') THEN settlement_payout.send_state WHEN batch_item.state IN ('unsent','cancelled') THEN batch_item.state WHEN b.state IN ('reversed','reversal_pending') THEN b.state ELSE t.status END"

func (s *Server) list(w http.ResponseWriter, r *http.Request, role string, mid int64, kind string, session identity.Session) {
	query, queryErr := url.ParseQuery(r.URL.RawQuery)
	if queryErr != nil {
		response(w, 400, -1, "invalid query", nil)
		return
	}
	export, exportErr := csvRequested(r)
	if exportErr != nil {
		response(w, 400, -1, exportErr.Error(), nil)
		return
	}
	page, limit := 1, 20
	if raw := query.Get("page"); raw != "" {
		v, e := strconv.Atoi(raw)
		if e != nil || v < 1 || v > 1000000 {
			response(w, 400, -1, "invalid page", nil)
			return
		}
		page = v
	}
	if raw := query.Get("limit"); raw != "" {
		v, e := strconv.Atoi(raw)
		if e != nil || v < 1 || v > 100 {
			response(w, 400, -1, "invalid limit", nil)
			return
		}
		limit = v
	}
	var from, columns, tenant, matching, order string
	switch kind {
	case "orders":
		from = "orders o" + consoleOrderReportingJoins + consoleOrderControlJoin
		columns = "o.id::text,o.trade_no,o.merchant_id::text,o.out_trade_no,o.name,o.type,o.amount::text,o.merchant_amount::text,o.platform_fee::text,o.fee_rate_bps,o.fee_policy,o.refund_fee_payer,o.status,o.created_at,o.paid_at,o.param,o.version"
		columns += ",CASE WHEN EXISTS(SELECT 1 FROM order_income_holds h WHERE h.order_id=o.id AND h.state='held') THEN 'held' WHEN EXISTS(SELECT 1 FROM order_income_holds h WHERE h.order_id=o.id) THEN 'released' ELSE 'none' END AS income_status"
		columns += "," + consoleOrderReportingColumns
		if role == "admin" {
			columns += ", (o.status='paid' AND o.notify_url<>'' AND COALESCE(console_notification.state,'none')<>'succeeded') AS can_batch_notify"
			columns += ",COALESCE(console_control.changes->>'status','') AS admin_status,COALESCE((console_control.changes->>'archived')='true',false) AS admin_archived,COALESCE(console_control.id::text,'0') AS admin_revision"
		}
		tenant = "o.merchant_id"
		matching = "concat_ws(' ',o.trade_no,o.out_trade_no,o.name)"
		order = "o.id DESC"
	case "merchants":
		if role != "admin" {
			response(w, 403, -1, "forbidden", nil)
			return
		}
		from = "merchants m"
		columns = "m.id::text,m.name,m.enabled"
		tenant = "0::bigint"
		matching = "concat_ws(' ',m.id,m.name)"
		order = "m.id"
	case "refunds":
		from = consoleRefundFrom
		columns = "f.id::text,f.merchant_id::text,f.order_id::text,o.trade_no,f.refund_no,f.amount::text,f.merchant_amount::text,f.fee_amount::text,f.status,f.created_at"
		columns += ",f.id::text AS platform_refund_no,f.refund_no AS out_refund_no"
		columns += "," + consoleRefundCompletedAt
		tenant = "f.merchant_id"
		matching = consoleRefundMatching
		order = "f.id DESC"
	case "transfers":
		from = "transfers t LEFT JOIN provider_transfer_attempts p ON p.transfer_id=t.id LEFT JOIN bank_transfer_lifecycle b ON b.transfer_id=t.id LEFT JOIN transfer_batch_items batch_item ON batch_item.transfer_id=t.id"
		from += " LEFT JOIN settlement_payouts settlement_payout ON settlement_payout.transfer_id=t.id"
		columns = "t.id::text,t.merchant_id::text,t.transfer_no,t.biz_no,t.amount::text,t.cost_amount::text,t.fee_amount::text,t.fee_rate_bps,COALESCE(p.channel_id,0)::text AS channel_id," + transferEffectiveStatus + " AS status,t.status AS ledger_status,COALESCE(b.state,'') AS bank_lifecycle,COALESCE(b.bank_fee_amount,0)::text AS bank_fee_amount,(b.bank_fee_amount IS NOT NULL) AS bank_fee_known,COALESCE(b.service_fee_held,0)::text AS bank_service_fee_held,COALESCE(b.returned_principal,0)::text AS bank_returned_principal,COALESCE(b.returned_service_fee,0)::text AS bank_returned_service_fee,COALESCE(b.returned_bank_fee,0)::text AS bank_returned_fee,COALESCE(batch_item.state,'') AS batch_send_state,COALESCE(batch_item.batch_id::text,'') AS batch_id"
		columns += ",CASE WHEN settlement_payout.request_id IS NULL THEN 'merchant_available' ELSE 'settlement' END AS funding_source,COALESCE(settlement_payout.request_id::text,'') AS settlement_request_id,COALESCE(settlement_payout.send_state,'') AS settlement_send_state"
		columns += transferListDisplayColumns
		tenant = "t.merchant_id"
		matching = "t.transfer_no"
		order = "t.id DESC"
	case "ledger":
		from = consoleLedgerFrom
		columns = consoleLedgerColumns
		tenant = "a.owner_id"
		matching = "concat_ws(' ',j.event_key,j.description)"
		order = "e.id DESC"
	default:
		response(w, 404, -1, "not found", nil)
		return
	}
	// All SQL identifiers above are fixed by the endpoint; user input is bound.
	searchQuery := query
	privateField := map[string]privatesearch.Field{"account": privatesearch.Account, "username": privatesearch.Name, "desc": privatesearch.Remark}[query.Get("search_field")]
	var privateIDs []int64
	privateActive := kind == "transfers" && privateField != ""
	if privateActive {
		if e := validateTransferListFilters(query); e != nil {
			response(w, 400, -1, "invalid search filter", nil)
			return
		}
		raw := query.Get("q")
		if !utf8.ValidString(raw) || len(raw) > 256 || strings.ContainsAny(raw, "\x00\r\n") {
			response(w, 400, -1, "invalid search filter", nil)
			return
		}
		if strings.TrimSpace(raw) != "" {
			owner := mid
			if v := query.Get("merchant_id"); v != "" {
				id, e := strconv.ParseInt(v, 10, 64)
				if e != nil || id <= 0 {
					response(w, 400, -1, "invalid merchant", nil)
					return
				}
				if role != "admin" && id != mid {
					response(w, 403, -1, "forbidden", nil)
					return
				}
				owner = id
			}
			var e error
			privateIDs, e = s.Store.SearchPrivateTransfers(r.Context(), owner, privateField, raw)
			if e != nil {
				if errors.Is(e, payment.ErrPrivateSearchBroad) {
					response(w, 422, -1, "搜索范围过大，请输入更完整的账号、姓名或备注", nil)
				} else {
					s.fail(w, e)
				}
				return
			}
		} else {
			privateActive = false
		}
		searchQuery = url.Values{}
		for k, v := range query {
			searchQuery[k] = append([]string(nil), v...)
		}
		searchQuery.Set("search_field", "")
		searchQuery.Set("q", "")
	}
	searchClause, search, searchErr := consoleOrderSearch(searchQuery, kind, matching)
	if searchErr != nil {
		response(w, 400, -1, "invalid search filter", nil)
		return
	}
	where := " WHERE ($1::bigint=0 OR " + tenant + "=$1) AND (" + searchClause + ")"
	filters, args, err := consoleListFilters(query, role, kind, tenant, mid, []any{mid, search})
	if err != nil {
		status := 400
		if err == errListFilterForbidden {
			status = 403
		}
		response(w, status, -1, err.Error(), nil)
		return
	}
	where += filters
	if privateActive {
		args = append(args, privateIDs)
		where += fmt.Sprintf(" AND t.id=ANY($%d::bigint[])", len(args))
	}
	if kind == "orders" && query.Get("include_archived") != "true" {
		where += " AND COALESCE((console_control.changes->>'archived')='true',false)=false"
	}
	if export {
		s.exportCSV(w, r, session, kind, "SELECT "+columns+" FROM "+from+where+" ORDER BY "+order, args)
		return
	}
	var total int64
	if e := s.Pool.QueryRow(r.Context(), "SELECT count(*) FROM "+from+where, args...).Scan(&total); e != nil {
		s.fail(w, e)
		return
	}
	limitIndex := len(args) + 1
	args = append(args, limit, (page-1)*limit)
	rows, e := s.Pool.Query(r.Context(), "SELECT "+columns+" FROM "+from+where+" ORDER BY "+order+fmt.Sprintf(" LIMIT $%d OFFSET $%d", limitIndex, limitIndex+1), args...)
	if e != nil {
		s.fail(w, e)
		return
	}
	defer rows.Close()
	fieldNames := make([]string, len(rows.FieldDescriptions()))
	for i, field := range rows.FieldDescriptions() {
		fieldNames[i] = field.Name
	}
	publicFields := fieldNames
	if kind == "transfers" {
		publicFields = transferPublicColumns(fieldNames)
	}
	items := []map[string]any{}
	for rows.Next() {
		values, e := rows.Values()
		if e != nil {
			s.fail(w, e)
			return
		}
		if kind == "transfers" {
			values, e = s.projectTransferListValues(fieldNames, values)
			if e != nil {
				s.fail(w, e)
				return
			}
		}
		v := map[string]any{}
		for i, field := range publicFields {
			v[field] = values[i]
		}
		items = append(items, v)
	}
	if rows.Err() != nil {
		s.fail(w, rows.Err())
		return
	}
	rows.Close()
	if kind == "orders" && role == "admin" {
		if e = s.consoleOrderCharges104(r.Context(), 0, items); e != nil {
			s.fail(w, e)
			return
		}
	}
	if kind == "orders" && role == "merchant" {
		if e = s.merchantOrderActions(r.Context(), session, items); e != nil {
			s.fail(w, e)
			return
		}
	}
	response(w, 200, 0, "", map[string]any{"items": items, "total": total, "page": page, "limit": limit})
}
