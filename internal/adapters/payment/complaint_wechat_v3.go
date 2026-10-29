package payment

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// WeChat 消费者投诉2.0 wire adapter. Mirrors the original
// WeChatPay\V3\ComplainService (/v3/merchant-service/complaints-v2 ...). It
// reuses the existing wechatV3Client (SignWechatV3Request + VerifyWechatV3Message
// on every request and response) so no new signing path is introduced. The
// adapter performs wire I/O only: it never settles money, never records a
// ledger entry, and never guesses an unknown outcome — a transport failure or
// a 5xx surfaces as *UnknownOutcome, a definitive 4xx as *ProviderError.

// ComplaintRecord is the provider-neutral shape the console/sync task persists.
// Amounts are integer cents; an unknown amount stays nil, never zero.
type ComplaintRecord struct {
	ThirdID     string             `json:"third_id"`               // provider complaint id
	ThirdMchID  string             `json:"third_mch_id,omitempty"` // complained merchant id
	PayType     string             `json:"pay_type"`               // "wxpay" | "alipay"
	Type        string             `json:"type"`                   // provider complaint category/reason
	Title       string             `json:"title,omitempty"`
	Content     string             `json:"content,omitempty"`
	Times       int                `json:"times"`
	AmountCents *int64             `json:"amount_cents,omitempty"`
	Status      string             `json:"status"` // provider status string, verbatim
	Phone       string             `json:"phone,omitempty"`
	TradeNos    []string           `json:"trade_nos,omitempty"`
	CreatedAt   string             `json:"created_at,omitempty"` // RFC3339 or provider format, verbatim
	ModifiedAt  string             `json:"modified_at,omitempty"`
	Negotiation []ComplaintHistory `json:"negotiation,omitempty"`
	Raw         json.RawMessage    `json:"-"` // verified upstream body, for the caller's audit
	Extra       map[string]string  `json:"extra,omitempty"`
}

// ComplaintHistory is one协商历史 turn.
type ComplaintHistory struct {
	Operator  string   `json:"operator"`
	Role      string   `json:"role"`
	Content   string   `json:"content"`
	Images    []string `json:"images,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
}

// ComplaintPage is one page of batchQuery.
type ComplaintPage struct {
	Limit      int               `json:"limit"`
	Offset     int               `json:"offset"`
	TotalCount int               `json:"total_count"`
	Items      []ComplaintRecord `json:"items"`
}

var ErrComplaint = errors.New("complaint operation unavailable")

// WechatV3Complaint reuses an existing verified wechatV3Client.
type WechatV3Complaint struct {
	client *wechatV3Client
	mchID  string
}

// NewWechatV3Complaint builds the adapter from the same trusted config a
// payment channel uses. The complained merchant id defaults to the channel's
// own MerchantID (the direct-merchant case the owner runs).
func NewWechatV3Complaint(c WechatV3Config) (*WechatV3Complaint, error) {
	client, e := newWechatV3Client(c)
	if e != nil {
		return nil, e
	}
	return &WechatV3Complaint{client: client, mchID: c.MerchantID}, nil
}

func (w *WechatV3Complaint) ID() string            { return "wxpay" }
func (w *WechatV3Complaint) ApplicationID() string { return w.client.config.AppID }

// complaintDate validates yyyy-MM-dd (the batch query window the original uses).
func complaintDate(s string) bool {
	if len(s) != 10 {
		return false
	}
	_, e := time.Parse("2006-01-02", s)
	return e == nil
}

// BatchQuery lists complaints for [beginDate,endDate] (inclusive, yyyy-MM-dd),
// pageNo starting at 1, pageSize 1..50 — exactly the original paging math.
func (w *WechatV3Complaint) BatchQuery(ctx context.Context, beginDate, endDate string, pageNo, pageSize int) (ComplaintPage, error) {
	if !complaintDate(beginDate) || !complaintDate(endDate) || pageNo < 1 || pageSize < 1 || pageSize > 50 {
		return ComplaintPage{}, ErrInvalid
	}
	offset := pageSize * (pageNo - 1)
	q := url.Values{
		"limit":      {strconv.Itoa(pageSize)},
		"offset":     {strconv.Itoa(offset)},
		"begin_date": {beginDate},
		"end_date":   {endDate},
	}
	raw, e := w.client.call(ctx, "GET", "/v3/merchant-service/complaints-v2?"+q.Encode(), nil)
	if e != nil {
		return ComplaintPage{}, e
	}
	return w.parsePage(raw, offset, pageSize)
}

func (w *WechatV3Complaint) parsePage(raw []byte, offset, limit int) (ComplaintPage, error) {
	out, e := v3Object(raw)
	if e != nil {
		return ComplaintPage{}, e
	}
	page := ComplaintPage{Limit: limit, Offset: offset, TotalCount: int(v3Int(out, "total_count"))}
	var list []json.RawMessage
	if len(out["data"]) > 0 {
		if json.Unmarshal(out["data"], &list) != nil {
			return ComplaintPage{}, ErrAuthentication
		}
	}
	for _, item := range list {
		rec, e := w.decodeRecord(item)
		if e != nil {
			return ComplaintPage{}, e
		}
		page.Items = append(page.Items, rec)
	}
	return page, nil
}

// Query reads one complaint detail by id.
func (w *WechatV3Complaint) Query(ctx context.Context, complaintID string) (ComplaintRecord, error) {
	if !validComplaintID(complaintID) {
		return ComplaintRecord{}, ErrInvalid
	}
	raw, e := w.client.call(ctx, "GET", "/v3/merchant-service/complaints-v2/"+url.PathEscape(complaintID), nil)
	if e != nil {
		return ComplaintRecord{}, e
	}
	return w.decodeRecord(raw)
}

// QueryHistory reads the协商历史 of one complaint.
func (w *WechatV3Complaint) QueryHistory(ctx context.Context, complaintID string) ([]ComplaintHistory, error) {
	if !validComplaintID(complaintID) {
		return nil, ErrInvalid
	}
	raw, e := w.client.call(ctx, "GET", "/v3/merchant-service/complaints-v2/"+url.PathEscape(complaintID)+"/negotiation-historys", nil)
	if e != nil {
		return nil, e
	}
	out, e := v3Object(raw)
	if e != nil {
		return nil, e
	}
	return decodeHistory(out["data"])
}

// Response replies to the complainant (回复用户).
func (w *WechatV3Complaint) Response(ctx context.Context, complaintID, content string, images []string) error {
	if !validComplaintID(complaintID) || !validComplaintText(content, 200) || !validMediaIDs(images, 4) {
		return ErrInvalid
	}
	body := map[string]any{"complainted_mchid": w.mchID, "response_content": content}
	if len(images) > 0 {
		body["response_images"] = images
	}
	return w.post(ctx, "/v3/merchant-service/complaints-v2/"+url.PathEscape(complaintID)+"/response", body)
}

// Complete marks the complaint handled (反馈处理完成, 仅微信).
func (w *WechatV3Complaint) Complete(ctx context.Context, complaintID string) error {
	if !validComplaintID(complaintID) {
		return ErrInvalid
	}
	return w.post(ctx, "/v3/merchant-service/complaints-v2/"+url.PathEscape(complaintID)+"/complete", map[string]any{"complainted_mchid": w.mchID})
}

// UpdateRefundProgress updates the退款审批结果 (仅微信). action is APPROVED or
// REJECTED; a rejected action requires a reason.
func (w *WechatV3Complaint) UpdateRefundProgress(ctx context.Context, complaintID, action, rejectReason, remark string) error {
	if !validComplaintID(complaintID) {
		return ErrInvalid
	}
	body := map[string]any{}
	switch action {
	case "APPROVED":
		body["action"] = "APPROVED"
	case "REJECTED":
		if !validComplaintText(rejectReason, 200) {
			return ErrInvalid
		}
		body["action"] = "REJECTED"
		body["reject_reason"] = rejectReason
	default:
		return ErrInvalid
	}
	if remark != "" {
		if !validComplaintText(remark, 200) {
			return ErrInvalid
		}
		body["remark"] = remark
	}
	return w.post(ctx, "/v3/merchant-service/complaints-v2/"+url.PathEscape(complaintID)+"/update-refund-progress", body)
}

func (w *WechatV3Complaint) post(ctx context.Context, target string, body map[string]any) error {
	raw, e := w.client.call(ctx, "POST", target, body)
	if e != nil {
		return e
	}
	// WeChat returns 204 (empty) on these actions; any other body is an
	// unknown outcome — the caller must re-query, never assume success.
	if len(strings.TrimSpace(string(raw))) != 0 {
		return &UnknownOutcome{Operation: target, Cause: ErrAuthentication}
	}
	return nil
}

// decodeRecord maps a signed v3 complaint object to the neutral record.
func (w *WechatV3Complaint) decodeRecord(raw json.RawMessage) (ComplaintRecord, error) {
	out, e := v3Object(raw)
	if e != nil {
		return ComplaintRecord{}, e
	}
	id := v3String(out, "complaint_id")
	if id == "" || !validComplaintID(id) {
		return ComplaintRecord{}, ErrAuthentication
	}
	rec := ComplaintRecord{
		ThirdID:    id,
		ThirdMchID: v3String(out, "complainted_mchid"),
		PayType:    "wxpay",
		Type:       v3String(out, "complaint_state"),
		Content:    v3Text(out, "complaint_detail", 2000),
		Phone:      v3String(out, "payer_phone"),
		Status:     v3String(out, "complaint_state"),
		CreatedAt:  v3String(out, "complaint_time"),
		Times:      1,
		Raw:        append(json.RawMessage(nil), raw...),
	}
	// complaint_order_info[].{out_trade_no,amount}; amount is in cents.
	var orders []json.RawMessage
	if len(out["complaint_order_info"]) > 0 {
		if json.Unmarshal(out["complaint_order_info"], &orders) != nil {
			return ComplaintRecord{}, ErrAuthentication
		}
	}
	var total int64
	known := len(orders) > 0
	for _, o := range orders {
		obj, e := v3Object(o)
		if e != nil {
			return ComplaintRecord{}, e
		}
		if tn := v3String(obj, "out_trade_no"); tn != "" {
			rec.TradeNos = append(rec.TradeNos, tn)
		}
		if _, ok := obj["amount"]; ok {
			total += v3Int(obj, "amount")
		} else {
			known = false
		}
	}
	if known {
		rec.AmountCents = &total
	}
	if hist, e := decodeHistory(out["complaint_full_refund"]); e == nil && len(hist) > 0 {
		rec.Negotiation = hist
	}
	return rec, nil
}

func decodeHistory(raw json.RawMessage) ([]ComplaintHistory, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		return nil, ErrAuthentication
	}
	out := make([]ComplaintHistory, 0, len(list))
	for _, item := range list {
		obj, e := v3Object(item)
		if e != nil {
			return nil, e
		}
		h := ComplaintHistory{
			Operator:  v3String(obj, "operator"),
			Role:      v3String(obj, "operate_type"),
			Content:   v3Text(obj, "operate_details", 2000),
			CreatedAt: v3String(obj, "operate_time"),
		}
		if len(obj["image_list"]) > 0 {
			var imgs []string
			if json.Unmarshal(obj["image_list"], &imgs) == nil {
				h.Images = imgs
			}
		}
		out = append(out, h)
	}
	return out, nil
}

// --- shared validation (used by both provider adapters) ---

func validComplaintID(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func validComplaintText(s string, limit int) bool {
	return s != "" && len(s) <= limit && !strings.ContainsRune(s, 0)
}

func validMediaIDs(ids []string, max int) bool {
	if len(ids) > max {
		return false
	}
	for _, id := range ids {
		if id == "" || len(id) > 512 || strings.ContainsAny(id, "\x00\r\n") {
			return false
		}
	}
	return true
}

// decodeMediaID is a tiny helper the image-download path uses to keep an opaque
// media id url-safe for the path segment.
func encodeMediaSegment(id string) string { return url.PathEscape(id) }

// base64Image is used by callers that must relay an image the provider returned
// as raw bytes without ever persisting a credential-bearing URL.
func base64Image(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
