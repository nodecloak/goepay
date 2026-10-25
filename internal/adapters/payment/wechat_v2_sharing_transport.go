package payment

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func (w *WechatV2) validateSharingV2Transport(cert bool) error {
	if w == nil || !w.config.EnableChannelSharing {
		return ErrUnsupported
	}
	u, e := url.Parse(w.config.BaseURL)
	if e != nil || u.Scheme != "https" || u.Host == "" || len(w.config.APIKey) != 32 {
		return ErrInvalid
	}
	client := w.config.HTTPClient
	if cert {
		client = w.config.RefundClient
		if !wechatClientCertificateConfigured(client) {
			return ErrUnsupported
		}
	}
	if client == nil {
		return ErrInvalid
	}
	if transport, ok := client.Transport.(*http.Transport); ok && transport.TLSClientConfig != nil {
		cfg := transport.TLSClientConfig
		if cfg.InsecureSkipVerify || cfg.MinVersion != 0 && cfg.MinVersion < tls.VersionTLS12 || cfg.MaxVersion != 0 && cfg.MaxVersion < tls.VersionTLS12 {
			return ErrInvalid
		}
	}
	return nil
}

// V2 sharing uses its own HMAC exchange. Ordinary payment, barcode and bank
// products retain their existing MD5 wire contract without signing fallback.
func (w *WechatV2) sharingV2Call(ctx context.Context, path string, fields map[string]string, write, cert, app bool) (map[string]string, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, ErrInvalid
	}
	if e := w.validateSharingV2Transport(cert); e != nil {
		return nil, e
	}
	switch path {
	case "/pay/profitsharingaddreceiver", "/pay/profitsharingremovereceiver", "/secapi/pay/profitsharing", "/pay/profitsharingquery", "/pay/profitsharingorderamountquery", "/secapi/pay/profitsharingfinish", "/secapi/pay/profitsharingreturn", "/pay/profitsharingreturnquery":
	default:
		return nil, ErrUnsupported
	}
	p := make(map[string]string, len(fields)+5)
	for key, value := range fields {
		p[key] = value
	}
	p["mch_id"] = w.config.MerchantID
	if app {
		p["appid"] = w.config.AppID
	}
	var e error
	p["nonce_str"], e = nonce()
	if e != nil {
		return nil, e
	}
	p["sign_type"] = "HMAC-SHA256"
	p["sign"], e = SignWechatV2(p, w.config.APIKey, "HMAC-SHA256")
	if e != nil {
		return nil, e
	}
	unknown := func(cause error) (map[string]string, error) {
		if write {
			return nil, &UnknownOutcome{Operation: "wechat-v2-sharing", Cause: cause}
		}
		return nil, cause
	}
	request, e := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(w.config.BaseURL, "/")+path, bytes.NewReader(encodeXML(p)))
	if e != nil {
		return nil, e
	}
	request.Header.Set("Content-Type", "application/xml; charset=utf-8")
	client := w.config.HTTPClient
	if cert {
		client = w.config.RefundClient
	}
	response, e := client.Do(request)
	if e != nil {
		return unknown(e)
	}
	defer response.Body.Close()
	if !authenticatedTLSExchange(request, response) || response.TLS.Version < tls.VersionTLS12 || cert && !wechatVerifiedMutualTLS(request, response, client) {
		return unknown(ErrAuthentication)
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 || response.StatusCode != http.StatusOK {
		return unknown(errors.New("invalid V2 sharing HTTP response"))
	}
	out, e := decodeXML(raw)
	if e != nil || out["return_code"] != "SUCCESS" || out["sign_type"] != "" && out["sign_type"] != "HMAC-SHA256" || !VerifyWechatV2(out, w.config.APIKey, "HMAC-SHA256", out["sign"]) || out["mch_id"] != w.config.MerchantID || !scanCancellationNumber.MatchString(out["nonce_str"]) || len(out["nonce_str"]) > 32 {
		return unknown(ErrAuthentication)
	}
	if app && out["appid"] != w.config.AppID || out["appid"] != "" && out["appid"] != w.config.AppID {
		return unknown(ErrAuthentication)
	}
	// Return APIs use return_code + result, without result_code. An optional
	// conflicting result_code must still fail rather than bypass the contract.
	returnAPI := path == "/secapi/pay/profitsharingreturn" || path == "/pay/profitsharingreturnquery"
	if out["result_code"] != "SUCCESS" && !(returnAPI && out["result_code"] == "") {
		// A transport/business error alone does not prove an allocation never
		// happened. Preserve the original claim for authenticated result queries.
		return unknown(&ProviderError{Code: "SHARING_REJECTED"})
	}
	return out, nil
}
