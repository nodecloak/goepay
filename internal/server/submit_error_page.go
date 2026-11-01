package server

import (
	"encoding/json"
	"fmt"
	"html"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

// Submit is a browser entry point, but SDKs may explicitly request JSON.
// Keep this presentation boundary separate from routing, signing and payment I/O.
func submitBrowserResponse(w http.ResponseWriter, r *http.Request) http.ResponseWriter {
	if _, wrapped := w.(*submitErrorWriter); wrapped {
		return w
	}
	if r.URL.Path != "/api/pay/submit" && r.URL.Path != "/submit.php" {
		return w
	}
	browser := r.Header.Get("Sec-Fetch-Mode") == "navigate"
	for _, value := range strings.Split(r.Header.Get("Accept"), ",") {
		media, params, err := mime.ParseMediaType(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		q := 1.0
		if raw, ok := params["q"]; ok {
			var e error
			q, e = strconv.ParseFloat(raw, 64)
			if e != nil {
				continue
			}
		}
		if (media == "text/html" || media == "application/xhtml+xml") && q > 0 && q <= 1 {
			browser = true
		}
	}
	if !browser {
		return w
	}
	return &submitErrorWriter{ResponseWriter: w}
}

type submitErrorWriter struct {
	http.ResponseWriter
	status   int
	sent     bool
	rendered bool
}

func (w *submitErrorWriter) WriteHeader(status int) {
	if w.status != 0 || w.sent {
		return
	}
	w.status = status
	if status >= 300 && status < 400 {
		w.commit()
	}
}
func (w *submitErrorWriter) commit() {
	if !w.sent {
		status := w.status
		if status == 0 {
			status = 200
		}
		w.ResponseWriter.WriteHeader(status)
		w.sent = true
	}
}
func (w *submitErrorWriter) Write(b []byte) (int, error) {
	if w.rendered {
		return len(b), nil
	}
	var envelope struct {
		Code    int    `json:"code"`
		Msg     string `json:"msg"`
		Message string `json:"message"`
	}
	isJSON := strings.HasPrefix(w.Header().Get("Content-Type"), "application/json")
	if isJSON && json.Unmarshal(b, &envelope) == nil && envelope.Code < 0 {
		reason := envelope.Msg
		if reason == "" {
			reason = envelope.Message
		}
		return w.failure(b, reason)
	}
	if w.status >= 400 && !w.sent {
		return w.failure(b, strings.TrimSpace(string(b)))
	}
	w.commit()
	return w.ResponseWriter.Write(b)
}
func (w *submitErrorWriter) failure(original []byte, reason string) (int, error) {
	message, code := submitFailureMessage(reason)
	status := w.status
	if status < 400 {
		status = http.StatusBadRequest
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	for _, key := range []string{"Content-Length", "Content-Encoding", "Location"} {
		h.Del(key)
	}
	w.rendered = true
	if !w.sent {
		w.ResponseWriter.WriteHeader(status)
		w.sent = true
	}
	_, err := fmt.Fprintf(w.ResponseWriter, `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>暂时无法发起支付</title><style>*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;padding:24px;background:#f5f7fb;color:#1e293b;font:16px/1.7 system-ui,-apple-system,"Segoe UI",sans-serif}.card{width:100%%;max-width:500px;padding:36px;border:1px solid #e2e8f0;border-radius:20px;background:white;box-shadow:0 12px 40px #0f172a08}.mark{width:48px;height:48px;border-radius:50%%;background:#fff3e6;color:#a64b00;display:grid;place-items:center;font-size:28px}h1{font-size:24px;line-height:1.4;margin:22px 0 12px}p{margin:12px 0}.hint{font-size:14px;color:#64748b}.code{margin-top:24px;padding-top:16px;border-top:1px solid #e2e8f0;font-size:12px;color:#64748b;overflow-wrap:anywhere}a{display:inline-block;color:#1d4ed8;margin-top:12px;text-decoration:none}a:focus-visible{outline:3px solid #93c5fd;outline-offset:4px}@media(max-width:400px){body{padding:16px}.card{padding:24px}}</style></head><body><main class="card"><div class="mark" aria-hidden="true">!</div><h1>暂时无法发起支付</h1><p role="alert">%s</p><p class="hint">请返回商户页面，核对支付信息后重新发起。若已扣款，请联系商户核实订单状态，避免重复付款。</p><a href="/">返回网站首页</a><p class="code">问题标识：%s</p></main></body></html>`, html.EscapeString(message), html.EscapeString(code))
	return len(original), err
}
func submitFailureMessage(reason string) (string, string) {
	switch reason {
	case "payment channel not permitted":
		return "该商户暂未开通当前支付通道，或当前支付方式不可用。请联系商户检查通道配置。", "channel_unavailable"
	case "支付方式(type)不存在":
		return "当前支付方式暂不可用，请返回商户页面选择其他支付方式。", "payment_type_unavailable"
	case "merchant unavailable":
		return "商户暂不可用，请联系商户确认账号状态与支付配置。", "merchant_unavailable"
	case "signature verification failed", "RSA merchant credentials required", "RSA credentials unavailable":
		return "支付请求验证失败，请返回商户页面重新发起；如仍失败，请联系商户检查接入配置。", "request_verification_failed"
	case "invalid payment parameters", "invalid parameters", "invalid form", "订单号(out_trade_no)格式不正确", "通知地址(notify_url)不能为空", "回调地址(return_url)不能为空":
		return "支付信息不完整或格式不正确，请返回商户页面检查后重新发起。", "invalid_payment_request"
	case "payment outcome requires query":
		return "暂时无法确认支付结果，请联系商户查询原订单后再操作，避免重复付款。", "payment_result_unconfirmed"
	case "method not allowed":
		return "当前提交方式不支持，请返回商户页面重新发起支付。", "request_method_unavailable"
	default:
		return "本次支付暂时无法继续，请联系商户检查订单及支付配置。", "payment_unavailable"
	}
}
