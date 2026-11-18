package server

import (
	"errors"
	"net/http"

	"goepay/internal/cryptopay"
	"goepay/internal/identity"
)

func (s *Server) cryptoMonitoring(w http.ResponseWriter, r *http.Request, session identity.Session, token string) {
	if r.Method != "GET" {
		w.Header().Set("Allow", "GET")
		response(w, 405, -1, "method not allowed", nil)
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "USDT 监控不接受查询参数", nil)
		return
	}
	if s.Pool == nil || s.Store == nil {
		response(w, 503, -1, "USDT 监控暂时无法读取，请稍后刷新", nil)
		return
	}
	// The admin snapshot is available from the local catalog even before any
	// runtime collection network is configured.
	service := cryptopay.Service{Pool: s.Pool}
	if s.Crypto != nil {
		service.Networks = s.Crypto.Networks
	}
	out, err := service.Monitoring(s.cryptoChannelContext(r.Context(), session, token))
	switch {
	case errors.Is(err, identity.ErrAdminSession):
		response(w, 403, -1, "需要当前超级管理员本人会话", nil)
	case err != nil:
		response(w, 503, -1, "USDT 监控暂时无法读取，请稍后刷新", nil)
	default:
		response(w, 200, 0, "", out)
	}
}
