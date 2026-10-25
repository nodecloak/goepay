package payment

import "net/http"

// Called only after the supplied signature, if any, has already been verified.
// An unsigned legacy business failure requires the same verified TLS stream and
// configured client certificate. Conflicting echoed identity is never accepted.
func wechatFailureAuthentication(req *http.Request, res *http.Response, client *http.Client, cert bool, out, expected map[string]string) string {
	allEchoed := true
	for field, value := range expected {
		got, present := out[field]
		if present && got != value {
			return ""
		}
		if !present {
			allEchoed = false
		}
	}
	if out["sign"] != "" && allEchoed {
		return "wechat_v2_md5"
	}
	if cert && wechatVerifiedMutualTLS(req, res, client) {
		return "wechat_v2_mtls"
	}
	return ""
}

func wechatVerifiedMutualTLS(req *http.Request, res *http.Response, client *http.Client) bool {
	return authenticatedTLSExchange(req, res) && wechatClientCertificateConfigured(client)
}
