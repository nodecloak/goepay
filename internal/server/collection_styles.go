package server

import (
	"encoding/json"
	"errors"
	"goepay/internal/backoffice"
	"goepay/internal/identity"
	"net/http"
	"strconv"
)

func (s *Server) collectionStylesConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || parts[1] != "collection-styles" {
		return false
	}
	if session.Issuer != nil {
		response(w, 403, -1, "own session required", nil)
		return true
	}
	svc := backoffice.New(s.Pool, s.Store)
	p := backoffice.Principal{Role: role, SubjectID: session.User.ID, Username: session.User.Username, AdminClass: session.User.AdminClass}
	fail := func(e error) {
		var feature backoffice.FeatureClosedError
		switch {
		case errors.As(e, &feature):
			response(w, 403, -1, feature.Message, nil)
		case errors.Is(e, backoffice.ErrInvalid):
			response(w, 422, -1, "invalid collection layout or revision", nil)
		case errors.Is(e, backoffice.ErrConflict):
			response(w, 409, -1, "style changed; reload before saving", nil)
		case errors.Is(e, backoffice.ErrNotFound):
			response(w, 404, -1, "style not found", nil)
		case errors.Is(e, backoffice.ErrForbidden):
			response(w, 403, -1, "style access forbidden", nil)
		default:
			s.fail(w, e)
		}
	}
	if r.URL.RawQuery != "" {
		response(w, 400, -1, "style query parameters not accepted", nil)
		return true
	}
	if len(parts) == 2 && r.Method == "GET" {
		items, e := svc.CollectionStyles(r.Context(), p)
		if e != nil {
			fail(e)
		} else {
			response(w, 200, 0, "", map[string]any{"items": items})
		}
		return true
	}
	if role != "admin" {
		response(w, 403, -1, "administrator required", nil)
		return true
	}
	if len(parts) == 2 && r.Method == "POST" {
		var input map[string]json.RawMessage
		if !decode(w, r, &input) {
			return true
		}
		item, e := svc.Create(r.Context(), p, "collection-styles", input)
		if e != nil {
			fail(e)
			return true
		}
		out, e := backoffice.StyleItem(item)
		if e != nil {
			fail(e)
		} else {
			response(w, 201, 0, "", out)
		}
		return true
	}
	if len(parts) == 3 {
		id, e := strconv.ParseInt(parts[2], 10, 64)
		if e != nil || id < 1 || strconv.FormatInt(id, 10) != parts[2] {
			response(w, 400, -1, "invalid style ID", nil)
			return true
		}
		switch r.Method {
		case "GET":
			item, e := svc.Get(r.Context(), p, "collection-styles", id)
			if e != nil {
				fail(e)
				return true
			}
			out, e := backoffice.StyleItem(item)
			if e != nil {
				fail(e)
			} else {
				response(w, 200, 0, "", out)
			}
			return true
		case "PUT":
			var input struct {
				Expected string                     `json:"expected_revision"`
				Style    map[string]json.RawMessage `json:"style"`
			}
			if !decode(w, r, &input) {
				return true
			}
			item, e := svc.UpdateCollectionStyle(r.Context(), p, id, input.Expected, input.Style)
			if e != nil {
				fail(e)
				return true
			}
			out, e := backoffice.StyleItem(item)
			if e != nil {
				fail(e)
			} else {
				response(w, 200, 0, "", out)
			}
			return true
		case "DELETE":
			e := svc.Delete(r.Context(), p, "collection-styles", id)
			if e != nil {
				fail(e)
			} else {
				response(w, 200, 0, "style deleted", nil)
			}
			return true
		}
	}
	response(w, 405, -1, "unsupported style action", nil)
	return true
}
