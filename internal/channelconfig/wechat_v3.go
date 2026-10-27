package channelconfig

import (
	"crypto/rsa"
	provider "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/platform/httpclient"
	"strings"
)

func buildWechatV3(c Channel) (provider.Provider, error) {
	sharing, _, e := sharingOptions(c)
	if e != nil {
		return nil, e
	}
	for _, field := range []string{"enable_scan_pay", "enable_bank_transfer"} {
		if value := c.Config[field]; value != "" && value != "false" {
			return nil, ErrImport
		}
	}
	var methods []string
	for _, product := range c.AppTypes {
		switch product {
		case "1":
			methods = append(methods, "native")
		case "2":
			methods = append(methods, "jsapi")
		case "3":
			methods = append(methods, "h5")
		case "5":
			methods = append(methods, "app")
		default:
			return nil, ErrImport
		}
	}
	if len(methods) == 0 {
		return nil, ErrImport
	}
	private, e := compat.ParsePrivateKey(c.Certificates["v3_merchant_key"])
	if e != nil {
		return nil, ErrImport
	}
	verifiers := map[string]*rsa.PublicKey{}
	for name, value := range c.Certificates {
		if !strings.HasPrefix(name, "v3_verifier_") {
			continue
		}
		serial := strings.TrimPrefix(name, "v3_verifier_")
		key, e := compat.ParsePublicKey(value)
		if e != nil || serial == "" {
			return nil, ErrImport
		}
		verifiers[serial] = key
	}
	mini, err := miniProgramConfig(c)
	if err != nil {
		return nil, err
	}
	return provider.NewWechatV3(provider.WechatV3Config{EnableChannelSharing: sharing, EncryptionSerial: c.Config["v3_encryption_serial"], AppID: c.Config["appid"], MerchantID: c.Config["appmchid"], MerchantSerial: c.Config["v3_merchant_serial"], APIv3Key: []byte(c.Config["v3_api_key"]), PrivateKey: private, VerificationKeys: verifiers, Methods: methods, MiniProgram: mini, H5SiteURL: c.Config["h5_site_url"], H5SiteName: c.Config["h5_site_name"], HTTPClient: httpclient.New()})
}
