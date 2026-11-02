package payment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/ledger"
	"goepay/internal/noticesource231"
	"goepay/internal/wxtransferlink231"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// The entire set must belong to one merchant and the original payment account.
// Missing, mixed or ambiguous orders produce an administrator-only complaint.
func (s *Store) complaintOwner231(ctx context.Context, tx pgx.Tx, ch int64, kind string, tradeNos []string, p adapter.Provider) (*int64, []string, error) {
	if p == nil || len(tradeNos) == 0 || len(tradeNos) > 100 {
		return nil, nil, nil
	}
	unique := map[string]bool{}
	for _, n := range tradeNos {
		if n == "" || len(n) > 128 {
			return nil, nil, nil
		}
		unique[n] = true
	}
	nos := make([]string, 0, len(unique))
	for n := range unique {
		nos = append(nos, n)
	}
	sort.Strings(nos)
	rows, e := tx.Query(ctx, `SELECT o.merchant_id,o.trade_no,a.id FROM orders o JOIN verified_payment_receipts v ON v.order_id=o.id AND v.channel_id=$1 JOIN provider_attempts a ON a.order_id=o.id AND a.channel_id=v.channel_id WHERE o.type=$2 AND o.trade_no=ANY($3::text[]) ORDER BY o.trade_no`, ch, kind, nos)
	if e != nil {
		return nil, nil, e
	}
	type match struct {
		mid, attempt int64
		no           string
	}
	var matched []match
	for rows.Next() {
		var x match
		if e = rows.Scan(&x.mid, &x.no, &x.attempt); e != nil {
			rows.Close()
			return nil, nil, e
		}
		matched = append(matched, x)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, nil, e
	}
	if len(matched) != len(nos) {
		return nil, nil, nil
	}
	mid := matched[0].mid
	for i, x := range matched {
		if x.mid != mid || x.no != nos[i] {
			return nil, nil, nil
		}
		if e = s.checkProviderAccount(ctx, tx, x.attempt, p); e != nil {
			if e == ErrConflict || e == ErrInvalid {
				return nil, nil, nil
			}
			return nil, nil, e
		}
	}
	return &mid, nos, nil
}
func (s *Store) recordComplaintNotices231(ctx context.Context, tx pgx.Tx, id, ch int64, rec adapter.ComplaintRecord, owner *int64, trades []string) {
	child, e := tx.Begin(ctx)
	if e != nil {
		return
	}
	defer child.Rollback(ctx)
	var fresh bool
	if e = child.QueryRow(ctx, `SELECT notification_transaction231=pg_current_xact_id() FROM complaints133 WHERE id=$1`, id).Scan(&fresh); e != nil || !fresh {
		return
	}
	mid := int64(0)
	if owner != nil {
		mid = *owner
	}
	params := map[string]string{"type": map[string]string{"wxpay": "微信支付", "alipay": "支付宝"}[rec.PayType], "trade_no": strings.Join(rec.TradeNos, ","), "title": rec.Title, "content": rec.Content, "name": "交易投诉", "money": "0.00", "time": time.Now().In(time.FixedZone("UTC+8", 28800)).Format("2006-01-02 15:04:05")}
	if params["title"] == "" {
		params["title"] = "交易投诉"
	}
	if params["content"] == "" {
		params["content"] = "请前往投诉详情核对"
	}
	if params["trade_no"] == "" {
		params["trade_no"] = "未匹配平台订单"
	}
	if rec.AmountCents != nil {
		params["money"] = ledger.Format(*rec.AmountCents)
	}
	if mid > 0 && len(trades) > 0 {
		params["trade_no"] = trades[0]
		var name string
		if child.QueryRow(ctx, `SELECT name FROM orders WHERE trade_no=$1 AND merchant_id=$2`, trades[0], mid).Scan(&name) == nil && name != "" {
			params["name"] = name
		}
	}
	// Notifications are a bounded preview; the immutable complaint retains the
	// complete provider order set and details for the authenticated detail page.
	for key, value := range params {
		params[key] = noticePreview231(value, "请前往投诉详情核对", key == "content")
	}
	source, inserted, e := noticesource231.Insert(ctx, child, fmt.Sprintf("complaint:%d", id), noticesource231.Facts{Kind: "complain", MerchantID: mid, ReferenceID: id, ChannelID: ch, Params: params})
	if e != nil || !inserted {
		return
	}
	if s.notice != nil && mid > 0 {
		if e = s.notice.CaptureSupplement231(ctx, child, source); e != nil {
			return
		}
	}
	if s.business != nil {
		if e = s.business.CaptureSupplement231(ctx, child, source); e != nil {
			return
		}
	}
	_ = child.Commit(ctx)
}
func noticePreview231(value, fallback string, multiline bool) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !(multiline && (r == '\n' || r == '\t')) {
			return ' '
		}
		return r
	}, value)
	if value == "" {
		return fallback
	}
	end := 0
	for i, r := range value {
		size := len(string(r))
		if i+size > 4096 {
			break
		}
		end = i + size
	}
	return value[:end]
}

func (s *Store) recordChannelLimitNotice231(ctx context.Context, tx pgx.Tx, r parentSuccessEvent) {
	if s.business == nil {
		return
	}
	child, e := tx.Begin(ctx)
	if e != nil {
		return
	}
	defer child.Rollback(ctx)
	var b RuntimeChannel
	b.id = r.Channel
	if e = child.QueryRow(ctx, `SELECT pay_type,identity_hash FROM runtime_channel_limits WHERE channel_id=$1`, r.Channel).Scan(&b.kind, &b.identity); e != nil {
		return
	}
	policy, configured, e := s.globalLimitPolicyTx(ctx, child, b, false)
	if e != nil || !configured {
		return
	}
	usage, _, money, e := s.globalLimitUsage(ctx, child, r.Channel, policy.Limits, r.ObservedAt)
	if e != nil {
		return
	}
	count, e := strconv.ParseInt(usage.SuccessfulCount, 10, 64)
	if e != nil {
		return
	}
	reasons := []string{}
	if policy.Limits.DayTop > 0 && money.Cmp(big.NewInt(policy.Limits.DayTop)) >= 0 && new(big.Int).Sub(new(big.Int).Set(money), big.NewInt(r.RealMoney)).Cmp(big.NewInt(policy.Limits.DayTop)) < 0 {
		reasons = append(reasons, "今日成功金额达到限额")
	}
	if policy.Limits.DayMaxOrder > 0 && count >= policy.Limits.DayMaxOrder && count-1 < policy.Limits.DayMaxOrder {
		reasons = append(reasons, "今日成功笔数达到限额")
	}
	for _, reason := range reasons {
		f := noticesource231.Facts{Kind: "channellimit", ReferenceID: r.OrderID, ChannelID: r.Channel, PolicyRevision: policy.Revision, Day: r.Day, Reason: reason, PolicyHash: hex.EncodeToString(policy.Proof), Params: map[string]string{"name": fmt.Sprintf("支付通道 %d", r.Channel), "id": fmt.Sprint(r.Channel), "reason": reason}}
		source, inserted, err := noticesource231.Insert(ctx, child, fmt.Sprintf("limit:%d:%s:%d:%s", r.Channel, r.Day, policy.Revision, reason), f)
		if err != nil {
			return
		}
		if inserted {
			if e = s.business.CaptureSupplement231(ctx, child, source); e != nil {
				return
			}
		}
	}
	_ = child.Commit(ctx)
}

func (s *Store) recordTransferConfirmNotice231(ctx context.Context, tx pgx.Tx, id int64, pkg string, p adapter.Provider) {
	if s.notice == nil || p == nil || !merchantTransferChannel(p, "wxpay") {
		return
	}
	app, ok := p.(interface{ ApplicationID() string })
	if !ok || app.ApplicationID() == "" {
		return
	}
	child, e := tx.Begin(ctx)
	if e != nil {
		return
	}
	defer child.Rollback(ctx)
	t, e := s.scanTransfer(child.QueryRow(ctx, "SELECT "+transferColumns+transferFrom+" WHERE t.id=$1", id))
	if e != nil || t.Type != "wxpay" || t.Account == "" {
		return
	}
	if e = s.checkTransferAccount(ctx, child, id, p); e != nil {
		return
	}
	var created time.Time
	if e = child.QueryRow(ctx, `SELECT created_at FROM transfers WHERE id=$1`, id).Scan(&created); e != nil {
		return
	}
	sum := sha256.Sum256([]byte(pkg))
	f := noticesource231.Facts{Kind: "wxtransfer", MerchantID: t.MerchantID, ReferenceID: id, ChannelID: t.ChannelID, Recipient: t.Account, AppID: app.ApplicationID(), PackageHash: hex.EncodeToString(sum[:]), Params: map[string]string{"money": ledger.Format(t.Amount), "time": created.In(time.FixedZone("UTC+8", 28800)).Format("2006-01-02 15:04:05"), "confirm_path": wxtransferlink231.Path(s.confirmationKey231, id, created)}}
	stamp, e := s.notice.WechatTransferReady231(ctx, child)
	if e != nil {
		return
	}
	f.Params["wx_template231"] = stamp
	source, inserted, e := noticesource231.Insert(ctx, child, fmt.Sprintf("wxtransfer:%d", id), f)
	if e != nil || !inserted {
		return
	}
	if e = s.notice.CaptureSupplement231(ctx, child, source); e != nil {
		return
	}
	_ = child.Commit(ctx)
}
