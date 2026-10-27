package server

import (
	"encoding/json"
	"errors"
	"goepay/internal/backoffice"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"io"
	"net/http"
	"strings"
)

func (s *Server) parentChannelSubject(w http.ResponseWriter, r *http.Request, session identity.Session, token, idText string) {
	id, ok := positiveFundsID(idText)
	if !ok || r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "invalid parent subject target", nil)
		return
	}
	if r.Method != "GET" && r.Method != "PATCH" {
		response(w, 405, -1, "method not allowed", nil)
		return
	}
	var in *backoffice.ParentChannelSubject
	expected := ""
	if r.Method == "PATCH" {
		if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
			response(w, 400, -1, "invalid subject content type", nil)
			return
		}
		raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 24000))
		fields, e2 := parentExactObject(raw)
		v := &backoffice.ParentChannelSubject{}
		if e != nil || e2 != nil || len(fields) != 3 || json.Unmarshal(fields["ordername"], &v.OrderName) != nil || json.Unmarshal(fields["ordername_rules"], &v.Rules) != nil || json.Unmarshal(fields["expected_fingerprint"], &expected) != nil || !identityHex(expected) {
			response(w, 400, -1, "invalid exact subject fields", nil)
			return
		}
		in = v
	}
	actor := payment.ParentChannelActor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
	out, e := backoffice.New(s.Pool, s.Store).ParentChannelSubject(r.Context(), actor, id, in, expected)
	if errors.Is(e, backoffice.ErrInvalid) {
		e = payment.ErrInvalid
	}
	if errors.Is(e, backoffice.ErrConflict) {
		e = payment.ErrConflict
	}
	if e != nil {
		parentChannelError(w, e)
		return
	}
	response(w, 200, 0, "", out)
}
