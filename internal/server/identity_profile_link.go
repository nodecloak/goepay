package server

import (
	"context"
	"net/http"
	"time"

	"goepay/internal/channelconfig"
	"goepay/internal/payment"
)

func profileMiniLinkReady(p channelconfig.IdentityProfile) bool {
	return p.URLLinkAvailable && p.Provider == "wxpay" && p.Kind == "mini" && p.MiniLink != nil && p.MiniLink.ID() == p.Provider && p.MiniLink.ApplicationID() == p.AppID
}

// The console owner, CSRF and original request binding have already been
// checked. GET only reads the durable intent; it never queries the provider.
func (s *Server) profileMiniLink(w http.ResponseWriter, r *http.Request, id, token, role string, p channelconfig.IdentityProfile, b payment.IdentityProfileBinding) {
	if !profileMiniLinkReady(p) {
		response(w, 503, -1, "original mini-program URL Link profile unavailable", nil)
		return
	}
	if r.Method == http.MethodGet {
		q, e := s.Store.ReadProfileIdentityLink(r.Context(), id, token, role, b)
		if e != nil {
			response(w, 409, -1, "original URL Link intent unavailable", nil)
			return
		}
		response(w, 200, 0, "local original URL Link status", q)
		return
	}
	if r.Method != http.MethodPost {
		response(w, 405, -1, "GET or POST required", nil)
		return
	}
	in, ok := identityJSON(w, r, "state")
	if !ok {
		return
	}
	if !identityHex(in["state"]) {
		response(w, 400, -1, "original SDK state required", nil)
		return
	}
	q, send, e := s.Store.BeginProfileIdentityLink(r.Context(), id, token, role, in["state"], b)
	if e != nil {
		response(w, 409, -1, "original URL Link intent unavailable", nil)
		return
	}
	if !send {
		response(w, 200, 0, "existing original URL Link intent; no generation repeated", q)
		return
	}
	// Recheck after the irreversible database claim and before credential I/O.
	meta, e := s.Store.ProfileIdentityLookupMetadata(r.Context(), id)
	if e != nil {
		response(w, 409, -1, "original identity request unavailable", nil)
		return
	}
	p, b, ready := s.identityProfileBinding(r.Context(), meta.MerchantID, meta.ProfileID)
	if !ready || !profileMiniLinkReady(p) || !s.Store.MatchesProfileIdentityLookup(meta, b) {
		response(w, 409, -1, "original URL Link profile changed", nil)
		return
	}
	if _, e = s.Store.ReadProfileIdentityLink(r.Context(), id, token, role, b); e != nil {
		response(w, 409, -1, "original URL Link authority unavailable", nil)
		return
	}
	result, generateError := p.MiniLink.GenerateIdentityLink(r.Context(), q.Generation)
	// Persist a terminal outcome even when the initiating browser disconnected.
	// All core methods revalidate the original session and current profile; a
	// revoked intent remains permanently claimed and cannot disclose the URL.
	finishCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, currentBinding, stillReady := s.identityProfileBinding(finishCtx, meta.MerchantID, meta.ProfileID)
	if !stillReady || !s.Store.MatchesProfileIdentityLookup(meta, currentBinding) {
		response(w, 409, -1, "original URL Link authority changed", nil)
		return
	}
	if generateError != nil {
		e = s.Store.FailProfileIdentityLink(finishCtx, id, token, role, currentBinding, generateError)
	} else {
		e = s.Store.CompleteProfileIdentityLink(finishCtx, id, token, role, currentBinding, result)
	}
	if e != nil {
		response(w, 409, -1, "URL Link outcome could not be stored; do not repeat generation", nil)
		return
	}
	stored, e := s.Store.ReadProfileIdentityLink(finishCtx, id, token, role, currentBinding)
	if e != nil {
		response(w, 409, -1, "original URL Link result unavailable", nil)
		return
	}
	response(w, 200, 0, "original URL Link status; SDK identity only", stored)
}
