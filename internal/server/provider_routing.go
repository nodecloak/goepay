package server

import "goepay/internal/compat"

func hasProviderMethod(methods []string, method string) bool {
	for _, m := range methods {
		if m == method {
			return true
		}
	}
	return false
}

// Defaults select only configured browser/customer materials. They must never
// promote a common web request into a scan charge or another financial action.
func (s *Server) defaultProviderMethod(mid, channel int64, p compat.Params) string {
	provider := s.ProviderFlow.Providers[channel]
	methods := provider.Capabilities().Methods
	if p["oauth"] == "1" && hasProviderMethod(methods, "jsapi") {
		return "jsapi"
	}
	choices := []string{"native", "h5", "app", "applet", "jsapi"}
	if provider.ID() == "alipay" {
		choices = []string{"page", "wap", "native", "app", "applet", "jsapi", "mini", "preauth"}
		if p["device"] == "mobile" {
			choices[0], choices[1] = choices[1], choices[0]
		}
	}
	for _, method := range choices {
		if !hasProviderMethod(methods, method) {
			continue
		}
		if method == "applet" {
			if _, _, _, ok := s.miniLaunchChannelReady(mid, channel); !ok {
				continue
			}
		}
		return method
	}
	return ""
}
