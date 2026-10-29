package backoffice

import (
	"encoding/json"
	"errors"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

func write(w http.ResponseWriter, status int, data any, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	code := 0
	if status >= 400 {
		code = -1
	}
	json.NewEncoder(w).Encode(map[string]any{"code": code, "message": message, "data": data})
}
func writeError(w http.ResponseWriter, e error) {
	status, msg := 500, "operation failed"
	var fc FeatureClosedError
	if errors.As(e, &fc) {
		write(w, 403, nil, fc.Message)
		return
	}
	var refused PaymentRefusedError
	if errors.As(e, &refused) {
		write(w, 403, nil, refused.Message)
		return
	}
	switch {
	case errors.Is(e, identity.ErrFundsAuthorization), errors.Is(e, identity.ErrFundsNotConfigured):
		write(w, 428, map[string]string{"error": "PAYMENT_AUTH_REQUIRED"}, "fresh funds action authorization required")
		return
	case errors.Is(e, identity.ErrFundsAuthentication):
		status, msg = 403, "funds authentication failed"
	case errors.Is(e, ErrNotFound), errors.Is(e, payment.ErrNotFound):
		status, msg = 404, "not found"
	case errors.Is(e, ErrForbidden):
		status, msg = 403, "operation forbidden"
	case errors.Is(e, ErrConflict), errors.Is(e, payment.ErrConflict), errors.Is(e, payment.ErrInsufficient):
		status, msg = 409, "resource conflict or insufficient funds"
	case errors.Is(e, ErrInvalid), errors.Is(e, payment.ErrInvalid), errors.Is(e, payment.ErrState):
		status, msg = 422, "invalid input or state"
	}
	write(w, status, nil, msg)
}
func flatten(i Item) map[string]any {
	out := map[string]any{"id": strconv.FormatInt(i.ID, 10)}
	for k, v := range i.Fields {
		if k != "password" && k != "api_key" && k != "rsa_public_key" {
			out[k] = v
		}
	}
	return out
}
func body(w http.ResponseWriter, r *http.Request) (map[string]json.RawMessage, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	var v map[string]json.RawMessage
	if e := d.Decode(&v); e != nil || v == nil {
		write(w, 400, nil, "invalid JSON")
		return nil, false
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		write(w, 400, nil, "trailing JSON")
		return nil, false
	}
	return v, true
}

// Serve runs only after the application's authentication, account-enabled, and
// CSRF middleware have validated Principal. parts begin with the resource ID.
func (s *Service) Serve(w http.ResponseWriter, r *http.Request, p Principal, parts []string) {
	if s.serveGroupRoutingCatalog(w, r, p, parts) {
		return
	}
	if s.serveCase(w, r, p, parts) {
		return
	}
	if len(parts) == 0 || len(parts) > 2 {
		write(w, 404, nil, "not found")
		return
	}
	resource := parts[0]
	if len(parts) == 2 && parts[1] == "schema" && r.Method == "GET" {
		schema, e := s.Schema(p, resource)
		if e != nil {
			writeError(w, e)
			return
		}
		write(w, 200, schema, "")
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case "GET":
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			query := Query{Page: page, Limit: limit, Search: r.URL.Query().Get("q")}
			if resource == "merchants" {
				params, parse := url.ParseQuery(r.URL.RawQuery)
				if parse != nil {
					write(w, 400, nil, "invalid merchant query")
					return
				}
				// CSV export (A6-08): detect and strip the format param before the
				// directory parser validates the remaining allow-listed keys.
				csvExport := false
				if formats, ok := params["format"]; ok {
					if len(formats) != 1 || formats[0] != "csv" {
						write(w, 400, nil, "format must be csv")
						return
					}
					csvExport = true
					params.Del("format")
				}
				filter, search, parse := ParseMerchantDirectoryQuery(params)
				if parse != nil {
					write(w, 400, nil, "invalid or unsupported merchant filter")
					return
				}
				if csvExport {
					s.exportMerchantDirectoryCSV(w, r, p, filter, search)
					return
				}
				query.Merchant = filter
				query.Search = search
			} else {
				// Generic per-field toolbar filters declared by the schema, plus
				// the bounded CSV export for an exportable resource.
				if d, e := definitionFor(p, resource, false); e == nil {
					for _, fs := range d.filters {
						if v := r.URL.Query().Get(fs.Param); v != "" {
							if query.Filter == nil {
								query.Filter = map[string]string{}
							}
							query.Filter[fs.Param] = v
						}
					}
					if formats, ok := r.URL.Query()["format"]; ok {
						if len(formats) != 1 || formats[0] != "csv" {
							write(w, 400, nil, "format must be csv")
							return
						}
						s.exportResourceCSV(w, r, p, resource, query)
						return
					}
				}
			}
			v, e := s.List(r.Context(), p, resource, query)
			if e != nil {
				writeError(w, e)
				return
			}
			items := []map[string]any{}
			for _, item := range v.Items {
				items = append(items, flatten(item))
			}
			write(w, 200, map[string]any{"items": items, "total": v.Total, "page": v.Page, "limit": v.Limit}, "")
			return
		case "POST":
			in, ok := body(w, r)
			if !ok {
				return
			}
			v, e := s.Create(r.Context(), p, resource, in)
			if e != nil {
				writeError(w, e)
				return
			}
			write(w, 201, flatten(v), "")
			return
		}
		write(w, 405, nil, "method not allowed")
		return
	}
	if len(parts) == 2 && parts[1] == "batch-delete" && r.Method == "POST" {
		s.serveBatchDelete(w, r, p, resource)
		return
	}
	id, e := strconv.ParseInt(parts[1], 10, 64)
	if e != nil || id < 0 || id == 0 && (resource != "groups" || parts[1] != "0") {
		write(w, 400, nil, "invalid ID")
		return
	}
	switch r.Method {
	case "GET":
		v, e := s.Get(r.Context(), p, resource, id)
		if e != nil {
			writeError(w, e)
			return
		}
		write(w, 200, flatten(v), "")
	case "PATCH", "PUT":
		in, ok := body(w, r)
		if !ok {
			return
		}
		v, e := s.Update(r.Context(), p, resource, id, in)
		if e != nil {
			writeError(w, e)
			return
		}
		write(w, 200, flatten(v), "")
	case "DELETE":
		if e = s.Delete(r.Context(), p, resource, id); e != nil {
			writeError(w, e)
			return
		}
		write(w, 200, map[string]bool{"deleted": true}, "")
	default:
		write(w, 405, nil, "method not allowed")
	}
}
