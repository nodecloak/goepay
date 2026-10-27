package enrollment

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
)

type accountVerification197 struct {
	http.Handler
	service *Service
}

func (h accountVerification197) AccountVerification197() *Service { return h.service }

// NewHandler is mounted before authenticated console routing. No handler here
// creates a session. Origin is the configured public origin, not proxy input.
func NewHandler(s *Service, origin string) http.Handler {
	u, e := url.Parse(origin)
	validOrigin := e == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/")
	configuredOrigin := ""
	if validOrigin {
		configuredOrigin = u.Scheme + "://" + u.Host
	}
	return accountVerification197{service: s, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if s == nil || !validOrigin {
			reply(w, 503, "verification unavailable", nil)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != configuredOrigin {
			reply(w, 403, "origin rejected", nil)
			return
		}
		const prefix = "/console/v1/public/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			reply(w, 404, "not found", nil)
			return
		}
		action := strings.TrimPrefix(r.URL.Path, prefix)
		if action == "capabilities" {
			if r.Method != "GET" {
				w.Header().Set("Allow", "GET")
				reply(w, 405, "method not allowed", nil)
				return
			}
			v, e := s.Capabilities(r.Context())
			if e != nil {
				replyError(w, e)
				return
			}
			reply(w, 200, "", v)
			return
		}
		if action != "verification-code" && action != "register" && action != "recover-password" {
			reply(w, 404, "not found", nil)
			return
		}
		if r.Method != "POST" {
			w.Header().Set("Allow", "POST")
			reply(w, 405, "method not allowed", nil)
			return
		}
		mt, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if e != nil || mt != "application/json" {
			reply(w, 415, "JSON content type required", nil)
			return
		}
		source, _, e := net.SplitHostPort(r.RemoteAddr)
		if e != nil {
			source = r.RemoteAddr
		}
		if net.ParseIP(source) == nil {
			reply(w, 400, "invalid connection source", nil)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		decode := func(out any) bool {
			d := json.NewDecoder(r.Body)
			d.DisallowUnknownFields()
			if e := d.Decode(out); e != nil {
				reply(w, 400, "invalid JSON input", nil)
				return false
			}
			if e := d.Decode(&struct{}{}); !errors.Is(e, io.EOF) {
				reply(w, 400, "invalid JSON input", nil)
				return false
			}
			return true
		}
		switch action {
		case "verification-code":
			var in struct {
				Purpose string `json:"purpose"`
				Email   string `json:"email"`
			}
			if !decode(&in) {
				return
			}
			v, e := s.Issue(r.Context(), VerificationRequest{Purpose: in.Purpose, Email: in.Email, Source: source})
			if e != nil {
				replyError(w, e)
				return
			}
			reply(w, 202, "verification requested", v)
		case "register":
			var in struct {
				ChallengeID    string `json:"challenge_id"`
				Code           string `json:"code"`
				Email          string `json:"email"`
				Username       string `json:"username"`
				Name           string `json:"name"`
				Password       string `json:"password"`
				InviteCode     string `json:"invite_code"`
				PromotionToken string `json:"promotion_token"`
			}
			if !decode(&in) {
				return
			}
			v, e := s.Register(r.Context(), RegisterInput{ChallengeID: in.ChallengeID, Code: in.Code, Email: in.Email, Username: in.Username, Name: in.Name, Password: in.Password, InviteCode: in.InviteCode, PromotionToken: in.PromotionToken, Source: source})
			if e != nil {
				replyError(w, e)
				return
			}
			reply(w, 201, "registration completed; sign in normally", v)
		case "recover-password":
			var in struct {
				ChallengeID string `json:"challenge_id"`
				Code        string `json:"code"`
				Email       string `json:"email"`
				Password    string `json:"new_password"`
			}
			if !decode(&in) {
				return
			}
			e := s.Recover(r.Context(), RecoverInput{ChallengeID: in.ChallengeID, Code: in.Code, Email: in.Email, Password: in.Password, Source: source})
			if e != nil {
				replyError(w, e)
				return
			}
			reply(w, 200, "request completed; sign in normally", nil)
		}
	})}
}
func reply(w http.ResponseWriter, status int, message string, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	code := 0
	if status >= 400 {
		code = -1
	}
	json.NewEncoder(w).Encode(struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    any    `json:"data"`
	}{code, message, data})
}
func replyError(w http.ResponseWriter, e error) {
	status, message := 503, "verification unavailable"
	switch {
	case errors.Is(e, ErrLimited):
		status, message = 429, "verification rate limit exceeded"
	case errors.Is(e, ErrClosed):
		status, message = 403, "registration is closed"
	case errors.Is(e, ErrCode):
		status, message = 422, "invalid or expired verification"
	case errors.Is(e, ErrInvalid):
		status, message = 422, "invalid input"
	case errors.Is(e, ErrConflict):
		status, message = 409, "registration unavailable"
	}
	reply(w, status, message, nil)
}
