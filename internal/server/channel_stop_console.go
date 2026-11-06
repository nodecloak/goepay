package server

import (
	"context"
	"crypto/subtle"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"sort"
	"time"
)

func (s *Server) channelStopConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, p []string, token string) bool {
	if len(p) < 2 || p[1] != "channel-stops" {
		return false
	}
	identityHeaders(w)
	cookie, original := identityCookie(r, "goepay_"+role)
	if role != "admin" || session.User.Role != "admin" || session.User.AdminClass != "super" || session.Issuer != nil || !original || subtle.ConstantTimeCompare([]byte(cookie), []byte(token)) != 1 {
		response(w, 403, -1, "own super administrator session required", nil)
		return true
	}
	if r.Method != "GET" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1 {
		response(w, 403, -1, "invalid CSRF token", nil)
		return true
	}
	if s.Store == nil || s.Pool == nil {
		response(w, 503, -1, "channel stop status unavailable", nil)
		return true
	}
	if (len(p) != 2 && len(p) != 3 && len(p) != 4) || r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "canonical channel without query required", nil)
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if s.ProviderFlow == nil {
		response(w, 503, -1, "runtime channel catalog unavailable", nil)
		return true
	}
	kinds, catalogErr := s.ProviderFlow.ConsoleChannelKinds(ctx)
	if catalogErr != nil {
		s.fail(w, catalogErr)
		return true
	}
	if len(p) == 2 && r.Method == "GET" {
		if s.ProviderFlow == nil || len(s.ProviderFlow.Providers) > 1000 {
			response(w, 503, -1, "runtime channel catalog unavailable", nil)
			return true
		}
		ids := []int64{}
		for id := range kinds {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		type item struct {
			payment.ChannelWriteStatus
			Provider string `json:"provider"`
		}
		items := []item{}
		for _, id := range ids {
			status, e := s.Store.ChannelWriteStatus(ctx, id)
			if e != nil {
				s.fail(w, e)
				return true
			}
			items = append(items, item{status, kinds[id]})
		}
		response(w, 200, 0, "runtime channel write states", map[string]any{"items": items})
		return true
	}
	if len(p) == 2 {
		response(w, 405, -1, "channel catalog is read-only", nil)
		return true
	}
	id, ok := positiveFundsID(p[2])
	if !ok {
		response(w, 400, -1, "canonical channel required", nil)
		return true
	}
	if _, exists := kinds[id]; !exists {
		response(w, 404, -1, "runtime channel not found", nil)
		return true
	}
	if len(p) == 3 && r.Method == "GET" {
		status, e := s.Store.ChannelWriteStatus(ctx, id)
		if e != nil {
			s.fail(w, e)
		} else {
			response(w, 200, 0, "effective channel write status", status)
		}
		return true
	}
	if len(p) == 4 && p[3] == "resume" && r.Method == "POST" {
		fields, ok := identityJSON(w, r, "expected_event_id", "expected_revision")
		if !ok {
			return true
		}
		event, eventOK := positiveFundsID(fields["expected_event_id"])
		revision, revisionOK := positiveFundsID(fields["expected_revision"])
		if len(fields) != 2 || !eventOK || !revisionOK || revision >= 4096 {
			response(w, 400, -1, "exact stop event and revision required", nil)
			return true
		}
		actor := payment.SettlementPlanActor{Role: "admin", Class: session.User.AdminClass, Name: session.User.Username, ID: session.User.ID}
		status, e := s.Store.ResumeChannel(ctx, id, event, revision, token, actor)
		if isFundsAuthorizationError(e) {
			fundsHTTPError(w, e)
		} else if e != nil {
			s.fail(w, e)
		} else {
			response(w, 200, 0, "channel restored; original transfers were not resent", status)
		}
		return true
	}
	response(w, 405, -1, "explicit channel status or restoration method required", nil)
	return true
}
