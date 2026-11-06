package server

import (
	"net/http"
	"strings"
	"time"

	"goepay/internal/captcha"
	"goepay/internal/logincaptcha"
)

func NewLoginCaptchaService205ForMaster(s *Server, verifier captcha.Verifier, master []byte, origin string) (*logincaptcha.Service, error) {
	if s == nil {
		return nil, logincaptcha.ErrUnavailable
	}
	return logincaptcha.New(s.Pool, master, origin, verifier, func(remote, action string) bool {
		return s.allowCollectionAttempt(remote, "login-captcha205:"+action, time.Now())
	})
}

// Fallback is for existing zero-injection fixtures only. Main injects its own
// bootstrap master and configured console origin; no provider mode is enabled.
func (s *Server) loginCaptchaService205() (*logincaptcha.Service, error) {
	if s.LoginCaptcha205 != nil {
		return s.LoginCaptcha205, nil
	}
	origin := ""
	if s.ProviderFlow != nil {
		origin = strings.TrimRight(s.ProviderFlow.CallbackBaseURL, "/")
	}
	return NewLoginCaptchaService205ForMaster(s, nil, s.CheckoutKey, origin)
}
func (s *Server) loginCaptchaRoutes205(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != logincaptcha.Path && r.URL.Path != logincaptcha.Path+"/init" {
		return false
	}
	service, e := s.loginCaptchaService205()
	if e != nil {
		LoginCaptchaError205(w, e)
		return true
	}
	service.ServeHTTP(w, r)
	return true
}
func LoginCaptchaError205(w http.ResponseWriter, e error) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	logincaptcha.WriteError(w, e)
}
