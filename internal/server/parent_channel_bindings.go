package server

import (
	"encoding/json"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) parentChannelWeixinOptions(w http.ResponseWriter, r *http.Request, session identity.Session, token, id string) {
	channel, ok := positiveFundsID(id)
	if !ok || r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "invalid original parent selector", nil)
		return
	}
	actor := payment.ParentChannelActor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
	resolved, err := s.managedWeixinRuntime(r.Context())
	if err != nil {
		parentChannelError(w, err)
		return
	}
	s = resolved
	out, err := s.Store.ParentChannelBindingCatalog(r.Context(), actor, channel, s.ProviderFlow.ParentFactories)
	if err != nil {
		parentChannelError(w, err)
		return
	}
	response(w, 200, 0, "", out)
}

func (s *Server) batchParentWeixin(w http.ResponseWriter, r *http.Request, session identity.Session, token string) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery || strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		response(w, 400, -1, "invalid original Weixin selection", nil)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
	if err != nil {
		response(w, 400, -1, "invalid Weixin body", nil)
		return
	}
	fields, err := parentExactObject(raw)
	var kind int
	var profile string
	var rows []json.RawMessage
	if err != nil || len(fields) != 3 || fields["type"] == nil || fields["weixin"] == nil || fields["items"] == nil || json.Unmarshal(fields["type"], &kind) != nil || kind != 0 && kind != 1 || json.Unmarshal(fields["weixin"], &profile) != nil || json.Unmarshal(fields["items"], &rows) != nil || len(rows) < 1 || len(rows) > 100 {
		response(w, 400, -1, "invalid original Weixin selection fields", nil)
		return
	}
	profileID, err := strconv.ParseInt(profile, 10, 64)
	if err != nil || profileID < 0 || strconv.FormatInt(profileID, 10) != profile {
		response(w, 400, -1, "invalid original Weixin ID", nil)
		return
	}
	items := []payment.ParentWeixinBatchItem{}
	for _, raw := range rows {
		row, err := parentExactObject(raw)
		var id, expected, catalog string
		if err != nil || len(row) != 3 || json.Unmarshal(row["id"], &id) != nil || json.Unmarshal(row["expected_fingerprint"], &expected) != nil || !identityHex(expected) || json.Unmarshal(row["expected_bindings_fingerprint"], &catalog) != nil || !identityHex(catalog) {
			response(w, 400, -1, "invalid exact parent selection", nil)
			return
		}
		channel, ok := positiveFundsID(id)
		if !ok {
			response(w, 400, -1, "invalid original parent ID", nil)
			return
		}
		items = append(items, payment.ParentWeixinBatchItem{ID: channel, Expected: expected, ExpectedBindings: catalog})
	}
	actor := payment.ParentChannelActor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
	resolved, e := s.managedWeixinRuntime(r.Context())
	if e != nil {
		parentChannelError(w, e)
		return
	}
	s = resolved
	out, err := s.Store.BatchBindParentWeixin(r.Context(), actor, kind, profileID, items, s.ProviderFlow.ParentFactories)
	if err != nil {
		parentChannelError(w, err)
		return
	}
	response(w, 200, 0, "", map[string]any{"type": kind, "weixin": profile, "items": out, "total": len(out)})
}
