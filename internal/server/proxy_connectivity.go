package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"goepay/internal/identity"
	"goepay/internal/platform/httpclient"
	"goepay/internal/proxyconfig"
)

// 测试连通性 (original admin/ajax.php testproxy -> check_proxy): reach an IP
// echo site THROUGH the configured proxy server and report the egress IP. The
// proxy password is taken from the encrypted store, never from the request, so
// it is never echoed to the browser.
func defaultProxyConnectivity(ctx context.Context, cfg httpclient.ProxyConfig) (string, error) {
	client, err := httpclient.NewProxied(cfg)
	if err != nil {
		return "", err
	}
	var last error
	for _, probe := range []string{"https://dl.amh.sh/ip.htm", "https://myip.ipip.net/"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, probe, nil)
		if err != nil {
			last = err
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			last = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return strings.TrimSpace(string(body)), nil
		}
	}
	if last == nil {
		last = errors.New("proxy did not return an IP echo")
	}
	return "", last
}

// proxyConnectivityConsole: POST /console/v1/admin/settings/proxy-test with the
// proxy-server fields the admin typed (type/server/port/user). Super admin only.
func (s *Server) proxyConnectivityConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) != 3 || parts[1] != "settings" || parts[2] != "proxy-test" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.Issuer != nil || session.User.AdminClass != "super" {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	var fields map[string]json.RawMessage
	if !decodeFundsJSON(w, r, &fields) {
		return true
	}
	var in struct{ Type, Server, Port, User string }
	for field, value := range fields {
		var dst *string
		switch field {
		case "type":
			dst = &in.Type
		case "server":
			dst = &in.Server
		case "port":
			dst = &in.Port
		case "user":
			dst = &in.User
		default:
			response(w, 400, -1, "unsupported proxy test field", nil)
			return true
		}
		if json.Unmarshal(value, dst) != nil {
			response(w, 400, -1, "string proxy test field required", nil)
			return true
		}
	}
	n, err := strconv.Atoi(in.Port)
	switch in.Type {
	case "http", "https", "sock4", "sock5", "sock5h":
	default:
		response(w, 422, -1, "请选择有效的代理协议", nil)
		return true
	}
	if in.Server == "" || err != nil || n < 1 || n > 65535 {
		response(w, 422, -1, "请填写代理IP与1-65535端口", nil)
		return true
	}
	// The password comes from the encrypted store, not the request body.
	store, err := s.proxyCredentialStore()
	if err != nil {
		response(w, 503, -1, "proxy credential settings unavailable", nil)
		return true
	}
	actor := proxyconfig.Actor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
	if _, err = store.Read(r.Context(), actor); err != nil {
		if err == identity.ErrAdminSession {
			response(w, 403, -1, "original super administrator session required", nil)
		} else {
			response(w, 503, -1, "proxy credential settings unavailable", nil)
		}
		return true
	}
	secrets, err := store.Resolve(r.Context())
	if err != nil {
		response(w, 503, -1, "proxy credential settings unavailable", nil)
		return true
	}
	check := s.ProxyConnectivity
	if check == nil {
		check = defaultProxyConnectivity
	}
	ip, err := check(r.Context(), httpclient.ProxyConfig{Type: in.Type, Server: in.Server, Port: in.Port, User: in.User, Password: secrets.Password})
	if err != nil {
		response(w, 200, 0, "", map[string]any{"reachable": false, "message": "代理连接失败，请检查代理配置"})
		return true
	}
	response(w, 200, 0, "", map[string]any{"reachable": true, "ip": ip})
	return true
}
