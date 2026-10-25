package channelconfig

import (
	"crypto/rsa"
	provider "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/platform/httpclient"
)

// BuildOAuth consumes only the already decrypted, explicitly supplied import.
// It does not discover files, exchange a code, or enable default/simulation OAuth.
func BuildOAuth(imp Import) (map[int64]provider.WebOAuthProvider, error) {
	out := map[int64]provider.WebOAuthProvider{}
	if imp.Version != 1 {
		return nil, ErrImport
	}
	seen := map[int64]bool{}
	for _, c := range imp.Channels {
		if c.ID <= 0 || seen[c.ID] {
			return nil, ErrImport
		}
		seen[c.ID] = true
		switch c.Config["enable_web_oauth"] {
		case "", "false":
			continue
		case "true":
		default:
			return nil, ErrImport
		}
		allowed := false
		for _, product := range c.AppTypes {
			if c.Plugin == "wxpay" && product == "2" || c.Plugin == "alipay" && product == "4" {
				allowed = true
			}
		}
		if !allowed {
			return nil, ErrImport
		}
		switch c.Plugin {
		case "wxpay":
			p, e := provider.NewWechatOAuth(provider.WechatOAuthConfig{AppID: c.Config["appid"], AppSecret: c.Config["oauth_appsecret"], HTTPClient: httpclient.New()})
			if e != nil {
				return nil, ErrImport
			}
			out[c.ID] = p
		case "alipay":
			private, e := compat.ParsePrivateKey(c.Config["appsecret"])
			if e != nil {
				return nil, ErrImport
			}
			var public *rsa.PublicKey
			var certificates *provider.AlipayCertificateConfig
			switch c.Config["sign_type"] {
			case "", "0":
				public, e = compat.ParsePublicKey(c.Config["appkey"])
				if e != nil {
					return nil, ErrImport
				}
			case "1":
				certificates = &provider.AlipayCertificateConfig{ApplicationPEM: c.Certificates["app_cert_path"], PlatformPEM: c.Certificates["alipay_cert_path"], RootPEM: c.Certificates["root_cert_path"]}
			default:
				return nil, ErrImport
			}
			p, e := provider.NewAlipay(provider.AlipayConfig{AppID: c.Config["appid"], PrivateKey: private, PublicKey: public, CertificateMode: certificates, Methods: []string{"jsapi"}, HTTPClient: httpclient.New()})
			if e != nil {
				return nil, ErrImport
			}
			out[c.ID] = p
		default:
			return nil, ErrImport
		}
	}
	return out, nil
}
