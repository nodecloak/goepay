package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func childExactObject(raw []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	v, e := d.Token()
	if e != nil || v != json.Delim('{') {
		return nil, payment.ErrInvalid
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		k, e := d.Token()
		key, ok := k.(string)
		if e != nil || !ok || out[key] != nil {
			return nil, payment.ErrInvalid
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(value, []byte("null")) {
			return nil, payment.ErrInvalid
		}
		out[key] = value
	}
	if v, e = d.Token(); e != nil || v != json.Delim('}') {
		return nil, payment.ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, payment.ErrInvalid
	}
	return out, nil
}

func childConsoleBody(w http.ResponseWriter, r *http.Request) (map[string]json.RawMessage, error) {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return nil, payment.ErrInvalid
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 262144))
	v, e := d.Token()
	if e != nil || v != json.Delim('{') {
		return nil, payment.ErrInvalid
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		k, e := d.Token()
		name, ok := k.(string)
		if e != nil || !ok || out[name] != nil {
			return nil, payment.ErrInvalid
		}
		var raw json.RawMessage
		if d.Decode(&raw) != nil || string(raw) == "null" {
			return nil, payment.ErrInvalid
		}
		out[name] = raw
	}
	v, e = d.Token()
	if e != nil || v != json.Delim('}') {
		return nil, payment.ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, payment.ErrInvalid
	}
	return out, nil
}
func childConsoleID(raw json.RawMessage) (int64, bool) {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return 0, false
	}
	return positiveFundsID(text)
}
func childConsoleError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrAdminSession), errors.Is(e, identity.ErrOriginalMerchantSession):
		response(w, 403, -1, "original account session required", nil)
	case errors.Is(e, payment.ErrNotFound):
		response(w, 404, -1, "payment subchannel unavailable", nil)
	case errors.Is(e, payment.ErrConflict):
		response(w, 409, -1, "payment subchannel changed; refresh before confirming", nil)
	case errors.Is(e, payment.ErrInvalid):
		response(w, 422, -1, "invalid payment subchannel settings", nil)
	case errors.Is(e, payment.ErrState):
		response(w, 422, -1, "payment subchannel is unavailable for the current group or parent", nil)
	default:
		response(w, 503, -1, "payment subchannel could not be confirmed", nil)
	}
}
func (s *Server) paymentSubchannelConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 2 || parts[1] != "payment-subchannels" {
		return false
	}
	identityHeaders(w)
	if (role != "admin" && role != "merchant") || session.Issuer != nil || role == "admin" && session.User.AdminClass != "super" {
		response(w, 403, -1, "original account session required", nil)
		return true
	}
	if s.Store == nil {
		response(w, 503, -1, "payment subchannels unavailable", nil)
		return true
	}
	a := payment.SubchannelActor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
	if len(parts) == 4 && parts[3] == "collection-link" {
		id, ok := positiveFundsID(parts[2])
		if !ok {
			childConsoleError(w, payment.ErrInvalid)
			return true
		}
		s.subchannelCollectionLink(w, r, a, id)
		return true
	}
	if len(parts) == 4 && parts[3] == "test" {
		id, ok := positiveFundsID(parts[2])
		if !ok {
			childConsoleError(w, payment.ErrInvalid)
			return true
		}
		s.subchannelPaymentTest(w, r, a, id)
		return true
	}
	if len(parts) == 3 && parts[2] == "policy" {
		mid := session.User.ID
		q := r.URL.Query()
		if role == "admin" {
			var ok bool
			mid, ok = positiveFundsID(q.Get("merchant_id"))
			if !ok || len(q) != 1 || len(q["merchant_id"]) != 1 {
				childConsoleError(w, payment.ErrInvalid)
				return true
			}
		} else if len(q) != 0 {
			childConsoleError(w, payment.ErrInvalid)
			return true
		}
		if r.Method == "GET" {
			out, e := s.Store.SubchannelPolicy(r.Context(), a, mid)
			if e != nil {
				childConsoleError(w, e)
			} else {
				response(w, 200, 0, "", out)
			}
			return true
		}
		if r.Method != "PATCH" {
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
		fields, e := childConsoleBody(w, r)
		var roll int
		var expected string
		if e != nil || len(fields) != 2 || json.Unmarshal(fields["roll_type"], &roll) != nil || json.Unmarshal(fields["expected_fingerprint"], &expected) != nil || !identityHex(expected) {
			childConsoleError(w, payment.ErrInvalid)
			return true
		}
		out, e := s.Store.UpdateSubchannelPolicy(r.Context(), a, mid, roll, expected)
		if e != nil {
			childConsoleError(w, e)
		} else {
			response(w, 200, 0, "", out)
		}
		return true
	}
	if len(parts) == 3 && parts[2] == "schemas" && r.Method == "GET" {
		if role != "admin" {
			response(w, 403, -1, "original super administrator required", nil)
			return true
		}
		if r.URL.RawQuery != "" || s.ProviderFlow == nil {
			response(w, 400, -1, "invalid schema request", nil)
			return true
		}
		if e := s.Store.CheckSubchannelActor(r.Context(), a); e != nil {
			childConsoleError(w, e)
			return true
		}
		items := []any{}
		catalog, e := s.ProviderFlow.ParentSubchannelCatalog(r.Context())
		if e != nil {
			childConsoleError(w, e)
			return true
		}
		for _, f := range catalog.SubchannelFactories {
			if f == nil {
				continue
			}
			schema, e := s.Store.PublicSubchannelSchema(f.Schema())
			if e != nil {
				childConsoleError(w, e)
				return true
			}
			items = append(items, schema)
		}
		response(w, 200, 0, "", map[string]any{"items": items, "total": len(items)})
		return true
	}
	if len(parts) == 3 && parts[2] != "schemas" && r.URL.RawQuery == "" && !r.URL.ForceQuery {
		id, ok := positiveFundsID(parts[2])
		if !ok {
			childConsoleError(w, payment.ErrInvalid)
			return true
		}
		current, e := s.Store.Subchannel(r.Context(), a, id)
		if e != nil {
			childConsoleError(w, e)
			return true
		}
		if r.Method == "GET" {
			s.respondSubchannel(w, r, a, current)
			return true
		}
		if r.Method == "DELETE" {
			if role != "admin" {
				response(w, 403, -1, "original super administrator required", nil)
				return true
			}
			fields, e := childConsoleBody(w, r)
			var expected string
			if e != nil || len(fields) != 1 || json.Unmarshal(fields["expected_fingerprint"], &expected) != nil || !identityHex(expected) {
				childConsoleError(w, payment.ErrInvalid)
				return true
			}
			out, e := s.Store.ArchiveSubchannel(r.Context(), a, id, expected)
			if e != nil {
				childConsoleError(w, e)
			} else {
				s.respondSubchannel(w, r, a, out)
			}
			return true
		}
		if r.Method != "PATCH" {
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
		fields, e := childConsoleBody(w, r)
		if e != nil {
			childConsoleError(w, e)
			return true
		}
		in, e := childConsolePatch(fields, role == "admin")
		if e != nil {
			childConsoleError(w, e)
			return true
		}
		if len(fields) == 2 && in.Enabled != nil && !*in.Enabled {
			out, e := s.Store.StopSubchannel(r.Context(), a, id, in.Expected)
			if e != nil {
				childConsoleError(w, e)
			} else {
				s.respondSubchannel(w, r, a, out)
			}
			return true
		}
		parent := current.ParentID
		if in.ParentID != 0 {
			parent = in.ParentID
		}
		view, e := s.withParentRuntime(r.Context(), current.MerchantID, "")
		if e != nil {
			childConsoleError(w, e)
			return true
		}
		s = view
		if !s.childParentAllowed(current.MerchantID, parent) || s.ProviderFlow == nil || s.ProviderFlow.SubchannelFactories[parent] == nil {
			childConsoleError(w, payment.ErrState)
			return true
		}
		b, e := s.ProviderFlow.RuntimeChannel(parent)
		if e != nil {
			childConsoleError(w, e)
			return true
		}
		out, e := s.Store.UpdateSubchannel(r.Context(), a, id, b, s.ProviderFlow.SubchannelFactories[parent], in)
		if e != nil {
			childConsoleError(w, e)
		} else {
			s.respondSubchannel(w, r, a, out)
		}
		return true
	}
	if len(parts) != 2 || r.URL.ForceQuery || r.Method != "GET" && r.URL.RawQuery != "" {
		response(w, 400, -1, "invalid subchannel request", nil)
		return true
	}
	if r.Method == "GET" {
		filter, e := childConsoleListFilter(r, role, session.User.ID)
		if e != nil {
			childConsoleError(w, e)
			return true
		}
		out, e := s.Store.SubchannelPage(r.Context(), a, filter)
		if e == nil && s.ProviderFlow != nil {
			out.Items, e = s.projectConsoleSubchannels(r.Context(), a, out.Items)
		}
		if e != nil {
			childConsoleError(w, e)
		} else {
			response(w, 200, 0, "", out)
		}
		return true
	}

	if r.Method != "POST" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if role != "admin" {
		response(w, 403, -1, "only the original super administrator can configure an account", nil)
		return true
	}
	fields, e := childConsoleBody(w, r)
	if e != nil {
		childConsoleError(w, e)
		return true
	}
	allowed := map[string]bool{"merchant_id": true, "parent_id": true, "name": true, "enabled": true, "weight": true, "min_amount": true, "max_amount": true, "values": true}
	if len(fields) != len(allowed) {
		childConsoleError(w, payment.ErrInvalid)
		return true
	}
	for k := range fields {
		if !allowed[k] {
			childConsoleError(w, payment.ErrInvalid)
			return true
		}
	}
	var in payment.SubchannelWrite
	var ok bool
	in.MerchantID, ok = childConsoleID(fields["merchant_id"])
	if !ok {
		childConsoleError(w, payment.ErrInvalid)
		return true
	}
	in.ParentID, ok = childConsoleID(fields["parent_id"])
	if !ok {
		childConsoleError(w, payment.ErrInvalid)
		return true
	}
	values, valuesError := childExactObject(fields["values"])
	if json.Unmarshal(fields["name"], &in.Name) != nil || json.Unmarshal(fields["enabled"], &in.Enabled) != nil || json.Unmarshal(fields["weight"], &in.Weight) != nil || valuesError != nil {
		childConsoleError(w, payment.ErrInvalid)
		return true
	}
	in.Values = map[string]string{}
	for key, raw := range values {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			childConsoleError(w, payment.ErrInvalid)
			return true
		}
		in.Values[key] = value
	}
	for k, dest := range map[string]*int64{"min_amount": &in.MinAmount, "max_amount": &in.MaxAmount} {
		var text string
		if json.Unmarshal(fields[k], &text) != nil {
			childConsoleError(w, payment.ErrInvalid)
			return true
		}
		n, e := strconv.ParseInt(text, 10, 64)
		if e != nil || n < 0 || strconv.FormatInt(n, 10) != text {
			childConsoleError(w, payment.ErrInvalid)
			return true
		}
		*dest = n
	}
	view, e := s.withParentRuntime(r.Context(), in.MerchantID, "")
	if e != nil {
		childConsoleError(w, e)
		return true
	}
	s = view
	if s.ProviderFlow == nil || s.ProviderFlow.SubchannelFactories[in.ParentID] == nil {
		childConsoleError(w, payment.ErrInvalid)
		return true
	}
	if !s.childParentAllowed(in.MerchantID, in.ParentID) {
		childConsoleError(w, payment.ErrState)
		return true
	}
	b, e := s.ProviderFlow.RuntimeChannel(in.ParentID)
	if e != nil {
		childConsoleError(w, e)
		return true
	}
	out, e := s.Store.CreateSubchannel(r.Context(), a, b, s.ProviderFlow.SubchannelFactories[in.ParentID], in)
	if e != nil {
		childConsoleError(w, e)
	} else {
		s.respondSubchannel(w, r, a, out)
	}
	return true
}

func childConsolePatch(fields map[string]json.RawMessage, admin bool) (payment.SubchannelPatch, error) {
	var in payment.SubchannelPatch
	if len(fields) < 2 || json.Unmarshal(fields["expected_fingerprint"], &in.Expected) != nil || !identityHex(in.Expected) {
		return in, payment.ErrInvalid
	}
	for k, raw := range fields {
		switch k {
		case "expected_fingerprint":
		case "name":
			if !admin {
				return in, payment.ErrInvalid
			}
			var v string
			if json.Unmarshal(raw, &v) != nil {
				return in, payment.ErrInvalid
			}
			in.Name = &v
		case "enabled":
			var v bool
			if json.Unmarshal(raw, &v) != nil {
				return in, payment.ErrInvalid
			}
			in.Enabled = &v
		case "weight":
			var v int64
			if json.Unmarshal(raw, &v) != nil {
				return in, payment.ErrInvalid
			}
			in.Weight = &v
		case "min_amount", "max_amount":
			var text string
			if json.Unmarshal(raw, &text) != nil {
				return in, payment.ErrInvalid
			}
			v, e := strconv.ParseInt(text, 10, 64)
			if e != nil || v < 0 || strconv.FormatInt(v, 10) != text {
				return in, payment.ErrInvalid
			}
			if k == "min_amount" {
				in.MinAmount = &v
			} else {
				in.MaxAmount = &v
			}
		case "parent_id":
			if !admin {
				return in, payment.ErrInvalid
			}
			v, ok := childConsoleID(raw)
			if !ok {
				return in, payment.ErrInvalid
			}
			in.ParentID = v
		case "values":
			if !admin {
				return in, payment.ErrInvalid
			}
			edits, e := childExactObject(raw)
			if e != nil || len(edits) == 0 {
				return in, payment.ErrInvalid
			}
			in.Values = map[string]payment.SubchannelValueEdit{}
			for key, raw := range edits {
				fields, e := childExactObject(raw)
				if e != nil || len(fields) < 1 || len(fields) > 2 {
					return in, payment.ErrInvalid
				}
				var edit payment.SubchannelValueEdit
				for name, raw := range fields {
					switch name {
					case "operation":
						if json.Unmarshal(raw, &edit.Operation) != nil {
							return in, payment.ErrInvalid
						}
					case "value":
						if json.Unmarshal(raw, &edit.Value) != nil {
							return in, payment.ErrInvalid
						}
					default:
						return in, payment.ErrInvalid
					}
				}
				in.Values[key] = edit
			}
		default:
			return in, payment.ErrInvalid
		}
	}
	return in, nil
}
func (s *Server) childParentAllowed(mid, parent int64) bool {
	for _, id := range s.MerchantChannels[mid] {
		if id == parent {
			return true
		}
	}
	return false
}

func childConsoleListFilter(r *http.Request, role string, mid int64) (payment.SubchannelListFilter, error) {
	out := payment.SubchannelListFilter{Page: 1, Size: 10}
	q := r.URL.Query()
	for key, values := range q {
		if len(values) != 1 {
			return out, payment.ErrInvalid
		}
		switch key {
		case "page", "size":
			n, e := strconv.Atoi(values[0])
			if e != nil || strconv.Itoa(n) != values[0] || n < 1 {
				return out, payment.ErrInvalid
			}
			if key == "page" {
				out.Page = n
			} else {
				out.Size = n
			}
		case "id", "parent_id", "merchant_id":
			n, ok := positiveFundsID(values[0])
			if !ok {
				return out, payment.ErrInvalid
			}
			if key == "id" {
				out.ID = n
			} else if key == "parent_id" {
				out.ParentID = n
			} else if role == "admin" {
				out.MerchantID = n
			} else {
				return out, payment.ErrInvalid
			}
		case "type":
			out.PayType = values[0]
		case "name":
			out.Name = values[0]
		default:
			return out, payment.ErrInvalid
		}
	}
	if role == "merchant" {
		out.MerchantID = mid
	}
	return out, nil
}

func (s *Server) respondSubchannel(w http.ResponseWriter, r *http.Request, a payment.SubchannelActor, out payment.PaymentSubchannel) {
	if s.ProviderFlow != nil {
		items, e := s.projectConsoleSubchannels(r.Context(), a, []payment.PaymentSubchannel{out})
		if e != nil {
			childConsoleError(w, e)
			return
		}
		out = items[0]
	}
	response(w, 200, 0, "", out)
}
