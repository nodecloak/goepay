package server

import (
	"errors"
	"net/http"

	"goepay/internal/cryptopay"
	"goepay/internal/identity"
)

// The payment channel console checks the role and CSRF before dispatching
// here. The service also proves the current durable session in its transaction.
func (s *Server) cryptoNetworkConsole(w http.ResponseWriter, r *http.Request, session identity.Session, suffix []string, token string) {
	if len(suffix) > 1 || len(suffix) == 1 && suffix[0] != "tron" && suffix[0] != "ethereum" {
		response(w, http.StatusNotFound, -1, "USDT 网络不存在", nil)
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, http.StatusBadRequest, -1, "USDT 网络设置不接受查询参数", nil)
		return
	}
	allow := http.MethodGet
	if len(suffix) == 1 {
		allow = http.MethodPatch
	}
	if r.Method != allow {
		w.Header().Set("Allow", allow)
		response(w, http.StatusMethodNotAllowed, -1, "method not allowed", nil)
		return
	}
	if s.Pool == nil || s.Store == nil {
		cryptoNetworkConsoleError(w, cryptopay.ErrUnavailable)
		return
	}
	service := s.Crypto
	if service == nil {
		service = &cryptopay.Service{Pool: s.Pool, ManageNetworks: true}
	}
	if r.Method == http.MethodGet {
		items, err := service.NetworkControls(s.cryptoChannelContext(r.Context(), session, token))
		if err != nil {
			cryptoNetworkConsoleError(w, err)
			return
		}
		response(w, http.StatusOK, 0, "", map[string]any{"items": items})
		return
	}
	var input struct {
		Enabled          *bool   `json:"enabled"`
		ExpectedRevision string  `json:"expected_revision"`
		Password         *string `json:"current_password"`
	}
	if !decodeFundsJSON(w, r, &input) {
		return
	}
	revision, valid := positiveFundsID(input.ExpectedRevision)
	if !valid || input.Enabled == nil || input.Password == nil {
		response(w, http.StatusBadRequest, -1, "USDT 网络设置字段无效或不完整", nil)
		return
	}
	ctx, ok := s.cryptoContext(w, r, "admin", session, *input.Password, true)
	if !ok {
		return
	}
	defer releaseLoginVerification()
	item, err := service.SetNetworkEnabled(ctx, suffix[0], *input.Enabled, revision, session.User.ID)
	if err != nil {
		cryptoNetworkConsoleError(w, err)
		return
	}
	response(w, http.StatusOK, 0, "", item)
}

func cryptoNetworkConsoleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cryptopay.ErrNetworkNotReady):
		response(w, http.StatusConflict, -1, "USDT 网络尚未满足开启条件，请重新核对钱包、收款地址池和监听服务", nil)
	case errors.Is(err, cryptopay.ErrConflict):
		response(w, http.StatusConflict, -1, "USDT 网络设置已变化，请重新读取当前状态后确认", nil)
	case errors.Is(err, cryptopay.ErrInvalid):
		response(w, http.StatusBadRequest, -1, "USDT 网络设置无效", nil)
	case errors.Is(err, identity.ErrAdminSession):
		response(w, http.StatusForbidden, -1, "需要当前超级管理员本人会话", nil)
	case errors.Is(err, errCredentialActor199), errors.Is(err, identity.ErrFundsAuthentication),
		errors.Is(err, errCurrentPassword), errors.Is(err, errCredentialUnset199):
		cryptoError(w, err)
	default:
		response(w, http.StatusServiceUnavailable, -1, "USDT 网络状态暂时无法核对，请重新读取当前设置后再操作", nil)
	}
}
