package server

import (
	"html/template"
	"io"
	"log/slog"
)

// checkoutFragments render the dynamic parts of the public checkout, cashier
// and collection pages. html/template escapes every value for its exact
// context (text, attribute or URL), so a new field cannot be emitted unescaped
// by forgetting a manual html.EscapeString at a call site. Provider-supplied
// form HTML (verified by the adapter where it can be) and the nonce scripts,
// whose values are JSON-encoded, stay as they are; the page CSP only runs
// scripts carrying the response nonce.
var checkoutFragments = template.Must(template.New("checkout").Parse(`
{{- define "qr-image" -}}
<img width="320" height="320" alt="{{.Label}}" style="display:block;width:248px;max-width:100%;height:auto;aspect-ratio:1;margin:18px auto;box-sizing:border-box" src="{{.Source}}">
{{- end -}}
{{- define "link" -}}
<p><a href="{{.Href}}">{{.Text}}</a></p>
{{- end -}}
{{- define "jump" -}}
<p>{{.Lead}}</p><a href="{{.Href}}">{{.Text}}</a>
{{- end -}}
{{- define "launch" -}}
<a href="{{.Href}}">{{.Text}}</a>{{with .Note}}<p>{{.}}</p>{{end}}
{{- end -}}
{{- define "provider-qr" -}}
<p>请使用{{.Client}}扫描{{.Label}}。</p><img width="320" height="320" alt="{{.Label}}" src="{{.Source}}"><p>{{.Material}}</p>
{{- end -}}
{{- define "order-status" -}}
<p>平台订单状态：{{.}}</p><p>以平台异步通知或查单结果为准。</p>
{{- end -}}
{{- define "mini-params" -}}
<p>请由商户客户端使用以下参数打开可信微信小程序：</p><pre>{{.}}</pre>
{{- end -}}
{{- define "cashier-form" -}}
<form method="post" action="/cashier?i={{.Payload}}&amp;t={{.Token}}"><fieldset><legend>支付方式</legend>
{{- range $i, $t := .Types}}<label><input type="radio" name="type" value="{{$t.Name}}"{{if eq $i 0}} checked{{end}}> {{$t.ShowName}}</label>{{end -}}
</fieldset><button type="submit">立即支付</button></form>
{{- end -}}
{{- define "collection-form" -}}
<p>收款说明：{{.Title}}</p><form method="post" action="{{.Action}}"><input type="hidden" name="external_no" value="{{.ExternalNo}}"><label>金额（元）<input name="amount" type="number" min="0.01" max="1000000" step="0.01" required{{if .FixedAmount}} value="{{.FixedAmount}}" readonly{{end}}></label><label>支付方式<select name="type">
{{- range .Options}}<option value="{{.Value}}">{{.Label}}</option>{{end -}}
</select></label><button type="submit">前往支付</button></form>
{{- end -}}
{{- define "scan-region-warning" -}}
<!doctype html><html lang="zh-CN"><meta name="viewport" content="width=device-width,initial-scale=1"><title>支付安全提示</title><body><main><h1>支付安全提示</h1><p>扫码地区与下单地区不一致，请确认订单由本人发起，谨防陌生人代付诈骗。</p><p>订单：{{.TradeNo}}</p><a href="{{.Continue}}">已核对订单，继续支付</a></main></body></html>
{{- end -}}
`))

type checkoutOption struct{ Value, Label string }

// launchLink is a payment-client link whose URL checkoutJumpAllowed accepted.
type launchLink struct {
	Href       template.URL
	Text, Note string
}

// trustedLaunchURL admits a checkout link only after checkoutJumpAllowed has
// accepted its scheme and form. html/template would otherwise replace app
// schemes such as weixin:// and alipays:// with an inert placeholder.
func (s *Server) trustedLaunchURL(raw string) (template.URL, bool) {
	if !s.checkoutJumpAllowed(raw) {
		return "", false
	}
	return template.URL(raw), true
}

// renderCheckoutFragment writes one fragment. A failure can only come from a
// template defect; it is logged rather than silently producing a broken page.
func renderCheckoutFragment(w io.Writer, name string, data any) {
	if e := checkoutFragments.ExecuteTemplate(w, name, data); e != nil {
		slog.Error("checkout fragment rendering failed", "fragment", name, "error", e)
	}
}
