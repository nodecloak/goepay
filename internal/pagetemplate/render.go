package pagetemplate

import (
	"encoding/json"
	"html/template"
	"io"
)

type frame struct {
	Resolved
	Page
	Width, Padding int
	LogoURL        string
	TextColor      string
}

var opening = template.Must(template.New("page-open").Parse(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Page.Title}}</title><style>body{font:16px system-ui,sans-serif;margin:0;background:{{.Config.BackgroundColor}};color:{{.TextColor}}}main{box-sizing:border-box;max-width:{{.Width}}px;margin:5vh auto;padding:{{.Padding}}px;background:#fff;border-radius:16px;overflow-wrap:anywhere}button,a{color:{{.Config.AccentColor}}}button,input,select{box-sizing:border-box;max-width:100%;padding:12px;font:inherit}label,input,select,button{display:block;margin:12px 0}img{max-width:100%;height:auto}.template-brand{display:flex;align-items:center;gap:12px}.template-logo{width:56px;height:56px;object-fit:contain}.template-brand-name{font-weight:700}.template-notice{white-space:pre-wrap;color:#425569;font-size:14px}.template-preview-note{border:1px solid #b36c00;padding:12px}.template-compact{border-top:5px solid {{.Config.AccentColor}}}@media(max-width:480px){main{margin:0;border-radius:0;min-height:100vh}}</style><body data-template="{{.Name}}" data-template-type="{{.Type}}"><main class="template-{{.Name}}"><header class="template-brand">{{if .LogoURL}}<img class="template-logo" alt="品牌图片" src="{{.LogoURL}}">{{end}}<span class="template-brand-name">{{.Config.BrandName}}</span></header><h1>{{.Page.Title}}</h1>{{if .Preview}}<p class="template-preview-note">仅为模板预览，不发起支付。</p>{{end}}{{if .Config.Notice}}<p class="template-notice">{{.Config.Notice}}</p>{{end}}{{if .TradeNo}}<p>订单：{{.TradeNo}}</p>{{end}}{{if .Amount}}<p>金额：{{.Amount}} 元</p>{{end}}{{if and .Config.ShowOrderName .OrderName}}<p>订单名称：{{.OrderName}}</p>{{end}}{{if .MerchantName}}<p>收款商户：{{.MerchantName}}</p>{{end}}{{if .Sandbox}}<p>本地模拟环境，不涉及真实资金。</p>{{end}}`))

func Open(w io.Writer, r Resolved, p Page) error {
	raw, _ := json.Marshal(r.Config.Public(r.Type))
	cfg, e := ParseConfig(r.Type, r.Name, raw)
	if e != nil {
		return e
	}
	r.Config = cfg
	f := frame{Resolved: r, Page: p, Width: 640, Padding: 28, TextColor: "#163047"}
	if p.ForegroundColor != "" {
		if !colorPattern.MatchString(p.ForegroundColor) {
			return ErrInvalid
		}
		f.TextColor = p.ForegroundColor
	}
	if r.Name == "compact" {
		f.Width = 440
	}
	if cfg.Density == "compact" {
		f.Padding = 16
	}
	if cfg.LogoAssetID != "" {
		if p.Preview {
			f.LogoURL = "/console/v1/admin/templates/images/" + cfg.LogoAssetID
		} else {
			f.LogoURL = "/template-assets/" + cfg.LogoAssetID
		}
	}
	return opening.Execute(w, f)
}
func Close(w io.Writer) error { _, e := io.WriteString(w, "</main></body></html>"); return e }
