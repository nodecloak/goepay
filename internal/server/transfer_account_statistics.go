package server

import (
	"context"
	"encoding/csv"
	"errors"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"strconv"
	"time"
)

// transferAccountStatMethods are the 付款方式 options epay_pro's accountStat
// (transfer/accountStat) offers in its type filter. qqpay is listed for parity
// of the filter vocabulary even though GOEPAY has no qqpay transfer rail; a
// qqpay filter simply matches nothing, like any rail with no payouts.
var transferAccountStatMethods = map[string]bool{"alipay": true, "wxpay": true, "qqpay": true, "bank": true}

// transferAccountStatRow is one payee-account group: a representative encrypted
// recipient (for the masked 付款账号/姓名 display) plus the aggregates.
type transferAccountStatRow struct {
	reprID, reprAmount             int64
	cipher                         []byte
	method                         string
	orders, paidOrders             int64
	paidMoneyMinor, costMoneyMinor int64
}

// transferAccountStatistics implements admin /transfer/stat 付款账号统计
// (epay_pro transfer/accountStat): one row per payee account with 付款账号 /
// 姓名 / 付款笔数 / 成功笔数 / 付款金额 / 花费金额. GOEPAY stores the recipient
// only inside the per-transfer encrypted intent
// (provider_transfer_attempts.recipient_cipher) with a keyed one-way index
// (provider_transfer_policies.recipient_hash), so the breakdown groups by that
// hash — every provider transfer has exactly one — and decrypts one
// representative intent per group through the same authenticated, masked
// display the transfer list uses. No recipient plaintext ever enters an SQL
// GROUP BY. The money-moving transfer status model is untouched.
func (s *Server) transferAccountStatistics(w http.ResponseWriter, r *http.Request, session identity.Session, start, end time.Time, page, limit int, export bool) {
	if s.Store == nil || s.Pool == nil {
		response(w, 503, -1, "transfer statistics unavailable", nil)
		return
	}
	method := r.URL.Query().Get("type")
	if method != "" && !transferAccountStatMethods[method] {
		response(w, 400, -1, "invalid transfer method", nil)
		return
	}
	rows, total, e := s.loadTransferAccountStats(r.Context(), method, start, end, page, limit, export)
	if e != nil {
		s.fail(w, e)
		return
	}
	columns := []reportColumn{
		{"dimension", "付款账号"},
		{"name", "姓名"},
		{"orders", "付款笔数"},
		{"paid_orders", "成功笔数"},
		{"paid_money", "付款金额（元）"},
		{"cost_money", "花费金额（元）"},
	}
	items := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		var display payment.TransferRecipientDisplay
		if len(row.cipher) > 0 {
			display, e = s.Store.TransferRecipientDisplay(row.reprID, row.reprAmount, row.cipher)
			if e != nil {
				s.fail(w, e)
				return
			}
		}
		items = append(items, map[string]string{
			"dimension":   accountStatDimension(display.AccountMasked),
			"name":        display.NameMasked,
			"orders":      strconv.FormatInt(row.orders, 10),
			"paid_orders": strconv.FormatInt(row.paidOrders, 10),
			"paid_money":  minorToYuan(row.paidMoneyMinor),
			"cost_money":  minorToYuan(row.costMoneyMinor),
		})
	}
	if export {
		s.exportTransferAccountCSV(r.Context(), w, session, columns, items)
		return
	}
	response(w, 200, 0, "", map[string]any{
		"columns":  columns,
		"items":    items,
		"total":    total,
		"page":     page,
		"limit":    limit,
		"timezone": "Asia/Shanghai",
	})
}

// accountStatDimension keeps the dimension cell non-empty so the table never
// renders a blank primary column. A historical reservation with no provider
// intent (empty cipher) has no recoverable account.
func accountStatDimension(masked string) string {
	if masked == "" {
		return "未记录"
	}
	return masked
}

func minorToYuan(minor int64) string {
	neg := minor < 0
	if neg {
		minor = -minor
	}
	whole := minor / 100
	frac := minor % 100
	out := strconv.FormatInt(whole, 10) + "." + pad2(frac)
	if neg {
		return "-" + out
	}
	return out
}

func pad2(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) < 2 {
		return "0" + s
	}
	return s
}

// loadTransferAccountStats aggregates payouts by recipient_hash and returns, per
// group, one representative (lowest transfer id) encrypted intent plus the
// counts/amounts. Ordering is by paid money then id so the page is stable; the
// representative is deterministic (MIN id) so the masked display never flickers
// between pages. method="" matches every rail (including historical NULL method
// rows); a concrete method filters provider_transfer_attempts.method, exactly
// like the transfer list 付款方式 filter.
func (s *Server) loadTransferAccountStats(ctx context.Context, method string, start, end time.Time, page, limit int, export bool) ([]transferAccountStatRow, int64, error) {
	paidState := "CASE WHEN b.state IN ('reversed','reversal_pending') THEN b.state WHEN t.status='succeeded' THEN 'paid' ELSE t.status END"
	// Settlement-sourced payouts carry their own fee/net; a plain transfer uses
	// its cost_amount. This mirrors reports.go's transfer net/cost expression so
	// 花费金额 agrees with the channel/merchant transfer reports.
	costExpr := "CASE WHEN sb.request_id IS NOT NULL THEN sp.net_amount WHEN sp.request_id IS NOT NULL THEN sp.gross_amount ELSE t.cost_amount END"
	// A provider transfer's payee index is its policy row; a wallet settlement
	// payout writes no policy row and keeps its own payee index on
	// settlement_payouts (a bank settlement payout writes both). The inner join
	// on policies used to drop wallet settlement payouts from this report while
	// the channel and merchant reports counted them.
	base := `FROM transfers t
 JOIN provider_transfer_attempts a ON a.transfer_id=t.id
 LEFT JOIN provider_transfer_policies pol ON pol.transfer_id=t.id
 LEFT JOIN bank_transfer_lifecycle b ON b.transfer_id=t.id
 LEFT JOIN settlement_payouts sp ON sp.transfer_id=t.id
 LEFT JOIN settlement_bank_payouts sb ON sb.transfer_id=t.id
 WHERE t.created_at >= $1 AND t.created_at < $2 AND ($3::text='' OR a.method=$3)`
	args := []any{start, end.AddDate(0, 0, 1), method}
	payee := "COALESCE(pol.recipient_hash, sp.recipient_hash)"

	// Per-group aggregate with the representative intent chosen by lowest id.
	grouped := `SELECT ` + payee + ` AS hash,
 (array_agg(t.id ORDER BY t.id))[1] AS repr_id,
 (array_agg(t.amount ORDER BY t.id))[1] AS repr_amount,
 (array_agg(a.recipient_cipher ORDER BY t.id))[1] AS repr_cipher,
 (array_agg(COALESCE(a.method,'') ORDER BY t.id))[1] AS repr_method,
 count(*) AS orders,
 count(*) FILTER (WHERE ` + paidState + `='paid') AS paid_orders,
 COALESCE(sum(t.amount) FILTER (WHERE ` + paidState + `='paid'),0) AS paid_money,
 COALESCE(sum(` + costExpr + `) FILTER (WHERE ` + paidState + `='paid'),0) AS cost_money
 ` + base + ` GROUP BY ` + payee

	var total int64
	if e := s.Pool.QueryRow(ctx, "SELECT count(*) FROM ("+grouped+") g", args...).Scan(&total); e != nil {
		return nil, 0, e
	}
	query := grouped + " ORDER BY paid_money DESC, repr_id ASC"
	if !export {
		query += " LIMIT $4 OFFSET $5"
		args = append(args, limit, (page-1)*limit)
	}
	queryRows, e := s.Pool.Query(ctx, query, args...)
	if e != nil {
		return nil, 0, e
	}
	defer queryRows.Close()
	out := make([]transferAccountStatRow, 0)
	for queryRows.Next() {
		var row transferAccountStatRow
		var hash []byte
		if e = queryRows.Scan(&hash, &row.reprID, &row.reprAmount, &row.cipher, &row.method, &row.orders, &row.paidOrders, &row.paidMoneyMinor, &row.costMoneyMinor); e != nil {
			return nil, 0, e
		}
		out = append(out, row)
	}
	if e = queryRows.Err(); e != nil {
		return nil, 0, e
	}
	return out, total, nil
}

// exportTransferAccountCSV buffers the fully-decrypted, masked rows before
// writing any response header, then audits and ships them through the shared
// finalizer — the same contract the transfer list CSV uses, because masking
// happens in Go, not SQL.
func (s *Server) exportTransferAccountCSV(ctx context.Context, w http.ResponseWriter, session identity.Session, columns []reportColumn, items []map[string]string) {
	select {
	case csvExportSlots <- struct{}{}:
		defer func() { <-csvExportSlots }()
	default:
		w.Header().Set("Retry-After", "5")
		response(w, 429, -1, "CSV exports busy", nil)
		return
	}
	exportCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	b := &csvBoundedBuffer{}
	_, _ = b.Write([]byte("\xef\xbb\xbf"))
	writer := csv.NewWriter(b)
	header := make([]string, len(columns))
	for i, c := range columns {
		header[i] = c.Label
	}
	e := writer.Write(header)
	for _, item := range items {
		if e != nil {
			break
		}
		record := make([]string, len(columns))
		for i, c := range columns {
			record[i] = csvExportCell(item[c.Key])
		}
		e = writer.Write(record)
	}
	writer.Flush()
	if e == nil {
		e = writer.Error()
	}
	if errors.Is(e, errCSVExportTooLarge) {
		response(w, 422, -1, e.Error(), nil)
		return
	}
	if e != nil {
		s.fail(w, e)
		return
	}
	s.finishCSVExport(exportCtx, w, session, "reports/transfer-accounts", b, len(items))
}
