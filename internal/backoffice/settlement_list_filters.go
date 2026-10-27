package backoffice

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type settlementListFilter struct {
	Page, Limit int
	MerchantID  int64
	Type        string
	State       int
	From, Until *time.Time
	Search      string
}

func parseSettlementListFilter(r *http.Request, p Principal) (settlementListFilter, error) {
	out := settlementListFilter{Page: 1, Limit: 20, State: -1}
	if p.Role == "merchant" {
		out.MerchantID = p.SubjectID
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return out, ErrInvalid
	}
	for k, v := range q {
		if len(v) != 1 {
			return out, ErrInvalid
		}
		switch k {
		case "page", "limit", "merchant_id", "uid", "type", "dstatus", "from", "to", "starttime", "endtime", "value":
		default:
			return out, ErrInvalid
		}
	}
	if values, ok := q["value"]; ok {
		if len(values[0]) > 256 || !utf8.ValidString(values[0]) || strings.ContainsAny(values[0], "\x00\r\n") {
			return out, ErrInvalid
		}
		out.Search = strings.TrimSpace(values[0])
	}
	alias := func(a, b string) (string, error) {
		av, aok := q[a]
		bv, bok := q[b]
		if aok && bok {
			return "", ErrInvalid
		}
		if aok {
			return av[0], nil
		}
		if bok {
			return bv[0], nil
		}
		return "", nil
	}
	for key, to := range map[string]*int{"page": &out.Page, "limit": &out.Limit} {
		if v, ok := q[key]; ok {
			n, e := strconv.ParseInt(v[0], 10, 32)
			if e != nil || n <= 0 || strconv.FormatInt(n, 10) != v[0] || key == "page" && n > 100000 || key == "limit" && n > 100 {
				return out, ErrInvalid
			}
			*to = int(n)
		}
	}
	mid, e := alias("merchant_id", "uid")
	if e != nil {
		return out, e
	}
	if mid != "" {
		n, e := strconv.ParseInt(mid, 10, 64)
		if e != nil || n <= 0 || strconv.FormatInt(n, 10) != mid {
			return out, ErrInvalid
		}
		if p.Role == "merchant" && n != p.SubjectID {
			return out, ErrForbidden
		}
		out.MerchantID = n
	} else if _, ok := q["merchant_id"]; ok {
		return out, ErrInvalid
	} else if _, ok := q["uid"]; ok {
		return out, ErrInvalid
	}
	if v, ok := q["type"]; ok {
		kind := map[string]string{"1": "alipay", "2": "wxpay", "3": "qqpay", "4": "bank", "alipay": "alipay", "wxpay": "wxpay", "qqpay": "qqpay", "bank": "bank"}[v[0]]
		if kind == "" {
			return out, ErrInvalid
		}
		out.Type = kind
	}
	if v, ok := q["dstatus"]; ok {
		n, e := strconv.Atoi(v[0])
		if e != nil || n < 0 || n > 6 || strconv.Itoa(n) != v[0] {
			return out, ErrInvalid
		}
		out.State = n
	}
	from, e := alias("from", "starttime")
	if e != nil {
		return out, e
	}
	to, e := alias("to", "endtime")
	if e != nil {
		return out, e
	}
	hasFrom := q.Has("from") || q.Has("starttime")
	hasTo := q.Has("to") || q.Has("endtime")
	if hasFrom != hasTo {
		return out, ErrInvalid
	}
	if hasFrom {
		zone := time.FixedZone("Beijing", 8*3600)
		a, e := time.ParseInLocation("2006-01-02", from, zone)
		if e != nil || a.Format("2006-01-02") != from || a.Year() < 1970 {
			return out, ErrInvalid
		}
		b, e := time.ParseInLocation("2006-01-02", to, zone)
		if e != nil || b.Format("2006-01-02") != to || b.Before(a) || b.Sub(a) > 366*24*time.Hour || b.Year() > 9998 {
			return out, ErrInvalid
		}
		until := b.AddDate(0, 0, 1)
		out.From = &a
		out.Until = &until
	}
	return out, nil
}

// One row per original request, using its immutable recipient configuration.
// Approval alone is waiting; completion requires the original sent payout and
// its paid reserve. Bank reversals retain their distinct existing UI states.
const settlementListRowsSQL = `WITH settlement_rows AS (
 SELECT r.*,COALESCE(c.recipient_type,'') AS original_type,
 CASE WHEN r.status='rejected' THEN 3
 WHEN p.request_id IS NULL OR p.send_state='unsent' THEN 0
 WHEN p.send_state='cancelled' THEN 4
 WHEN b.state='reversed' THEN 6 WHEN b.state='reversal_pending' THEN 5
 WHEN t.status='failed' THEN 3
 WHEN r.status='approved' AND p.send_state='claimed' AND t.status='succeeded' AND f.request_id IS NOT NULL AND (a.balance=0 OR bp.request_id IS NOT NULL AND a.balance>0) THEN 1
 ELSE 2 END AS original_state
 FROM ledger_requests r
 LEFT JOIN settlement_recipients sr ON sr.request_id=r.id
 LEFT JOIN settlement_recipient_configs c ON c.id=sr.config_id AND c.merchant_id=r.merchant_id
 LEFT JOIN settlement_payouts p ON p.request_id=r.id
 LEFT JOIN transfers t ON t.id=p.transfer_id
 LEFT JOIN bank_transfer_lifecycle b ON b.transfer_id=t.id
 LEFT JOIN settlement_bank_payouts bp ON bp.request_id=r.id
 LEFT JOIN settlement_fundings f ON f.request_id=r.id
 LEFT JOIN accounts a ON a.id=f.account_id
 WHERE r.kind='settlements') `
const settlementListWhereSQL = `WHERE ($1::bigint=0 OR merchant_id=$1)
 AND ($2::text='' OR original_type=$2) AND ($3::int=-1 OR original_state=$3)
 AND ($4::timestamptz IS NULL OR created_at>=$4) AND ($5::timestamptz IS NULL OR created_at<$5)`

func (f settlementListFilter) args() []any {
	return []any{f.MerchantID, f.Type, f.State, f.From, f.Until}
}
