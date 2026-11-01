package server

import (
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// The executable catalog is distinct from the legacy channels resource. Only
// loaded runtime provider IDs enter this initial bootstrap read; typed parent
// account heads extend it through their authenticated authorization origin.
func (s *Server) paymentParentChannels(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 2 || parts[1] != "payment-channels" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.User.AdminClass != "super" || session.Issuer != nil {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if s.Store == nil || s.ProviderFlow == nil {
		response(w, 503, -1, "runtime parent channel registry unavailable", nil)
		return true
	}
	if len(parts) == 4 && parts[3] == "subject" {
		s.parentChannelSubject(w, r, session, token, parts[2])
		return true
	}
	if s.parentPaymentTestRoute(w, r, session, token, parts) {
		return true
	}
	if len(parts) == 3 && parts[2] == "templates" && r.Method == "GET" {
		s.parentChannelTemplates(w, r, session, token)
		return true
	}
	if len(parts) == 4 && parts[3] == "weixin-options" && r.Method == "GET" {
		s.parentChannelWeixinOptions(w, r, session, token, parts[2])
		return true
	}
	if len(parts) == 2 && r.Method == "POST" {
		s.createParentChannel(w, r, session, token)
		return true
	}
	if len(parts) == 3 && parts[2] == "batch" && r.Method == "POST" {
		s.batchParentChannels(w, r, session, token)
		return true
	}
	if len(parts) == 3 && parts[2] == "bind-weixin" && r.Method == "POST" {
		s.batchParentWeixin(w, r, session, token)
		return true
	}
	if len(parts) == 4 && parts[3] == "adopt" && r.Method == "POST" {
		s.adoptParentChannel(w, r, session, token, parts[2])
		return true
	}
	if len(parts) == 4 && parts[3] == "statistics" && r.Method == "GET" {
		id, ok := positiveFundsID(parts[2])
		if !ok || r.URL.RawQuery != "" || r.URL.ForceQuery {
			response(w, 400, -1, "invalid parent statistics", nil)
			return true
		}
		if _, e := s.ProviderFlow.AdminParentRuntimeChannel(r.Context(), id); e != nil {
			parentChannelError(w, e)
			return true
		}
		actor := payment.ParentChannelActor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
		items, e := s.Store.ParentChannelStatistics(r.Context(), actor, id)
		if e != nil {
			parentChannelError(w, e)
			return true
		}
		response(w, 200, 0, "", map[string]any{"channel_id": parts[2], "items": items, "cost_basis": "unknown", "money_basis": "verified paid receipts; refunds by original creation date; counts by original order creation date"})
		return true
	}
	if len(parts) == 4 && parts[2] == "requests" && r.Method == "GET" {
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			response(w, 400, -1, "invalid original request lookup", nil)
			return true
		}
		actor := payment.ParentChannelActor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
		out, e := s.Store.ReadParentChannelRequest(r.Context(), actor, parts[3], s.ProviderFlow.ParentFactories)
		if e != nil {
			parentChannelError(w, e)
			return true
		}
		response(w, 200, 0, "", out)
		return true
	}
	if len(parts) == 3 && r.Method == "PATCH" {
		id, ok := positiveFundsID(parts[2])
		if !ok {
			response(w, 400, -1, "invalid runtime parent ID", nil)
			return true
		}
		s.updateParentChannel(w, r, session, token, id)
		return true
	}
	if r.Method != "GET" || len(parts) != 2 && len(parts) != 3 {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	filters, valid := parentChannelFilters(r.URL.RawQuery, r.URL.ForceQuery)
	if !valid || len(parts) == 3 && r.URL.RawQuery != "" {
		response(w, 400, -1, "invalid parent channel filters", nil)
		return true
	}
	var detailID int64
	if len(parts) == 3 {
		var ok bool
		detailID, ok = positiveFundsID(parts[2])
		if !ok {
			response(w, 400, -1, "invalid runtime parent ID", nil)
			return true
		}
	}
	// A broken current app catalog does not hide recorded parent metadata or
	// prevent the operator from disabling a channel. Binding selectors remain
	// strict, while this read can describe its sealed account as non-current.
	if resolved, err := s.managedWeixinRuntime(r.Context()); err == nil {
		s = resolved
	}
	ids := make([]int64, 0, len(s.ProviderFlow.Providers))
	for id, p := range s.ProviderFlow.Providers {
		if id > 0 && p != nil && (p.ID() == "wxpay" || p.ID() == "alipay") {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	actor := payment.ParentChannelActor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
	managed, e := s.ProviderFlow.ParentChannelViews(r.Context(), actor)
	if e != nil {
		parentChannelError(w, e)
		return true
	}
	records := map[int64]any{}
	for _, id := range ids {
		p := s.ProviderFlow.Providers[id]
		records[id] = map[string]any{"id": strconv.FormatInt(id, 10), "name": fmt.Sprintf("通道 #%d", id), "pay_type": p.ID(), "plugin": p.ID(), "configured": false}
	}
	for _, row := range managed {
		if _, exists := records[row.ID]; !exists {
			ids = append(ids, row.ID)
		}
		records[row.ID] = row
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	items := make([]any, 0, len(ids))
	for _, id := range ids {
		if detailID > 0 {
			if id == detailID {
				response(w, 200, 0, "", records[id])
				return true
			}
			continue
		}
		name, kind, plugin, status, archived := parentChannelFilterValues(records[id])
		if archived || filters.kw != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(filters.kw)) && strconv.FormatInt(id, 10) != filters.kw || filters.kind != "" && kind != filters.kind || filters.plugin != "" && plugin != filters.plugin || filters.status != "-1" && filters.status != status {
			continue
		}
		items = append(items, records[id])
	}
	if detailID > 0 {
		response(w, 404, -1, "runtime parent channel unavailable", nil)
		return true
	}
	total := len(items)
	start := (filters.page - 1) * filters.size
	if start > total {
		start = total
	}
	end := start + filters.size
	if end > total {
		end = total
	}
	response(w, 200, 0, "", map[string]any{"items": items[start:end], "total": total, "page": filters.page, "size": filters.size})
	return true
}

type parentFilters struct {
	kw, kind, plugin, status string
	page, size               int
}

func parentChannelFilters(raw string, force bool) (parentFilters, bool) {
	f := parentFilters{status: "-1", page: 1, size: 20}
	if force && raw == "" {
		return f, false
	}
	q, e := url.ParseQuery(raw)
	if e != nil {
		return f, false
	}
	for key, v := range q {
		if len(v) != 1 {
			return f, false
		}
		switch key {
		case "kw":
			if len(v[0]) > 120 {
				return f, false
			}
			f.kw = v[0]
		case "type":
			if v[0] != "" && !adapter.CatalogPayType(v[0]) {
				return f, false
			}
			f.kind = v[0]
		case "plugin":
			if v[0] != "" && v[0] != "wxpay" && v[0] != "alipay" {
				if _, ok := adapter.LookupPlugin(v[0]); !ok {
					return f, false
				}
			}
			f.plugin = v[0]
		case "dstatus":
			if v[0] != "-1" && v[0] != "0" && v[0] != "1" {
				return f, false
			}
			f.status = v[0]
		case "page", "size":
			n, e := strconv.Atoi(v[0])
			if e != nil || n <= 0 || strconv.Itoa(n) != v[0] || key == "size" && n > 100 || key == "page" && n > 1000000 {
				return f, false
			}
			if key == "page" {
				f.page = n
			} else {
				f.size = n
			}
		default:
			return f, false
		}
	}
	return f, true
}
func parentChannelFilterValues(value any) (name, kind, plugin, status string, archived bool) {
	switch row := value.(type) {
	case payment.ParentChannelView:
		name, kind, plugin, archived = row.Name, row.PayType, row.Plugin, row.Archived
		status = "0"
		if row.Enabled {
			status = "1"
		}
	case map[string]any:
		name, _ = row["name"].(string)
		kind, _ = row["pay_type"].(string)
		plugin, _ = row["plugin"].(string)
		status = "unknown"
	}
	return
}

func (s *Server) parentChannelTemplates(w http.ResponseWriter, r *http.Request, session identity.Session, token string) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "invalid runtime template request", nil)
		return
	}
	resolved, resolveErr := s.managedWeixinRuntime(r.Context())
	if resolveErr != nil {
		parentChannelError(w, resolveErr)
		return
	}
	s = resolved
	tx, e := s.Pool.Begin(r.Context())
	if e != nil {
		s.fail(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	a := payment.ParentChannelActor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
	if e = s.Store.CheckParentChannelActorTx(r.Context(), tx, a); e != nil {
		response(w, 403, -1, "original super administrator session required", nil)
		return
	}
	ids := []int64{}
	for id := range s.ProviderFlow.ParentFactories {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	items := []payment.ParentChannelTemplateView{}
	for _, id := range ids {
		t, e := s.ProviderFlow.ParentChannelTemplate(id, s.MerchantChannels)
		if e != nil {
			continue
		}
		items = append(items, t.Public())
	}
	if e = s.Store.CheckParentChannelActorTx(r.Context(), tx, a); e != nil {
		response(w, 403, -1, "original super administrator session required", nil)
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		s.fail(w, e)
		return
	}
	response(w, 200, 0, "", map[string]any{"items": items, "total": len(items)})
}
