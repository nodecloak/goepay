package payment

import (
	"context"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// Pro 分账记录 (profitsharing/order) server-side list: filters, offset paging
// with a total, a bounded statistics aggregate for the same filter, and the
// join that supplies 订单金额 / 支付方式 / 分账规则 id from the original payment
// declaration (provider_attempt_sharing) — all a4 N5. The list uses a light
// projection (no per-row cryptographic re-verification; the detail and query
// endpoints keep doing that). Amounts stay integer minor units.

// ChannelSharingOrderListView is one Pro 分账记录 row. It extends the base order
// summary with the joined columns the flat cursor list could not show.
type ChannelSharingOrderListView struct {
	ID          int64     `json:"id,string"`
	ExternalNo  string    `json:"external_no"`
	OutNo       string    `json:"out_no"`
	State       string    `json:"state"`
	ShareAmount int64     `json:"share_amount,string"` // SUM(order_items.amount) 分账金额
	OrderAmount int64     `json:"order_amount,string"` // provider_attempt_sharing.gross_amount 订单金额
	Provider    string    `json:"provider"`            // 支付方式 (provider key)
	Method      string    `json:"method"`
	RuleID      int64     `json:"rule_id,string"` // 分账规则 id ("0" when the payment declared none)
	ChannelID   int64     `json:"channel_id,string"`
	CreatedAt   time.Time `json:"created_at"`
}

type ChannelSharingOrderListPage struct {
	Items []ChannelSharingOrderListView `json:"items"`
	Total int64                         `json:"total"`
	Limit int                           `json:"limit"`
}

// ChannelSharingOrderStats is the bounded 统计 aggregate over the same filter.
type ChannelSharingOrderStats struct {
	Count          int64 `json:"count,string"`
	ShareAmount    int64 `json:"share_amount,string"`
	ReleasedAmount int64 `json:"released_amount,string"`
	SuccessCount   int64 `json:"success_count,string"`
	FailCount      int64 `json:"fail_count,string"`
}

// ChannelSharingOrderQuery carries Pro's filter fields. Field selects which
// column 搜索内容 matches: external_no (系统订单号), out_no (接口订单号) or
// share_amount (分账金额, an exact minor-unit value). States is the 分账状态
// filter (a set; empty means all). From/To bound created_at (inclusive).
type ChannelSharingOrderQuery struct {
	MerchantID int64
	Field      string
	Keyword    string
	States     []string
	RuleID     int64 // 分账规则ID filter (0 = any)
	From, To   time.Time
	Offset     int
	Limit      int
}

var sharingOrderStates = map[string]bool{
	"unsent": true, "claimed": true, "pending": true, "succeeded": true,
	"failed": true, "partial": true, "cancelled": true, "unknown": true,
}

// where builds the shared WHERE clause + positional args used by the list,
// count, aggregate and CSV. The projection joins the base order to its payment
// declaration (1:1 on order_id) and pre-aggregates the item sum.
func (q ChannelSharingOrderQuery) where() (string, []any, bool) {
	// Validate every input before it reaches SQL; reject rather than coerce.
	if q.MerchantID <= 0 {
		return "", nil, false
	}
	if q.Field != "" && q.Field != "external_no" && q.Field != "out_no" && q.Field != "share_amount" {
		return "", nil, false
	}
	if len(q.Keyword) > 128 || !utf8.ValidString(q.Keyword) {
		return "", nil, false
	}
	for _, r := range q.Keyword {
		if r < 32 || r == 127 {
			return "", nil, false
		}
	}
	for _, st := range q.States {
		if !sharingOrderStates[st] {
			return "", nil, false
		}
	}
	if q.RuleID < 0 {
		return "", nil, false
	}
	// share_amount search is an exact integer minor-unit match.
	var shareExact int64 = -1
	if q.Field == "share_amount" && q.Keyword != "" {
		v, ok := parseMinorAmount(q.Keyword)
		if !ok {
			return "", nil, false
		}
		shareExact = v
	}
	args := []any{q.MerchantID}
	conds := []string{"o.merchant_id=$1"}
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	if q.Field == "external_no" && q.Keyword != "" {
		add("o.external_no=?", q.Keyword)
	} else if q.Field == "out_no" && q.Keyword != "" {
		add("o.out_no=?", q.Keyword)
	} else if shareExact >= 0 {
		add("COALESCE(si.share_amount,0)=?", shareExact)
	}
	if len(q.States) > 0 {
		add("o.state = ANY(?)", q.States)
	}
	if q.RuleID > 0 {
		add("d.rule_id=?", q.RuleID)
	}
	if !q.From.IsZero() {
		add("o.created_at>=?", q.From)
	}
	if !q.To.IsZero() {
		add("o.created_at<=?", q.To)
	}
	from := ` FROM channel_sharing_orders o
	 LEFT JOIN provider_attempt_sharing d ON d.order_id=o.order_id
	 LEFT JOIN (SELECT sharing_id, SUM(amount) AS share_amount FROM channel_sharing_order_items GROUP BY sharing_id) si ON si.sharing_id=o.id
	 WHERE ` + strings.Join(conds, " AND ")
	return from, args, true
}

const sharingOrderListSelect = `SELECT o.id, o.external_no, o.out_no, o.state,
	 COALESCE(si.share_amount,0) AS share_amount,
	 COALESCE(d.gross_amount,0) AS order_amount,
	 COALESCE(d.provider,'') AS provider,
	 COALESCE(d.method,'') AS method,
	 COALESCE(d.rule_id,0) AS rule_id,
	 COALESCE(d.channel_id,0) AS channel_id,
	 o.created_at`

func (s *Store) ListChannelSharingOrdersFiltered(ctx context.Context, q ChannelSharingOrderQuery, actor TransferBatchActor) (ChannelSharingOrderListPage, error) {
	out := ChannelSharingOrderListPage{Items: []ChannelSharingOrderListView{}, Limit: q.Limit}
	if !actor.valid(q.MerchantID) || q.Offset < 0 || q.Offset > 1_000_000 || q.Limit < 1 || q.Limit > 100 {
		return out, ErrInvalid
	}
	where, args, ok := q.where()
	if !ok {
		return out, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = tx.QueryRow(ctx, "SELECT count(*)"+where, args...).Scan(&out.Total); e != nil {
		return out, e
	}
	pageArgs := append(append([]any{}, args...), q.Limit, q.Offset)
	rows, e := tx.Query(ctx, sharingOrderListSelect+where+" ORDER BY o.id DESC LIMIT $"+strconv.Itoa(len(args)+1)+" OFFSET $"+strconv.Itoa(len(args)+2), pageArgs...)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var v ChannelSharingOrderListView
		if e = rows.Scan(&v.ID, &v.ExternalNo, &v.OutNo, &v.State, &v.ShareAmount, &v.OrderAmount, &v.Provider, &v.Method, &v.RuleID, &v.ChannelID, &v.CreatedAt); e != nil {
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	rows.Close()
	return out, tx.Commit(ctx)
}

func (s *Store) ChannelSharingOrderStatistics(ctx context.Context, q ChannelSharingOrderQuery, actor TransferBatchActor) (ChannelSharingOrderStats, error) {
	var stats ChannelSharingOrderStats
	if !actor.valid(q.MerchantID) {
		return stats, ErrInvalid
	}
	// Paging does not affect the aggregate; normalize to a valid shape.
	q.Offset, q.Limit = 0, 1
	where, args, ok := q.where()
	if !ok {
		return stats, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return stats, e
	}
	defer tx.Rollback(ctx)
	e = tx.QueryRow(ctx, `SELECT count(*),
	 COALESCE(SUM(COALESCE(si.share_amount,0)),0),
	 COALESCE(SUM(CASE WHEN c.order_id IS NOT NULL THEN c.release_amount ELSE 0 END),0),
	 COALESCE(SUM(CASE WHEN o.state='succeeded' THEN 1 ELSE 0 END),0),
	 COALESCE(SUM(CASE WHEN o.state='failed' THEN 1 ELSE 0 END),0)`+
		strings.Replace(where, " WHERE ", " LEFT JOIN channel_sharing_completions c ON c.order_id=o.order_id WHERE ", 1),
		args...).Scan(&stats.Count, &stats.ShareAmount, &stats.ReleasedAmount, &stats.SuccessCount, &stats.FailCount)
	if e != nil {
		return stats, e
	}
	return stats, tx.Commit(ctx)
}

// ChannelSharingOrderCSVQuery returns the fixed SELECT + args for the server CSV
// export, built only from validated filter expressions (server.exportCSV wraps
// it with the bounded row limit, BOM and formula-injection guard). Columns are
// Pro's 分账记录 headers.
func (q ChannelSharingOrderQuery) ChannelSharingOrderCSVQuery(actor TransferBatchActor) (string, []any, bool) {
	if !actor.valid(q.MerchantID) {
		return "", nil, false
	}
	q.Offset, q.Limit = 0, 1
	where, args, ok := q.where()
	if !ok {
		return "", nil, false
	}
	query := `SELECT o.external_no AS "系统订单号", o.out_no AS "接口订单号",
	 (COALESCE(si.share_amount,0)::numeric/100) AS "分账金额",
	 (COALESCE(d.gross_amount,0)::numeric/100) AS "订单金额",
	 COALESCE(d.provider,'') AS "支付方式",
	 COALESCE(d.rule_id,0) AS "分账规则",
	 to_char(o.created_at AT TIME ZONE 'UTC+08:00','YYYY-MM-DD HH24:MI:SS') AS "时间",
	 o.state AS "分账状态"` + where + " ORDER BY o.id DESC"
	return query, args, true
}

// parseMinorAmount reads a yuan string ("12.34", "1234", "0.5") as integer minor
// units (cents). Returns false for anything not a bounded non-negative amount
// with at most two decimal places.
func parseMinorAmount(s string) (int64, bool) {
	if s == "" || len(s) > 20 {
		return 0, false
	}
	neg := false
	body := s
	if strings.HasPrefix(body, "-") {
		neg, body = true, body[1:]
	}
	intPart, fracPart := body, ""
	if i := strings.IndexByte(body, '.'); i >= 0 {
		intPart, fracPart = body[:i], body[i+1:]
	}
	if intPart == "" && fracPart == "" {
		return 0, false
	}
	if len(fracPart) > 2 {
		return 0, false
	}
	whole := int64(0)
	if intPart != "" {
		v, e := strconv.ParseInt(intPart, 10, 64)
		if e != nil || v < 0 || v > 90_000_000_000_000 {
			return 0, false
		}
		whole = v
	}
	cents := int64(0)
	if fracPart != "" {
		for _, r := range fracPart {
			if r < '0' || r > '9' {
				return 0, false
			}
		}
		padded := (fracPart + "00")[:2]
		v, e := strconv.ParseInt(padded, 10, 64)
		if e != nil {
			return 0, false
		}
		cents = v
	}
	total := whole*100 + cents
	if neg {
		return 0, false
	}
	return total, true
}
