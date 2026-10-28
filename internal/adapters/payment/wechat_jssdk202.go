package payment

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var ErrWechatJSSDK202 = errors.New("WeChat native scan configuration unavailable")

type WechatJSConfig202 struct {
	Debug     bool     `json:"debug"`
	AppID     string   `json:"appId"`
	Timestamp int64    `json:"timestamp"`
	NonceStr  string   `json:"nonceStr"`
	Signature string   `json:"signature"`
	JSAPIList []string `json:"jsApiList"`
}

type WechatJSSDKSigner202 interface {
	Sign(context.Context, string) (WechatJSConfig202, error)
}

// Credentials remain private inside the signer created from the encrypted
// import. They are never returned through the runtime profile or configuration.
type WechatJSSDKConfig202 struct {
	AppID      string           `json:"app_id"`
	AppSecret  string           `json:"-"`
	HTTPClient *http.Client     `json:"-"`
	Now        func() time.Time `json:"-"`
}
type WechatJSSDK202 struct {
	appID, secret           string
	client                  *http.Client
	now                     func() time.Time
	mu                      sync.Mutex
	token, ticket           string
	tokenUntil, ticketUntil time.Time
}

// Only a standard transport's trusted roots and dialing hook are copied.
// Every request still uses the fixed official hostname and verified TLS.
func NewWechatJSSDK202(c WechatJSSDKConfig202) (*WechatJSSDK202, error) {
	if !wechatMiniLinkIdentifier(c.AppID, 128) || !wechatMiniLinkValue(c.AppSecret, 512) {
		return nil, ErrInvalid
	}
	t := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "api.weixin.qq.com"}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 32 << 10, DisableKeepAlives: true}
	t.DialContext = (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	timeout := 10 * time.Second
	if c.HTTPClient != nil {
		if c.HTTPClient.Timeout < 0 {
			return nil, ErrInvalid
		}
		if c.HTTPClient.Timeout > 0 && c.HTTPClient.Timeout < timeout {
			timeout = c.HTTPClient.Timeout
		}
		if c.HTTPClient.Transport != nil {
			base, ok := c.HTTPClient.Transport.(*http.Transport)
			if !ok || base.DialTLS != nil || base.DialTLSContext != nil {
				return nil, ErrInvalid
			}
			if base.TLSClientConfig != nil {
				cfg := base.TLSClientConfig
				if cfg.InsecureSkipVerify || cfg.ServerName != "" && cfg.ServerName != "api.weixin.qq.com" {
					return nil, ErrInvalid
				}
				if cfg.RootCAs != nil {
					t.TLSClientConfig.RootCAs = cfg.RootCAs.Clone()
				}
			}
			if base.DialContext != nil {
				t.DialContext = base.DialContext
			}
		}
	}
	client := &http.Client{Transport: t, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if c.Now == nil {
		c.Now = time.Now
	}
	return &WechatJSSDK202{appID: c.AppID, secret: c.AppSecret, client: client, now: c.Now}, nil
}

func (w *WechatJSSDK202) object(ctx context.Context, method, target string, body any) (map[string]json.RawMessage, error) {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(raw))
	if err != nil {
		return nil, ErrWechatJSSDK202
	}
	req.GetBody = nil
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := w.client.Do(req)
	if err != nil {
		return nil, ErrWechatJSSDK202
	}
	raw, err = oauthBody(res)
	if err != nil {
		return nil, ErrWechatJSSDK202
	}
	node, err := oauthObject(raw)
	if err != nil {
		return nil, ErrWechatJSSDK202
	}
	if value, ok := node["errcode"]; ok {
		var code int
		if json.Unmarshal(value, &code) != nil || code != 0 {
			return nil, ErrWechatJSSDK202
		}
	}
	return node, nil
}

func jsLifetime202(node map[string]json.RawMessage) (time.Duration, error) {
	var seconds int64
	if json.Unmarshal(node["expires_in"], &seconds) != nil || seconds <= 200 || seconds > 86400 {
		return 0, ErrWechatJSSDK202
	}
	return time.Duration(seconds-200) * time.Second, nil
}

func (w *WechatJSSDK202) currentTicket(ctx context.Context) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ctx.Err() != nil {
		return "", ErrWechatJSSDK202
	}
	now := w.now()
	if w.ticket != "" && now.Before(w.ticketUntil) {
		return w.ticket, nil
	}
	if w.token == "" || !now.Before(w.tokenUntil) {
		node, err := w.object(ctx, "POST", "https://api.weixin.qq.com/cgi-bin/stable_token", map[string]string{"grant_type": "client_credential", "appid": w.appID, "secret": w.secret})
		if err != nil {
			return "", ErrWechatJSSDK202
		}
		token := text(node, "access_token")
		life, err := jsLifetime202(node)
		if err != nil || !wechatMiniLinkValue(token, 4096) {
			return "", ErrWechatJSSDK202
		}
		w.token, w.tokenUntil = token, now.Add(life)
	}
	q := url.Values{"access_token": {w.token}, "type": {"jsapi"}}
	node, err := w.object(ctx, "GET", "https://api.weixin.qq.com/cgi-bin/ticket/getticket?"+q.Encode(), nil)
	if err != nil {
		w.token, w.ticket = "", ""
		return "", ErrWechatJSSDK202
	}
	ticket := text(node, "ticket")
	life, err := jsLifetime202(node)
	if err != nil || !wechatMiniLinkValue(ticket, 4096) {
		return "", ErrWechatJSSDK202
	}
	w.ticket, w.ticketUntil = ticket, now.Add(life)
	return ticket, nil
}

func (w *WechatJSSDK202) Sign(ctx context.Context, page string) (WechatJSConfig202, error) {
	u, err := url.Parse(page)
	if err != nil || len(page) > 2048 || strings.ContainsAny(page, "\r\n\x00") || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" || u.Fragment != "" || strings.Contains(page, "#") || u.Path != "/merchant/scanpay" || u.EscapedPath() != "/merchant/scanpay" {
		return WechatJSConfig202{}, ErrWechatJSSDK202
	}
	return w.sign(ctx, page, "scanQRCode")
}

// WechatTransferJSSigner signs the 商家转账（新） payee page, whose only API is
// requestMerchantTransfer (original paypage/wxtrans.php getJsapiConfig).
type WechatTransferJSSigner interface {
	SignMerchantTransfer(context.Context, string) (WechatJSConfig202, error)
}

func (w *WechatJSSDK202) SignMerchantTransfer(ctx context.Context, page string) (WechatJSConfig202, error) {
	u, err := url.Parse(page)
	if err != nil || len(page) > 2048 || strings.ContainsAny(page, "\r\n\x00") || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" || u.Fragment != "" || strings.Contains(page, "#") || u.Path != "/pay/wxtrans" || u.EscapedPath() != "/pay/wxtrans" {
		return WechatJSConfig202{}, ErrWechatJSSDK202
	}
	return w.sign(ctx, page, "requestMerchantTransfer")
}

func (w *WechatJSSDK202) sign(ctx context.Context, page, api string) (WechatJSConfig202, error) {
	var out WechatJSConfig202
	ticket, err := w.currentTicket(ctx)
	if err != nil {
		return out, err
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return out, ErrWechatJSSDK202
	}
	out = WechatJSConfig202{AppID: w.appID, Timestamp: w.now().Unix(), NonceStr: hex.EncodeToString(nonce[:]), JSAPIList: []string{api}}
	// WeChat signs these lexicographically ordered keys with raw values.
	raw := fmt.Sprintf("jsapi_ticket=%s&noncestr=%s&timestamp=%d&url=%s", ticket, out.NonceStr, out.Timestamp, page)
	digest := sha1.Sum([]byte(raw))
	out.Signature = hex.EncodeToString(digest[:])
	return out, nil
}
