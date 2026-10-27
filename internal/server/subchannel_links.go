package server

import (
	"context"
	"goepay/internal/grouppolicy"
	"goepay/internal/payment"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func childTestIntent(raw string) bool {
	if len(raw) != 32 {
		return false
	}
	for _, ch := range raw {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}
func (s *Server) subchannelTestLookup(w http.ResponseWriter, r *http.Request, a payment.SubchannelActor, id int64) {
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil || r.URL.ForceQuery || len(q) != 1 || len(q["intent"]) != 1 || !childTestIntent(q.Get("intent")) {
		childConsoleError(w, payment.ErrInvalid)
		return
	}
	c, e := s.Store.Subchannel(r.Context(), a, id)
	if e != nil {
		childConsoleError(w, e)
		return
	}
	no := "subchannel-test:" + strconv.FormatInt(id, 10) + ":" + q.Get("intent")
	order, attempt, e := s.Store.SubchannelTestOrder(r.Context(), c.MerchantID, id, no)
	if e != nil {
		childConsoleError(w, e)
		return
	}
	if order.Name != "子通道支付测试" || order.Param != "subchannel-test:"+strconv.FormatInt(id, 10) || order.Type != c.PayType {
		childConsoleError(w, payment.ErrConflict)
		return
	}
	s.childTestResponse(w, order, attempt)
}
func (s *Server) subchannelCollectionLink(w http.ResponseWriter, r *http.Request, a payment.SubchannelActor, id int64) {
	if r.Method != "GET" || r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 405, -1, "method not allowed", nil)
		return
	}
	c, e := s.Store.Subchannel(r.Context(), a, id)
	if e != nil {
		childConsoleError(w, e)
		return
	}
	if s.ProviderFlow == nil || len(s.CheckoutKey) < 32 {
		childConsoleError(w, payment.ErrState)
		return
	}
	var code int64
	e = s.Pool.QueryRow(r.Context(), `SELECT id FROM backoffice_records WHERE resource='collection-codes' AND owner_id=$1 AND body->>'enabled'='true' AND body->>'archived' IS DISTINCT FROM 'true' ORDER BY id LIMIT 1`, c.MerchantID).Scan(&code)
	if e != nil {
		response(w, 404, -1, "该商户尚无可用收款码，请先配置聚合收款", nil)
		return
	}
	record, e := s.collectionRecord(r, strconv.FormatInt(code, 10))
	if e != nil || record.Owner != c.MerchantID {
		childConsoleError(w, payment.ErrConflict)
		return
	}
	var address string
	var expires any
	if record.Generation != nil {
		address = s.generatedCollectionURL(*record.Generation)
	} else {
		expiry := time.Now().Add(2 * time.Hour)
		raw := strconv.FormatInt(expiry.Unix(), 10)
		q := url.Values{"expires": {raw}, "token": {s.checkoutToken(strconv.FormatInt(code, 10), "collect", raw)}}
		address = strings.TrimRight(s.ProviderFlow.CallbackBaseURL, "/") + "/collect/" + strconv.FormatInt(code, 10) + "?" + q.Encode()
		expires = expiry.UTC().Format(time.RFC3339)
	}
	u, e := url.Parse(address)
	if e != nil {
		childConsoleError(w, payment.ErrInvalid)
		return
	}
	q := u.Query()
	q.Set("subchannel_id", strconv.FormatInt(id, 10))
	q.Set("type", c.PayType)
	u.RawQuery = q.Encode()
	response(w, 200, 0, "", map[string]any{"url": u.String(), "expires_at": expires})
}

type collectionChildRoute struct {
	id   int64
	kind string
}

func collectionChildQuery(r *http.Request) (collectionChildRoute, *http.Request, error) {
	var out collectionChildRoute
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return out, nil, payment.ErrInvalid
	}
	for _, key := range []string{"subchannel_id", "channel_id", "type"} {
		if len(q[key]) > 1 || len(q[key]) == 1 && q.Get(key) == "" {
			return out, nil, payment.ErrInvalid
		}
	}
	canonical, legacy := q.Get("subchannel_id"), q.Get("channel_id")
	if canonical != "" && legacy != "" && canonical != legacy {
		return out, nil, payment.ErrInvalid
	}
	if canonical == "" {
		canonical = legacy
	}
	if canonical != "" {
		id, ok := positiveFundsID(canonical)
		if !ok {
			return out, nil, payment.ErrInvalid
		}
		out.id = id
	}
	out.kind = q.Get("type")
	if out.kind != "" && out.kind != "wxpay" && out.kind != "alipay" {
		return out, nil, payment.ErrInvalid
	}
	auth := r.Clone(r.Context())
	copyURL := *r.URL
	auth.URL = &copyURL
	for _, key := range []string{"subchannel_id", "channel_id", "type"} {
		q.Del(key)
	}
	auth.URL.RawQuery = q.Encode()
	return out, auth, nil
}
func (s *Server) validateCollectionChild(ctx context.Context, owner int64, route collectionChildRoute) (collectionChildRoute, error) {
	if route.id == 0 {
		return route, nil
	}
	c, e := s.Store.CollectionSubchannel(ctx, owner, route.id)
	if e != nil {
		return route, e
	}
	view, e := s.withParentRuntime(ctx, owner, "")
	if e != nil {
		return route, e
	}
	s = view
	if !c.Enabled || route.kind != "" && route.kind != c.PayType || !s.childParentAllowed(owner, c.ParentID) {
		return route, payment.ErrState
	}
	route.kind = c.PayType
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return route, e
	}
	defer tx.Rollback(ctx)
	group, e := s.Store.GroupPaymentRoutingTx(ctx, tx, owner, c.PayType)
	if e != nil {
		return route, e
	}
	if group.Choice.Kind() != grouppolicy.Subchannels {
		return route, payment.ErrState
	}
	binding, e := s.ProviderFlow.RuntimeChannel(c.ParentID)
	if e != nil {
		return route, e
	}
	allowed, _, e := s.Store.RuntimeChannelAllowedTx(ctx, tx, binding)
	if e != nil {
		return route, e
	}
	if !allowed {
		return route, payment.ErrState
	}
	status, e := s.Store.ChannelWriteStatusTx(ctx, tx, c.ParentID)
	if e != nil || status.Blocked {
		return route, payment.ErrState
	}
	flow, execution, e := s.ProviderFlow.WithSubchannelAccountTx(ctx, tx, c)
	if e != nil {
		return route, e
	}
	child := s.withPaymentExecution(flow, execution, c.ParentID)
	browser := false
	for _, m := range child.ProviderFlow.Providers[c.ParentID].Capabilities().Methods {
		if m == "native" || m == "page" || m == "wap" || m == "h5" || m == "app" {
			browser = true
		}
	}
	if !browser {
		return route, payment.ErrState
	}
	return route, tx.Commit(ctx)
}
