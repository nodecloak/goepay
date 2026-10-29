package backoffice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
)

// Merchant directory CSV export (epay_pro user/export parity, audit A6-08).
//
// The original ThinkPHP user/export streams the merchant roster to a CSV. GOEPAY
// reuses the bounded export pattern already established for orders/refunds/ledger
// (internal/server/csv_export.go): one SELECT, no paging, a row and byte ceiling,
// formula-injection neutralisation, BOM for Excel, and an audit row written before
// the body. Columns are an EXPLICIT non-secret allow-list — never api_key, RSA
// material, settlement account numbers or encrypted payment fields — so the export
// cannot leak a secret the list projection already withholds.
const merchantExportMaxRows = 50000
const merchantExportMaxBytes = 8 << 20

var merchantExportSlots = make(chan struct{}, 2)
var errMerchantExportTooLarge = errors.New("CSV exceeds 50000 rows or 8 MiB; narrow the filters")

// Fixed, non-secret column expressions selected against the merchantConsoleFrom
// join. Header labels mirror the Pro export / console field labels.
var merchantExportColumns = []struct{ header, expr string }{
	{"商户ID", "merchants.id::text"},
	{"商户名称", "COALESCE(merchants.name,'')"},
	{"登录账号", "COALESCE((SELECT username FROM console_users WHERE role='merchant' AND subject_id=merchants.id ORDER BY id LIMIT 1),'')"},
	{"状态", "CASE WHEN merchants.enabled THEN '启用' ELSE '禁用' END"},
	{"支付状态", "CASE COALESCE(to_jsonb(merchants)->>'pay','1') WHEN '0' THEN '关闭' WHEN '2' THEN '维护' ELSE '正常' END"},
	{"结算权限", "CASE merchant_settlement_permission(merchants.profile) WHEN '1' THEN '开启' ELSE '关闭' END"},
	{"当前有效用户组", "CASE WHEN console_effective_group.group_id IS NULL THEN '' ELSE COALESCE(console_group.body->>'name',CASE WHEN console_effective_group.group_id=0 THEN '默认用户组' ELSE '' END) END"},
	{"邮箱", "CASE WHEN jsonb_typeof(merchants.profile->'email')='string' THEN merchants.profile->>'email' ELSE '' END"},
	{"手机", "CASE WHEN jsonb_typeof(merchants.profile->'phone')='string' THEN merchants.profile->>'phone' ELSE '' END"},
	{"QQ", "CASE WHEN jsonb_typeof(merchants.profile->'qq')='string' THEN merchants.profile->>'qq' ELSE '' END"},
	{"账户余额（分）", "console_balances.available_minor"},
	{"冻结预留（分）", "console_balances.frozen_minor"},
	{"待解冻收入（分）", "console_balances.income_held_minor"},
	{"签名方式", "COALESCE(merchants.signature_algorithm,'')"},
	{"注册时间", "CASE WHEN merchants.registration_time_known THEN to_char(merchants.created_at AT TIME ZONE 'Asia/Shanghai','YYYY-MM-DD HH24:MI:SS') ELSE '' END"},
}

type merchantExportBuffer struct{ bytes.Buffer }

func (b *merchantExportBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > merchantExportMaxBytes {
		return 0, errMerchantExportTooLarge
	}
	return b.Buffer.Write(p)
}

// Guard against spreadsheet formula execution while preserving the text.
func merchantExportCell(v any) string {
	var text string
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		text = string(t)
	default:
		text = fmt.Sprint(t)
	}
	start := strings.TrimLeftFunc(text, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
	if start != "" && strings.ContainsRune("=+-@", rune(start[0])) {
		return "'" + text
	}
	return text
}

// exportMerchantDirectoryCSV writes the merchant roster that matches the same
// filter the list GET uses. Admin-only; the merchants list is never served to a
// merchant principal (special.go scopes it to admin), and this refuses any other
// role defensively.
func (s *Service) exportMerchantDirectoryCSV(w http.ResponseWriter, r *http.Request, p Principal, f *MerchantDirectoryFilter, search string) {
	if p.Role != "admin" {
		write(w, 403, nil, "forbidden")
		return
	}
	select {
	case merchantExportSlots <- struct{}{}:
		defer func() { <-merchantExportSlots }()
	default:
		w.Header().Set("Retry-After", "5")
		write(w, 429, nil, "CSV exports busy")
		return
	}
	where, order, args, e := merchantDirectorySQL(f, search)
	if e != nil {
		writeError(w, e)
		return
	}
	selected := make([]string, len(merchantExportColumns))
	for i, c := range merchantExportColumns {
		selected[i] = c.expr
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	query := "SELECT " + strings.Join(selected, ",") + " FROM " + merchantConsoleFrom + where + " ORDER BY " + order + fmt.Sprintf(" LIMIT %d", merchantExportMaxRows+1)
	rows, e := s.pool.Query(ctx, query, args...)
	if e != nil {
		writeError(w, e)
		return
	}
	defer rows.Close()
	b := &merchantExportBuffer{}
	_, _ = b.Write([]byte("\xef\xbb\xbf"))
	writer := csv.NewWriter(b)
	header := make([]string, len(merchantExportColumns))
	for i, c := range merchantExportColumns {
		header[i] = c.header
	}
	e = writer.Write(header)
	count := 0
	for e == nil && rows.Next() {
		count++
		if count > merchantExportMaxRows {
			e = errMerchantExportTooLarge
			break
		}
		var values []any
		values, e = rows.Values()
		if e != nil {
			break
		}
		record := make([]string, len(values))
		for i, v := range values {
			record[i] = merchantExportCell(v)
		}
		e = writer.Write(record)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	writer.Flush()
	if e == nil {
		e = writer.Error()
	}
	if errors.Is(e, errMerchantExportTooLarge) {
		write(w, 422, nil, e.Error())
		return
	}
	if e != nil {
		writeError(w, e)
		return
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(b.Bytes()))
	if _, e = s.pool.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'merchants',0,'export-csv',jsonb_build_object('rows',$4::integer,'sha256',$5::text))", p.Role, p.SubjectID, p.Username, count, digest); e != nil {
		writeError(w, e)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"merchants.csv\"")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Export-Rows", fmt.Sprint(count))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, bytes.NewReader(b.Bytes()))
}
