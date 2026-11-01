package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func parentExactObject(raw []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return nil, payment.ErrInvalid
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		token, e = d.Token()
		key, ok := token.(string)
		if e != nil || !ok || out[key] != nil {
			return nil, payment.ErrInvalid
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(value, []byte("null")) {
			return nil, payment.ErrInvalid
		}
		out[key] = value
	}
	if token, e = d.Token(); e != nil || token != json.Delim('}') {
		return nil, payment.ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, payment.ErrInvalid
	}
	return out, nil
}
func parentValueEdits(raw json.RawMessage) (map[string]adapter.ParentChannelValueEdit, error) {
	fields, e := parentExactObject(raw)
	if e != nil {
		return nil, e
	}
	out := map[string]adapter.ParentChannelValueEdit{}
	for key, value := range fields {
		if key == "" || len(key) > 180 {
			return nil, payment.ErrInvalid
		}
		edit, e := parentExactObject(value)
		if e != nil {
			return nil, e
		}
		var operation string
		if json.Unmarshal(edit["operation"], &operation) != nil {
			return nil, payment.ErrInvalid
		}
		if operation == "keep" || operation == "clear" {
			if len(edit) != 1 {
				return nil, payment.ErrInvalid
			}
			out[key] = adapter.ParentChannelValueEdit{Operation: operation}
		} else if operation == "replace" {
			var value string
			if len(edit) != 2 || json.Unmarshal(edit["value"], &value) != nil {
				return nil, payment.ErrInvalid
			}
			out[key] = adapter.ParentChannelValueEdit{Operation: operation, Value: &value}
		} else {
			return nil, payment.ErrInvalid
		}
	}
	return out, nil
}
func parentAccountChange(raw json.RawMessage) (adapter.ParentChannelAccountChange, error) {
	var out adapter.ParentChannelAccountChange
	fields, e := parentExactObject(raw)
	if e != nil {
		return out, e
	}
	for key, value := range fields {
		switch key {
		case "expected_bindings_fingerprint":
			if json.Unmarshal(value, &out.ExpectedBindings) != nil || !identityHex(out.ExpectedBindings) {
				return out, payment.ErrInvalid
			}
		case "appwxmp", "appwxa":
			var id string
			if json.Unmarshal(value, &id) != nil {
				return out, payment.ErrInvalid
			}
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil || n < 0 || strconv.FormatInt(n, 10) != id {
				return out, payment.ErrInvalid
			}
			if key == "appwxmp" {
				out.AppWXMP = &n
			} else {
				out.AppWXA = &n
			}
		case "app_types":
			var products []string
			if json.Unmarshal(value, &products) != nil || len(products) > 16 {
				return out, payment.ErrInvalid
			}
			seen := map[string]bool{}
			for _, product := range products {
				if seen[product] {
					return out, payment.ErrInvalid
				}
				seen[product] = true
			}
			out.AppTypes = &products
		case "config":
			out.Config, e = parentValueEdits(value)
		case "certificates":
			out.Certificates, e = parentValueEdits(value)
		default:
			return out, payment.ErrInvalid
		}
		if e != nil {
			return out, e
		}
	}
	return out, nil
}
func parentChannelError(w http.ResponseWriter, e error) {
	var pgerr *pgconn.PgError
	switch {
	case errors.Is(e, identity.ErrAdminSession):
		response(w, 403, -1, "original super administrator session required", nil)
	case errors.Is(e, payment.ErrNotFound):
		response(w, 404, -1, "runtime parent channel unavailable", nil)
	case errors.Is(e, payment.ErrConflict), errors.As(e, &pgerr) && pgerr.Code == "23505":
		response(w, 409, -1, "runtime parent channel changed; read and confirm again", nil)
	case errors.Is(e, payment.ErrInvalid), errors.Is(e, payment.ErrState):
		response(w, 422, -1, "invalid or unavailable runtime parent configuration", nil)
	default:
		response(w, 503, -1, "runtime parent configuration could not be confirmed", nil)
	}
}
func (s *Server) createParentChannel(w http.ResponseWriter, r *http.Request, session identity.Session, token string) {
	resolved, resolveErr := s.managedWeixinRuntime(r.Context())
	if resolveErr != nil {
		parentChannelError(w, resolveErr)
		return
	}
	s = resolved
	if r.URL.RawQuery != "" || r.URL.ForceQuery || strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		response(w, 400, -1, "invalid parent configuration request", nil)
		return
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1048576))
	if e != nil {
		response(w, 400, -1, "invalid parent configuration body", nil)
		return
	}
	fields, e := parentExactObject(raw)
	allowed := map[string]bool{"source_channel_id": true, "expected_source_fingerprint": true, "request_id": true, "name": true, "rate": true, "mode": true, "account": true, "copy_from_channel_id": true, "expected_copy_fingerprint": true}
	if e != nil || len(fields) != 7 && len(fields) != 9 {
		response(w, 400, -1, "invalid parent configuration fields", nil)
		return
	}
	for key := range fields {
		if !allowed[key] {
			response(w, 400, -1, "invalid parent configuration fields", nil)
			return
		}
	}
	var source string
	var in payment.ParentChannelCreate
	if fields["copy_from_channel_id"] != nil || fields["expected_copy_fingerprint"] != nil {
		var copyID string
		if json.Unmarshal(fields["copy_from_channel_id"], &copyID) != nil || json.Unmarshal(fields["expected_copy_fingerprint"], &in.ExpectedCopy) != nil {
			response(w, 400, -1, "invalid copy source", nil)
			return
		}
		var ok bool
		in.CopyFrom, ok = positiveFundsID(copyID)
		if !ok || !identityHex(in.ExpectedCopy) {
			response(w, 400, -1, "invalid copy source", nil)
			return
		}
	}
	if json.Unmarshal(fields["source_channel_id"], &source) != nil || json.Unmarshal(fields["expected_source_fingerprint"], &in.ExpectedSource) != nil || json.Unmarshal(fields["request_id"], &in.RequestID) != nil || json.Unmarshal(fields["name"], &in.Name) != nil || json.Unmarshal(fields["rate"], &in.Rate) != nil || json.Unmarshal(fields["mode"], &in.Mode) != nil {
		response(w, 400, -1, "invalid parent configuration values", nil)
		return
	}
	sourceID, ok := positiveFundsID(source)
	if !ok {
		response(w, 400, -1, "invalid runtime authorization source", nil)
		return
	}
	in.Account, e = parentAccountChange(fields["account"])
	if e != nil {
		response(w, 400, -1, "invalid parent account fields", nil)
		return
	}
	t, e := s.ProviderFlow.ParentChannelTemplate(sourceID, s.MerchantChannels)
	if e != nil {
		parentChannelError(w, e)
		return
	}
	actor := payment.ParentChannelActor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
	reserved := []int64{}
	for id := range s.ProviderFlow.Providers {
		reserved = append(reserved, id)
	}
	out, e := s.Store.CreateParentChannel(r.Context(), actor, t, in, reserved)
	if e != nil {
		parentChannelError(w, e)
		return
	}
	response(w, 200, 0, "", out)
}

func (s *Server) updateParentChannel(w http.ResponseWriter, r *http.Request, session identity.Session, token string, id int64) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery || strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		response(w, 400, -1, "invalid parent configuration request", nil)
		return
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1048576))
	if e != nil {
		response(w, 400, -1, "invalid parent configuration body", nil)
		return
	}
	fields, e := parentExactObject(raw)
	if e != nil || len(fields) < 2 {
		response(w, 400, -1, "invalid parent configuration fields", nil)
		return
	}
	var in payment.ParentChannelPatch
	if json.Unmarshal(fields["expected_fingerprint"], &in.Expected) != nil || !identityHex(in.Expected) {
		response(w, 400, -1, "invalid original state fingerprint", nil)
		return
	}
	for key, value := range fields {
		switch key {
		case "expected_fingerprint":
		case "name":
			var v string
			e = json.Unmarshal(value, &v)
			in.Name = &v
		case "rate":
			var v string
			e = json.Unmarshal(value, &v)
			in.Rate = &v
		case "enabled":
			var v bool
			e = json.Unmarshal(value, &v)
			in.Enabled = &v
		case "archived":
			var v bool
			e = json.Unmarshal(value, &v)
			in.Archived = &v
		case "mode":
			var v int
			e = json.Unmarshal(value, &v)
			in.Mode = &v
		case "account":
			var v adapter.ParentChannelAccountChange
			v, e = parentAccountChange(value)
			in.Account = &v
		default:
			response(w, 400, -1, "invalid parent configuration field", nil)
			return
		}
		if e != nil {
			response(w, 400, -1, "invalid parent configuration values", nil)
			return
		}
	}
	actor := payment.ParentChannelActor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
	if resolved, err := s.managedWeixinRuntime(r.Context()); err == nil {
		s = resolved
	} else if in.Account != nil && (in.Account.AppWXMP != nil || in.Account.AppWXA != nil) {
		parentChannelError(w, err)
		return
	}
	out, e := s.Store.UpdateParentChannel(r.Context(), actor, id, in, s.ProviderFlow.ParentFactories)
	if e != nil {
		parentChannelError(w, e)
		return
	}
	response(w, 200, 0, "", out)
}
