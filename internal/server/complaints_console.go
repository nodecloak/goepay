package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"goepay/internal/backoffice"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

// complaintsConsole serves the provider-backed complaint actions that the W6
// 交易投诉 pages drive. The local read/list/comment/state endpoints are the
// existing backoffice `complaints` resource + cases API; THIS handler adds the
// actions that require the channel's provider credentials:
//
//	POST /console/v1/{role}/complaints-sync            {channel_id}         manual pull now
//	POST /console/v1/{role}/complaints/{id}/provider-reply      {content,images?}
//	POST /console/v1/{role}/complaints/{id}/provider-complete   {feedback_code?,content?,images?}
//	POST /console/v1/{role}/complaints/{id}/provider-supplement {content,images?}  (Alipay)
//	POST /console/v1/{role}/complaints-image?channel_id=N       multipart image -> media_id (WeChat)
//
// Everything is gated by complain_open (default off). Merchant calls are scoped
// to the caller's own merchant id; admin calls are unscoped. Refund-on-complaint
// is NOT here: it flows through the existing /orders/{trade}/refund endpoint,
// which keeps its admin funds authorization and immutable ledger — the complaint
// detail exposes the linked trade_no for that.
func (s *Server) complaintsConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 {
		return false
	}
	switch parts[1] {
	case "complaints-sync", "complaints-image", "complaints133", "complaints-summary", "complaints-charts", "complaint-sync-tasks", "complaint-channels":
	case "complaints":
		// Only intercept the provider-* actions; the resource reads/cases fall
		// through to the backoffice handler (local tickets via CasePanel).
		if len(parts) != 4 || !strings.HasPrefix(parts[3], "provider-") {
			return false
		}
	default:
		return false
	}
	identityHeaders(w)
	// Feature gate: complain_open (default off).
	on, e := backoffice.New(s.Pool, s.Store).MerchantFeatureEnabled(r.Context(), "complain_open")
	if e != nil {
		response(w, 503, -1, "complaint feature unavailable", nil)
		return true
	}
	if !on {
		response(w, 403, -1, "complaint feature disabled", nil)
		return true
	}
	mid := int64(0)
	if role != "admin" {
		mid = session.User.ID
	}

	// Read endpoints over complaints133 (the provider-synced store the Pro pages
	// list). These need no ProviderFlow — only the DB.
	if r.Method == "GET" {
		switch {
		case parts[1] == "complaints-summary" && len(parts) == 2:
			s.complaintSummaryRead(w, r, mid)
			return true
		case parts[1] == "complaints-charts" && len(parts) == 2:
			s.complaintChartsRead(w, r, mid)
			return true
		case parts[1] == "complaints133" && len(parts) == 2:
			s.complaintListRead(w, r, mid)
			return true
		case parts[1] == "complaints133" && len(parts) == 3 && parts[2] == "export":
			s.complaintListExport(w, r, mid)
			return true
		case parts[1] == "complaints133" && len(parts) == 3:
			s.complaintDetailRead(w, r, mid, parts[2])
			return true
		case parts[1] == "complaint-sync-tasks" && len(parts) == 2:
			s.complaintSyncTasksList(w, r, mid)
			return true
		case parts[1] == "complaint-channels" && len(parts) == 2:
			s.complaintSupportedChannelsList(w, r, mid)
			return true
		}
	}

	// PUT /complaint-sync-tasks/{channel_id}: admin-only upsert of a channel's
	// sync task. Needs no provider flow — it writes the task row + audit.
	if r.Method == "PUT" && parts[1] == "complaint-sync-tasks" && len(parts) == 3 {
		s.complaintSyncTaskUpsert(w, r, role, session, parts[2])
		return true
	}
	// PATCH /complaint-sync-tasks/{channel_id}: admin-only inline status toggle.
	if r.Method == "PATCH" && parts[1] == "complaint-sync-tasks" && len(parts) == 3 {
		s.complaintSyncTaskStatus(w, r, role, session, parts[2])
		return true
	}
	// DELETE /complaint-sync-tasks/{channel_id}: admin-only per-row delete.
	if r.Method == "DELETE" && parts[1] == "complaint-sync-tasks" && len(parts) == 3 {
		s.complaintSyncTaskDelete(w, r, role, session, []string{parts[2]})
		return true
	}
	// POST /complaint-sync-tasks/delete: admin-only batch delete. Handled here
	// (before the provider-live gate) because task config needs no provider flow.
	if r.Method == "POST" && parts[1] == "complaint-sync-tasks" && len(parts) == 3 && parts[2] == "delete" {
		s.complaintSyncTaskBatchDelete(w, r, role, session)
		return true
	}

	if r.Method != "POST" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	// Provider write actions require the live flow.
	if s.ProviderFlow == nil || s.Sandbox || s.ProviderFlow.LocalSimulation || !s.ProviderFlow.AllowLive {
		response(w, 503, -1, "provider operations unavailable", nil)
		return true
	}

	switch parts[1] {
	case "complaints-image":
		s.complaintImageUpload(w, r, mid)
		return true
	case "complaints-sync":
		s.complaintSyncNow(w, r, mid)
		return true
	}
	// parts: complaints / {id} / provider-<action>
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id <= 0 {
		response(w, 404, -1, "complaint not found", nil)
		return true
	}
	in, ok := complaintActionBody(w, r)
	if !ok {
		return true
	}
	switch parts[3] {
	case "provider-reply":
		s.complaintError(w, s.ProviderFlow.ReplyComplaint(r.Context(), id, mid, in.Content, in.Images))
	case "provider-complete":
		s.complaintError(w, s.ProviderFlow.CompleteComplaint(r.Context(), id, mid, in.FeedbackCode, in.Content, in.Images))
	case "provider-supplement":
		s.complaintError(w, s.ProviderFlow.SupplementComplaint(r.Context(), id, mid, in.Content, in.Images))
	default:
		response(w, 404, -1, "not found", nil)
	}
	return true
}

type complaintActionInput struct {
	Content      string   `json:"content"`
	FeedbackCode string   `json:"feedback_code"`
	Images       []string `json:"images"`
}

func complaintActionBody(w http.ResponseWriter, r *http.Request) (complaintActionInput, bool) {
	var in complaintActionInput
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		response(w, 400, -1, "application/json required", nil)
		return in, false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	d.DisallowUnknownFields()
	if e := d.Decode(&in); e != nil {
		response(w, 400, -1, "invalid complaint action body", nil)
		return in, false
	}
	if len(in.Images) > 10 {
		response(w, 400, -1, "too many images", nil)
		return in, false
	}
	return in, true
}

// complaintSyncNow triggers a pull for one channel immediately (admin only) and
// records the result on complaint_tasks133 (same failure accounting as the
// scheduled task). A merchant cannot sync; the admin owns channel credentials.
func (s *Server) complaintSyncNow(w http.ResponseWriter, r *http.Request, mid int64) {
	if mid != 0 {
		response(w, 403, -1, "channel sync is an administrator action", nil)
		return
	}
	var body struct {
		ChannelID int64 `json:"channel_id"`
		Num       int   `json:"num"`
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		response(w, 400, -1, "application/json required", nil)
		return
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if e := d.Decode(&body); e != nil || body.ChannelID <= 0 {
		response(w, 400, -1, "invalid channel", nil)
		return
	}
	if body.Num < 1 || body.Num > 200 {
		body.Num = 20
	}
	count, syncErr := s.ProviderFlow.SyncComplaints(r.Context(), body.ChannelID, body.Num, 0)
	_ = s.ProviderFlow.Store.RecordComplaintSyncResult(r.Context(), body.ChannelID, syncErr)
	if syncErr != nil {
		s.complaintError(w, syncErr)
		return
	}
	response(w, 200, 0, "complaints synced", map[string]any{"channel_id": strconv.FormatInt(body.ChannelID, 10), "synced": count})
}

// complaintImageUpload relays a WeChat V3 complaint image (multipart) and
// returns its media_id for a subsequent reply/complete.
func (s *Server) complaintImageUpload(w http.ResponseWriter, r *http.Request, mid int64) {
	channel, err := strconv.ParseInt(r.URL.Query().Get("channel_id"), 10, 64)
	if err != nil || channel <= 0 {
		response(w, 400, -1, "invalid channel", nil)
		return
	}
	if e := r.ParseMultipartForm(4 << 20); e != nil {
		response(w, 400, -1, "invalid upload", nil)
		return
	}
	file, header, e := r.FormFile("file")
	if e != nil {
		response(w, 400, -1, "missing file", nil)
		return
	}
	defer file.Close()
	data, e := io.ReadAll(io.LimitReader(file, (2<<20)+1))
	if e != nil || len(data) == 0 || len(data) > 2<<20 {
		response(w, 400, -1, "image too large", nil)
		return
	}
	mediaID, e := s.ProviderFlow.UploadComplaintImage(r.Context(), channel, mid, header.Filename, data)
	if e != nil {
		s.complaintError(w, e)
		return
	}
	response(w, 200, 0, "image uploaded", map[string]string{"media_id": mediaID})
}

// complaintListRead serves GET /complaints133 — the provider-synced 投诉记录
// list, scoped to the caller's merchant for the merchant role, with the Pro
// filters (搜索字段/状态/支付方式) and offset paging + total.
func (s *Server) complaintListRead(w http.ResponseWriter, r *http.Request, mid int64) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	f := payment.ComplaintListFilter{
		OwnerScope: mid,
		Field:      q.Get("field"),
		Keyword:    q.Get("q"),
		Status:     q.Get("status"),
		PayType:    q.Get("pay_type"),
		ChannelID:  q.Get("channel_id"),
		StartTime:  q.Get("starttime"),
		EndTime:    q.Get("endtime"),
		Page:       page,
		Limit:      limit,
	}
	items, total, e := s.Store.ComplaintList(r.Context(), f)
	if e != nil {
		s.fail(w, e)
		return
	}
	response(w, 200, 0, "", map[string]any{"items": items, "total": total})
}

// complaintDetailRead serves GET /complaints133/{id} — one complaint with its
// images and reply/negotiation history, scoped to the caller.
func (s *Server) complaintDetailRead(w http.ResponseWriter, r *http.Request, mid int64, idStr string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		response(w, 404, -1, "complaint not found", nil)
		return
	}
	v, e := s.Store.ComplaintDetail(r.Context(), id, mid)
	if errors.Is(e, payment.ErrNotFound) {
		response(w, 404, -1, "complaint not found", nil)
		return
	}
	if e != nil {
		s.fail(w, e)
		return
	}
	response(w, 200, 0, "", v)
}

// complaintSummaryRead serves GET /complaints-summary — the today/yesterday
// banner computed from complaints133 (scoped for merchants).
func (s *Server) complaintSummaryRead(w http.ResponseWriter, r *http.Request, mid int64) {
	v, e := s.Store.ComplaintSummary(r.Context(), mid)
	if e != nil {
		s.fail(w, e)
		return
	}
	response(w, 200, 0, "", map[string]any{"summary": v})
}

// complaintChartsRead serves GET /complaints-charts — 投诉统计: the summary plus
// the per-merchant ranking table, all from complaints133.
func (s *Server) complaintChartsRead(w http.ResponseWriter, r *http.Request, mid int64) {
	summary, e := s.Store.ComplaintSummary(r.Context(), mid)
	if e != nil {
		s.fail(w, e)
		return
	}
	items, e := s.Store.ComplaintChartRanking(r.Context(), mid, 50)
	if e != nil {
		s.fail(w, e)
		return
	}
	response(w, 200, 0, "", map[string]any{
		"summary": summary,
		"items":   items,
		"columns": []map[string]string{
			{"key": "dimension", "label": "商户ID"},
			{"key": "today_count", "label": "今日投诉总条数"},
			{"key": "today_money", "label": "今日投诉金额"},
			{"key": "today_alipay", "label": "今日支付宝投诉"},
			{"key": "today_wxpay", "label": "今日微信投诉"},
			{"key": "yesterday_count", "label": "昨日投诉总条数"},
			{"key": "yesterday_money", "label": "昨日投诉金额"},
			{"key": "yesterday_alipay", "label": "昨日支付宝投诉"},
			{"key": "yesterday_wxpay", "label": "昨日微信投诉"},
			{"key": "complaint_rate", "label": "近7日投诉率"},
			{"key": "total_count", "label": "累计投诉"},
		},
	})
}

// complaintSyncTasksList serves GET /complaint-sync-tasks — the 同步任务 page's
// rows (complaint_tasks133 joined with channel name/provider). Admin only.
func (s *Server) complaintSyncTasksList(w http.ResponseWriter, r *http.Request, mid int64) {
	if mid != 0 {
		response(w, 403, -1, "complaint sync tasks are an administrator view", nil)
		return
	}
	items, e := s.Store.ComplaintSyncTasks(r.Context())
	if e != nil {
		s.fail(w, e)
		return
	}
	response(w, 200, 0, "", map[string]any{"items": items, "total": len(items)})
}

// complaintSupportedChannelsList serves GET /complaint-channels — channels whose
// provider supports complaints (for the 投诉记录 page's 自动获取 picker). Admin only.
func (s *Server) complaintSupportedChannelsList(w http.ResponseWriter, r *http.Request, mid int64) {
	if mid != 0 {
		response(w, 403, -1, "complaint channels are an administrator view", nil)
		return
	}
	items, e := s.Store.ComplaintSupportedChannels(r.Context())
	if e != nil {
		s.fail(w, e)
		return
	}
	response(w, 200, 0, "", map[string]any{"items": items, "total": len(items)})
}

// complaintSyncTaskUpsert serves PUT /complaint-sync-tasks/{channel_id}: create
// or update a channel's sync task {enabled,frequency,num}. Admin only; the
// channel's provider must support complaints (else a clear refusal). Disabling
// keeps the row + history. Audited.
func (s *Server) complaintSyncTaskUpsert(w http.ResponseWriter, r *http.Request, role string, session identity.Session, channelStr string) {
	if role != "admin" {
		response(w, 403, -1, "complaint sync task configuration is an administrator action", nil)
		return
	}
	channel, err := strconv.ParseInt(channelStr, 10, 64)
	if err != nil || channel <= 0 {
		response(w, 400, -1, "invalid channel", nil)
		return
	}
	var body struct {
		Enabled   bool `json:"enabled"`
		Days      int  `json:"days"`
		Frequency int  `json:"frequency"`
		Num       int  `json:"num"`
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		response(w, 400, -1, "application/json required", nil)
		return
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if e := d.Decode(&body); e != nil {
		response(w, 400, -1, "invalid sync task body", nil)
		return
	}
	v, e := s.Store.UpsertComplaintSyncTask(r.Context(), channel, body.Enabled, body.Days, body.Frequency, body.Num, session.User.ID, session.User.Username)
	switch {
	case e == nil:
		response(w, 200, 0, "sync task saved", v)
	case errors.Is(e, payment.ErrNotFound):
		response(w, 404, -1, "channel not found", nil)
	case errors.Is(e, payment.ErrInvalid):
		response(w, 422, -1, "channel does not support complaints (only WeChat Pay V3 / Alipay), or frequency/num/days out of range (frequency 1-1440 min, num 1-200, days >= 0)", nil)
	default:
		s.fail(w, e)
	}
}

// complaintSyncTaskStatus serves PATCH /complaint-sync-tasks/{channel_id}: the
// 同步任务 page's inline 状态 switch (setTaskStatus). Admin-only; the task must
// already exist.
func (s *Server) complaintSyncTaskStatus(w http.ResponseWriter, r *http.Request, role string, session identity.Session, channelStr string) {
	if role != "admin" {
		response(w, 403, -1, "complaint sync task configuration is an administrator action", nil)
		return
	}
	channel, err := strconv.ParseInt(channelStr, 10, 64)
	if err != nil || channel <= 0 {
		response(w, 400, -1, "invalid channel", nil)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		response(w, 400, -1, "application/json required", nil)
		return
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	d.DisallowUnknownFields()
	if e := d.Decode(&body); e != nil {
		response(w, 400, -1, "invalid sync task status body", nil)
		return
	}
	v, e := s.Store.SetComplaintTaskStatus(r.Context(), channel, body.Enabled, session.User.ID, session.User.Username)
	switch {
	case e == nil:
		response(w, 200, 0, "sync task status updated", v)
	case errors.Is(e, payment.ErrNotFound):
		response(w, 404, -1, "sync task not found", nil)
	case errors.Is(e, payment.ErrInvalid):
		response(w, 400, -1, "invalid channel", nil)
	default:
		s.fail(w, e)
	}
}

// complaintSyncTaskDelete serves DELETE /complaint-sync-tasks/{channel_id} and
// the per-row 删除 action; complaintSyncTaskBatchDelete serves 批量删除. Both
// remove only the task configuration (complaint_tasks133) — the synced
// complaints themselves stay, preserving the append-only complaint ledger.
func (s *Server) complaintSyncTaskDelete(w http.ResponseWriter, r *http.Request, role string, session identity.Session, channelStrs []string) {
	if role != "admin" {
		response(w, 403, -1, "complaint sync task configuration is an administrator action", nil)
		return
	}
	channels := make([]int64, 0, len(channelStrs))
	for _, cs := range channelStrs {
		c, err := strconv.ParseInt(strings.TrimSpace(cs), 10, 64)
		if err != nil || c <= 0 {
			response(w, 400, -1, "invalid channel", nil)
			return
		}
		channels = append(channels, c)
	}
	deleted, e := s.Store.DeleteComplaintSyncTasks(r.Context(), channels, session.User.ID, session.User.Username)
	switch {
	case e == nil:
		response(w, 200, 0, "sync task deleted", map[string]any{"deleted": deleted})
	case errors.Is(e, payment.ErrInvalid):
		response(w, 400, -1, "invalid channel", nil)
	default:
		s.fail(w, e)
	}
}

func (s *Server) complaintSyncTaskBatchDelete(w http.ResponseWriter, r *http.Request, role string, session identity.Session) {
	if role != "admin" {
		response(w, 403, -1, "complaint sync task configuration is an administrator action", nil)
		return
	}
	var body struct {
		ChannelIDs []int64 `json:"channel_ids"`
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		response(w, 400, -1, "application/json required", nil)
		return
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if e := d.Decode(&body); e != nil || len(body.ChannelIDs) == 0 || len(body.ChannelIDs) > 500 {
		response(w, 400, -1, "invalid channel list", nil)
		return
	}
	deleted, e := s.Store.DeleteComplaintSyncTasks(r.Context(), body.ChannelIDs, session.User.ID, session.User.Username)
	switch {
	case e == nil:
		response(w, 200, 0, "sync tasks deleted", map[string]any{"deleted": deleted})
	case errors.Is(e, payment.ErrInvalid):
		response(w, 400, -1, "invalid channel list", nil)
	default:
		s.fail(w, e)
	}
}

// complaintListExport serves GET /complaints133/export — a CSV of the 投诉记录
// list under the SAME filters as the list (搜索字段/状态/支付方式/通道ID/日期范围),
// scoped to the caller's merchant for the merchant role (complain/export). It
// pages through the filtered result (bounded) so a large list still exports
// without an unbounded query.
func (s *Server) complaintListExport(w http.ResponseWriter, r *http.Request, mid int64) {
	q := r.URL.Query()
	base := payment.ComplaintListFilter{
		OwnerScope: mid,
		Field:      q.Get("field"),
		Keyword:    q.Get("q"),
		Status:     q.Get("status"),
		PayType:    q.Get("pay_type"),
		ChannelID:  q.Get("channel_id"),
		StartTime:  q.Get("starttime"),
		EndTime:    q.Get("endtime"),
		Limit:      100,
	}
	const maxRows = 50000
	var all []payment.ComplaintListItem
	for page := 1; len(all) < maxRows; page++ {
		base.Page = page
		items, total, e := s.Store.ComplaintList(r.Context(), base)
		if e != nil {
			s.fail(w, e)
			return
		}
		all = append(all, items...)
		if len(all) >= int(total) || len(items) == 0 {
			break
		}
	}
	if len(all) > maxRows {
		all = all[:maxRows]
	}
	statusWord := func(s string) string {
		switch s {
		case "open":
			return "待处理"
		case "processing":
			return "处理中"
		case "done":
			return "已处理"
		}
		return s
	}
	amount := func(cents string) string {
		if n, err := strconv.ParseInt(cents, 10, 64); err == nil {
			return payment.CentsToYuan(n)
		}
		return ""
	}
	var b strings.Builder
	b.WriteString("\ufeff") // UTF-8 BOM so Excel reads the Chinese header correctly
	b.WriteString("ID,商户号,通道ID,关联订单号,问题类型,订单金额,投诉原因,投诉详情,联系电话,渠道子商户号,投诉次数,状态,创建时间,更新时间\r\n")
	csvCell := func(s string) string {
		if strings.ContainsAny(s, ",\"\r\n") {
			return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
		}
		return s
	}
	for _, it := range all {
		row := []string{
			it.ID, it.OwnerID, it.ChannelID, it.TradeNo, it.Category, amount(it.Amount),
			it.Title, it.Content, it.Phone, it.ThirdMchID, it.Times, statusWord(it.Status),
			it.CreatedAt, it.UpdatedAt,
		}
		for i, c := range row {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(csvCell(c))
		}
		b.WriteString("\r\n")
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="complaints.csv"`)
	w.WriteHeader(200)
	_, _ = w.Write([]byte(b.String()))
}

func (s *Server) complaintError(w http.ResponseWriter, e error) {
	if e == nil {
		response(w, 200, 0, "complaint action submitted", nil)
		return
	}
	switch {
	case errors.Is(e, payment.ErrInvalid):
		response(w, 422, -1, "invalid complaint action input", nil)
	case errors.Is(e, payment.ErrNotFound):
		response(w, 404, -1, "complaint not found", nil)
	case errors.Is(e, payment.ErrConflict):
		response(w, 409, -1, "complaint channel mismatch or busy", nil)
	case errors.Is(e, payment.ErrLiveDisabled):
		response(w, 503, -1, "live provider operations disabled", nil)
	default:
		s.recordRuntimeFailure(e)
		response(w, 502, -1, "provider complaint action could not be confirmed", nil)
	}
}
