package payment

import (
	"context"
	"encoding/json"
	"strings"
)

// Alipay 交易投诉处理 wire adapter. Mirrors the original
// Alipay\AlipayComplainService (alipay.merchant.tradecomplain.*). It reuses the
// existing *Alipay client's AOP call path (SignAlipayRSA2 + verifyResponse on
// every exchange), so no new signing path is introduced. Like the WeChat
// adapter it does wire I/O only and never settles money or guesses an outcome.

// AlipayComplaint reuses an existing verified *Alipay client.
type AlipayComplaint struct{ client *Alipay }

// NewAlipayComplaint builds the adapter from the same trusted config a payment
// channel uses.
func NewAlipayComplaint(c AlipayConfig) (*AlipayComplaint, error) {
	client, e := NewAlipay(c)
	if e != nil {
		return nil, e
	}
	return &AlipayComplaint{client: client}, nil
}

func (a *AlipayComplaint) ID() string            { return "alipay" }
func (a *AlipayComplaint) ApplicationID() string { return a.client.config.AppID }

// alipayComplaintTime validates the "yyyy-MM-dd HH:mm:ss" window Alipay expects.
func alipayComplaintTime(s string) bool {
	if s == "" {
		return true // optional
	}
	if len(s) != 19 || strings.ContainsAny(s, "\x00\r\n") {
		return false
	}
	return true
}

// BatchQuery lists complaints; status/beginTime/endTime are optional,
// pageNum>=1, pageSize 1..20 (Alipay's cap).
func (a *AlipayComplaint) BatchQuery(ctx context.Context, status, beginTime, endTime string, pageNum, pageSize int) (ComplaintPage, error) {
	if pageNum < 1 || pageSize < 1 || pageSize > 20 || !alipayComplaintTime(beginTime) || !alipayComplaintTime(endTime) {
		return ComplaintPage{}, ErrInvalid
	}
	biz := map[string]any{"page_num": pageNum, "page_size": pageSize}
	if status != "" {
		biz["status"] = status
	}
	if beginTime != "" {
		biz["begin_time"] = beginTime
	}
	if endTime != "" {
		biz["end_time"] = endTime
	}
	out, e := a.client.call(ctx, "alipay.merchant.tradecomplain.batchquery", biz, false)
	if e != nil {
		return ComplaintPage{}, e
	}
	page := ComplaintPage{Limit: pageSize, Offset: pageSize * (pageNum - 1)}
	page.TotalCount = int(alipayJSONInt(out, "total_num"))
	var list []json.RawMessage
	if len(out["trade_complain_infos"]) > 0 {
		if json.Unmarshal(out["trade_complain_infos"], &list) != nil {
			return ComplaintPage{}, ErrAuthentication
		}
	}
	for _, item := range list {
		rec, e := a.decodeRecord(item)
		if e != nil {
			return ComplaintPage{}, e
		}
		page.Items = append(page.Items, rec)
	}
	return page, nil
}

// Query reads one complaint detail by the Alipay complain_event_id.
func (a *AlipayComplaint) Query(ctx context.Context, eventID string) (ComplaintRecord, error) {
	if !validComplaintID(eventID) {
		return ComplaintRecord{}, ErrInvalid
	}
	out, e := a.client.call(ctx, "alipay.merchant.tradecomplain.query", map[string]any{"complain_event_id": eventID}, false)
	if e != nil {
		return ComplaintRecord{}, e
	}
	return a.decodeNode(out, eventID)
}

// FeedbackSubmit handles the complaint (商家处理交易投诉). feedbackCode is the
// Alipay feedback category id; images is a comma-joined id list, optional.
func (a *AlipayComplaint) FeedbackSubmit(ctx context.Context, eventID, feedbackCode, content string, images []string) error {
	if !validComplaintID(eventID) || !validComplaintText(feedbackCode, 64) || !validComplaintText(content, 1000) || !validMediaIDs(images, 10) {
		return ErrInvalid
	}
	biz := map[string]any{"complain_event_id": eventID, "feedback_code": feedbackCode, "feedback_content": content}
	if len(images) > 0 {
		biz["feedback_images"] = strings.Join(images, ",")
	}
	return a.write(ctx, "alipay.merchant.tradecomplain.feedback.submit", biz)
}

// ReplySubmit leaves a reply to the complainant (商家留言回复).
func (a *AlipayComplaint) ReplySubmit(ctx context.Context, eventID, content string, images []string) error {
	if !validComplaintID(eventID) || !validComplaintText(content, 1000) || !validMediaIDs(images, 10) {
		return ErrInvalid
	}
	biz := map[string]any{"complain_event_id": eventID, "reply_content": content}
	if len(images) > 0 {
		biz["reply_images"] = strings.Join(images, ",")
	}
	return a.write(ctx, "alipay.merchant.tradecomplain.reply.submit", biz)
}

// SupplementSubmit supplies supplementary evidence (商家补充凭证, 仅支付宝).
func (a *AlipayComplaint) SupplementSubmit(ctx context.Context, eventID, content string, images []string) error {
	if !validComplaintID(eventID) || !validComplaintText(content, 1000) || !validMediaIDs(images, 10) {
		return ErrInvalid
	}
	biz := map[string]any{"complain_event_id": eventID, "supplement_content": content}
	if len(images) > 0 {
		biz["supplement_images"] = strings.Join(images, ",")
	}
	return a.write(ctx, "alipay.merchant.tradecomplain.supplement.submit", biz)
}

// write runs a mutating AOP call (write=true so a transport failure is an
// UnknownOutcome) and requires the standard 10000 success code, which
// Alipay.call already enforces before returning.
func (a *AlipayComplaint) write(ctx context.Context, method string, biz map[string]any) error {
	_, e := a.client.call(ctx, method, biz, true)
	return e
}

func (a *AlipayComplaint) decodeRecord(raw json.RawMessage) (ComplaintRecord, error) {
	var out map[string]json.RawMessage
	if json.Unmarshal(raw, &out) != nil {
		return ComplaintRecord{}, ErrAuthentication
	}
	return a.decodeNode(out, "")
}

func (a *AlipayComplaint) decodeNode(out map[string]json.RawMessage, wantID string) (ComplaintRecord, error) {
	id := text(out, "complain_event_id")
	if id == "" || !validComplaintID(id) || wantID != "" && id != wantID {
		return ComplaintRecord{}, ErrAuthentication
	}
	raw, _ := json.Marshal(out)
	rec := ComplaintRecord{
		ThirdID:    id,
		PayType:    "alipay",
		Type:       text(out, "leaf_category_name"),
		Title:      text(out, "complain_reason"),
		Content:    text(out, "content"),
		Phone:      text(out, "phone_no"),
		Status:     text(out, "status"),
		CreatedAt:  text(out, "gmt_create"),
		ModifiedAt: text(out, "gmt_modified"),
		Times:      1,
		Raw:        raw,
	}
	if tn := text(out, "merchant_order_no"); tn != "" {
		rec.TradeNos = append(rec.TradeNos, tn)
	}
	if amt := text(out, "trade_amount"); amt != "" {
		if cents, e := parseYuan(amt); e == nil {
			rec.AmountCents = &cents
		}
	}
	if hist, e := decodeAlipayReplies(out["reply_detail_infos"]); e == nil && len(hist) > 0 {
		rec.Negotiation = hist
	}
	return rec, nil
}

func decodeAlipayReplies(raw json.RawMessage) ([]ComplaintHistory, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var list []map[string]json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		return nil, ErrAuthentication
	}
	out := make([]ComplaintHistory, 0, len(list))
	for _, obj := range list {
		h := ComplaintHistory{
			Role:      text(obj, "reply_role"),
			Content:   text(obj, "reply_content"),
			CreatedAt: text(obj, "reply_time"),
		}
		if len(obj["reply_images"]) > 0 {
			var imgs []string
			if json.Unmarshal(obj["reply_images"], &imgs) == nil {
				h.Images = imgs
			}
		}
		out = append(out, h)
	}
	return out, nil
}

// alipayJSONInt reads an Alipay numeric field that may arrive as a number or a
// quoted string.
func alipayJSONInt(p map[string]json.RawMessage, key string) int64 {
	raw := p[key]
	if len(raw) == 0 {
		return 0
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		s = strings.TrimSpace(s)
		var v int64
		for _, c := range s {
			if c < '0' || c > '9' {
				return 0
			}
			v = v*10 + int64(c-'0')
		}
		return v
	}
	return 0
}
