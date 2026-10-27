package server

import (
	"encoding/json"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"io"
	"net/http"
	"strings"
)

func (s *Server) batchParentChannels(w http.ResponseWriter, r *http.Request, session identity.Session, token string) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery || strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		response(w, 400, -1, "invalid batch request", nil)
		return
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
	if e != nil {
		response(w, 400, -1, "invalid batch body", nil)
		return
	}
	fields, e := parentExactObject(raw)
	var action string
	var rows []json.RawMessage
	if e != nil || len(fields) != 2 || json.Unmarshal(fields["action"], &action) != nil || json.Unmarshal(fields["items"], &rows) != nil || len(rows) == 0 || len(rows) > 100 {
		response(w, 400, -1, "invalid batch fields", nil)
		return
	}
	items := []payment.ParentChannelBatchItem{}
	for _, raw := range rows {
		row, e := parentExactObject(raw)
		var id, expected string
		if e != nil || len(row) != 2 || json.Unmarshal(row["id"], &id) != nil || json.Unmarshal(row["expected_fingerprint"], &expected) != nil || !identityHex(expected) {
			response(w, 400, -1, "invalid original batch target", nil)
			return
		}
		n, ok := positiveFundsID(id)
		if !ok {
			response(w, 400, -1, "invalid original batch target", nil)
			return
		}
		items = append(items, payment.ParentChannelBatchItem{ID: n, Expected: expected})
	}
	actor := payment.ParentChannelActor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
	out, e := s.Store.BatchParentChannels(r.Context(), actor, action, items, s.ProviderFlow.ParentFactories)
	if e != nil {
		parentChannelError(w, e)
		return
	}
	response(w, 200, 0, "", map[string]any{"action": action, "items": out, "total": len(out)})
}

func (s *Server) adoptParentChannel(w http.ResponseWriter, r *http.Request, session identity.Session, token, id string) {
	channel, ok := positiveFundsID(id)
	if !ok || r.URL.RawQuery != "" || r.URL.ForceQuery || strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		response(w, 400, -1, "invalid registered channel request", nil)
		return
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if e != nil {
		response(w, 400, -1, "invalid adoption body", nil)
		return
	}
	fields, e := parentExactObject(raw)
	var expected string
	if e != nil || len(fields) != 1 || json.Unmarshal(fields["expected_source_fingerprint"], &expected) != nil || !identityHex(expected) {
		response(w, 400, -1, "invalid original registered source", nil)
		return
	}
	t, e := s.ProviderFlow.ParentChannelTemplate(channel, s.MerchantChannels)
	if e != nil {
		parentChannelError(w, e)
		return
	}
	actor := payment.ParentChannelActor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
	out, e := s.Store.AdoptParentChannel(r.Context(), actor, t, expected)
	if e != nil {
		parentChannelError(w, e)
		return
	}
	response(w, 200, 0, "", out)
}
