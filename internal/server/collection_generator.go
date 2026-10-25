package server

import (
	"crypto/hmac"
	"errors"
	"goepay/internal/backoffice"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/payment"
	"net/http"
	"net/url"
	"strconv"
)

func (s *Server) generatedCollectionURL(g payment.CollectionGeneration) string {
	id := strconv.FormatInt(g.CodeID, 10)
	q := url.Values{"mode": {"generated"}, "config": {g.ConfigHash}, "token": {s.checkoutToken(id, "collection-generated:"+g.ConfigHash, "permanent")}}
	return g.Origin + "/collect/" + id + "?" + q.Encode()
}
func (s *Server) collectionGenerationAuthorized(r *http.Request, id, hash string) bool {
	q := r.URL.Query()
	if len(s.CheckoutKey) < 32 || len(q) != 3 || len(q["mode"]) != 1 || q.Get("mode") != "generated" || len(q["config"]) != 1 || q.Get("config") != hash || len(q["token"]) != 1 {
		return false
	}
	return hmac.Equal([]byte(q.Get("token")), []byte(s.checkoutToken(id, "collection-generated:"+hash, "permanent")))
}
func (s *Server) collectionGeneratorConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || parts[1] != "collection-generator" {
		return false
	}
	if role != "merchant" || session.Issuer != nil {
		response(w, 403, -1, "own merchant session required", nil)
		return true
	}
	if len(parts) != 2 || r.URL.RawQuery != "" {
		response(w, 400, -1, "invalid generator path or query", nil)
		return true
	}
	if s.ProviderFlow == nil || s.Store == nil || len(s.CheckoutKey) < 32 {
		response(w, 503, -1, "collection configuration unavailable", nil)
		return true
	}
	origin, e := payment.CollectionOrigin(s.ProviderFlow.CallbackBaseURL)
	if e != nil {
		response(w, 503, -1, "collection origin unavailable", nil)
		return true
	}
	fail := func(e error) {
		switch {
		case errors.Is(e, payment.ErrInvalid):
			response(w, 422, -1, "invalid collection configuration or unavailable origin/style", nil)
		case errors.Is(e, payment.ErrConflict):
			response(w, 409, -1, "generation request changed or source invalid", nil)
		case errors.Is(e, payment.ErrState):
			response(w, 403, -1, "merchant disabled", nil)
		default:
			s.fail(w, e)
		}
	}
	result := func(g payment.CollectionGeneration) map[string]any {
		out := map[string]any{"code_id": strconv.FormatInt(g.CodeID, 10), "recipient_name": g.RecipientName, "style_id": strconv.FormatInt(g.StyleID, 10), "origin": g.Origin, "fixed_amount_cents": strconv.FormatInt(g.FixedAmount, 10), "config_hash": g.ConfigHash, "enabled": g.Enabled, "replayed": g.Replayed, "url": "", "expires_at": nil}
		if g.Enabled {
			out["url"] = s.generatedCollectionURL(g)
		}
		return out
	}
	switch r.Method {
	case "GET":
		var name string
		if e = s.Pool.QueryRow(r.Context(), `SELECT name FROM merchants WHERE id=$1 AND enabled`, session.User.ID).Scan(&name); e != nil {
			response(w, 403, -1, "merchant unavailable", nil)
			return true
		}
		styles, e := backoffice.New(s.Pool, s.Store).CollectionStyles(r.Context(), backoffice.Principal{Role: role, SubjectID: session.User.ID})
		if e != nil {
			s.fail(w, e)
			return true
		}
		latest, e := s.Store.LatestCollectionGeneration(r.Context(), session.User.ID)
		if e != nil {
			fail(e)
			return true
		}
		var current any
		if latest != nil {
			current = result(*latest)
		}
		response(w, 200, 0, "", map[string]any{"recipient_name": name, "styles": styles, "domains": []map[string]string{{"origin": origin, "label": origin}}, "latest": current})
	case "POST":
		var input struct {
			ExternalNo    string `json:"external_no"`
			RecipientName string `json:"recipient_name"`
			StyleID       string `json:"style_id"`
			Origin        string `json:"origin"`
			Amount        string `json:"amount"`
		}
		if !decode(w, r, &input) {
			return true
		}
		sid, parse := strconv.ParseInt(input.StyleID, 10, 64)
		var amount int64
		if input.Amount != "" {
			amount, e = ledger.Parse(input.Amount)
			if e != nil || amount < 1 {
				response(w, 422, -1, "invalid fixed amount", nil)
				return true
			}
		}
		if parse != nil || sid < 0 || strconv.FormatInt(sid, 10) != input.StyleID {
			response(w, 422, -1, "invalid collection style", nil)
			return true
		}
		g, e := s.Store.GenerateCollectionCode(r.Context(), session.User.ID, payment.CollectionGenerationInput{ExternalNo: input.ExternalNo, RecipientName: input.RecipientName, StyleID: sid, Origin: input.Origin, FixedAmount: amount}, origin, session.User.Username)
		if e != nil {
			fail(e)
			return true
		}
		status := 201
		if g.Replayed {
			status = 200
		}
		response(w, status, 0, "", result(g))
	default:
		response(w, 405, -1, "unsupported generator operation", nil)
	}
	return true
}
