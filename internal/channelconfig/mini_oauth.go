package channelconfig

import (
	provider "goepay/internal/adapters/payment"
	"goepay/internal/platform/httpclient"
	"regexp"
	"strings"
)

var miniOAuthPath = regexp.MustCompile(`^[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*$`)

// BuildMiniOAuth only reads the supplied decrypted import. Application startup
// must separately opt in; no key discovery or identity exchange happens here.
func BuildMiniOAuth(imp Import) (map[int64]provider.MiniOAuthProvider, error) {
	if imp.Version != 1 {
		return nil, ErrImport
	}
	out := map[int64]provider.MiniOAuthProvider{}
	seen := map[int64]bool{}
	for _, c := range imp.Channels {
		if c.ID <= 0 || seen[c.ID] {
			return nil, ErrImport
		}
		seen[c.ID] = true
		switch c.Config["enable_mini_launch"] {
		case "", "false":
			continue
		case "true":
		default:
			return nil, ErrImport
		}
		mini, e := miniProgramConfig(c)
		if e != nil || mini == nil {
			return nil, ErrImport
		}
		if mini.AppID == "" {
			mini.AppID = c.Config["appid"]
		}
		if mini.AppID != c.Config["appid"] || len(mini.Path) > 256 || !miniOAuthPath.MatchString(mini.Path) {
			return nil, ErrImport
		}
		for _, part := range strings.Split(mini.Path, "/") {
			if part == "." || part == ".." {
				return nil, ErrImport
			}
		}
		allowed := false
		for _, product := range c.AppTypes {
			if c.Plugin == "wxpay" && product == "2" || c.Plugin == "alipay" && product == "7" {
				allowed = true
			}
		}
		if !allowed {
			return nil, ErrImport
		}
		switch c.Plugin {
		case "wxpay":
			p, e := provider.NewWechatMiniOAuth(provider.WechatOAuthConfig{AppID: c.Config["appid"], AppSecret: c.Config["mini_secret"], HTTPClient: httpclient.New()})
			if e != nil {
				return nil, ErrImport
			}
			out[c.ID] = p
		case "alipay":
			// Share the exact signed token/certificate constructor used by web OAuth.
			// This private copy changes no imported settings or payment capabilities.
			clone := c
			clone.Config = map[string]string{}
			for k, v := range c.Config {
				clone.Config[k] = v
			}
			clone.Config["enable_web_oauth"] = "true"
			clone.AppTypes = []string{"4"}
			web, e := BuildOAuth(Import{Version: 1, Channels: []Channel{clone}})
			if e != nil {
				return nil, ErrImport
			}
			p, ok := web[c.ID].(provider.MiniOAuthProvider)
			if !ok {
				return nil, ErrImport
			}
			out[c.ID] = p
		default:
			return nil, ErrImport
		}
	}
	return out, nil
}
