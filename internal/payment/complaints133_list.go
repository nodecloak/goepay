package payment

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Read-only projections of complaints133 for the console 交易投诉 pages. The
// provider-synced rows live here (not in the backoffice `complaints` resource),
// so these are the queries the Pro list/detail/summary/charts read. Merchant
// scope: owner_id must equal the caller's merchant id. Admin scope: owner 0 =
// unscoped. Everything the console exposes is gated by complain_open upstream.

// ComplaintListItem is one row of the 投诉记录 list.
type ComplaintListItem struct {
	ID         string   `json:"id"`
	PayType    string   `json:"pay_type"`
	ThirdID    string   `json:"third_id"`
	ThirdMchID string   `json:"third_mch_id"`
	ChannelID  string   `json:"channel_id"`
	OwnerID    string   `json:"owner_id"`
	TradeNos   []string `json:"trade_nos"`
	TradeNo    string   `json:"trade_no"` // first linked order, for the column/refund
	Category   string   `json:"category"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Times      string   `json:"times"`
	Amount     string   `json:"amount"` // integer cents or "" (unknown)
	Status     string   `json:"status"` // local_state: open|processing|done
	Phone      string   `json:"phone"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
}

// ComplaintListFilter carries the server-side filters the 搜索字段/状态/支付方式
// controls offer. Empty fields are not applied.
type ComplaintListFilter struct {
	OwnerScope int64 // >0 = merchant scope (owner_id must match); 0 = admin
	Field      string
	Keyword    string
	Status     string // open|processing|done
	PayType    string // wxpay|alipay
	Page       int
	Limit      int
}

// complaintSearchColumns maps a 搜索字段 option to its complaints133 predicate.
// 关联订单 matches the trade_nos array (GIN-indexed, migration 000140).
func complaintSearchPredicate(field string, argPos int) (string, bool) {
	switch field {
	case "trade_no":
		return "$" + itoaPos(argPos) + "=ANY(trade_nos)", true
	case "third_id":
		return "third_id=$" + itoaPos(argPos), true
	case "category":
		return "category ILIKE '%'||$" + itoaPos(argPos) + "||'%'", true
	case "title":
		return "title ILIKE '%'||$" + itoaPos(argPos) + "||'%'", true
	case "content":
		return "content ILIKE '%'||$" + itoaPos(argPos) + "||'%'", true
	case "phone":
		return "phone=$" + itoaPos(argPos), true
	case "third_mch_id":
		return "third_mch_id=$" + itoaPos(argPos), true
	case "merchant_id":
		return "owner_id::text=$" + itoaPos(argPos), true
	}
	return "", false
}

func itoaPos(n int) string {
	return strconv.Itoa(n)
}

// ComplaintList returns a filtered, offset-paged page with a total.
func (s *Store) ComplaintList(ctx context.Context, f ComplaintListFilter) ([]ComplaintListItem, int64, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}
	where := []string{"true"}
	args := []any{}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(clause, "$?", "$"+itoaPos(len(args))))
	}
	if f.OwnerScope > 0 {
		add("owner_id=$?", f.OwnerScope)
	}
	if f.Status == "open" || f.Status == "processing" || f.Status == "done" {
		add("local_state=$?", f.Status)
	}
	if f.PayType == "wxpay" || f.PayType == "alipay" {
		add("pay_type=$?", f.PayType)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		if f.Field == "" {
			// 综合搜索: order no OR third id OR content.
			args = append(args, kw)
			p := itoaPos(len(args))
			where = append(where, "($"+p+"=ANY(trade_nos) OR third_id=$"+p+" OR content ILIKE '%'||$"+p+"||'%')")
		} else if pred, ok := complaintSearchPredicate(f.Field, len(args)+1); ok {
			args = append(args, kw)
			where = append(where, pred)
		}
	}
	cond := strings.Join(where, " AND ")

	var total int64
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM complaints133 WHERE "+cond, args...).Scan(&total); e != nil {
		return nil, 0, e
	}
	limArg := itoaPos(len(args) + 1)
	offArg := itoaPos(len(args) + 2)
	args = append(args, f.Limit, (f.Page-1)*f.Limit)
	rows, e := s.pool.Query(ctx, `SELECT id,pay_type,third_id,COALESCE(third_mch_id,''),channel_id,COALESCE(owner_id,0),trade_nos,
 COALESCE(category,''),COALESCE(title,''),COALESCE(content,''),complaint_times,amount_cents,local_state,COALESCE(phone,''),first_seen_at,last_seen_at
 FROM complaints133 WHERE `+cond+` ORDER BY id DESC LIMIT $`+limArg+` OFFSET $`+offArg, args...)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	items := []ComplaintListItem{}
	for rows.Next() {
		var it ComplaintListItem
		var id, channel, owner int64
		var times int
		var amount *int64
		var created, updated time.Time
		var tradeNos []string
		if e = rows.Scan(&id, &it.PayType, &it.ThirdID, &it.ThirdMchID, &channel, &owner, &tradeNos,
			&it.Category, &it.Title, &it.Content, &times, &amount, &it.Status, &it.Phone, &created, &updated); e != nil {
			return nil, 0, e
		}
		it.ID = itoa64(id)
		it.ChannelID = itoa64(channel)
		if owner > 0 {
			it.OwnerID = itoa64(owner)
		}
		it.TradeNos = tradeNos
		if len(tradeNos) > 0 {
			it.TradeNo = tradeNos[0]
		}
		it.Times = itoa64(int64(times))
		if amount != nil {
			it.Amount = itoa64(*amount)
		}
		it.CreatedAt = created.UTC().Format(time.RFC3339)
		it.UpdatedAt = updated.UTC().Format(time.RFC3339)
		items = append(items, it)
	}
	return items, total, rows.Err()
}

// ComplaintDetailView adds the verified provider detail (images + negotiation
// history) to a list item.
type ComplaintDetailView struct {
	ComplaintListItem
	Images      []string               `json:"images"`
	Negotiation []ComplaintHistoryItem `json:"negotiation"`
	Handling    json.RawMessage        `json:"handling"`
}

type ComplaintHistoryItem struct {
	Operator  string   `json:"operator"`
	Role      string   `json:"role"`
	Content   string   `json:"content"`
	Images    []string `json:"images"`
	CreatedAt string   `json:"created_at"`
}

// ComplaintDetail reads one complaint (scoped) with its images + reply history,
// extracted from the stored verified provider detail body.
func (s *Store) ComplaintDetail(ctx context.Context, id, ownerScope int64) (ComplaintDetailView, error) {
	var v ComplaintDetailView
	var pk, channel, owner int64
	var times int
	var amount *int64
	var created, updated time.Time
	var tradeNos []string
	var detail, handling []byte
	e := s.pool.QueryRow(ctx, `SELECT id,pay_type,third_id,COALESCE(third_mch_id,''),channel_id,COALESCE(owner_id,0),trade_nos,
 COALESCE(category,''),COALESCE(title,''),COALESCE(content,''),complaint_times,amount_cents,local_state,COALESCE(phone,''),first_seen_at,last_seen_at,detail,handling
 FROM complaints133 WHERE id=$1 AND ($2::bigint=0 OR owner_id=$2)`, id, ownerScope).Scan(
		&pk, &v.PayType, &v.ThirdID, &v.ThirdMchID, &channel, &owner, &tradeNos,
		&v.Category, &v.Title, &v.Content, &times, &amount, &v.Status, &v.Phone, &created, &updated, &detail, &handling)
	if e != nil {
		if e == pgx.ErrNoRows {
			return v, ErrNotFound
		}
		return v, e
	}
	v.ID = itoa64(pk)
	v.ChannelID = itoa64(channel)
	if owner > 0 {
		v.OwnerID = itoa64(owner)
	}
	v.TradeNos = tradeNos
	if len(tradeNos) > 0 {
		v.TradeNo = tradeNos[0]
	}
	v.Times = itoa64(int64(times))
	if amount != nil {
		v.Amount = itoa64(*amount)
	}
	v.CreatedAt = created.UTC().Format(time.RFC3339)
	v.UpdatedAt = updated.UTC().Format(time.RFC3339)
	if len(handling) > 0 {
		v.Handling = json.RawMessage(handling)
	}
	v.Images, v.Negotiation = extractComplaintDetail(v.PayType, detail)
	if v.Images == nil {
		v.Images = []string{}
	}
	if v.Negotiation == nil {
		v.Negotiation = []ComplaintHistoryItem{}
	}
	return v, nil
}

// extractComplaintDetail pulls images + negotiation history out of the stored
// verified provider body (best-effort; an unparseable body yields empties).
func extractComplaintDetail(payType string, detail []byte) ([]string, []ComplaintHistoryItem) {
	if len(detail) == 0 {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(detail, &obj) != nil {
		return nil, nil
	}
	images := []string{}
	history := []ComplaintHistoryItem{}
	// Negotiation turns carry their own images; collect both.
	histKey := "complaint_full_refund"
	if payType == "alipay" {
		histKey = "reply_detail_infos"
	}
	var list []map[string]json.RawMessage
	if raw := obj[histKey]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &list)
	}
	for _, turn := range list {
		h := ComplaintHistoryItem{
			Operator:  jsonStr(turn, "operator"),
			Role:      firstNonEmpty(jsonStr(turn, "operate_type"), jsonStr(turn, "reply_role")),
			Content:   firstNonEmpty(jsonStr(turn, "operate_details"), jsonStr(turn, "reply_content")),
			CreatedAt: firstNonEmpty(jsonStr(turn, "operate_time"), jsonStr(turn, "reply_time")),
		}
		for _, k := range []string{"image_list", "reply_images"} {
			if raw := turn[k]; len(raw) > 0 {
				var imgs []string
				if json.Unmarshal(raw, &imgs) == nil {
					h.Images = append(h.Images, imgs...)
					images = append(images, imgs...)
				}
			}
		}
		history = append(history, h)
	}
	return images, history
}

func jsonStr(m map[string]json.RawMessage, key string) string {
	raw := m[key]
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ComplaintSummaryView is the today/yesterday banner + charts summary computed
// from complaints133 (not the local backoffice resource).
type ComplaintSummaryView struct {
	TodayCount      string `json:"today_count"`
	TodayAlipay     string `json:"today_alipay"`
	TodayWxpay      string `json:"today_wxpay"`
	YesterdayCount  string `json:"yesterday_count"`
	YesterdayAlipay string `json:"yesterday_alipay"`
	YesterdayWxpay  string `json:"yesterday_wxpay"`
}

// ComplaintSummary counts today's and yesterday's complaints (by first_seen_at,
// UTC+8 business day), scoped to an owner for merchants.
func (s *Store) ComplaintSummary(ctx context.Context, ownerScope int64) (ComplaintSummaryView, error) {
	var v ComplaintSummaryView
	var tc, ta, tw, yc, ya, yw int64
	e := s.pool.QueryRow(ctx, `WITH d AS (SELECT (now() AT TIME ZONE 'Asia/Shanghai')::date AS today)
 SELECT
  count(*) FILTER(WHERE (first_seen_at AT TIME ZONE 'Asia/Shanghai')::date=d.today),
  count(*) FILTER(WHERE (first_seen_at AT TIME ZONE 'Asia/Shanghai')::date=d.today AND pay_type='alipay'),
  count(*) FILTER(WHERE (first_seen_at AT TIME ZONE 'Asia/Shanghai')::date=d.today AND pay_type='wxpay'),
  count(*) FILTER(WHERE (first_seen_at AT TIME ZONE 'Asia/Shanghai')::date=d.today-1),
  count(*) FILTER(WHERE (first_seen_at AT TIME ZONE 'Asia/Shanghai')::date=d.today-1 AND pay_type='alipay'),
  count(*) FILTER(WHERE (first_seen_at AT TIME ZONE 'Asia/Shanghai')::date=d.today-1 AND pay_type='wxpay')
 FROM complaints133,d WHERE ($1::bigint=0 OR owner_id=$1)`, ownerScope).Scan(&tc, &ta, &tw, &yc, &ya, &yw)
	if e != nil {
		return v, e
	}
	v.TodayCount, v.TodayAlipay, v.TodayWxpay = itoa64(tc), itoa64(ta), itoa64(tw)
	v.YesterdayCount, v.YesterdayAlipay, v.YesterdayWxpay = itoa64(yc), itoa64(ya), itoa64(yw)
	return v, nil
}

func itoa64(n int64) string {
	return strconv.FormatInt(n, 10)
}

// ComplaintChartRow is one 排行榜 row (per merchant) for 投诉统计, from
// complaints133. Counts are today's, by first_seen_at in UTC+8.
type ComplaintChartRow struct {
	Dimension   string `json:"dimension"` // merchant id ("-" when unknown)
	TodayCount  string `json:"today_count"`
	TodayAlipay string `json:"today_alipay"`
	TodayWxpay  string `json:"today_wxpay"`
	TotalCount  string `json:"total_count"`
}

// ComplaintChartRanking returns the per-merchant complaint ranking (today desc,
// then total desc), scoped to an owner for merchants, from complaints133.
func (s *Store) ComplaintChartRanking(ctx context.Context, ownerScope int64, limit int) ([]ComplaintChartRow, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, e := s.pool.Query(ctx, `WITH d AS (SELECT (now() AT TIME ZONE 'Asia/Shanghai')::date AS today)
 SELECT COALESCE(owner_id::text,'-') AS dimension,
  count(*) FILTER(WHERE (first_seen_at AT TIME ZONE 'Asia/Shanghai')::date=d.today) AS today_count,
  count(*) FILTER(WHERE (first_seen_at AT TIME ZONE 'Asia/Shanghai')::date=d.today AND pay_type='alipay') AS today_alipay,
  count(*) FILTER(WHERE (first_seen_at AT TIME ZONE 'Asia/Shanghai')::date=d.today AND pay_type='wxpay') AS today_wxpay,
  count(*) AS total_count
 FROM complaints133,d WHERE ($1::bigint=0 OR owner_id=$1)
 GROUP BY owner_id ORDER BY today_count DESC,total_count DESC LIMIT $2`, ownerScope, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ComplaintChartRow{}
	for rows.Next() {
		var r ComplaintChartRow
		var tc, ta, tw, total int64
		if e = rows.Scan(&r.Dimension, &tc, &ta, &tw, &total); e != nil {
			return nil, e
		}
		r.TodayCount, r.TodayAlipay, r.TodayWxpay, r.TotalCount = itoa64(tc), itoa64(ta), itoa64(tw), itoa64(total)
		out = append(out, r)
	}
	return out, rows.Err()
}
