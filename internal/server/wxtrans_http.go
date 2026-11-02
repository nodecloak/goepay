package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/ledger"
	"goepay/internal/siteinfo"
	"goepay/internal/wxtransferlink231"
)

// 商家转账（新） payee confirmation (original paypage/wxtrans.php with
// wxtrans_confirm.php / wxtrans_success.php). A bill waiting for the payee is
// confirmed in WeChat with requestMerchantTransfer. The original link was the
// unsigned biz_no; this one is an HMAC-signed transfer id.

var wxtransZone = time.FixedZone("Asia/Shanghai", 8*3600)

func (s *Server) wxtransSign(n, t string) string {
	return wxtransferlink231.Sign(s.CheckoutKey, n, t)
}

func (s *Server) wxtransBase() string {
	if s.ProviderFlow == nil {
		return ""
	}
	return strings.TrimRight(s.ProviderFlow.CallbackBaseURL, "/")
}

// wxtransConfirmURL is the payee's 确认收款 link (and QR content).
func (s *Server) wxtransConfirmURL(id int64, created time.Time) string {
	return s.wxtransBase() + wxtransferlink231.Path(s.CheckoutKey, id, created)
}

// transferConfirmURLs maps transfers still waiting for the payee (a recorded
// 商家转账 package and no final result) to their 确认收款 link: the original
// list shows 确认收款 under 正在处理 when a jumpurl exists.
func (s *Server) transferConfirmURLs(ctx context.Context, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(ids) == 0 || s.Pool == nil || len(s.CheckoutKey) == 0 {
		return out, nil
	}
	rows, err := s.Pool.Query(ctx, "SELECT t.id,t.created_at FROM wechat_transfer_packages w JOIN transfers t ON t.id=w.transfer_id WHERE t.id=ANY($1::bigint[]) AND t.status IN ('pending','unknown')", ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var created time.Time
		if err = rows.Scan(&id, &created); err != nil {
			return nil, err
		}
		out[id] = s.wxtransConfirmURL(id, created)
	}
	return out, rows.Err()
}

// attachTransferConfirmURLs adds confirm_url to console transfer list rows.
func (s *Server) attachTransferConfirmURLs(ctx context.Context, items []map[string]any) error {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		raw, _ := item["id"].(string)
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	links, err := s.transferConfirmURLs(ctx, ids)
	if err != nil {
		return err
	}
	for _, item := range items {
		raw, _ := item["id"].(string)
		id, _ := strconv.ParseInt(raw, 10, 64)
		if link := links[id]; link != "" {
			item["confirm_url"] = link
		}
	}
	return nil
}

// wxtransSigner finds the WeChat official account (an enabled web identity
// profile) of the bill's appid, preferring one bound to the channel: the
// original channel appwxmp binding.
func (s *Server) wxtransSigner(appID string, channelID int64) adapter.WechatTransferJSSigner {
	ids := make([]int64, 0, len(s.ScanJSProfiles202))
	for id := range s.ScanJSProfiles202 {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var found adapter.WechatTransferJSSigner
	for _, id := range ids {
		p := s.ScanJSProfiles202[id]
		signer, ok := p.Signer.(adapter.WechatTransferJSSigner)
		if !ok || appID == "" || p.Profile.AppID != appID || p.Profile.Provider != "wxpay" || p.Profile.Kind != "web" {
			continue
		}
		if p.Profile.ChannelID == channelID {
			return signer
		}
		if found == nil {
			found = signer
		}
	}
	return found
}

type wxtransPageData struct {
	Nonce, Title, Icon, Heading, Money, AddTime, PayTime, SiteName, SuccessURL string
	Year                                                                       int
	Confirm, Close                                                             bool
	Config                                                                     adapter.WechatJSConfig202
	Transfer                                                                   map[string]string
}

var wxtransPage = template.Must(template.New("wxtrans").Parse(`<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1.0,maximum-scale=1.0,user-scalable=0"><meta name="robots" content="noindex,nofollow"><title>{{.Title}}</title>
<style nonce="{{.Nonce}}">body{margin:0;background:#fff;color:#111;font-family:-apple-system,"PingFang SC","Microsoft YaHei",sans-serif;text-align:center}.msg{max-width:480px;margin:0 auto;padding:48px 24px 24px}.icon{width:64px;height:64px;margin:0 auto 24px;border-radius:50%;line-height:64px;color:#fff;font-size:36px}.icon.waiting{background:#10aeff}.icon.success{background:#07c160}.icon.warn{background:#fa5151}h2{font-size:18px;font-weight:400;margin:0 0 8px}.money{margin:0 0 24px}.money .cny{font-size:34px;font-weight:700;line-height:64px}.money .num{font-size:44px;font-weight:700;vertical-align:top}.list{list-style:none;margin:0 0 32px;padding:16px 0;border-top:1px solid #eee;text-align:left}.list li{display:flex;justify-content:space-between;padding:6px 0;font-size:14px}.list label{color:#888}.btn{display:block;width:184px;margin:0 auto 16px;padding:10px 0;border:0;border-radius:8px;font-size:17px;background:#f2f2f2;color:#06ae56}.btn.primary{background:#07c160;color:#fff}.btn:disabled{opacity:.4}.tips{font-size:14px;color:#888}.footer{margin-top:48px;font-size:12px;color:#aaa}.toast{position:fixed;left:50%;top:40%;transform:translateX(-50%);padding:16px 24px;border-radius:8px;background:rgba(17,17,17,.7);color:#fff;font-size:14px}</style>
</head><body><div class="msg"><div class="icon {{.Icon}}" aria-hidden="true">{{if eq .Icon "success"}}✓{{else if eq .Icon "warn"}}!{{else}}…{{end}}</div><h2>{{.Heading}}</h2>
{{if .Money}}<p class="money"><span class="cny">¥</span><span class="num">{{.Money}}</span></p>{{end}}
{{if .AddTime}}<ul class="list"><li><label>转账时间</label><span>{{.AddTime}}</span></li>{{if .PayTime}}<li><label>收款时间</label><span>{{.PayTime}}</span></li>{{end}}</ul>{{end}}
{{if .Confirm}}<button type="button" class="btn primary" id="Confirm" disabled>收款</button><p class="tips">1天内未确认，将退还给商家</p>{{end}}
{{if .Close}}<button type="button" class="btn" id="Close">关闭</button>{{end}}
<p class="footer">Copyright © {{.Year}} {{.SiteName}}</p></div>
{{if .Confirm}}<div class="toast" id="loadingToast" role="alert">正在加载</div>
<script src="https://res.wx.qq.com/open/js/jweixin-1.6.0.js" nonce="{{.Nonce}}"></script>
<script nonce="{{.Nonce}}">
var config = {{.Config}}, transfer = {{.Transfer}}, done = {{.SuccessURL}};
var button = document.getElementById('Confirm');
wx.config(config);
wx.ready(function () {
  document.getElementById('loadingToast').style.display = 'none';
  wx.checkJsApi({jsApiList: ['requestMerchantTransfer'], success: function (res) {
    if (!res.checkResult || !res.checkResult['requestMerchantTransfer']) { alert('你的微信版本过低，请更新至最新版本。'); return; }
    button.disabled = false;
    button.addEventListener('click', function () {
      WeixinJSBridge.invoke('requestMerchantTransfer', transfer, function (r) {
        if (r.err_msg === 'requestMerchantTransfer:ok') { window.location.href = done; }
      });
    });
  }});
});
</script>{{end}}
{{if .Close}}<script nonce="{{.Nonce}}">
document.getElementById('Close').addEventListener('click', function () {
  if (typeof WeixinJSBridge !== 'undefined') { WeixinJSBridge.call('closeWindow'); }
});
</script>{{end}}
</body></html>`))

func (s *Server) wxtransRender(ctx context.Context, w http.ResponseWriter, status int, d wxtransPageData) {
	nonce := make([]byte, 18)
	if _, err := rand.Read(nonce); err != nil {
		http.Error(w, "page unavailable", 500)
		return
	}
	d.Nonce = hex.EncodeToString(nonce)
	if d.Title == "" {
		d.Title = "确认收款页面"
	}
	d.Year = time.Now().In(wxtransZone).Year()
	if s.Pool != nil {
		if site, err := siteinfo.Load(ctx, s.Pool); err == nil {
			d.SiteName = site.Name
		}
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+d.Nonce+"' https://res.wx.qq.com; style-src 'nonce-"+d.Nonce+"'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.WriteHeader(status)
	_ = wxtransPage.Execute(w, d)
}

func (s *Server) wxtransError(ctx context.Context, w http.ResponseWriter, status int, message string) {
	s.wxtransRender(ctx, w, status, wxtransPageData{Title: "提示", Icon: "warn", Heading: message})
}

func wxtransTime(raw string) string {
	if at, err := time.Parse(time.RFC3339, raw); err == nil {
		return at.In(wxtransZone).Format("2006-01-02 15:04:05")
	}
	return raw
}

// wxtransPublic serves GET /pay/wxtrans?n=&t=&s=[&do=success].
func (s *Server) wxtransPublic(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/pay/wxtrans" {
		return false
	}
	ctx := r.Context()
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", 405)
		return true
	}
	q := r.URL.Query()
	n, t, sig := q.Get("n"), q.Get("t"), q.Get("s")
	id, err := strconv.ParseInt(n, 10, 64)
	if s.Pool == nil || s.Store == nil || s.ProviderFlow == nil || len(s.CheckoutKey) == 0 || err != nil || id <= 0 || len(sig) != 64 || !hmac.Equal([]byte(sig), []byte(s.wxtransSign(n, t))) {
		s.wxtransError(ctx, w, 400, "链接无效或已被修改")
		return true
	}
	var owner int64
	var no string
	var created time.Time
	if err = s.Pool.QueryRow(ctx, "SELECT merchant_id,transfer_no,created_at FROM transfers WHERE id=$1", id).Scan(&owner, &no, &created); err != nil || strconv.FormatInt(created.Unix(), 10) != t {
		s.wxtransError(ctx, w, 404, "转账订单号不存在")
		return true
	}
	tr, err := s.Store.Transfer(ctx, owner, no)
	if err != nil {
		s.wxtransError(ctx, w, 404, "转账订单号不存在")
		return true
	}
	if tr.Type != "wxpay" {
		s.wxtransError(ctx, w, 400, "转账类型错误")
		return true
	}
	if tr.Status == "failed" {
		s.wxtransError(ctx, w, 200, "转账已失败，请重新提交！")
		return true
	}
	flow, err := s.ProviderFlow.ParentTransferIntent(ctx, tr)
	var product adapter.MerchantTransferProduct
	if err == nil {
		product, _ = flow.Providers[tr.ChannelID].(adapter.MerchantTransferProduct)
	}
	if product == nil || !product.UsesMerchantTransfer() {
		s.wxtransError(ctx, w, 503, "当前支付通道信息不存在")
		return true
	}
	mchID, appID, _ := product.MerchantTransferConfirmation("")
	signer := s.wxtransSigner(appID, tr.ChannelID)
	if signer == nil {
		s.wxtransError(ctx, w, 503, "支付通道绑定的微信公众号不存在")
		return true
	}
	if !strings.Contains(r.UserAgent(), "MicroMessenger") {
		s.wxtransError(ctx, w, 200, "请在微信客户端打开链接")
		return true
	}
	// Refresh the bill first, like the original: a confirmed bill shows the
	// receipt and a closed one shows its failure (its funds were released).
	if current, qerr := s.ProviderFlow.QueryTransfer(ctx, owner, no); qerr == nil {
		tr = current
	}
	page := wxtransPageData{Money: ledger.Format(tr.Amount), AddTime: created.In(wxtransZone).Format("2006-01-02 15:04:05")}
	switch {
	case tr.Status == "failed":
		s.wxtransError(ctx, w, 200, "转账已失败，请重新提交！")
		return true
	case tr.Status == "succeeded" || q.Get("do") == "success":
		page.Icon, page.Heading, page.Close = "success", "你已收款，资金已存入零钱", true
		page.PayTime = time.Now().In(wxtransZone).Format("2006-01-02 15:04:05")
		if tr.Status == "succeeded" && tr.PayDate != "" {
			page.PayTime = wxtransTime(tr.PayDate)
		}
		s.wxtransRender(ctx, w, 200, page)
		return true
	}
	pkg, err := s.Store.TransferPackage(ctx, id)
	if err != nil {
		s.wxtransError(ctx, w, 409, "package_info不存在")
		return true
	}
	config, err := signer.SignMerchantTransfer(ctx, s.wxtransBase()+r.URL.RequestURI())
	if err != nil || config.AppID != appID || config.Debug || len(config.JSAPIList) != 1 || config.JSAPIList[0] != "requestMerchantTransfer" {
		s.wxtransError(ctx, w, 503, "微信JS接口签名失败，请稍后重试")
		return true
	}
	page.Icon, page.Heading, page.Confirm = "waiting", "待你收款", true
	page.Config = config
	page.Transfer = map[string]string{"mchId": mchID, "appId": appID, "package": pkg}
	page.SuccessURL = "/pay/wxtrans?" + url.Values{"n": {n}, "t": {t}, "s": {sig}, "do": {"success"}}.Encode()
	s.wxtransRender(ctx, w, 200, page)
	return true
}
