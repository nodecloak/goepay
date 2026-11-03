package payment

import (
	"encoding/json"
	"html"
	"net/url"
	"sort"
	"strings"
)

// BrowserCheckoutPresentation is a verified, in-memory browser submission of an
// existing payment attempt. It does not create or persist a provider transaction.
type BrowserCheckoutPresentation struct {
	HTML          string
	Method        string
	GatewayOrigin string
}

// PrepareBrowserCheckout validates the saved form before refreshing its return
// URL and timestamp. A desktop page presentation adds only Alipay's QR display
// settings to a temporary business payload, retaining the original payment method.
func (a *Alipay) PrepareBrowserCheckout(material, tradeNo, returnURL string, desktop bool) (BrowserCheckoutPresentation, error) {
	refreshed, err := a.RefreshCheckoutHTML(material, tradeNo, returnURL)
	if err != nil {
		return BrowserCheckoutPresentation{}, err
	}
	gatewayURL, err := url.Parse(a.config.GatewayURL)
	if err != nil || !validPaymentURL(a.config.GatewayURL) {
		return BrowserCheckoutPresentation{}, ErrInvalid
	}
	gateway := a.config.GatewayURL + "?" + (url.Values{"charset": {"utf-8"}}).Encode()
	prefix := `<form method="post" accept-charset="UTF-8" action="` + html.EscapeString(gateway) + `">`
	const suffix = `<button type="submit">支付宝支付</button></form>`
	inputs := strings.TrimSuffix(strings.TrimPrefix(refreshed, prefix), suffix)
	params := make(map[string]string)
	for inputs != "" {
		match := alipayCheckoutInput.FindStringSubmatch(inputs)
		if len(match) != 3 {
			return BrowserCheckoutPresentation{}, ErrAuthentication
		}
		params[match[1]] = html.UnescapeString(match[2])
		inputs = strings.TrimPrefix(inputs, match[0])
	}

	// Decode raw values instead of interface{} so unknown fields and exact
	// decimal/integer values survive the display-only update without rounding.
	decoder := json.NewDecoder(strings.NewReader(params["biz_content"]))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return BrowserCheckoutPresentation{}, ErrAuthentication
	}
	biz := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return BrowserCheckoutPresentation{}, ErrAuthentication
		}
		key, ok := token.(string)
		if !ok {
			return BrowserCheckoutPresentation{}, ErrAuthentication
		}
		if _, duplicate := biz[key]; duplicate {
			return BrowserCheckoutPresentation{}, ErrAuthentication
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return BrowserCheckoutPresentation{}, ErrAuthentication
		}
		biz[key] = value
	}
	if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') {
		return BrowserCheckoutPresentation{}, ErrAuthentication
	}

	method := "wap"
	if params["method"] == "alipay.trade.page.pay" {
		method = "page"
	}
	if desktop && method == "page" {
		biz["qr_pay_mode"] = json.RawMessage(`"4"`)
		biz["qrcode_width"] = json.RawMessage(`"230"`)
		body, err := json.Marshal(biz)
		if err != nil {
			return BrowserCheckoutPresentation{}, err
		}
		params["biz_content"] = string(body)
		params["sign"], err = SignAlipayRSA2(params, a.config.PrivateKey)
		if err != nil {
			return BrowserCheckoutPresentation{}, err
		}
		keys := make([]string, 0, len(params))
		for key := range params {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var form strings.Builder
		form.WriteString(prefix)
		for _, key := range keys {
			form.WriteString(`<input type="hidden" name="` + html.EscapeString(key) + `" value="` + html.EscapeString(params[key]) + `">`)
		}
		form.WriteString(suffix)
		refreshed = form.String()
	}
	return BrowserCheckoutPresentation{
		HTML: refreshed, Method: method, GatewayOrigin: gatewayURL.Scheme + "://" + gatewayURL.Host,
	}, nil
}
